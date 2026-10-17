package contentverify_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/storage"
)

type originalControl func(context.Context, contracts.FrozenContentReadRequest, contracts.BlobRef) (contracts.BlobReadRequest, error)

func (f originalControl) ResolveOriginalRead(c context.Context, r contracts.FrozenContentReadRequest, b contracts.BlobRef) (contracts.BlobReadRequest, error) {
	return f(c, r, b)
}

func TestApprovedOriginalRejectsRoundedAndInvalidFrozenSizes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, size := range []string{"16.000000000000000000000001", "16.5", "null", "-1", "67108865", "1e100"} {
		t.Run(size, func(t *testing.T) {
			q, f, r, store, b, _ := approvedOriginalFixture(t, ctx)
			r.record.Payload = []byte(strings.Replace(string(r.record.Payload), `"size":16e0`, `"size":`+size, 1))
			hashes, err := canonical.Frozen(r.record.Payload)
			if err != nil {
				t.Fatal(err)
			}
			f.ContentHash, f.BlocksHash = contracts.Digest(hashes.ContentHash), contracts.Digest(hashes.BlocksHash)
			called := false
			control := originalControl(func(context.Context, contracts.FrozenContentReadRequest, contracts.BlobRef) (contracts.BlobReadRequest, error) {
				called = true
				return contracts.BlobReadRequest{}, nil
			})
			if err := contentverify.VerifyApprovedOriginal(ctx, r, store, control, q, f, b.ID); err == nil || called {
				t.Fatal("invalid/rounded size reached grant resolution")
			}
		})
	}
}

func approvedOriginalFixture(t *testing.T, ctx context.Context, media ...string) (contracts.FrozenContentReadRequest, contracts.FrozenRevisionFact, *reader, storage.LocalBlobStore, contracts.BlobRef, originalControl) {
	t.Helper()
	data := []byte("实际原件😀")
	q := originalRequest(data)
	if len(media) > 0 {
		q.Blob.MediaType = media[0]
	}
	store := storage.LocalBlobStore{Root: t.TempDir(), Authorize: func(_ context.Context, s contracts.AccessScope, _ contracts.BlobRef) error {
		if s.SubjectID != "subject" || s.Action != "blob.read" {
			return errors.New("original grant required")
		}
		return nil
	}}
	b, err := store.Put(ctx, contracts.BlobWriteRequest{Request: q.Request, Scope: q.Scope, ExpectedSHA256: q.Blob.SHA256, Size: q.Blob.Size, MediaType: q.Blob.MediaType, IdempotencyKey: "original"}, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := document.BuildFrozenContent(contracts.Draft{ID: "draft", DocumentID: "document", RowVersion: 1, State: "draft", Blobs: []contracts.BlobRef{b}, Blocks: []contracts.DraftBlock{{ID: "block", DraftID: "draft", RowVersion: 1, Content: contracts.Content{Kind: contracts.TextBlock, Text: "实际原件😀", Source: contracts.SourceAnchor{Kind: "original", BlobID: b.ID, Locator: contracts.Locator{Precision: "coarse"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	req := contracts.FrozenContentReadRequest{Request: q.Request, Scope: q.Scope, DocumentID: "document", RevisionID: "revision"}
	req.Scope.Action = "knowledge.search"
	fact := contracts.FrozenRevisionFact{ID: req.RevisionID, DocumentID: req.DocumentID, ContentHash: frozen.ContentHash, BlocksHash: frozen.BlocksHash, HashFormat: frozen.HashFormat}
	r := &reader{record: contracts.FrozenContentRecord{DocumentID: req.DocumentID, RevisionID: req.RevisionID, Payload: frozen.Payload}}
	control := originalControl(func(_ context.Context, parent contracts.FrozenContentReadRequest, blob contracts.BlobRef) (contracts.BlobReadRequest, error) {
		scope := parent.Scope
		scope.Action = "blob.read"
		return contracts.BlobReadRequest{Request: parent.Request, Scope: scope, Blob: blob}, nil
	})
	return req, fact, r, store, b, control
}
func TestApprovedOriginalUsesVerifiedInventoryAndSeparateReadGrant(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	q, f, r, store, b, control := approvedOriginalFixture(t, ctx)
	baseControl := control
	controlCalls := 0
	control = func(c context.Context, p contracts.FrozenContentReadRequest, b contracts.BlobRef) (contracts.BlobReadRequest, error) {
		controlCalls++
		return baseControl(c, p, b)
	}
	if err := contentverify.VerifyApprovedOriginal(ctx, r, store, control, q, f, b.ID); err != nil {
		t.Fatal(err, "grant calls", controlCalls, "reads", r.calls)
	}
	if r.calls != 2 || q.Scope.Action != "knowledge.search" || q.Scope.KnowledgeBaseIDs[0] != "kb" {
		t.Fatal("approval not refreshed or parent grant mutated")
	}
	called := false
	control = func(c context.Context, p contracts.FrozenContentReadRequest, b contracts.BlobRef) (contracts.BlobReadRequest, error) {
		called = true
		return contracts.BlobReadRequest{}, nil
	}
	if err := contentverify.VerifyApprovedOriginal(ctx, r, store, control, q, f, "not-in-approved-content"); err == nil || called {
		t.Fatal("unapproved blob reached grant resolution")
	}
}
func TestApprovedOriginalRejectsGrantSubstitutionAndLateRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, kind := range []string{"blob", "subject", "space", "kb", "deadline", "late-grant", "late-expiry", "expiry-during-final-read", "late-approval", "reuse-search"} {
		t.Run(kind, func(t *testing.T) {
			q, f, r, store, b, base := approvedOriginalFixture(t, ctx)
			calls := 0
			control := originalControl(func(c context.Context, p contracts.FrozenContentReadRequest, blob contracts.BlobRef) (contracts.BlobReadRequest, error) {
				calls++
				grant, err := base(c, p, blob)
				switch kind {
				case "blob":
					grant.Blob.Size++
				case "subject":
					grant.Scope.SubjectID = "other"
				case "space":
					grant.Scope.SpaceID = "other"
				case "kb":
					grant.Scope.KnowledgeBaseIDs = []contracts.ID{"other"}
				case "deadline":
					grant.Request.Deadline = grant.Request.Deadline.Add(time.Hour)
				case "late-grant":
					if calls == 2 {
						return grant, errors.New("revoked")
					}
				case "late-expiry":
					if calls == 2 {
						grant.Request.Deadline = time.Now().Add(-time.Second)
						grant.Scope.ExpiresAt = grant.Request.Deadline
					}
				case "expiry-during-final-read":
					if calls == 2 {
						grant.Request.Deadline = time.Now().Add(5 * time.Millisecond)
						r.change = func() { time.Sleep(15 * time.Millisecond) }
					}
				case "late-approval":
					if calls == 2 {
						r.err = errors.New("withdrawn")
					}
				case "reuse-search":
					grant.Scope.Action = "knowledge.search"
				}
				return grant, err
			})
			if err := contentverify.VerifyApprovedOriginal(ctx, r, store, control, q, f, b.ID); err == nil {
				t.Fatal("substituted or revoked grant accepted")
			}
		})
	}
}
