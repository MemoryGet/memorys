package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

var currentReadSamples []map[string]any

var evidenceBackendSamples []map[string]any

type fixtureOriginalControl func(context.Context, contracts.FrozenContentReadRequest, contracts.BlobRef) (contracts.BlobReadRequest, error)

func (f fixtureOriginalControl) ResolveOriginalRead(c context.Context, r contracts.FrozenContentReadRequest, b contracts.BlobRef) (contracts.BlobReadRequest, error) {
	return f(c, r, b)
}

type fixtureTextProjector func(context.Context, string, []byte) (contentverify.OriginalTextProjection, error)

func (f fixtureTextProjector) ProjectOriginalText(c context.Context, m string, b []byte) (contentverify.OriginalTextProjection, error) {
	return f(c, m, b)
}

type fixtureControlAuth func(context.Context, contracts.Credential) (contracts.Principal, error)

func (f fixtureControlAuth) Authenticate(c context.Context, q contracts.Credential) (contracts.Principal, error) {
	return f(c, q)
}

type fixtureControlAuthorize func(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error)

func (f fixtureControlAuthorize) Authorize(c context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
	return f(c, q)
}

type fixtureEvidenceControl struct {
	db            *sql.DB
	scope         contracts.AccessScope
	verify        postgres.VerifyScope
	profile       contracts.Digest
	calls, denyAt int
}

func (p *fixtureEvidenceControl) Refresh(ctx context.Context, who contracts.Principal, r contracts.RequestContext) (queryread.CurrentControlState, error) {
	p.calls++
	scope := p.scope
	scope.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	check := scope
	check.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	if p.calls == p.denyAt || who.SubjectID != scope.SubjectID || who.SubjectType != "user" || who.Issuer != "fixture" || r.ProfileDigest != p.profile || p.verify(ctx, check) != nil {
		return queryread.CurrentControlState{}, errors.New("fixture current control denied")
	}
	var version string
	if err := p.db.QueryRowContext(ctx, `SELECT policy_version::text FROM akos_withdraw_v1.review_policy_heads WHERE space_id='space' AND kb_id='kb'`).Scan(&version); err != nil {
		return queryread.CurrentControlState{}, err
	}
	n, err := strconv.ParseUint(version, 10, 64)
	if err != nil {
		return queryread.CurrentControlState{}, err
	}
	return queryread.CurrentControlState{Principal: who, Scope: scope, ProfileDigest: p.profile, ReviewPolicies: map[contracts.ID]uint64{"kb": n}}, nil
}

func verifyEvidenceBackend(ctx context.Context, db *sql.DB, scope contracts.AccessScope, verify postgres.VerifyScope, originals contracts.BlobStore, q contracts.RetrieveRequest, facts []contracts.PublishedFact, actual []contracts.IndexProjection) (func(contracts.ID) error, error) {
	unit := postgres.QueryReads{DB: db, Verify: verify}
	observations := map[contracts.ID]queryread.CurrentObservation{}
	byID := map[contracts.ID]contracts.PublishedFact{}
	for _, fact := range facts {
		byID[fact.DocumentID] = fact
		o, err := queryread.ObserveCurrent(ctx, unit, scope, []contracts.ID{fact.DocumentID}, queryread.CurrentLimits{MaxDocuments: 1, MaxBytes: 4 << 20})
		if err != nil {
			return nil, err
		}
		observations[fact.DocumentID] = o
	}
	denyOriginal := false
	originalGrantCalls := 0
	originalBudgetClips := 0
	var originalBudgetSamples []map[string]any
	var approvedBlob contracts.BlobRef
	originalControl := fixtureOriginalControl(func(c context.Context, r contracts.FrozenContentReadRequest, b contracts.BlobRef) (contracts.BlobReadRequest, error) {
		originalGrantCalls++
		if c.Err() != nil || denyOriginal || (r.Scope.SubjectID != "reader" && r.Scope.SubjectID != "historical-second-reader") || r.Scope.SpaceID != "space" {
			return contracts.BlobReadRequest{}, errors.New("fixture original read denied")
		}
		s := r.Scope
		s.ScopeID = "original-evidence-scope"
		s.Action = "original.verify"
		s.KnowledgeBaseIDs = append([]contracts.ID(nil), s.KnowledgeBaseIDs...)
		approvedBlob = b
		request := r.Request
		if s.ExpiresAt.Before(request.Deadline) {
			request.Deadline = s.ExpiresAt
		}
		if deadline, ok := c.Deadline(); ok && deadline.Before(request.Deadline) {
			request.Deadline = deadline
		}
		if request.Deadline.Before(r.Request.Deadline) {
			originalBudgetClips++
		}
		stageDeadline, ok := c.Deadline()
		if !ok || request.Deadline.After(stageDeadline) || request.Deadline.After(s.ExpiresAt) || request.Deadline.After(r.Request.Deadline) {
			return contracts.BlobReadRequest{}, errors.New("original grant exceeded parent budget")
		}
		originalBudgetSamples = append(originalBudgetSamples, map[string]any{"parent": r.Request.Deadline, "scope": s.ExpiresAt, "stage": stageDeadline, "grant": request.Deadline})
		return contracts.BlobReadRequest{Request: request, Scope: s, Blob: b}, nil
	})
	projectionCalls := 0
	projector := fixtureTextProjector(func(c context.Context, m string, b []byte) (contentverify.OriginalTextProjection, error) {
		projectionCalls++
		return (contentverify.UTF8OriginalText{}).ProjectOriginalText(c, m, b)
	})
	backend := postgres.EvidenceBackend{Reads: unit, Originals: originals, OriginalControl: originalControl, Projector: projector}
	control := &fixtureEvidenceControl{db: db, scope: scope, verify: verify, profile: q.Request.ProfileDigest}
	principal := contracts.Principal{SubjectID: scope.SubjectID, SubjectType: "user", Issuer: "fixture", ClientID: "identity-client"}
	makeOp := func(id contracts.ID, e []contracts.Evidence) queryread.CheckOperation {
		return queryread.CheckOperation{Request: q.Request, Principal: principal, Scope: scope, Expectation: queryread.CurrentExpectation{StoreEpochID: observations[id].Identity.StoreEpochID, ScopeVersion: scope.PolicyVersion, Published: []contracts.PublishedFact{byID[id]}}, Evidence: e}
	}
	var positive contracts.IndexProjection
	var manual contracts.IndexProjection
	for _, r := range actual {
		if r.DocumentID == "evidence-original-document" && positive.Chunk.ID == "" && strings.TrimSpace(r.Chunk.Text) != "" {
			positive = r
		}
		if strings.HasPrefix(string(r.DocumentID), "final-") && manual.Chunk.ID == "" {
			manual = r
		}
	}
	if positive.Chunk.ID == "" || manual.Chunk.ID == "" {
		return nil, errors.New("native evidence source records missing")
	}
	quote := contracts.Evidence{Citation: positive.Citation, SourceExcerpt: positive.Chunk.Text, Rank: 1}
	op := makeOp(positive.DocumentID, []contracts.Evidence{quote})
	authCalls, authorizeCalls := 0, 0
	nativeAuth, credential, err := nativeIdentityAuthenticator(ctx, db)
	if err != nil {
		return nil, err
	}
	defer clear(credential.Secret)
	defer clear(historicalSecondCredential.Secret)
	auth := fixtureControlAuth(func(c context.Context, q contracts.Credential) (contracts.Principal, error) {
		authCalls++
		return nativeAuth.Authenticate(c, q)
	})

	nativeAuthorize, revokePolicy, err := nativePolicyAuthorizer(ctx, db, scope, principal, positive.DocumentID)
	if err != nil {
		return nil, err
	}
	authorize := fixtureControlAuthorize(func(c context.Context, r contracts.AuthorizeRequest) (contracts.AccessScope, error) {
		authorizeCalls++
		return nativeAuthorize.Authorize(c, r)
	})
	nativeFacts, changeProfile, err := nativeQueryFacts(ctx, db, q.Request, scope)
	if err != nil {
		return nil, err
	}
	readUnit := postgres.QueryReads{DB: db, Verify: func(c context.Context, s contracts.AccessScope) error {
		// Two known fixture actors only; both still require current native ACLs.
		if s.SubjectID != principal.SubjectID && s.SubjectID != "historical-second-reader" {
			return postgres.ErrAuthorization
		}
		actor := principal
		actor.SubjectID = s.SubjectID
		fresh, e := nativeAuthorize.Authorize(c, contracts.AuthorizeRequest{Principal: actor, Action: s.Action, SpaceID: s.SpaceID, KnowledgeBaseIDs: s.KnowledgeBaseIDs, ResourceIDs: []contracts.ID{positive.DocumentID}})
		if e != nil {
			return e
		}
		return queryread.MatchRefreshedScope(c, actor, s, actor, fresh, []contracts.ID{s.KnowledgeBaseIDs[0]})
	}}
	readContent := postgres.PublishedFrozenReader{Reads: readUnit}
	historySource := &observedHistorySource{inner: postgres.HistoricalFrozenReader{Reads: readUnit, Authorizer: nativeAuthorize}}
	historicalReadSource = historySource
	var cursorKey [32]byte
	if _, err := rand.Read(cursorKey[:]); err != nil {
		return nil, err
	}
	cursorCodec, err := queryread.NewHMACReadCursor("native-fixture", cursorKey[:])
	clear(cursorKey[:])
	if err != nil {
		return nil, err
	}
	readService, readErr := knowledge.NewCurrentRead(knowledge.CurrentReadDependencies{Auth: nativeAuth, Locator: nativeAuthorize, Reads: readUnit, Content: readContent, Facts: nativeFacts, Verifier: contentverify.TextReadVerifier{Reader: readContent, Originals: originals, Control: originalControl, Projector: projector}, Pagination: &knowledge.ReadPagination{Codec: cursorCodec, PageSize: 1, TTL: time.Minute}, History: &knowledge.HistoryReadDependencies{Authorizer: nativeAuthorize, Content: historySource, Verifier: contentverify.TextHistoricalReadVerifier{Reader: historySource, Originals: originals, Control: originalControl, Projector: projector}}})
	if readErr != nil {
		return nil, readErr
	}
	ownedLifecycleCredential := credential
	ownedLifecycleCredential.Secret = append([]byte(nil), credential.Secret...)
	ownedDeleteCredential := historicalSecondCredential
	ownedDeleteCredential.Secret = append([]byte(nil), historicalSecondCredential.Secret...)
	releaseHistoricalApp = func() { clear(ownedLifecycleCredential.Secret); clear(ownedDeleteCredential.Secret) }
	runManagementDeletionProbe = func(c context.Context) error {
		return verifyManagementDeletion(c, db, nativeAuth, ownedLifecycleCredential, ownedDeleteCredential)
	}
	startHistoricalTransport = func(c context.Context) (*historicalTransport, error) {
		return newHistoricalTransport(c, db, nativeAuth, readService, ownedLifecycleCredential)
	}
	readLifecyclePage = func(c context.Context, revision contracts.ID, cursor string) (knowledge.ReadPageResult, error) {
		deadline, _ := c.Deadline()
		var profile contracts.Digest
		if e := db.QueryRowContext(c, `SELECT b.profile_digest FROM akos_withdraw_v1.query_profile_heads h JOIN akos_withdraw_v1.query_profile_bindings b USING(space_id,kb_id,version) WHERE h.space_id='space' AND h.kb_id='kb'`).Scan(&profile); e != nil {
			return knowledge.ReadPageResult{}, e
		}
		historicalAppProfile = profile
		return readService.ReadPage(c, knowledge.CurrentReadInput{Credential: ownedLifecycleCredential, DocumentID: historicalTarget.Document, RevisionID: revision, Request: contracts.RequestContext{RequestID: "historical-lifecycle-application", Deadline: deadline, ProfileDigest: profile}}, knowledge.ReadPageOptions{Cursor: cursor})
	}
	readGrantStart, readProjectionStart := originalGrantCalls, projectionCalls
	readResult, readErr := readService.ReadCurrent(ctx, knowledge.CurrentReadInput{Credential: credential, Request: q.Request, DocumentID: positive.DocumentID})
	if readErr != nil || readResult.RevisionID != byID[positive.DocumentID].RevisionID || len(readResult.Payload) == 0 {
		return nil, errors.New("native authenticated current-content application failed")
	}
	projection, projectionErr := api.ProjectReadContent(readResult, nil)
	if projectionErr != nil {
		return nil, projectionErr
	}
	if originalGrantCalls-readGrantStart < 4 || projectionCalls-readProjectionStart != 1 {
		return nil, errors.New("read source verification bypassed")
	}
	readVerifiedGrantCalls := originalGrantCalls - readGrantStart
	readVerifiedProjectionCalls := projectionCalls - readProjectionStart
	// Exercise clipping explicitly. Ordinary calls may already carry exactly the
	// stage deadline, so a zero incidental clip count is not a budget violation.
	shortStage, stopStage := context.WithTimeout(ctx, time.Second)
	shortGrant, shortErr := originalControl(shortStage, contracts.FrozenContentReadRequest{Request: q.Request, Scope: scope, DocumentID: positive.DocumentID, RevisionID: positive.Chunk.RevisionID}, approvedBlob)
	shortDeadline, _ := shortStage.Deadline()
	stopStage()
	if shortErr != nil || shortGrant.Request.Deadline.After(shortDeadline) || !shortGrant.Request.Deadline.Before(q.Request.Deadline) {
		return nil, errors.New("explicit short-stage original budget was not clipped")
	}
	currentReadSamples = append(currentReadSamples, map[string]any{"request": q.Request, "record": readResult, "published": byID[positive.DocumentID], "projection": projection, "authentication": "actual-rs256-current-status", "authorization": "postgres-explicit-read", "original_grant_calls": readVerifiedGrantCalls, "original_projection_calls": readVerifiedProjectionCalls, "original_authorization": "fixture-independent-read"})
	if err := verifyLocalRuntimeSearch(ctx, db, nativeAuth, credential); err != nil {
		return nil, err
	}
	if err := verifyKnowledgeApplication(ctx, db, nativeAuth, nativeAuthorize, credential, nativeFacts, unit, backend, byID[positive.DocumentID], observations[positive.DocumentID].Identity.StoreEpochID, strings.TrimSpace(quote.SourceExcerpt), readService); err != nil {
		return nil, err
	}
	currentFacts := countedQueryFacts(func(c context.Context, r contracts.RequestContext, s contracts.AccessScope) (queryread.ControlFacts, error) {
		control.calls++
		return nativeFacts.LoadCurrentFacts(c, r, s)
	})
	authenticated, err := queryread.NewAuthenticatedControl(op, credential, auth, authorize, currentFacts)
	if err != nil {
		return nil, err
	}
	defer authenticated.Close()
	if err := queryread.CheckCurrent(ctx, op, authenticated, unit, backend, queryread.CurrentLimits{MaxDocuments: 1000, MaxBytes: 4 << 20}); err != nil {
		return nil, errors.New("real native current evidence verification failed")
	}
	if control.calls != 2 || projectionCalls < 1 || authCalls != 2 || authorizeCalls != 2 {
		return nil, errors.New("current control/source pipeline was not executed")
	}
	frozenReader := postgres.PublishedFrozenReader{Reads: unit}
	readRequest := contracts.FrozenContentReadRequest{Request: q.Request, Scope: scope, DocumentID: positive.DocumentID, RevisionID: positive.Chunk.RevisionID}
	frozen, err := frozenReader.ReadFrozenContent(ctx, readRequest)
	if err != nil {
		return nil, err
	}
	grant, err := originalControl(ctx, readRequest, approvedBlob)
	if err != nil {
		return nil, err
	}
	stream, err := originals.Open(ctx, grant)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(stream, approvedBlob.Size+1))
	closeErr := stream.Close()
	if err != nil || closeErr != nil {
		return nil, errors.New("actual original sample unavailable")
	}
	retainedBlob := approvedBlob
	deletedOriginalProbe = func(c context.Context) (map[string]any, error) {
		deadline, _ := c.Deadline()
		grantScope := scope
		grantScope.ScopeID = "original-evidence-scope"
		grantScope.Action = "original.verify"
		grantScope.ExpiresAt = deadline
		r := q.Request
		r.Deadline = deadline
		stream, e := originals.Open(c, contracts.BlobReadRequest{Request: r, Scope: grantScope, Blob: retainedBlob})
		if e != nil {
			return nil, e
		}
		data, e := io.ReadAll(io.LimitReader(stream, retainedBlob.Size+1))
		closed := stream.Close()
		if e != nil || closed != nil || int64(len(data)) != retainedBlob.Size {
			return nil, errors.New("retained deletion original unavailable")
		}
		return map[string]any{"blob": retainedBlob, "bytes": data, "authority": "independent-original-fixture"}, nil
	}
	evidenceBackendSamples = append(evidenceBackendSamples, map[string]any{"operation": op, "observed": observations[positive.DocumentID], "frozen": frozen, "original_blob": approvedBlob, "original_bytes": raw, "actual_index_record": positive, "control_refreshes": control.calls, "authenticator_calls": authCalls, "authorizer_calls": authorizeCalls, "identity_provider": "rs256_sdk_postgres_status_test_issuer", "authorization_provider": "postgres-sealed-acl", "facts_provider": "postgres-current-references", "original_grant_budget_clips": originalBudgetClips, "original_grant_budget_samples": originalBudgetSamples, "explicit_short_stage_budget_probe": true})
	if err := changeProfile(); err != nil {
		return nil, err
	}
	if queryread.CheckCurrent(ctx, op, authenticated, unit, backend, queryread.CurrentLimits{MaxDocuments: 1000, MaxBytes: 4 << 20}) == nil {
		return nil, errors.New("old evidence request accepted changed profile")
	}
	if err := revokePolicy(); err != nil {
		return nil, err
	}
	if queryread.CheckCurrent(ctx, op, authenticated, unit, backend, queryread.CurrentLimits{MaxDocuments: 1000, MaxBytes: 4 << 20}) == nil {
		return nil, errors.New("current evidence accepted revoked database policy")
	}
	bad := quote
	bad.SourceExcerpt = "模型新增但不在索引的事实"
	if backend.Verify(ctx, q.Request, scope, observations[positive.DocumentID], []contracts.Evidence{bad}) == nil {
		return nil, errors.New("invented indexed evidence accepted")
	}
	bad = quote
	bad.Citation.BlockID = "wrong-index-block"
	if backend.Verify(ctx, q.Request, scope, observations[positive.DocumentID], []contracts.Evidence{bad}) == nil {
		return nil, errors.New("forged index citation accepted")
	}
	before := projectionCalls
	if backend.Verify(ctx, q.Request, scope, observations[manual.DocumentID], []contracts.Evidence{{Citation: manual.Citation, SourceExcerpt: manual.Chunk.Text, Rank: 1}}) == nil || projectionCalls <= before {
		return nil, errors.New("native approved edit was accepted as original or source projection not tested")
	}
	denyOriginal = true
	if backend.Verify(ctx, q.Request, scope, observations[positive.DocumentID], []contracts.Evidence{quote}) == nil {
		return nil, errors.New("original-read denial ignored")
	}
	denyOriginal = false
	control.calls = 0
	control.denyAt = 2
	if queryread.CheckCurrent(ctx, op, control, unit, backend, queryread.CurrentLimits{MaxDocuments: 1000, MaxBytes: 4 << 20}) == nil || control.calls != 2 {
		return nil, errors.New("final current-control denial ignored")
	}
	control.denyAt = 0
	// Delete another included source after initial index inspection, during the
	// actual original projection. The final index pass must detect the change.
	deletedID := contracts.ID("keyword-deleted-document")
	if err := backend.Verify(ctx, q.Request, scope, observations[deletedID], nil); err != nil {
		return nil, errors.New("source not ready before in-projection deletion")
	}
	joined := observations[positive.DocumentID]
	joined.Documents = append(joined.Documents, observations[deletedID].Documents...)
	racing := backend
	racing.Projector = fixtureTextProjector(func(c context.Context, m string, b []byte) (contentverify.OriginalTextProjection, error) {
		s := scope
		s.Action = "index.delete"
		cleanup := postgres.IndexCleanup{DB: db, Verify: func(context.Context, contracts.AccessScope) error { return nil }}
		if err := cleanup.Delete(c, contracts.IndexDeleteRequest{Request: q.Request, Scope: s, GenerationID: byID[deletedID].GenerationSet.Channels[contracts.KeywordChannel].ID}); err != nil {
			return contentverify.OriginalTextProjection{}, err
		}
		return projector(c, m, b)
	})
	beforeProjection, beforeGrants := projectionCalls, originalGrantCalls
	if err := racing.Verify(ctx, q.Request, scope, joined, []contracts.Evidence{quote}); err == nil || projectionCalls != beforeProjection+1 || originalGrantCalls < beforeGrants+4 {
		return nil, errors.New("index deletion during original projection ignored")
	}
	// A chunk tombstone becomes authoritative before physical row cleanup.
	logicalID := contracts.ID("final-readiness-lock-document")
	if backend.Verify(ctx, q.Request, scope, observations[logicalID], nil) != nil {
		return nil, errors.New("logical-fence source was not initially ready")
	}
	var logicalRecord contracts.IndexProjection
	for _, r := range actual {
		if r.DocumentID == logicalID {
			logicalRecord = r
			break
		}
	}
	if logicalRecord.Chunk.ID == "" {
		return nil, errors.New("logical-fence chunk missing")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.index_chunk_deletion_fences(generation_id,chunk_id) VALUES($1,$2)`, string(logicalRecord.GenerationID), string(logicalRecord.Chunk.ID)); err != nil {
		return nil, err
	}
	var retained int
	var fenced bool
	if db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM akos_withdraw_v1.index_projection_rows WHERE generation_id=$1),EXISTS(SELECT 1 FROM akos_withdraw_v1.index_chunk_deletion_fences WHERE generation_id=$1 AND chunk_id=$2)`, string(logicalRecord.GenerationID), string(logicalRecord.Chunk.ID)).Scan(&retained, &fenced) != nil || retained < 1 || !fenced {
		return nil, errors.New("logical test removed actual rows")
	}
	if backend.Verify(ctx, q.Request, scope, observations[logicalID], nil) == nil {
		return nil, errors.New("logical deleted chunk remained verifiable")
	}
	evidenceBackendSamples[len(evidenceBackendSamples)-1]["logical_chunk_fence"] = map[string]any{"document_id": logicalID, "generation_id": logicalRecord.GenerationID, "chunk_id": logicalRecord.Chunk.ID, "retained_rows": retained, "tombstone_exists": fenced}
	return func(id contracts.ID) error {
		if backend.Verify(ctx, q.Request, scope, observations[id], nil) == nil {
			return errors.New("withdrawn/revoked/deleted native source verified")
		}
		return nil
	}, nil
}
