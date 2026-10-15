package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

func candidateReadFixture(t *testing.T) (context.Context, document.CandidateReadRequest, CandidateReader, *candidateDriverState, *sql.DB) {
	t.Helper()
	ctx, _, command, db, state := candidateCommandFixture(t)
	payload, _ := json.Marshal(command.Candidate)
	receipt, _ := json.Marshal(document.CandidateReceipt{CandidateID: command.CandidateID, DraftID: command.Candidate.DraftID, DocumentID: command.DocumentID, Applied: true, DraftVersion: 2})
	digest, err := candidateRequestDigest(command.Scope, command.DocumentID, command.Candidate)
	if err != nil {
		t.Fatal(err)
	}
	state.candidateRead = []driver.Value{payload, receipt, string(command.Scope.SubjectID), digest}
	scope := cloneScope(command.Scope)
	scope.Action = "document.candidate.read"
	scope.SubjectID = "reviewer"
	request := document.CandidateReadRequest{Request: command.Request, Scope: scope, DocumentID: command.DocumentID, DraftID: command.Candidate.DraftID, CandidateID: command.CandidateID}
	reader := CandidateReader{DB: db, Verify: func(context.Context, contracts.AccessScope) error { return nil }, AuthorizeRead: func(context.Context, document.CandidateReadRequest) error { return nil }}
	return ctx, request, reader, state, db
}

func TestCandidateReaderVerifiesStoredActorAndOwnsReturnedContent(t *testing.T) {
	ctx, request, reader, state, _ := candidateReadFixture(t)
	record, err := reader.ReadCandidate(ctx, request)
	if err != nil || record.ActorID != "actor" || record.Candidate.Blocks[0].Content.Text != "candidate" || state.commits != 1 {
		t.Fatalf("reviewer could not read verified original actor: %+v %v", record, err)
	}
	record.Candidate.Blocks[0].Content.Text = "edited output"
	again, err := reader.ReadCandidate(ctx, request)
	if err != nil || again.Candidate.Blocks[0].Content.Text != "candidate" {
		t.Fatal("record alias leaked across reads", err)
	}
}

func TestCandidateReaderRejectsCorruptOwnershipReceiptAndDigest(t *testing.T) {
	for _, kind := range []string{"payload", "actor", "digest", "receipt", "missing"} {
		t.Run(kind, func(t *testing.T) {
			ctx, request, reader, state, _ := candidateReadFixture(t)
			switch kind {
			case "payload":
				state.candidateRead[0] = []byte(strings.Replace(string(state.candidateRead[0].([]byte)), "candidate", "tampered", 1))
			case "actor":
				state.candidateRead[2] = "different actor"
			case "digest":
				state.candidateRead[3] = strings.Repeat("0", 64)
			case "receipt":
				state.candidateRead[1] = []byte(`{"CandidateID":"foreign","DraftVersion":2}`)
			case "missing":
				state.candidateRead = nil
			}
			record, err := reader.ReadCandidate(ctx, request)
			if !errors.Is(err, document.ErrCandidateRead) || record.ActorID != "" || len(record.Candidate.Blocks) != 0 {
				t.Fatalf("corrupt record exposed: %+v %v", record, err)
			}
		})
	}
}

func TestCandidateReaderRequiresIndependentReadGrant(t *testing.T) {
	for _, kind := range []string{"missing", "denied", "write action", "revoked"} {
		t.Run(kind, func(t *testing.T) {
			ctx, request, reader, state, _ := candidateReadFixture(t)
			calls := 0
			reader.AuthorizeRead = func(context.Context, document.CandidateReadRequest) error {
				calls++
				if kind == "denied" || kind == "revoked" && calls == 3 {
					return errors.New("denied")
				}
				return nil
			}
			if kind == "missing" {
				reader.AuthorizeRead = nil
			}
			if kind == "write action" {
				request.Scope.Action = "document.parse"
			}
			record, err := reader.ReadCandidate(ctx, request)
			if !errors.Is(err, document.ErrCandidateRead) || len(record.Candidate.Blocks) != 0 || state.commits != 0 {
				t.Fatal("read admitted without current resource grant", err)
			}
			if kind != "revoked" && (state.begins != 0 || state.queries != 0) {
				t.Fatal("unauthorized read reached SQL")
			}
			if kind == "revoked" && (calls != 3 || state.rollbacks != 1) {
				t.Fatal("late revocation did not discard read")
			}
		})
	}
}

func TestCandidateReaderFreezesScopeAcrossAuthorizationCallbacks(t *testing.T) {
	ctx, request, reader, state, _ := candidateReadFixture(t)
	reader.AuthorizeRead = func(_ context.Context, owned document.CandidateReadRequest) error {
		if owned.Scope.KnowledgeBaseIDs[0] != "kb" || owned.DraftID != "draft" {
			return errors.New("request aliased")
		}
		owned.Scope.KnowledgeBaseIDs[0] = "modified callback"
		request.Scope.KnowledgeBaseIDs[0] = "modified external"
		return nil
	}
	if _, err := reader.ReadCandidate(ctx, request); err != nil {
		t.Fatal(err)
	}
	if len(state.readArgs) != 5 || state.readArgs[1].Value != "kb" || state.readArgs[3].Value != "draft" {
		t.Fatal("SQL read scope changed")
	}
}
