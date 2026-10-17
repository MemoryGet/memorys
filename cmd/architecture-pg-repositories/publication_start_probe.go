package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sync"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

var publicationContractSamples []map[string]any
var publicationPolicySamples []document.ReviewPolicySnapshot

type approvedPublicationFixture struct {
	Revision  contracts.Revision
	Approval  contracts.Approval
	Originals contracts.BlobStore
}
type lostPublicationStartResponse struct{ inner document.PublicationStarter }

func (p lostPublicationStartResponse) StartPublication(ctx context.Context, r contracts.PublishRequest) (document.PublicationStartResult, error) {
	if _, err := p.inner.StartPublication(ctx, r); err != nil {
		return document.PublicationStartResult{}, err
	}
	return document.PublicationStartResult{}, document.ErrPublicationStartUnknown
}

func verifyPublicationStarts(ctx context.Context, db *sql.DB, scope contracts.AccessScope, request contracts.RequestContext, revokedRevision contracts.Revision, oldApproval contracts.Approval, newApproved func(string) (approvedPublicationFixture, error), revoke func(approvedPublicationFixture) error) error {
	for _, version := range []uint64{7, 8, 9} {
		accepted := uint64(7)
		if version == 8 {
			accepted = 8
		}
		policy := document.ReviewPolicySnapshot{Version: version, AcceptedDecisionPolicyVersions: []uint64{accepted}}
		var err error
		policy.Digest, err = document.ReviewPolicyDigest(policy)
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(policy)
		if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.review_policy_snapshots VALUES('space','kb',$1::numeric,$2::text,$3::jsonb)`, version, string(policy.Digest), string(payload)); err != nil {
			return err
		}
		publicationPolicySamples = append(publicationPolicySamples, policy)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.review_policy_heads VALUES('space','kb',7,1)`); err != nil {
		return err
	}
	for _, version := range []uint64{1, 2} {
		profile, err := referenceIndexProfile(version)
		if err != nil {
			return err
		}
		epoch := contracts.ID("publication-backend-1")
		if version == 2 {
			epoch = "publication-backend-2"
		}
		backend := document.PublicationBackendSnapshot{EpochID: epoch, Number: version, CutoverWatermark: "backend-definition-" + string(epoch), IndexProfile: contracts.ProfileReference{ID: profile.ID, Version: profile.Version}, ProfileDigest: profile.Digest, RequiredChannels: profile.Policy.RequiredChannels}
		payload, _ := json.Marshal(backend)
		if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.publication_backend_epochs VALUES('space','kb',$1,$2::jsonb)`, string(backend.EpochID), string(payload)); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.publication_backend_heads VALUES('space','kb','publication-backend-1',1)`); err != nil {
		return err
	}
	scope.SubjectID = "publisher"
	scope.Action = "publication.start"
	scope.ScopeID = "publication-start-scope"
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SubjectID != "publisher" || s.Action != "publication.start" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("publisher scope fixture denied")
		}
		return nil
	}
	authorize := func(c context.Context, r contracts.PublishRequest) error {
		if verify(c, r.Command.Scope) != nil || r.Command.Principal.Issuer != "fixture" || r.DocumentID == "" || r.RevisionID == "" || r.ApprovalID == "" {
			return errors.New("publisher resource fixture denied")
		}
		return nil
	}
	repository := postgres.PublicationStarts{DB: db, Verify: verify, AuthorizeStart: authorize, ProfileDigest: request.ProfileDigest}
	command := func(f approvedPublicationFixture, key string) contracts.PublishRequest {
		return contracts.PublishRequest{DocumentID: f.Revision.DocumentID, RevisionID: f.Revision.ID, ApprovalID: f.Approval.ID, ExpectedEpoch: 0, Command: contracts.CommandContext{Request: request, Scope: scope, Principal: contracts.Principal{SubjectID: "publisher", SubjectType: "user", Issuer: "fixture"}, IdempotencyKey: key}}
	}
	assertNoIntent := func(r contracts.PublishRequest) error {
		var count int
		if db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.publication_intents WHERE actor_id='publisher' AND command_key=$1`, r.Command.IdempotencyKey).Scan(&count) != nil || count != 0 {
			return errors.New("failed publication start retained intent")
		}
		return nil
	}
	old := command(approvedPublicationFixture{Revision: revokedRevision, Approval: oldApproval}, "revoked-start")
	if result, err := repository.StartPublication(ctx, old); err == nil || result.Operation.ID != "" {
		return errors.New("revoked approval started publication")
	}
	if err := assertNoIntent(old); err != nil {
		return err
	}
	fixture, err := newApproved("start-publication")
	if err != nil {
		return err
	}
	firstCommand := command(fixture, "start-publication")
	if result, err := (lostPublicationStartResponse{repository}).StartPublication(ctx, firstCommand); !errors.Is(err, document.ErrPublicationStartUnknown) || result.Operation.ID != "" {
		return errors.New("publication lost response not uncertain")
	}
	cold, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cold.Close()
	repository.DB = cold
	retry := firstCommand
	retry.Command.Request.RequestID = "publication-retry-request"
	retry.Command.Scope.ScopeID = "renewed-publication-scope"
	first, err := repository.StartPublication(ctx, retry)
	if err != nil || first.Operation.State != "preparing" || first.Operation.GenerationSetID != nil || first.Job.State != "queued" {
		return errors.New("cold publication intent replay failed")
	}
	publicationContractSamples = append(publicationContractSamples, publicationStartSamples(first)...)
	var payload []byte
	var plan document.PublicationStartPlan
	if cold.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.publication_intents WHERE id=$1`, string(first.Operation.ID)).Scan(&payload) != nil || json.Unmarshal(payload, &plan) != nil || plan.Review.Approval.ID != fixture.Approval.ID || plan.Review.Validity.ReviewPolicyVersion != 7 || plan.Backend.EpochID != "publication-backend-1" || plan.BackendHeadVersion != 1 || plan.ReviewPolicyHeadVersion != 1 || plan.ProfileDigest != request.ProfileDigest || !reflect.DeepEqual(plan.Result, first) {
		return errors.New("publication plan bindings missing")
	}
	for _, table := range []string{"publication_queued_jobs", "publication_start_audit", "publication_prepare_outbox"} {
		var count int
		if cold.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.`+table+` WHERE operation_id=$1`, string(first.Operation.ID)).Scan(&count) != nil || count != 1 {
			return errors.New("publication start replay duplicated effects")
		}
	}
	denied := repository
	denied.AuthorizeStart = func(context.Context, contracts.PublishRequest) error { return errors.New("publish grant revoked") }
	if result, err := denied.StartPublication(ctx, firstCommand); err == nil || result.Operation.ID != "" {
		return errors.New("unauthorized publication replay disclosed plan")
	}
	conflict := firstCommand
	conflict.ExpectedEpoch = 1
	if result, err := repository.StartPublication(ctx, conflict); err == nil || result.Operation.ID != "" {
		return errors.New("publication key accepted changed epoch")
	}
	// Current rules can reject old approval policy 7, or explicitly accept it.
	if _, err := cold.ExecContext(ctx, `UPDATE akos_withdraw_v1.review_policy_heads SET policy_version=8,row_version=2 WHERE space_id='space' AND kb_id='kb'`); err != nil {
		return err
	}
	current := command(fixture, "current-policy-start")
	if result, err := repository.StartPublication(ctx, current); err == nil || result.Operation.ID != "" {
		return errors.New("stale decision policy started publication")
	}
	if err := assertNoIntent(current); err != nil {
		return err
	}
	if _, err := cold.ExecContext(ctx, `UPDATE akos_withdraw_v1.review_policy_heads SET policy_version=9,row_version=3 WHERE space_id='space' AND kb_id='kb'`); err != nil {
		return err
	}
	accepted, err := repository.StartPublication(ctx, current)
	if err != nil {
		return errors.New("current policy 9 could not accept historical decision policy 7")
	}
	publicationContractSamples = append(publicationContractSamples, publicationStartSamples(accepted)...)
	if cold.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.publication_intents WHERE id=$1`, string(accepted.Operation.ID)).Scan(&payload) != nil || json.Unmarshal(payload, &plan) != nil || plan.Review.Validity.ReviewPolicyVersion != 9 || plan.Review.Approval.PolicyVersion != 7 || plan.ReviewPolicyHeadVersion != 3 {
		return errors.New("current policy conflated with historical approval")
	}
	if _, err := cold.ExecContext(ctx, `UPDATE akos_withdraw_v1.publication_backend_heads SET backend_epoch_id='publication-backend-2',row_version=2 WHERE space_id='space' AND kb_id='kb'`); err != nil {
		return err
	}
	backendCommand := command(fixture, "backend-policy-start")
	backend, err := repository.StartPublication(ctx, backendCommand)
	if err != nil || backend.Job.ProfileDigest == first.Job.ProfileDigest {
		return errors.New("new backend profile not captured")
	}
	publicationContractSamples = append(publicationContractSamples, publicationStartSamples(backend)...)
	// A replay after review revocation is just the old registration receipt.
	if err := revoke(fixture); err != nil {
		return err
	}
	oldResult, err := repository.StartPublication(ctx, firstCommand)
	if err != nil || !reflect.DeepEqual(oldResult, first) {
		return errors.New("original publication receipt changed after revocation")
	}
	late := command(fixture, "late-revoked-start")
	if result, err := repository.StartPublication(ctx, late); err == nil || result.Operation.ID != "" {
		return errors.New("new start accepted revoked review")
	}
	for _, table := range []string{"publication_start_audit", "publication_prepare_outbox"} {
		f, err := newApproved("fail-" + table)
		if err != nil {
			return err
		}
		cmd := command(f, "fail-"+table)
		if _, err := cold.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.fail_publication_start() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture publication start failure'; END $$`); err != nil {
			return err
		}
		if _, err := cold.ExecContext(ctx, `CREATE TRIGGER publication_start_failure BEFORE INSERT ON akos_withdraw_v1.`+table+` FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.fail_publication_start()`); err != nil {
			return err
		}
		result, startErr := repository.StartPublication(ctx, cmd)
		_, dropErr := cold.ExecContext(ctx, `DROP TRIGGER publication_start_failure ON akos_withdraw_v1.`+table)
		if dropErr != nil {
			return dropErr
		}
		if !errors.Is(startErr, document.ErrPublicationStartRejected) || result.Operation.ID != "" {
			return errors.New("publication start partial failure not rolled back")
		}
		if err := assertNoIntent(cmd); err != nil {
			return err
		}
	}
	f, err := newApproved("missing-publication-event")
	if err != nil {
		return err
	}
	missing := command(f, "missing-publication-event")
	if _, err := cold.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.skip_publication_start() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$`); err != nil {
		return err
	}
	if _, err := cold.ExecContext(ctx, `CREATE TRIGGER publication_start_skip BEFORE INSERT ON akos_withdraw_v1.publication_prepare_outbox FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.skip_publication_start()`); err != nil {
		return err
	}
	result, startErr := repository.StartPublication(ctx, missing)
	_, dropErr := cold.ExecContext(ctx, `DROP TRIGGER publication_start_skip ON akos_withdraw_v1.publication_prepare_outbox`)
	if dropErr != nil {
		return dropErr
	}
	if startErr == nil || result.Operation.ID != "" {
		return errors.New("missing publication event passed commit gate")
	}
	if err := assertNoIntent(missing); err != nil {
		return err
	}
	if _, err := repository.StartPublication(ctx, missing); err != nil {
		return errors.New("publication start could not retry confirmed rollback")
	}
	f, err = newApproved("authority-publication-start")
	if err != nil {
		return err
	}
	authorityCommand := command(f, "authority-publication-start")
	revoking := repository
	calls := 0
	revoking.AuthorizeStart = func(c context.Context, r contracts.PublishRequest) error {
		calls++
		if calls == 3 {
			return errors.New("authority revoked at commit")
		}
		return authorize(c, r)
	}
	if result, err := revoking.StartPublication(ctx, authorityCommand); err == nil || result.Operation.ID != "" || calls != 3 {
		return errors.New("precommit publish revocation accepted")
	}
	if err := assertNoIntent(authorityCommand); err != nil {
		return err
	}
	// Verify the policy/backend/document writers are fenced until start commits.
	fenced := repository
	calls = 0
	fenced.AuthorizeStart = func(c context.Context, r contracts.PublishRequest) error {
		calls++
		if calls == 3 {
			for _, query := range []string{`SELECT row_version FROM akos_withdraw_v1.review_policy_heads WHERE space_id='space' AND kb_id='kb' FOR UPDATE NOWAIT`, `SELECT row_version FROM akos_withdraw_v1.publication_backend_heads WHERE space_id='space' AND kb_id='kb' FOR UPDATE NOWAIT`, `SELECT row_version FROM akos_withdraw_v1.documents WHERE id='` + string(f.Revision.DocumentID) + `' FOR UPDATE NOWAIT`} {
				tx, err := cold.BeginTx(c, nil)
				if err != nil {
					return err
				}
				var value string
				err = tx.QueryRowContext(c, query).Scan(&value)
				tx.Rollback()
				var code interface{ SQLState() string }
				if !errors.As(err, &code) || code.SQLState() != "55P03" {
					return errors.New("publication head not fenced")
				}
			}
		}
		return authorize(c, r)
	}
	if _, err := fenced.StartPublication(ctx, authorityCommand); err != nil {
		return errors.New("publication authority fence probe failed")
	}
	f, err = newApproved("race-publication-start")
	if err != nil {
		return err
	}
	racing := command(f, "race-publication-start")
	var wg sync.WaitGroup
	wins := make(chan bool, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := repository.StartPublication(ctx, racing); wins <- err == nil }()
	}
	wg.Wait()
	close(wins)
	success := 0
	for won := range wins {
		if won {
			success++
		}
	}
	if success < 1 {
		return errors.New("concurrent publication start had no winner")
	}
	if _, err := repository.StartPublication(ctx, racing); err != nil {
		return errors.New("concurrent publication receipt not recoverable")
	}
	var count int
	if cold.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.publication_intents WHERE actor_id='publisher' AND command_key='race-publication-start'`).Scan(&count) != nil || count != 1 {
		return errors.New("concurrent publication start created duplicate tasks")
	}
	var unchanged bool
	if cold.QueryRowContext(ctx, `SELECT publication_epoch=0 AND active_revision_id IS NULL AND active_generation_set_id IS NULL FROM akos_withdraw_v1.documents WHERE id=$1`, string(f.Revision.DocumentID)).Scan(&unchanged) != nil || !unchanged {
		return errors.New("preparation intent changed publication head")
	}
	return verifyPublicationTasks(ctx, db, scope, request, func(name string) (publicationTaskFixture, error) {
		f, err := newApproved(name)
		if err != nil {
			return publicationTaskFixture{}, err
		}
		started, err := repository.StartPublication(ctx, command(f, "start-"+name))
		return publicationTaskFixture{Start: started, Approved: f}, err
	}, revoke)
}

func publicationStartSamples(r document.PublicationStartResult) []map[string]any {
	return []map[string]any{{"schema": "PublicationOperation", "payload": map[string]any{"operation_id": r.Operation.ID, "document_id": r.Operation.DocumentID, "revision_id": r.Operation.RevisionID, "approval_id": r.Operation.ApprovalID, "expected_epoch": r.Operation.ExpectedEpoch, "state": r.Operation.State, "generation_set_id": nil, "job_id": r.Job.ID}}, {"schema": "Job", "payload": map[string]any{"id": r.Job.ID, "space_id": r.Job.SpaceID, "resource_id": r.Operation.ID, "state": r.Job.State, "stage": r.Job.Stage, "attempt": r.Job.Attempt, "row_version": r.Job.RowVersion, "progress": r.Job.Progress}}}
}
