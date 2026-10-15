package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/ingestion"
	"github.com/MemoryGet/memorys/pkg/parser"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/storage"
)

type countingCandidateParser struct {
	inner contracts.Parser
	calls int
}

func (p *countingCandidateParser) Parse(ctx context.Context, r contracts.ParseRequest) (contracts.ParseResult, error) {
	p.calls++
	return p.inner.Parse(ctx, r)
}

// The real repository commits, then this transport fixture loses its response.
// Retrying must read the original SQL receipt without invoking the parser again.
type lostCandidateResponse struct{ repository document.CandidateRepository }

func (p lostCandidateResponse) CommitCandidate(ctx context.Context, c document.CandidateCommand) (document.CandidateReceipt, error) {
	if _, err := p.repository.CommitCandidate(ctx, c); err != nil {
		return document.CandidateReceipt{}, err
	}
	return document.CandidateReceipt{}, document.ErrCandidateCommitUnknown
}

func verifyCandidateApplication(ctx context.Context, db *sql.DB, scope contracts.AccessScope) error {
	folder, err := os.MkdirTemp("", "akos-candidate-app-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(folder)
	scope.Action = "document.parse"
	scope.ScopeID = "application-parse-scope"
	deadline, _ := ctx.Deadline()
	request := contracts.RequestContext{RequestID: "app-request", ProfileDigest: contracts.Digest(strings.Repeat("e", 64)), Deadline: deadline}
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.ScopeID != "application-parse-scope" || s.SubjectID != "actor" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" || s.Action != "document.parse" {
			return errors.New("application fixture scope denied")
		}
		return nil
	}
	text := "  中文设备维护😀\r\n\r\n保留原文空白  "
	data := []byte(text)
	sum := sha256.Sum256(data)
	sourceDigest := contracts.Digest(hex.EncodeToString(sum[:]))
	var blob contracts.BlobRef
	store := storage.LocalBlobStore{Root: filepath.Join(folder, "blobs"), Authorize: func(c context.Context, s contracts.AccessScope, b contracts.BlobRef) error {
		if s.Action == "document.upload" {
			// Put requests authorization to create a new blob before it has a
			// BlobRef. Source bytes/expected digest are checked by Put itself.
			if c.Err() != nil || s.ScopeID != "application-upload-scope" || s.SubjectID != "actor" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" || b != (contracts.BlobRef{}) {
				return errors.New("fixture upload grant rejected")
			}
			return nil
		}
		if verify(c, s) != nil || blob.ID == "" || b != blob {
			return errors.New("fixture original-read grant rejected")
		}
		return nil
	}}
	uploadScope := scope
	uploadScope.Action = "document.upload"
	uploadScope.ScopeID = "application-upload-scope"
	blob, err = (ingestion.UploadService{Store: store}).Upload(ctx, contracts.BlobWriteRequest{Request: request, Scope: uploadScope, ExpectedSHA256: sourceDigest, Size: int64(len(data)), MediaType: "text/markdown", IdempotencyKey: "application-source"}, bytes.NewReader(data))
	if err != nil {
		return err
	}
	draft := contracts.Draft{ID: "app-draft", DocumentID: "app-document", RowVersion: 1, State: "draft", Blobs: []contracts.BlobRef{blob}}
	payload, _ := json.Marshal(draft)
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.documents(space_id,kb_id,id,publication_epoch,row_version) VALUES('space','kb','app-document',0,1)`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_heads VALUES('space','kb','app-document','app-draft',1,'draft',$1::jsonb)`, string(payload)); err != nil {
		return err
	}
	authorizeWrite := func(c context.Context, w document.CandidateCommand) error {
		if c.Err() != nil || w.DocumentID != draft.DocumentID || w.Candidate.DraftID != draft.ID || w.Candidate.Input != blob {
			return errors.New("application fixture write denied")
		}
		return nil
	}
	repository := postgres.Candidates{DB: db, Verify: verify, AuthorizeCandidate: authorizeWrite}
	counted := &countingCandidateParser{inner: parser.MarkdownParser{Store: store}}
	service := ingestion.CandidateService{Parser: counted, Repository: lostCandidateResponse{repository}, ProfileDigest: request.ProfileDigest, Authorize: func(c context.Context, r ingestion.DraftParseCommand) error {
		if verify(c, r.Scope) != nil || r.Principal.SubjectID != "actor" || r.Principal.Issuer != "fixture" || r.DocumentID != draft.DocumentID || r.DraftID != draft.ID || r.Input != blob {
			return errors.New("application fixture command denied")
		}
		return nil
	}}
	command := ingestion.DraftParseCommand{Request: request, Scope: scope, Principal: contracts.Principal{SubjectID: "actor", SubjectType: "user", Issuer: "fixture"}, DocumentID: draft.DocumentID, DraftID: draft.ID, CandidateID: "app-first", ExpectedVersion: 1, Input: blob, Format: "markdown"}
	prepared, err := service.Prepare(ctx, command)
	if err != nil {
		return err
	}
	if receipt, err := service.Commit(ctx, prepared); !errors.Is(err, document.ErrCandidateCommitUnknown) || receipt.CandidateID != "" {
		return errors.New("lost SQL response did not remain uncertain")
	}
	cold, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cold.Close()
	service.Repository = postgres.Candidates{DB: cold, Verify: verify, AuthorizeCandidate: authorizeWrite}
	receipt, err := service.Commit(ctx, prepared)
	if err != nil || !receipt.Applied || receipt.DraftVersion != 2 || counted.calls != 1 {
		return errors.New("application cold retry reparsed or lost original commit")
	}
	readScope := cloneCandidateReadScope(scope)
	reader := postgres.CandidateReader{DB: cold, Verify: func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SubjectID != "reviewer" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" || s.Action != "document.candidate.read" {
			return errors.New("application fixture reviewer denied")
		}
		return nil
	}, AuthorizeRead: func(c context.Context, r document.CandidateReadRequest) error {
		if c.Err() != nil || r.DocumentID != draft.DocumentID || r.DraftID != draft.ID {
			return errors.New("application fixture read denied")
		}
		return nil
	}}
	readRequest := document.CandidateReadRequest{Request: request, Scope: readScope, DocumentID: draft.DocumentID, DraftID: draft.ID, CandidateID: "app-first"}
	record, err := reader.ReadCandidate(ctx, readRequest)
	if err != nil || record.Receipt != receipt || record.ActorID != "actor" || len(record.Candidate.Blocks) != 1 {
		return errors.New("application persisted candidate read failed")
	}
	content := record.Candidate.Blocks[0].Content
	if content.Text != text || content.Source.BlobID != blob.ID || content.Source.Locator.Precision != "exact" || content.Source.Locator.Characters == nil || content.Source.Locator.Characters.End != utf8.RuneCountInString(text) {
		return errors.New("application source/Unicode text lost")
	}
	// A human edit committed after preparation must survive a later parse result.
	if db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.draft_heads WHERE id='app-draft'`).Scan(&payload) != nil || json.Unmarshal(payload, &draft) != nil {
		return errors.New("application draft read failed")
	}
	frozenBefore, err := document.BuildFrozenContent(draft)
	if err != nil || frozenBefore.HashFormat != "akos.freeze/1" || frozenBefore.ContentHash == "" || !bytes.Contains(frozenBefore.Payload, []byte("中文设备维护😀")) {
		return errors.New("real parsed draft did not map to frozen content")
	}
	frozenCopy := append([]byte(nil), frozenBefore.Payload...)
	draft.RowVersion = 3
	draft.Blocks[0].Content.Text = "人工修订"
	// Edited prose is no longer an exact original-text quotation.
	draft.Blocks[0].Content.Source.Locator.Precision = "coarse"
	draft.Blocks[0].Content.Source.Locator.Characters = nil
	draft.Blocks[0].RowVersion++
	payload, _ = json.Marshal(draft)
	updated, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.draft_heads SET row_version=3,payload=$1::jsonb WHERE id='app-draft' AND row_version=2`, string(payload))
	if err != nil {
		return err
	}
	if n, err := updated.RowsAffected(); err != nil || n != 1 {
		return errors.New("fixture human edit CAS failed")
	}
	command.CandidateID = "app-late"
	late, err := service.Prepare(ctx, command)
	if err != nil {
		return err
	}
	lateReceipt, err := service.Commit(ctx, late)
	if err != nil || lateReceipt.Applied || lateReceipt.Reason != "draft_changed" || lateReceipt.DraftVersion != 3 || counted.calls != 2 {
		return errors.New("application late candidate not retained")
	}
	readRequest.CandidateID = "app-late"
	lateRecord, err := reader.ReadCandidate(ctx, readRequest)
	if err != nil || lateRecord.Candidate.Blocks[0].Content.Text != text {
		return errors.New("late original output not durably readable")
	}
	if db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.draft_heads WHERE id='app-draft'`).Scan(&payload) != nil || json.Unmarshal(payload, &draft) != nil || draft.Blocks[0].Content.Text != "人工修订" {
		return errors.New("late parser overwrote human edit")
	}
	if !bytes.Equal(frozenBefore.Payload, frozenCopy) || bytes.Contains(frozenBefore.Payload, []byte("人工修订")) {
		return errors.New("later draft edit altered prepared frozen bytes")
	}
	for _, table := range []string{"candidate_audit", "candidate_outbox"} {
		var count int
		if db.QueryRowContext(ctx, "SELECT count(*) FROM akos_withdraw_v1."+table+" WHERE candidate_id IN('app-first','app-late')").Scan(&count) != nil || count != 2 {
			return errors.New("application retry duplicated durable effects")
		}
	}
	var unpublished bool
	if db.QueryRowContext(ctx, `SELECT publication_epoch=0 AND active_revision_id IS NULL AND active_generation_set_id IS NULL FROM akos_withdraw_v1.documents WHERE id='app-document'`).Scan(&unpublished) != nil || !unpublished {
		return errors.New("candidate pipeline altered publication")
	}
	if err := verifySubmissions(ctx, db, scope, request, store, blob); err != nil {
		return err
	}
	if err := os.RemoveAll(folder); err != nil {
		return err
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		return errors.New("application fixture files not cleaned")
	}
	return nil
}
