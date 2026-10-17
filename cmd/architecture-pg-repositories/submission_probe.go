package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

type lostSubmissionResponse struct{ repository document.SubmissionRepository }

func (p lostSubmissionResponse) SubmitDraft(ctx context.Context, command document.SubmissionCommand) (contracts.SubmitResult, error) {
	if _, err := p.repository.SubmitDraft(ctx, command); err != nil {
		return contracts.SubmitResult{}, err
	}
	return contracts.SubmitResult{}, document.ErrSubmissionUnknown
}

// Executes the real submit transaction on the actual file/parsed/edited draft.
// Scope/identity policy and human edits are fixtures, not production IAM/UI.
func verifySubmissions(ctx context.Context, db *sql.DB, readScope contracts.AccessScope, request contracts.RequestContext, store contracts.BlobStore, original contracts.BlobRef) error {
	scope := readScope
	scope.Action = "document.submit"
	scope.ScopeID = "submit-scope"
	command := document.SubmissionCommand{DocumentID: "app-document", Request: contracts.SubmitRequest{DraftID: "app-draft", ExpectedVersion: 3, Command: contracts.CommandContext{Scope: scope, Request: request, Principal: contracts.Principal{SubjectID: "actor", SubjectType: "user", Issuer: "fixture"}, IdempotencyKey: "submit-original"}}}
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SubjectID != "actor" || s.Action != "document.submit" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("submit scope fixture denied")
		}
		return nil
	}
	authorize := func(c context.Context, cmd document.SubmissionCommand) error {
		if verify(c, cmd.Request.Command.Scope) != nil || cmd.Request.Command.Principal.Issuer != "fixture" || cmd.DocumentID == "" || cmd.Request.DraftID == "" {
			return errors.New("submit resource fixture denied")
		}
		return nil
	}
	content := func(c context.Context, _ contracts.AccessScope, draft contracts.Draft) error {
		if len(draft.Blobs) != 1 || draft.Blobs[0] != original {
			return errors.New("original association changed")
		}
		// Original reading uses its independently authorized parse/read scope,
		// never treats submit permission as a general blob-read grant.
		reader, err := store.Open(c, contracts.BlobReadRequest{Request: request, Scope: readScope, Blob: original})
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(reader, original.Size+1))
		closeErr := reader.Close()
		if err != nil || closeErr != nil || int64(len(data)) != original.Size {
			return errors.New("original read failed")
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != string(original.SHA256) {
			return errors.New("original digest changed")
		}
		return nil
	}
	repository := postgres.Submissions{DB: db, Verify: verify, AuthorizeSubmission: authorize, VerifyContent: content, ReviewPolicyVersion: 7, ProfileDigest: request.ProfileDigest}
	// Simulate a lost response after the real transaction commits, then reconnect.
	if result, err := (lostSubmissionResponse{repository}).SubmitDraft(ctx, command); !errors.Is(err, document.ErrSubmissionUnknown) || result.Revision.ID != "" {
		return fmt.Errorf("real submit lost response: %w", err)
	}
	cold, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cold.Close()
	repository.DB = cold
	replay := command
	replay.Request.Command.Request.RequestID = "submit-retry-request"
	replay.Request.Command.Scope.ScopeID = "renewed-submit-scope"
	first, err := repository.SubmitDraft(ctx, replay)
	if err != nil || first.Revision.ID == "" || first.ApprovalRequestID == "" {
		return errors.New("cold submit receipt missing")
	}
	recovered, err := repository.SubmitDraft(ctx, replay)
	if err != nil || !reflect.DeepEqual(first, recovered) {
		return errors.New("cold submit replay changed original version")
	}
	var payload, frozen []byte
	var hash string
	if cold.QueryRowContext(ctx, `SELECT payload,frozen_payload,content_hash FROM akos_withdraw_v1.revision_contents WHERE revision_id=$1`, string(first.Revision.ID)).Scan(&payload, &frozen, &hash) != nil {
		return errors.New("immutable revision missing")
	}
	var revision contracts.Revision
	if json.Unmarshal(payload, &revision) != nil || !reflect.DeepEqual(revision, first.Revision) || hash != string(first.Revision.ContentHash) || !bytes.Contains(frozen, []byte("人工修订")) {
		return errors.New("frozen authoritative content differs")
	}
	var approval contracts.ApprovalRequest
	if cold.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.approval_requests WHERE id=$1`, string(first.ApprovalRequestID)).Scan(&payload) != nil || json.Unmarshal(payload, &approval) != nil || approval.State != "submitted" || approval.RowVersion != 0 || approval.PolicyVersion != 7 || approval.PolicyVersion == scope.PolicyVersion || approval.ContentHash != revision.ContentHash {
		return errors.New("initial review contract or independent policy missing")
	}
	var draft contracts.Draft
	if cold.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.draft_heads WHERE id='app-draft'`).Scan(&payload) != nil || json.Unmarshal(payload, &draft) != nil || draft.State != "submitted" || draft.RowVersion != 4 {
		return errors.New("submitted CAS missing")
	}
	if _, err := cold.ExecContext(ctx, `UPDATE akos_withdraw_v1.revision_contents SET frozen_payload='changed'::bytea WHERE revision_id=$1`, string(revision.ID)); err == nil {
		return errors.New("immutable revision update accepted")
	}
	if _, err := cold.ExecContext(ctx, `DELETE FROM akos_withdraw_v1.revisions WHERE id=$1`, string(revision.ID)); err == nil {
		return errors.New("immutable revision deletion accepted")
	}
	conflict := command
	conflict.Request.ExpectedVersion = 4
	if result, err := repository.SubmitDraft(ctx, conflict); err == nil || result.Revision.ID != "" {
		return errors.New("same key changed semantics accepted")
	}
	denied := repository
	denied.AuthorizeSubmission = func(context.Context, document.SubmissionCommand) error { return errors.New("revoked") }
	if result, err := denied.SubmitDraft(ctx, command); err == nil || result.Revision.ID != "" {
		return errors.New("unauthorized replay disclosed version")
	}
	for _, table := range []string{"submission_receipts", "submission_audit", "submission_outbox"} {
		var count int
		if cold.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.`+table+` WHERE revision_id=$1`, string(revision.ID)).Scan(&count) != nil || count != 1 {
			return errors.New("submit replay duplicated effects")
		}
	}
	var profile, reviewPolicy string
	if cold.QueryRowContext(ctx, `SELECT profile_digest,review_policy_version::text FROM akos_withdraw_v1.submission_receipts WHERE revision_id=$1`, string(revision.ID)).Scan(&profile, &reviewPolicy) != nil || profile != string(request.ProfileDigest) || reviewPolicy != "7" {
		return errors.New("trusted submit configuration not persisted")
	}
	// The action-specific ledger's key scope is space+actor+key, across KBs.
	_, keyErr := cold.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.submission_receipts SELECT space_id,'other-kb',actor_id,command_key,document_id,draft_id,revision_id,approval_id,profile_digest,review_policy_version,request_digest,receipt FROM akos_withdraw_v1.submission_receipts WHERE revision_id=$1`, string(revision.ID))
	var keyed interface{ SQLState() string }
	if !errors.As(keyErr, &keyed) || keyed.SQLState() != "23505" {
		return errors.New("cross-KB idempotency namespace not enforced")
	}
	// Fresh draft fixtures share the actual checked original, never a mock DB.
	seed := func(name string) (document.SubmissionCommand, error) {
		var d contracts.Draft
		raw, _ := json.Marshal(draft)
		if json.Unmarshal(raw, &d) != nil {
			return document.SubmissionCommand{}, errors.New("seed clone failed")
		}
		if name == "evidence-original" || name == "history-source" {
			stream, err := store.Open(ctx, contracts.BlobReadRequest{Request: request, Scope: readScope, Blob: original})
			if err != nil {
				return document.SubmissionCommand{}, err
			}
			raw, err := io.ReadAll(io.LimitReader(stream, original.Size+1))
			closeErr := stream.Close()
			if err != nil || closeErr != nil || int64(len(raw)) != original.Size || !strings.Contains(string(raw), "中文设备维护") {
				return document.SubmissionCommand{}, errors.New("original source fixture unavailable")
			}
			d.Blocks[0].Content.Text = string(raw)
			// Partition the actual source into two pages without duplicating its
			// index workload. Unicode boundaries preserve the original bytes/text.
			second := d.Blocks[0]
			second.ID = "original-page-second-block"
			second.Order = len(d.Blocks)
			text := []rune(string(raw))
			middle := len(text) / 2
			d.Blocks[0].Content.Text = string(text[:middle])
			second.Content.Text = string(text[middle:])
			d.Blocks = append(d.Blocks, second)
		}
		d.ID = contracts.ID(name + "-draft")
		d.DocumentID = contracts.ID(name + "-document")
		d.State = "draft"
		d.RowVersion = 1
		for i := range d.Blocks {
			d.Blocks[i].DraftID = d.ID
		}
		b, _ := json.Marshal(d)
		if _, err := cold.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.documents(space_id,kb_id,id,publication_epoch,row_version) VALUES('space','kb',$1,0,1)`, string(d.DocumentID)); err != nil {
			return document.SubmissionCommand{}, err
		}
		if _, err := cold.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_heads VALUES('space','kb',$1,$2,1,'draft',$3::jsonb)`, string(d.DocumentID), string(d.ID), string(b)); err != nil {
			return document.SubmissionCommand{}, err
		}
		cmd := command
		cmd.DocumentID = d.DocumentID
		cmd.Request.DraftID = d.ID
		cmd.Request.ExpectedVersion = 1
		cmd.Request.Command.IdempotencyKey = name
		return cmd, nil
	}
	assertRolledBack := func(cmd document.SubmissionCommand) error {
		var state, version string
		var count int
		if cold.QueryRowContext(ctx, `SELECT state,row_version::text FROM akos_withdraw_v1.draft_heads WHERE id=$1`, string(cmd.Request.DraftID)).Scan(&state, &version) != nil || state != "draft" || version != "1" {
			return errors.New("failed submit changed draft")
		}
		if cold.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.revisions WHERE document_id=$1`, string(cmd.DocumentID)).Scan(&count) != nil || count != 0 {
			return errors.New("failed submit leaked revision")
		}
		return nil
	}
	for _, table := range []string{"submission_audit", "submission_outbox"} {
		cmd, err := seed("fail-" + table)
		if err != nil {
			return err
		}
		if _, err := cold.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.fail_submit_effect() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture write failure'; END $$`); err != nil {
			return err
		}
		if _, err := cold.ExecContext(ctx, `CREATE TRIGGER submit_failure BEFORE INSERT ON akos_withdraw_v1.`+table+` FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.fail_submit_effect()`); err != nil {
			return err
		}
		result, submitErr := repository.SubmitDraft(ctx, cmd)
		_, dropErr := cold.ExecContext(ctx, `DROP TRIGGER submit_failure ON akos_withdraw_v1.`+table)
		if dropErr != nil {
			return dropErr
		}
		if !errors.Is(submitErr, document.ErrSubmissionRejected) || result.Revision.ID != "" {
			return errors.New("submit write failure not rejected")
		}
		if err := assertRolledBack(cmd); err != nil {
			return err
		}
	}
	// A sink silently omitting the event must fail the deferred commit gate.
	missing, err := seed("missing-submit-event")
	if err != nil {
		return err
	}
	if _, err := cold.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.skip_submit_effect() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$`); err != nil {
		return err
	}
	if _, err := cold.ExecContext(ctx, `CREATE TRIGGER submit_skip BEFORE INSERT ON akos_withdraw_v1.submission_outbox FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.skip_submit_effect()`); err != nil {
		return err
	}
	missingResult, missingErr := repository.SubmitDraft(ctx, missing)
	_, dropErr := cold.ExecContext(ctx, `DROP TRIGGER submit_skip ON akos_withdraw_v1.submission_outbox`)
	if dropErr != nil {
		return dropErr
	}
	if missingErr == nil || missingResult.Revision.ID != "" {
		return errors.New("missing event passed deferred gate")
	}
	if err := assertRolledBack(missing); err != nil {
		return err
	}
	// Rejection is safe to retry only after the actual transaction rolled back.
	if _, err := repository.SubmitDraft(ctx, missing); err != nil {
		return errors.New("rolled back submit could not retry same key")
	}
	cmd, err := seed("revoke-submit")
	if err != nil {
		return err
	}
	revoked := repository
	calls := 0
	revoked.AuthorizeSubmission = func(c context.Context, cmd document.SubmissionCommand) error {
		calls++
		if calls == 4 {
			return errors.New("authority revoked before commit")
		}
		return authorize(c, cmd)
	}
	if result, err := revoked.SubmitDraft(ctx, cmd); err == nil || result.Revision.ID != "" || calls != 4 {
		return errors.New("precommit revocation accepted")
	}
	if err := assertRolledBack(cmd); err != nil {
		return err
	}
	cmd, err = seed("race-submit")
	if err != nil {
		return err
	}
	other := cmd
	other.Request.Command.IdempotencyKey = "other-race-key"
	var wg sync.WaitGroup
	success := make(chan bool, 2)
	for _, c := range []document.SubmissionCommand{cmd, other} {
		wg.Add(1)
		go func(c document.SubmissionCommand) {
			defer wg.Done()
			_, err := repository.SubmitDraft(ctx, c)
			success <- err == nil
		}(c)
	}
	wg.Wait()
	close(success)
	winners := 0
	for ok := range success {
		if ok {
			winners++
		}
	}
	var count int
	if winners != 1 || cold.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.revisions WHERE document_id=$1`, string(cmd.DocumentID)).Scan(&count) != nil || count != 1 {
		return errors.New("concurrent submissions did not select one version")
	}
	var epoch string
	var inactive bool
	if cold.QueryRowContext(ctx, `SELECT publication_epoch::text,active_revision_id IS NULL AND active_generation_set_id IS NULL FROM akos_withdraw_v1.documents WHERE id='app-document'`).Scan(&epoch, &inactive) != nil || epoch != "0" || !inactive {
		return errors.New("submission published content")
	}
	return verifyReviewTransactions(ctx, db, scope, request, revision, store, func(name string) (contracts.SubmitResult, error) {
		cmd, err := seed(name)
		if err != nil {
			return contracts.SubmitResult{}, err
		}
		return repository.SubmitDraft(ctx, cmd)
	})
}
