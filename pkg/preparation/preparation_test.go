package preparation_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/chunk"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

func fixture(t *testing.T) (document.PublicationWork, contracts.IndexProfile, []contracts.Chunk) {
	t.Helper()
	source := "中文😀\r\n来源"
	sum := sha256.Sum256([]byte(source))
	blob := contracts.BlobRef{ID: "blob", SHA256: contracts.Digest(hex.EncodeToString(sum[:])), Size: int64(len(source)), MediaType: "text/plain"}
	d := contracts.Draft{ID: "draft", DocumentID: "document", RowVersion: 1, State: "draft", Blobs: []contracts.BlobRef{blob}, Blocks: []contracts.DraftBlock{{ID: "block", DraftID: "draft", RowVersion: 1, Content: contracts.Content{Kind: contracts.TextBlock, Text: source, Source: contracts.SourceAnchor{Kind: "original", BlobID: "blob", Locator: contracts.Locator{Precision: "coarse"}}}}}}
	p, err := document.PrepareSubmission(d, 1, "revision", "review", "author", 7, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	w := contracts.ModuleBinding{InstanceID: "writer", Capability: contracts.IndexWriting, ContractVersion: "1.0"}
	s := contracts.ModuleBinding{InstanceID: "splitter", Capability: contracts.Splitting, ContractVersion: "1.0"}
	profile := contracts.IndexProfile{ID: "profile", Version: 1, Mode: "independent", Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.IndexWriting: w, contracts.Splitting: s}, Routes: []contracts.IndexRoute{{ID: "keyword", Channels: []contracts.Channel{contracts.KeywordChannel}, Binding: w}}, Policy: contracts.IndexPolicy{SchemaVersion: 1, Chunking: contracts.ChunkPolicy{Strategy: "characters", MaxCodepoints: 2}, RequiredChannels: []contracts.Channel{contracts.KeywordChannel}, ProjectionFormatVersion: "akos.chunks/1"}}
	profile.Digest, err = preparation.ProfileDigest(profile)
	if err != nil {
		t.Fatal(err)
	}
	lease := contracts.Lease{JobID: "job", WorkerID: "worker", AttemptID: "attempt", FencingToken: 1, ExpiresAt: time.Now().Add(time.Minute)}
	work := document.PublicationWork{Revision: p.Revision, FrozenPayload: p.Frozen.Payload, Lease: lease, Job: contracts.Job{ID: "job", OperationID: "operation", ProfileDigest: profile.Digest, State: "running", FencingToken: 1, RowVersion: 2}, Plan: document.PublicationStartPlan{SpaceID: "space", KnowledgeBaseID: "kb", Result: document.PublicationStartResult{Operation: contracts.PublicationOperation{ID: "operation"}}, Review: contracts.CurrentPublicationReview{Revision: contracts.FrozenRevisionFact{ID: p.Revision.ID, ContentHash: p.Revision.ContentHash}}, Backend: document.PublicationBackendSnapshot{EpochID: "epoch", Number: 1, CutoverWatermark: "backend-watermark", IndexProfile: contracts.ProfileReference{ID: profile.ID, Version: profile.Version}, ProfileDigest: profile.Digest, RequiredChannels: []contracts.Channel{contracts.KeywordChannel}}}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	chunks, err := (chunk.SimpleSplitter{}).Split(ctx, contracts.SplitRequest{Request: contracts.RequestContext{ProfileDigest: profile.Digest}, RevisionID: work.Revision.ID, Blocks: work.Revision.Blocks, Policy: profile.Policy.Chunking})
	if err != nil {
		t.Fatal(err)
	}
	return work, profile, chunks
}

func TestPreparationChecksCoverageAndStableAttemptNamespace(t *testing.T) {
	w, p, chunks := fixture(t)
	a, s, err := preparation.Build(w, p, chunks)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Records) != len(chunks) || s.Records[0].Published || a.GenerationSet.Channels[contracts.KeywordChannel].Readiness.ContentDigest != w.Revision.ContentHash {
		t.Fatal("preparation lost content or publication separation")
	}
	if _, _, err := preparation.Build(w, p, chunks[1:]); err == nil {
		t.Fatal("omitted source accepted")
	}
	w.Lease.AttemptID = "next-attempt"
	w.Lease.FencingToken++
	w.Job.FencingToken++
	next, _, err := preparation.Build(w, p, chunks)
	if err != nil || next.GenerationSet.ID == a.GenerationSet.ID {
		t.Fatal("new attempt reused generation namespace", err)
	}
}
func TestPreparationRejectsChangedTextAndProfile(t *testing.T) {
	w, p, chunks := fixture(t)
	chunks[0].Text = "forged"
	if _, _, err := preparation.Build(w, p, chunks); err == nil {
		t.Fatal("forged text accepted")
	}
	w, p, chunks = fixture(t)
	p.Policy.Chunking.MaxCodepoints++
	if _, _, err := preparation.Build(w, p, chunks); err == nil {
		t.Fatal("profile digest did not cover chunking")
	}
}
func TestOpaqueBackendMappingCanChangeWithoutChangingPlatformFacts(t *testing.T) {
	w, p, chunks := fixture(t)
	expected, _, err := preparation.Build(w, p, chunks)
	if err != nil {
		t.Fatal(err)
	}
	actual, _, err := preparation.Build(w, p, chunks)
	if err != nil {
		t.Fatal(err)
	}
	g := actual.GenerationSet.Channels[contracts.KeywordChannel]
	g.BackendBinding.BackendID = "other-adapter-private-reference"
	actual.GenerationSet.Channels[contracts.KeywordChannel] = g
	actual.Digest, _ = preparation.ArtifactDigest(actual)
	if err := preparation.MatchArtifact(actual, expected); err != nil {
		t.Fatal("vendor mapping leaked into platform compatibility", err)
	}
	actual.GenerationSet.Channels[contracts.KeywordChannel].Readiness.Generations[0].ChunkCount++
	actual.Digest, _ = preparation.ArtifactDigest(actual)
	if preparation.MatchArtifact(actual, expected) == nil {
		t.Fatal("different source count accepted")
	}
}
