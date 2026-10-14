package queryread_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type repository struct {
	pages  []contracts.PublishedPage
	calls  []contracts.PublishedPageRequest
	err    error
	cancel context.CancelFunc
}

func (r *repository) ReadPublishedPage(_ context.Context, q contracts.PublishedPageRequest) (contracts.PublishedPage, error) {
	r.calls = append(r.calls, q)
	if r.cancel != nil {
		r.cancel()
	}
	if r.err != nil {
		return contracts.PublishedPage{}, r.err
	}
	i := len(r.calls) - 1
	if i >= len(r.pages) {
		return contracts.PublishedPage{}, errors.New("unexpected extra read")
	}
	return r.pages[i], nil
}
func (*repository) LoadActiveEpoch(context.Context, contracts.ID) (contracts.BackendEpoch, error) {
	return contracts.BackendEpoch{}, errors.New("unused")
}
func (*repository) LoadCurrentDocument(context.Context, contracts.ID) (contracts.CurrentDocumentState, error) {
	return contracts.CurrentDocumentState{}, errors.New("unused")
}

var identity = contracts.QueryReadIdentity{StoreEpochID: "store", ReadVersion: "read", Watermark: "watermark"}
var limits = queryread.Limits{PageSize: 1, MaxPages: 4, MaxFacts: 3, MaxGenerations: 3}

func scope() contracts.AccessScope {
	return contracts.AccessScope{ScopeID: "scope", SubjectID: "user", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "knowledge.search", PolicyVersion: 1}
}
func page(doc, rev, gen, next string) contracts.PublishedPage {
	return contracts.PublishedPage{Identity: identity, NextCursor: next, Facts: []contracts.PublishedFact{{DocumentID: contracts.ID(doc), SpaceID: "space", KnowledgeBaseID: "kb", RevisionID: contracts.ID(rev), RowVersion: 1, PublicationEpoch: 1, ApprovalID: "approval", ContentDigest: contracts.Digest(strings.Repeat("a", 64)), GenerationSet: contracts.GenerationSet{ID: contracts.ID("set-" + rev), RevisionID: contracts.ID(rev), BackendEpochID: "epoch", State: "ready", Channels: map[contracts.Channel]contracts.IndexGeneration{contracts.KeywordChannel: {ID: contracts.ID(gen), KnowledgeBaseID: "kb", RevisionID: contracts.ID(rev), Channel: contracts.KeywordChannel, ProfileDigest: contracts.Digest(strings.Repeat("b", 64)), BackendEpochID: "epoch", State: "ready"}}}}}}
}
func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return c
}

func TestCollectCompleteInventoryAndFixedRequestIdentity(t *testing.T) {
	r := &repository{pages: []contracts.PublishedPage{page("a", "ra", "ga", "cursor"), page("b", "rb", "gb", "")}}
	out, e := queryread.Collect(ctx(t), r, scope(), identity, []contracts.ID{"kb"}, limits)
	if e != nil || len(out) != 2 || len(r.calls) != 2 {
		t.Fatal(len(out), e)
	}
	if r.calls[1].Cursor != "cursor" || r.calls[0].Identity != r.calls[1].Identity {
		t.Fatal("observation changed")
	}
}

func TestCollectRejectsCrossPageDuplicatesAndChangedView(t *testing.T) {
	for _, change := range []func(*contracts.PublishedPage){func(p *contracts.PublishedPage) { p.Identity.StoreEpochID = "restored" }, func(p *contracts.PublishedPage) {
		p.Facts[0].RevisionID = "ra"
		p.Facts[0].GenerationSet.RevisionID = "ra"
		g := p.Facts[0].GenerationSet.Channels[contracts.KeywordChannel]
		g.RevisionID = "ra"
		p.Facts[0].GenerationSet.Channels[contracts.KeywordChannel] = g
	}, func(p *contracts.PublishedPage) {
		g := p.Facts[0].GenerationSet.Channels[contracts.KeywordChannel]
		g.ID = "ga"
		p.Facts[0].GenerationSet.Channels[contracts.KeywordChannel] = g
	}} {
		last := page("b", "rb", "gb", "")
		change(&last)
		r := &repository{pages: []contracts.PublishedPage{page("a", "ra", "ga", "cursor"), last}}
		out, e := queryread.Collect(ctx(t), r, scope(), identity, []contracts.ID{"kb"}, limits)
		if e != queryread.ErrInvalid || out != nil {
			t.Fatal("invalid partial inventory escaped", e)
		}
	}
}

func TestCollectRejectsLongCursorCycle(t *testing.T) {
	r := &repository{pages: []contracts.PublishedPage{page("a", "ra", "ga", "x"), page("b", "rb", "gb", "y"), page("c", "rc", "gc", "x")}}
	out, e := queryread.Collect(ctx(t), r, scope(), identity, []contracts.ID{"kb"}, limits)
	if e != queryread.ErrInvalid || out != nil {
		t.Fatal("cursor cycle accepted", e)
	}
}

func TestCollectLimitsFailWithoutTruncation(t *testing.T) {
	for _, l := range []queryread.Limits{{1, 1, 3, 3}, {1, 4, 1, 3}, {1, 4, 3, 1}} {
		r := &repository{pages: []contracts.PublishedPage{page("a", "ra", "ga", "x"), page("b", "rb", "gb", "")}}
		out, e := queryread.Collect(ctx(t), r, scope(), identity, []contracts.ID{"kb"}, l)
		if e != queryread.ErrLimit || out != nil {
			t.Fatal("truncated inventory returned", e)
		}
		if l.MaxFacts == 1 && len(r.calls) != 1 {
			t.Fatal("read after facts budget exhausted")
		}
	}
}

func TestCollectCancellationReadFailureAndAdmission(t *testing.T) {
	c, cancel := context.WithCancel(ctx(t))
	r := &repository{pages: []contracts.PublishedPage{page("a", "ra", "ga", "")}, cancel: cancel}
	if out, e := queryread.Collect(c, r, scope(), identity, []contracts.ID{"kb"}, limits); !errors.Is(e, context.Canceled) || out != nil {
		t.Fatal(e)
	}
	r = &repository{err: errors.New("private database detail")}
	if _, e := queryread.Collect(ctx(t), r, scope(), identity, []contracts.ID{"kb"}, limits); e != queryread.ErrRead {
		t.Fatal("backend error exposed", e)
	}
	r = &repository{}
	if _, e := queryread.Collect(ctx(t), r, scope(), identity, []contracts.ID{"other"}, limits); e != queryread.ErrInvalid || len(r.calls) != 0 {
		t.Fatal("unauthorized selection read", e)
	}
}
