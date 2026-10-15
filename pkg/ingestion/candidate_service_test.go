package ingestion_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/ingestion"
)

type parseCandidateFunc func(context.Context, contracts.ParseRequest) (contracts.ParseResult, error)

func (f parseCandidateFunc) Parse(c context.Context, r contracts.ParseRequest) (contracts.ParseResult, error) {
	return f(c, r)
}

type commitCandidateFunc func(context.Context, document.CandidateCommand) (document.CandidateReceipt, error)

func (f commitCandidateFunc) CommitCandidate(c context.Context, r document.CandidateCommand) (document.CandidateReceipt, error) {
	return f(c, r)
}

func serviceFixture(t *testing.T) (context.Context, ingestion.DraftParseCommand, ingestion.CandidateService, *int, *int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	deadline, _ := ctx.Deadline()
	command := ingestion.DraftParseCommand{Request: contracts.RequestContext{RequestID: "request", ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), Deadline: deadline}, Principal: contracts.Principal{SubjectID: "actor", SubjectType: "user", Issuer: "fixture"}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "actor", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "document.parse", PolicyVersion: 1, ExpiresAt: deadline}, DocumentID: "document", DraftID: "draft", CandidateID: "candidate", ExpectedVersion: 1, Input: contracts.BlobRef{ID: "blob", Size: 100, SHA256: contracts.Digest(strings.Repeat("b", 64))}, Format: "pdf", Options: contracts.ParseOptions{Languages: []string{"zh"}}}
	parses, commits := new(int), new(int)
	service := ingestion.CandidateService{ProfileDigest: command.Request.ProfileDigest, Authorize: func(context.Context, ingestion.DraftParseCommand) error { return nil }}
	service.Parser = parseCandidateFunc(func(_ context.Context, r contracts.ParseRequest) (contracts.ParseResult, error) {
		*parses++
		r.Scope.KnowledgeBaseIDs[0] = "supplier mutation"
		r.Options.Languages[0] = "supplier mutation"
		return contracts.ParseResult{ParserVersion: "fixture/1", Blocks: []contracts.ParsedBlock{{ID: "block", Content: contracts.Content{Kind: contracts.TextBlock, Text: "candidate", Source: contracts.SourceAnchor{Kind: "original", BlobID: r.Input.ID, Locator: contracts.Locator{Precision: "coarse"}}}}}}, nil
	})
	service.Repository = commitCandidateFunc(func(_ context.Context, c document.CandidateCommand) (document.CandidateReceipt, error) {
		*commits++
		return document.CandidateReceipt{CandidateID: c.CandidateID, DraftID: c.Candidate.DraftID, DocumentID: c.DocumentID, Applied: true, DraftVersion: 2}, nil
	})
	return ctx, command, service, parses, commits
}

func TestCandidateServiceFreezesAuthorizationAndSupplierInput(t *testing.T) {
	ctx, command, service, parses, commits := serviceFixture(t)
	service.Authorize = func(_ context.Context, c ingestion.DraftParseCommand) error {
		if c.Scope.KnowledgeBaseIDs[0] != "kb" || c.Options.Languages[0] != "zh" {
			return errors.New("binding changed")
		}
		c.Scope.KnowledgeBaseIDs[0] = "callback mutation"
		command.Scope.KnowledgeBaseIDs[0] = "caller mutation"
		command.Options.Languages[0] = "caller mutation"
		return nil
	}
	prepared, err := service.Prepare(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.Commit(ctx, prepared)
	if err != nil || !receipt.Applied || *parses != 1 || *commits != 1 {
		t.Fatalf("flow failed: %+v %v", receipt, err)
	}
}

func TestCandidateServiceRejectsBeforeParsingAndAfterRevocation(t *testing.T) {
	for _, name := range []string{"denied", "profile", "principal", "revoked"} {
		t.Run(name, func(t *testing.T) {
			ctx, command, service, parses, commits := serviceFixture(t)
			calls := 0
			service.Authorize = func(context.Context, ingestion.DraftParseCommand) error {
				calls++
				if name == "denied" || name == "revoked" && calls == 2 {
					return errors.New("denied")
				}
				return nil
			}
			if name == "profile" {
				command.Request.ProfileDigest = contracts.Digest(strings.Repeat("c", 64))
			}
			if name == "principal" {
				command.Principal.SubjectID = "other"
			}
			if _, err := service.Prepare(ctx, command); err == nil {
				t.Fatal("request admitted")
			}
			want := 0
			if name == "revoked" {
				want = 1
			}
			if *parses != want || *commits != 0 {
				t.Fatalf("unauthorized effects: %d %d", *parses, *commits)
			}
		})
	}
}

func TestCandidateServiceUnknownCommitRetriesExactPreparedOutput(t *testing.T) {
	ctx, command, service, parses, _ := serviceFixture(t)
	prepared, err := service.Prepare(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	service.Repository = commitCandidateFunc(func(_ context.Context, c document.CandidateCommand) (document.CandidateReceipt, error) {
		calls++
		if c.Candidate.Blocks[0].Content.Text != "candidate" {
			t.Fatal("retry input mutated")
		}
		c.Candidate.Blocks[0].Content.Text = "repository mutation"
		if calls == 1 {
			return document.CandidateReceipt{}, context.DeadlineExceeded
		}
		return document.CandidateReceipt{CandidateID: c.CandidateID, DraftID: c.Candidate.DraftID, DocumentID: c.DocumentID, Applied: true, DraftVersion: 2}, nil
	})
	if result, err := service.Commit(ctx, prepared); !errors.Is(err, document.ErrCandidateCommitUnknown) || result.CandidateID != "" {
		t.Fatalf("unknown outcome hidden: %+v %v", result, err)
	}
	if result, err := service.Commit(ctx, prepared); err != nil || !result.Applied || calls != 2 || *parses != 1 {
		t.Fatalf("retry reparsed: %+v %v", result, err)
	}
}

func TestCandidateServiceRejectsInvalidReceiptAndClassifiesKnownRejection(t *testing.T) {
	for _, known := range []bool{false, true} {
		ctx, command, service, _, _ := serviceFixture(t)
		prepared, err := service.Prepare(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
		service.Repository = commitCandidateFunc(func(context.Context, document.CandidateCommand) (document.CandidateReceipt, error) {
			if known {
				return document.CandidateReceipt{}, document.ErrCandidateRejected
			}
			return document.CandidateReceipt{CandidateID: "foreign"}, nil
		})
		result, err := service.Commit(ctx, prepared)
		want := document.ErrCandidateCommitUnknown
		if known {
			want = ingestion.ErrCandidateRequest
		}
		if !errors.Is(err, want) || result.CandidateID != "" {
			t.Fatalf("bad receipt classification: %+v %v", result, err)
		}
	}
}

func TestCandidateServiceClipsParserDeadlineToScope(t *testing.T) {
	ctx, command, service, _, commits := serviceFixture(t)
	command.Scope.ExpiresAt = time.Now().Add(30 * time.Millisecond)
	service.Parser = parseCandidateFunc(func(c context.Context, _ contracts.ParseRequest) (contracts.ParseResult, error) {
		<-c.Done()
		return contracts.ParseResult{}, c.Err()
	})
	if _, err := service.Prepare(ctx, command); err == nil || *commits != 0 {
		t.Fatal("expired parse reached commit")
	}
}

func TestCandidateServiceUnknownTakesPrecedenceOverRejection(t *testing.T) {
	ctx, command, service, _, _ := serviceFixture(t)
	prepared, err := service.Prepare(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	service.Repository = commitCandidateFunc(func(context.Context, document.CandidateCommand) (document.CandidateReceipt, error) {
		return document.CandidateReceipt{}, errors.Join(document.ErrCandidateRejected, document.ErrCandidateCommitUnknown)
	})
	if _, err := service.Commit(ctx, prepared); !errors.Is(err, document.ErrCandidateCommitUnknown) {
		t.Fatalf("unknown outcome downgraded: %v", err)
	}
}
