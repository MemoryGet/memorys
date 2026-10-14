package contracts_test

import (
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func pageFixture() (contracts.PublishedPage, contracts.PublishedPageRequest, contracts.AccessScope) {
	id := contracts.QueryReadIdentity{StoreEpochID: "store", ReadVersion: "view", Watermark: "watermark"}
	r := contracts.PublishedPageRequest{Identity: id, KnowledgeBaseIDs: []contracts.ID{"kb"}, Limit: 2}
	s := contracts.AccessScope{ScopeID: "scope", SubjectID: "user", Action: "knowledge.search", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1}
	f := contracts.PublishedFact{DocumentID: "doc-b", SpaceID: "space", KnowledgeBaseID: "kb", RevisionID: "rev", RowVersion: 1, PublicationEpoch: 1, ApprovalID: "approval", ContentDigest: contracts.Digest(strings.Repeat("a", 64)), GenerationSet: contracts.GenerationSet{ID: "set", RevisionID: "rev", BackendEpochID: "epoch", State: "ready", Channels: map[contracts.Channel]contracts.IndexGeneration{contracts.KeywordChannel: {ID: "gen", KnowledgeBaseID: "kb", RevisionID: "rev", Channel: contracts.KeywordChannel, ProfileDigest: contracts.Digest(strings.Repeat("b", 64)), BackendEpochID: "epoch", State: "ready"}}}}
	return contracts.PublishedPage{Identity: id, Facts: []contracts.PublishedFact{f}, NextCursor: "next"}, r, s
}

func TestPublishedPageRelationshipsAndEmptyEnd(t *testing.T) {
	p, r, s := pageFixture()
	if err := contracts.ValidatePublishedPage(p, r, s, "doc-a"); err != nil {
		t.Fatal(err)
	}
	p.Facts = nil
	p.NextCursor = ""
	if err := contracts.ValidatePublishedPage(p, r, s, "doc-a"); err != nil {
		t.Fatal("empty terminal page rejected", err)
	}
}

func TestPublishedPageRejectsObservationScopeAndOrder(t *testing.T) {
	for _, change := range []func(*contracts.PublishedPage, *contracts.PublishedPageRequest){
		func(p *contracts.PublishedPage, _ *contracts.PublishedPageRequest) { p.Identity.ReadVersion = "other" },
		func(p *contracts.PublishedPage, _ *contracts.PublishedPageRequest) { p.Facts[0].SpaceID = "other" },
		func(p *contracts.PublishedPage, _ *contracts.PublishedPageRequest) {
			p.Facts[0].KnowledgeBaseID = "other"
		},
		func(p *contracts.PublishedPage, _ *contracts.PublishedPageRequest) {
			p.Facts = append(p.Facts, p.Facts[0])
		},
		func(_ *contracts.PublishedPage, r *contracts.PublishedPageRequest) {
			r.KnowledgeBaseIDs = append(r.KnowledgeBaseIDs, "other")
		},
		func(_ *contracts.PublishedPage, r *contracts.PublishedPageRequest) { r.Limit = 0 },
	} {
		p, r, s := pageFixture()
		change(&p, &r)
		if err := contracts.ValidatePublishedPage(p, r, s, "doc-a"); err == nil {
			t.Fatal("invalid page accepted")
		}
	}
	p, r, s := pageFixture()
	if err := contracts.ValidatePublishedPage(p, r, s, "doc-c"); err == nil {
		t.Fatal("cross-page duplicate/order accepted")
	}
}

func TestPublishedPageRejectsGenerationAndPublicationMismatch(t *testing.T) {
	for _, change := range []func(*contracts.PublishedFact){
		func(f *contracts.PublishedFact) { f.ApprovalID = "" },
		func(f *contracts.PublishedFact) { f.PublicationEpoch = 0 },
		func(f *contracts.PublishedFact) { f.GenerationSet.RevisionID = "old" },
		func(f *contracts.PublishedFact) {
			g := f.GenerationSet.Channels[contracts.KeywordChannel]
			g.KnowledgeBaseID = "other"
			f.GenerationSet.Channels[contracts.KeywordChannel] = g
		},
		func(f *contracts.PublishedFact) {
			g := f.GenerationSet.Channels[contracts.KeywordChannel]
			g.BackendEpochID = "old"
			f.GenerationSet.Channels[contracts.KeywordChannel] = g
		},
	} {
		p, r, s := pageFixture()
		change(&p.Facts[0])
		if err := contracts.ValidatePublishedPage(p, r, s, ""); err == nil {
			t.Fatal("bad publication fact accepted")
		}
	}
}

func TestPublishedPageRejectsCursorLoopAndOversizedPage(t *testing.T) {
	p, r, s := pageFixture()
	r.Cursor = p.NextCursor
	if err := contracts.ValidatePublishedPage(p, r, s, ""); err == nil {
		t.Fatal("cursor loop accepted")
	}
	p.Facts = nil
	r.Cursor = ""
	if err := contracts.ValidatePublishedPage(p, r, s, ""); err == nil {
		t.Fatal("empty nonterminal page accepted")
	}
	p, r, s = pageFixture()
	r.Limit = 1
	p.Facts = append(p.Facts, p.Facts[0])
	if err := contracts.ValidatePublishedPage(p, r, s, ""); err == nil {
		t.Fatal("page limit ignored")
	}
}
