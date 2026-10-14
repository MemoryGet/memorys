package contentverify_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type reader struct {
	record contracts.FrozenContentRecord
	err    error
	calls  int
	change func()
}

func (r *reader) ReadFrozenContent(_ context.Context, request contracts.FrozenContentReadRequest) (contracts.FrozenContentRecord, error) {
	r.calls++
	request.Scope.KnowledgeBaseIDs[0] = "changed"
	if r.change != nil {
		r.change()
	}
	return r.record, r.err
}
func fixture(t *testing.T) (contracts.FrozenContentReadRequest, contracts.FrozenRevisionFact, *reader) {
	t.Helper()
	raw, err := os.ReadFile("../../docs/architecture/examples/frozen-content-golden.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var suite struct {
		Cases []struct {
			Input  string                 `json:"input_json"`
			Hashes canonical.FrozenHashes `json:"hashes"`
		}
	}
	if json.Unmarshal(raw, &suite) != nil || len(suite.Cases) == 0 {
		t.Fatal("missing frozen content fixture")
	}
	f := suite.Cases[0]
	expected := contracts.FrozenRevisionFact{ID: "revision", DocumentID: "document", ContentHash: contracts.Digest(f.Hashes.ContentHash), BlocksHash: contracts.Digest(f.Hashes.BlocksHash), HashFormat: f.Hashes.Format}
	request := contracts.FrozenContentReadRequest{Request: contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Minute)}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "subject", SpaceID: "space", Action: "knowledge.search", PolicyVersion: 1, KnowledgeBaseIDs: []contracts.ID{"kb"}, ExpiresAt: time.Now().Add(time.Minute)}, DocumentID: expected.DocumentID, RevisionID: expected.ID}
	return request, expected, &reader{record: contracts.FrozenContentRecord{DocumentID: expected.DocumentID, RevisionID: expected.ID, Payload: []byte(f.Input)}}
}
func TestVerifyFrozenReadsActualCanonicalContent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	req, f, r := fixture(t)
	if err := contentverify.VerifyFrozen(ctx, r, req, f); err != nil {
		t.Fatal(err)
	}
	if req.Scope.KnowledgeBaseIDs[0] != "kb" {
		t.Fatal("reader mutated caller scope")
	}
	normalized, err := canonical.JSON(r.record.Payload)
	if err != nil {
		t.Fatal(err)
	}
	r.record.Payload = append([]byte(" \n"), normalized...)
	if err := contentverify.VerifyFrozen(ctx, r, req, f); err != nil {
		t.Fatal("equivalent canonical bytes rejected", err)
	}
}
func TestVerifyFrozenRejectsCorruptionMismatchedOwnershipAndPartialBytes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, kind := range []string{"content", "blocks", "document", "revision", "bytes", "oversize", "error"} {
		t.Run(kind, func(t *testing.T) {
			req, f, r := fixture(t)
			switch kind {
			case "content":
				f.ContentHash = contracts.Digest(strings.Repeat("0", 64))
			case "blocks":
				f.BlocksHash = contracts.Digest(strings.Repeat("0", 64))
			case "document":
				r.record.DocumentID = "other"
			case "revision":
				r.record.RevisionID = "other"
			case "bytes":
				r.record.Payload = r.record.Payload[:len(r.record.Payload)/2]
			case "oversize":
				r.record.Payload = []byte(strings.Repeat(" ", 4<<20+1))
			case "error":
				r.err = errors.New("private path/token")
			}
			if contentverify.VerifyFrozen(ctx, r, req, f) != contentverify.ErrContent {
				t.Fatal("unverified content accepted")
			}
		})
	}
}
func TestVerifyFrozenEnforcesParentBudgetAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	req, f, r := fixture(t)
	req.Request.Deadline = time.Now().Add(-time.Second)
	if contentverify.VerifyFrozen(ctx, r, req, f) != context.DeadlineExceeded || r.calls != 0 {
		t.Fatal("expired request read content")
	}
	req, f, r = fixture(t)
	if contentverify.VerifyFrozen(context.Background(), r, req, f) != contentverify.ErrContent || r.calls != 0 {
		t.Fatal("unbounded request read content")
	}
	req, f, r = fixture(t)
	r.change = cancel
	if contentverify.VerifyFrozen(ctx, r, req, f) != context.Canceled {
		t.Fatal("canceled verification succeeded")
	}
}
