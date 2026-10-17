package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
	"github.com/MemoryGet/memorys/pkg/withdrawal"
)

var queryReadSamples []map[string]any

// The suite performs hundreds of sequential transactions. Query/output is a
// separate bounded fixture phase, not a continuation of a production request.
// Recreate its fixture request/scope; never extend an existing business grant.
func verifyQueryReadPhase(db *sql.DB, command contracts.CommandContext, originals contracts.BlobStore) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	command.Request.Deadline = deadline
	command.Scope.ExpiresAt = deadline
	return verifyQueryReads(ctx, db, command, originals)
}

func verifyQueryReads(ctx context.Context, db *sql.DB, command contracts.CommandContext, originals contracts.BlobStore) error {
	scope := command.Scope
	scope.Action = "knowledge.search"
	scope.SubjectID = "reader"
	scope.ScopeID = "reader-scope"
	revoked, unavailable := false, false
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || revoked || unavailable || s.SubjectID != "reader" || s.SpaceID != "space" || s.Action != "knowledge.search" || s.PolicyVersion != 1 || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("current read scope denied")
		}
		s.KnowledgeBaseIDs[0] = "changed-verifier-argument"
		return nil
	}
	unit := postgres.QueryReads{DB: db, Verify: verify}
	calledWithoutStore := false
	if err := unit.WithinRead(ctx, scope, func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error {
		calledWithoutStore = true
		return nil
	}); err == nil || calledWithoutStore {
		return errors.New("uninitialized store admitted read callback")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.query_store_head VALUES(true,'read-store-a',1)`); err != nil {
		return err
	}
	assertUnsearchable, err := verifyKeywordRetrieval(ctx, db, scope, verify, originals)
	if err != nil {
		return err
	}
	sample := map[string]any{}
	var escaped contracts.QueryReadRepository
	var oldIdentity contracts.QueryReadIdentity
	var oldCursor string
	var firstFact, secondFact contracts.PublishedFact

	err = unit.WithinRead(ctx, scope, func(c context.Context, id contracts.QueryReadIdentity, repo contracts.QueryReadRepository) error {
		escaped = repo
		oldIdentity = id
		page, err := repo.ReadPublishedPage(c, contracts.PublishedPageRequest{Identity: id, KnowledgeBaseIDs: []contracts.ID{"kb"}, Limit: 1})
		if err != nil {
			return err
		}
		if len(page.Facts) != 1 || page.NextCursor == "" {
			return errors.New("missing multi-page actual publication inventory")
		}
		firstFact = page.Facts[0]
		oldCursor = page.NextCursor
		before, err := repo.LoadCurrentDocument(c, firstFact.DocumentID)
		if err != nil {
			return err
		}
		epoch, err := repo.LoadActiveEpoch(c, "kb")
		if err != nil || epoch.Number != 1 || epoch.ID != firstFact.GenerationSet.BackendEpochID || epoch.IndexProfile.ID != "index-profile" || epoch.CutoverWatermark == "" || epoch.RevisionGenerationSets[firstFact.RevisionID] != firstFact.GenerationSet.ID {
			return errors.New("real active epoch mapping differs")
		}
		page.Facts[0].GenerationSet.Channels[contracts.KeywordChannel].BackendBinding.BackendID = "mutated-page"
		freshPage, e := repo.ReadPublishedPage(c, contracts.PublishedPageRequest{Identity: id, KnowledgeBaseIDs: []contracts.ID{"kb"}, Limit: 1})
		if e != nil || freshPage.Facts[0].GenerationSet.Channels[contracts.KeywordChannel].BackendBinding.BackendID == "mutated-page" {
			return errors.New("read page aliases caller")
		}
		firstFact = freshPage.Facts[0]
		before.GenerationSet.Channels[contracts.KeywordChannel].BackendBinding.BackendID = "mutated-state"
		again, err := repo.LoadCurrentDocument(c, firstFact.DocumentID)
		if err != nil || again.GenerationSet.Channels[contracts.KeywordChannel].BackendBinding.BackendID == "mutated-state" {
			return errors.New("read state aliases caller")
		}
		if err := queryread.MatchCurrentReview(scope, queryread.CurrentExpectation{StoreEpochID: id.StoreEpochID, ScopeVersion: scope.PolicyVersion, Published: []contracts.PublishedFact{firstFact}}, queryread.CurrentObservation{Identity: id, Documents: []contracts.CurrentDocumentState{again}}, map[contracts.ID]uint64{"kb": 9}); err != nil {
			return errors.New("actual current metadata rejected before withdrawal")
		}
		// Commit a real withdrawal outside the read transaction while its pages
		// and current-state reads remain bound to the original observation.
		withdrawCommand := command
		withdrawCommand.Scope.Action = "publication.withdraw"
		withdrawCommand.IdempotencyKey = "withdraw-during-query-read"
		preparer := withdrawal.Preparer{Verify: func(context.Context, contracts.WithdrawRequest) error { return nil }}
		service := withdrawal.Service{Transactions: postgres.WithdrawalAdapter{DB: db, Verify: func(context.Context, contracts.AccessScope) error { return nil }}, Prepare: preparer.Prepare}
		if _, err := service.Withdraw(c, contracts.WithdrawRequest{Command: withdrawCommand, DocumentID: firstFact.DocumentID, ExpectedEpoch: firstFact.PublicationEpoch, Reason: "查询分页期间撤回"}); err != nil {
			return err
		}
		next, err := repo.ReadPublishedPage(c, contracts.PublishedPageRequest{Identity: id, KnowledgeBaseIDs: []contracts.ID{"kb"}, Cursor: oldCursor, Limit: 1000})
		if err != nil || next.Identity != id || len(next.Facts) == 0 || next.NextCursor != "" {
			return errors.New("read snapshot changed across pages")
		}
		secondFact = next.Facts[0]
		observed, err := repo.LoadCurrentDocument(c, firstFact.DocumentID)
		if err != nil || observed.Document.ActiveGenerationSetID == nil || observed.Document.PublicationEpoch != firstFact.PublicationEpoch {
			return errors.New("current read mixed committed withdrawal into old view")
		}
		all, err := queryread.Collect(c, repo, scope, id, []contracts.ID{"kb"}, queryread.Limits{PageSize: 1, MaxPages: 1000, MaxFacts: 1000, MaxGenerations: 3000})
		if err != nil || len(all) < 2 {
			return errors.New("native multi-page collection failed")
		}
		sample["identity"], sample["facts"], sample["epoch"], sample["current"] = id, all, epoch, observed
		if _, err := repo.LoadCurrentDocument(c, "unknown' OR true --"); err == nil {
			return errors.New("unscoped document probe accepted")
		}
		if _, err := repo.LoadActiveEpoch(c, "outside-scope"); err == nil {
			return errors.New("out-of-scope backend epoch exposed")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if _, err := escaped.LoadCurrentDocument(ctx, firstFact.DocumentID); !errors.Is(err, queryread.ErrClosed) {
		return errors.New("SQL query handle escaped callback")
	}
	if err := unit.WithinRead(ctx, scope, func(c context.Context, id contracts.QueryReadIdentity, repo contracts.QueryReadRepository) error {
		if id.ReadVersion == oldIdentity.ReadVersion {
			return errors.New("new read reused old identity")
		}
		if _, err := repo.ReadPublishedPage(c, contracts.PublishedPageRequest{Identity: oldIdentity, KnowledgeBaseIDs: []contracts.ID{"kb"}, Limit: 1}); err == nil {
			return errors.New("old view identity accepted")
		}
		if _, err := repo.ReadPublishedPage(c, contracts.PublishedPageRequest{Identity: id, KnowledgeBaseIDs: []contracts.ID{"kb"}, Cursor: oldCursor, Limit: 1}); err == nil {
			return errors.New("cursor reused across views")
		}
		state, err := repo.LoadCurrentDocument(c, firstFact.DocumentID)
		if err != nil || state.Document.ActiveRevisionID != nil || state.Document.PublicationEpoch != firstFact.PublicationEpoch+1 {
			return errors.New("fresh SQL observation missed withdrawal")
		}
		sample["withdrawn_current"] = state
		page, err := repo.ReadPublishedPage(c, contracts.PublishedPageRequest{Identity: id, KnowledgeBaseIDs: []contracts.ID{"kb"}, Limit: 1000})
		if err != nil {
			return err
		}
		for _, fact := range page.Facts {
			if fact.DocumentID == firstFact.DocumentID {
				return errors.New("withdrawn source entered fresh inventory")
			}
		}
		return nil
	}); err != nil {
		return err
	}
	observed, err := queryread.ObserveCurrent(ctx, unit, scope, []contracts.ID{firstFact.DocumentID}, queryread.CurrentLimits{MaxDocuments: 1, MaxBytes: 4 << 20})
	if err != nil {
		return err
	}
	expected := queryread.CurrentExpectation{StoreEpochID: oldIdentity.StoreEpochID, ScopeVersion: scope.PolicyVersion, Published: []contracts.PublishedFact{firstFact}}
	if !errors.Is(queryread.MatchCurrentReview(scope, expected, observed, map[contracts.ID]uint64{"kb": 9}), queryread.ErrStale) {
		return errors.New("real current check accepted withdrawn evidence")
	}
	if err := assertUnsearchable(firstFact.DocumentID); err != nil {
		return err
	}
	if err := unit.WithinRead(ctx, scope, func(c context.Context, id contracts.QueryReadIdentity, repo contracts.QueryReadRepository) error {
		revoked = true
		return nil
	}); err == nil {
		return errors.New("revoked read returned success")
	}
	revoked = false
	unavailable = true
	called := false
	if err := unit.WithinRead(ctx, scope, func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error {
		called = true
		return nil
	}); err == nil || called {
		return errors.New("unavailable authorizer admitted view")
	}
	unavailable = false
	wrong := scope
	wrong.KnowledgeBaseIDs = []contracts.ID{"outside-scope"}
	if err := unit.WithinRead(ctx, wrong, func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error { return nil }); err == nil {
		return errors.New("wrong KB scope admitted")
	}
	child, cancel := context.WithCancel(ctx)
	if err := unit.WithinRead(child, scope, func(c context.Context, id contracts.QueryReadIdentity, repo contracts.QueryReadRepository) error {
		cancel()
		if _, err := repo.LoadCurrentDocument(c, secondFact.DocumentID); err == nil {
			return errors.New("cancelled SQL read succeeded")
		}
		return nil
	}); err == nil {
		return errors.New("cancelled read view returned success")
	}
	cancel()
	// Rotation committed while the old view is open must reject its result.
	if err := unit.WithinRead(ctx, scope, func(c context.Context, id contracts.QueryReadIdentity, repo contracts.QueryReadRepository) error {
		_, err := db.ExecContext(c, `UPDATE akos_withdraw_v1.query_store_head SET store_epoch_id='read-store-b',row_version=row_version+1 WHERE singleton`)
		return err
	}); err == nil {
		return errors.New("store rotation did not invalidate open view")
	}
	if err := unit.WithinRead(ctx, scope, func(c context.Context, id contracts.QueryReadIdentity, repo contracts.QueryReadRepository) error {
		if id.StoreEpochID != "read-store-b" {
			return errors.New("new store epoch not observed")
		}
		sample["rotated_identity"] = id
		return nil
	}); err != nil {
		return err
	}
	// Actual current approval revocation removes the remaining native source.
	var state contracts.CurrentDocumentState
	if err := unit.WithinRead(ctx, scope, func(c context.Context, id contracts.QueryReadIdentity, repo contracts.QueryReadRepository) error {
		v, e := repo.LoadCurrentDocument(c, secondFact.DocumentID)
		state = v
		return e
	}); err != nil {
		return err
	}
	if state.PublicationReview == nil {
		return errors.New("current SQL approval missing")
	}
	reviewCommand := command
	reviewCommand.Principal = contracts.Principal{SubjectID: "reviewer", SubjectType: "user", Issuer: "fixture"}
	reviewCommand.Scope.SubjectID = "reviewer"
	reviewCommand.Scope.Action = "revision.approve"
	reviewCommand.IdempotencyKey = "revoke-published-for-read"
	reviews := postgres.Reviews{DB: db, Verify: func(context.Context, contracts.AccessScope) error { return nil }, AuthorizeDecision: func(context.Context, document.ReviewCommand) error { return nil }, ProfileDigest: command.Request.ProfileDigest, ReviewPolicyVersion: 7}
	if _, err := reviews.DecideReview(ctx, document.ReviewCommand{DocumentID: secondFact.DocumentID, Request: contracts.ApprovalDecisionRequest{Command: reviewCommand, RevisionID: secondFact.RevisionID, ExpectedApprovalVersion: state.PublicationReview.Approval.RowVersion, Decision: "revoke", Reason: "检索读取前撤销批准"}}); err != nil {
		return err
	}
	if err := unit.WithinRead(ctx, scope, func(c context.Context, id contracts.QueryReadIdentity, repo contracts.QueryReadRepository) error {
		current, err := repo.LoadCurrentDocument(c, secondFact.DocumentID)
		if err != nil || current.PublicationReview == nil || current.PublicationReview.Validity.State == "effective" {
			return errors.New("fresh view missed approval revocation")
		}
		sample["revoked_current"] = current
		page, err := repo.ReadPublishedPage(c, contracts.PublishedPageRequest{Identity: id, KnowledgeBaseIDs: []contracts.ID{"kb"}, Limit: 1000})
		if err != nil {
			return err
		}
		for _, fact := range page.Facts {
			if fact.DocumentID == secondFact.DocumentID {
				return errors.New("revoked source entered inventory")
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := assertUnsearchable(secondFact.DocumentID); err != nil {
		return err
	}
	queryReadSamples = append(queryReadSamples, sample)
	return nil
}
