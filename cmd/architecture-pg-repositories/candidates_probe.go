package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

func verifyDraftCandidates(ctx context.Context, db *sql.DB, scope contracts.AccessScope) error {
	scope.Action = "document.parse"
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SubjectID != "actor" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" || s.Action != "document.parse" {
			return errors.New("fixture candidate authority rejected")
		}
		return nil
	}
	blob := contracts.BlobRef{ID: "candidate-blob", SHA256: contracts.Digest(strings.Repeat("a", 64)), Size: 100}
	draft := contracts.Draft{ID: "candidate-draft", DocumentID: "candidate-document", State: "draft", RowVersion: 1, Blobs: []contracts.BlobRef{blob}}
	bytes, _ := json.Marshal(draft)
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.documents(space_id,kb_id,id,publication_epoch,row_version) VALUES('space','kb','candidate-document',0,1)`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_heads VALUES('space','kb','candidate-document','candidate-draft',1,'draft',$1::jsonb)`, string(bytes)); err != nil {
		return err
	}
	request := contracts.RequestContext{RequestID: "candidate-request", ProfileDigest: contracts.Digest(strings.Repeat("b", 64)), Deadline: time.Now().Add(15 * time.Second)}
	parsed := contracts.ParseResult{ParserVersion: "fixture-parser/1", Blocks: []contracts.ParsedBlock{{ID: "candidate-block", Content: contracts.Content{Kind: contracts.TextBlock, Text: "中文解析候选", Source: contracts.SourceAnchor{Kind: "original", BlobID: blob.ID, Locator: contracts.Locator{Precision: "coarse"}}}}}}
	candidate, err := document.CandidateFromParse(contracts.ParseRequest{DraftID: draft.ID, Input: blob, Request: request}, 1, parsed)
	if err != nil {
		return err
	}
	command := document.CandidateCommand{Request: request, Scope: scope, DocumentID: draft.DocumentID, CandidateID: "candidate-first", Candidate: candidate}
	authorize := func(c context.Context, command document.CandidateCommand) error {
		if c.Err() != nil || command.DocumentID != "candidate-document" || command.Candidate.DraftID != "candidate-draft" || command.Candidate.Input.ID != "candidate-blob" {
			return errors.New("fixture resource denied")
		}
		return nil
	}
	repository := postgres.Candidates{DB: db, Verify: verify, AuthorizeCandidate: authorize}
	receipt, err := repository.CommitCandidate(ctx, command)
	if err != nil || !receipt.Applied || receipt.DraftVersion != 2 {
		return fmt.Errorf("candidate atomic commit: %v", err)
	}
	var rowVersion string
	if db.QueryRowContext(ctx, `SELECT row_version::text FROM akos_withdraw_v1.draft_heads WHERE id='candidate-draft'`).Scan(&rowVersion) != nil || rowVersion != "2" {
		return errors.New("draft CAS missing")
	}
	// Read through a new database pool to prove durable replay, not an in-memory receipt.
	cold, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cold.Close()
	replay, err := (postgres.Candidates{DB: cold, Verify: verify, AuthorizeCandidate: authorize}).CommitCandidate(ctx, command)
	if err != nil || replay != receipt {
		return errors.New("cold candidate receipt replay failed")
	}
	readScope := cloneCandidateReadScope(scope)
	reader := postgres.CandidateReader{DB: cold, Verify: func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.Action != "document.candidate.read" || s.SubjectID != "reviewer" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("fixture read scope rejected")
		}
		return nil
	}, AuthorizeRead: func(c context.Context, r document.CandidateReadRequest) error {
		if c.Err() != nil || r.DocumentID != "candidate-document" || r.DraftID != "candidate-draft" {
			return errors.New("fixture read resource rejected")
		}
		return nil
	}}
	readRequest := document.CandidateReadRequest{Request: request, Scope: readScope, DocumentID: command.DocumentID, DraftID: draft.ID, CandidateID: command.CandidateID}
	readBack, err := reader.ReadCandidate(ctx, readRequest)
	if err != nil || readBack.ActorID != "actor" || readBack.Receipt != receipt || len(readBack.Candidate.Blocks) != 1 || readBack.Candidate.Blocks[0].Content.Text != "中文解析候选" {
		return errors.New("candidate read/actor/digest verification failed")
	}
	deniedReader := reader
	deniedReader.AuthorizeRead = func(context.Context, document.CandidateReadRequest) error {
		return errors.New("fixture resource denied")
	}
	if record, err := deniedReader.ReadCandidate(ctx, readRequest); err == nil || len(record.Candidate.Blocks) != 0 {
		return errors.New("unauthorized candidate exposed")
	}
	conflict := command
	conflict.Candidate.ProfileDigest = contracts.Digest(strings.Repeat("c", 64))
	conflict.Request.ProfileDigest = conflict.Candidate.ProfileDigest
	if result, err := repository.CommitCandidate(ctx, conflict); err == nil || result.CandidateID != "" {
		return errors.New("candidate ID admitted changed content")
	}
	late := command
	late.CandidateID = "candidate-late"
	result, err := repository.CommitCandidate(ctx, late)
	if err != nil || result.Applied || result.Reason != "draft_changed" || result.DraftVersion != 2 {
		return errors.New("late parse did not retain candidate")
	}
	var persisted []byte
	if db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.draft_candidates WHERE id='candidate-late'`).Scan(&persisted) != nil {
		return errors.New("late candidate missing")
	}
	var retained document.ParseCandidate
	if json.Unmarshal(persisted, &retained) != nil || retained.Blocks[0].Content.Text != "中文解析候选" {
		return errors.New("late candidate content lost")
	}
	// Fail after draft update/candidate insertion. All effects must roll back.
	if _, err := db.ExecContext(ctx, `ALTER TABLE akos_withdraw_v1.candidate_audit ADD CONSTRAINT candidate_failure CHECK(candidate_id<>'candidate-rollback')`); err != nil {
		return err
	}
	rollback := command
	rollback.CandidateID = "candidate-rollback"
	rollback.Candidate.ExpectedVersion = 2
	if result, err := repository.CommitCandidate(ctx, rollback); err == nil || result.CandidateID != "" {
		return errors.New("candidate failure returned committed receipt")
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE akos_withdraw_v1.candidate_audit DROP CONSTRAINT candidate_failure`); err != nil {
		return err
	}
	var count int
	if db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.draft_candidates WHERE id='candidate-rollback'`).Scan(&count) != nil || count != 0 {
		return errors.New("candidate escaped rollback")
	}
	if db.QueryRowContext(ctx, `SELECT row_version::text FROM akos_withdraw_v1.draft_heads WHERE id='candidate-draft'`).Scan(&rowVersion) != nil || rowVersion != "2" {
		return errors.New("draft update escaped rollback")
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE akos_withdraw_v1.candidate_outbox ADD CONSTRAINT candidate_outbox_failure CHECK(candidate_id<>'candidate-outbox-failure')`); err != nil {
		return err
	}
	rollback.CandidateID = "candidate-outbox-failure"
	if result, err := repository.CommitCandidate(ctx, rollback); err == nil || result.CandidateID != "" {
		return errors.New("outbox failure returned committed candidate")
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE akos_withdraw_v1.candidate_outbox DROP CONSTRAINT candidate_outbox_failure`); err != nil {
		return err
	}
	if db.QueryRowContext(ctx, `SELECT row_version::text FROM akos_withdraw_v1.draft_heads WHERE id='candidate-draft'`).Scan(&rowVersion) != nil || rowVersion != "2" {
		return errors.New("outbox failure changed draft")
	}
	calls := 0
	revoked := postgres.Candidates{DB: db, Verify: func(c context.Context, s contracts.AccessScope) error {
		calls++
		if calls == 3 {
			return errors.New("revoked before commit")
		}
		return verify(c, s)
	}, AuthorizeCandidate: authorize}
	rollback.CandidateID = "candidate-revoked"
	if result, err := revoked.CommitCandidate(ctx, rollback); err == nil || result.CandidateID != "" || calls != 3 {
		return errors.New("revoked candidate committed")
	}
	if db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.draft_candidates WHERE id='candidate-revoked'`).Scan(&count) != nil || count != 0 {
		return errors.New("revoked candidate persisted")
	}
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.draft_heads SET state='submitted',row_version=3,payload=jsonb_set(jsonb_set(payload,'{State}','"submitted"'),'{RowVersion}','3') WHERE id='candidate-draft'`); err != nil {
		return err
	}
	rollback.CandidateID = "candidate-submitted"
	rollback.Candidate.ExpectedVersion = 3
	result, err = repository.CommitCandidate(ctx, rollback)
	if err != nil || result.Applied || result.Reason != "draft_not_editable" {
		return errors.New("submitted draft overwritten")
	}
	for _, table := range []string{"draft_candidates", "candidate_audit", "candidate_outbox"} {
		if db.QueryRowContext(ctx, "SELECT count(*) FROM akos_withdraw_v1."+table).Scan(&count) != nil || count != 3 {
			return errors.New("candidate/audit/outbox effects not atomic or duplicated")
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.draft_candidates SET request_digest=repeat('d',64) WHERE id='candidate-first'`); err == nil {
		return errors.New("immutable candidate modified")
	}
	return nil
}

func cloneCandidateReadScope(scope contracts.AccessScope) contracts.AccessScope {
	scope.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	scope.Action = "document.candidate.read"
	scope.SubjectID = "reviewer"
	return scope
}
