package knowledge_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type pagedFixture struct {
	service                *knowledge.CurrentReadService
	input                  knowledge.CurrentReadInput
	repo                   *readFixture
	who                    contracts.Principal
	scope                  contracts.AccessScope
	authCalls, sourceCalls int
	deny                   bool
	onSeal                 func()
	issued                 []contracts.ReadCursorState
	dependencies           knowledge.CurrentReadDependencies
}

type observedReadCursor struct {
	inner contracts.ReadCursorCodec
	hook  func(contracts.ReadCursorState)
}

func (c observedReadCursor) SealReadCursor(ctx context.Context, state contracts.ReadCursorState) (string, error) {
	c.hook(state)
	return c.inner.SealReadCursor(ctx, state)
}
func (c observedReadCursor) OpenReadCursor(ctx context.Context, token string) (contracts.ReadCursorState, error) {
	return c.inner.OpenReadCursor(ctx, token)
}

func newPagedFixture(t *testing.T) *pagedFixture {
	t.Helper()
	p := &pagedFixture{}
	draft := contracts.Draft{ID: "draft", DocumentID: "doc", State: "draft", RowVersion: 1}
	for i, id := range []contracts.ID{"one", "two", "three"} {
		draft.Blocks = append(draft.Blocks, contracts.DraftBlock{ID: id, DraftID: "draft", Order: i, RowVersion: 1, Content: contracts.Content{Kind: contracts.TextBlock, Text: "中文正文", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: id, Locator: contracts.Locator{Precision: "coarse"}}}})
	}
	frozen, err := document.BuildFrozenContent(draft)
	if err != nil {
		t.Fatal(err)
	}
	profile := contracts.Digest(strings.Repeat("a", 64))
	rev, setID := contracts.ID("rev"), contracts.ID("set")
	set := contracts.GenerationSet{ID: setID, RevisionID: rev, BackendEpochID: "backend", State: "ready", Channels: map[contracts.Channel]contracts.IndexGeneration{contracts.KeywordChannel: {ID: "gen", Channel: contracts.KeywordChannel, RevisionID: rev, KnowledgeBaseID: "kb", BackendEpochID: "backend", State: "ready", ProfileDigest: profile}}}
	p.repo = &readFixture{state: contracts.CurrentDocumentState{Document: contracts.Document{ID: "doc", SpaceID: "space", KnowledgeBaseID: "kb", RowVersion: 1, PublicationEpoch: 1, ActiveRevisionID: &rev, ActiveGenerationSetID: &setID}, ActiveBackendEpochID: "backend", GenerationSet: &set, PublicationReview: &contracts.CurrentPublicationReview{Revision: contracts.FrozenRevisionFact{ID: rev, DocumentID: "doc", ContentHash: frozen.ContentHash, BlocksHash: frozen.BlocksHash, HashFormat: frozen.HashFormat}, Approval: contracts.Approval{ID: "approval", RevisionID: rev, ContentHash: frozen.ContentHash, Decision: "approve", PolicyVersion: 1, RowVersion: 1, ActorID: "reviewer", DecidedAt: time.Now()}, Validity: contracts.ApprovalValidityFact{ApprovalID: "approval", ApprovalRowVersion: 1, ReviewPolicyVersion: 1, State: "effective"}}}}
	p.who = contracts.Principal{SubjectID: "reader", SubjectType: "user", Issuer: "issuer", ClientID: "client"}
	p.scope = contracts.AccessScope{ScopeID: "ephemeral", SubjectID: "reader", Action: "knowledge.read", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}
	codec, _ := queryread.NewHMACReadCursor("test", []byte(strings.Repeat("k", 32)))
	p.dependencies = knowledge.CurrentReadDependencies{
		Auth: auth(func(context.Context, contracts.Credential) (contracts.Principal, error) {
			p.authCalls++
			if p.deny {
				return contracts.Principal{}, errors.New("revoked")
			}
			return p.who, nil
		}),
		Locator: locateRead(func(context.Context, contracts.Principal, contracts.ID) (contracts.ReadableDocument, error) {
			return contracts.ReadableDocument{DocumentID: "doc", SpaceID: "space", KnowledgeBaseID: "kb", Scope: p.scope}, nil
		}),
		Reads: p.repo,
		Facts: facts(func(context.Context, contracts.RequestContext, contracts.AccessScope) (queryread.ControlFacts, error) {
			return queryread.ControlFacts{ProfileDigest: profile, ReviewPolicies: map[contracts.ID]uint64{"kb": 1}}, nil
		}),
		Content: frozenRead(func(context.Context, contracts.FrozenContentReadRequest) (contracts.FrozenContentRecord, error) {
			return contracts.FrozenContentRecord{DocumentID: "doc", RevisionID: "rev", Payload: frozen.Payload}, nil
		}),
		Verifier: verifyRead(func(context.Context, contracts.FrozenContentReadRequest, contracts.FrozenRevisionFact, contracts.FrozenContentRecord) error {
			p.sourceCalls++
			return nil
		}),
		Pagination: &knowledge.ReadPagination{Codec: observedReadCursor{inner: codec, hook: func(state contracts.ReadCursorState) {
			p.issued = append(p.issued, state)
			if p.onSeal != nil {
				p.onSeal()
			}
		}}, PageSize: 1, TTL: time.Minute},
	}
	p.service, err = knowledge.NewCurrentRead(p.dependencies)
	if err != nil {
		t.Fatal(err)
	}
	p.input = knowledge.CurrentReadInput{Credential: contracts.Credential{Kind: "access_token", Secret: []byte("token")}, DocumentID: "doc", Request: contracts.RequestContext{RequestID: "first", ProfileDigest: profile, Deadline: time.Now().Add(time.Minute)}}
	return p
}

func TestReadPagesAreCompleteAndReauthenticateWithoutScanning(t *testing.T) {
	p := newPagedFixture(t)
	var ids []contracts.ID
	options := knowledge.ReadPageOptions{}
	for i := 0; i < 3; i++ {
		page, err := p.service.ReadPage(context.Background(), p.input, options)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, page.BlockIDs...)
		if (i < 2) != (page.NextCursor != "") {
			t.Fatal("cursor completion differs")
		}
		options.Cursor = page.NextCursor
		p.scope.ScopeID = "refreshed-scope"
	}
	if !reflect.DeepEqual(ids, []contracts.ID{"one", "two", "three"}) || p.authCalls != 6 || p.sourceCalls != 3 {
		t.Fatal("page completeness/current verification", ids, p.authCalls, p.sourceCalls)
	}
	if len(p.issued) != 2 || p.issued[1].ExpiresAt.After(p.issued[0].ExpiresAt) {
		t.Fatal("continuation extended initial expiry")
	}
}

func TestReadPageSigningPrecedesFinalAuthorityCheck(t *testing.T) {
	for _, mode := range []string{"identity", "withdraw"} {
		t.Run(mode, func(t *testing.T) {
			p := newPagedFixture(t)
			p.onSeal = func() {
				if mode == "identity" {
					p.deny = true
				} else {
					p.repo.state.Document.ActiveRevisionID = nil
				}
			}
			out, err := p.service.ReadPage(context.Background(), p.input, knowledge.ReadPageOptions{})
			if err == nil || len(out.Record.Payload) != 0 || out.NextCursor != "" || p.authCalls != 2 || len(p.issued) != 1 {
				t.Fatal("signing-time revocation released output", err)
			}
		})
	}
}

func TestReadCursorRejectsChangedAuthorityPublicationAndSelection(t *testing.T) {
	for _, mode := range []string{"subject", "issuer", "client", "policy", "publication", "approval", "generation", "store", "withdraw", "revoke", "selection", "explicit-empty", "tamper"} {
		t.Run(mode, func(t *testing.T) {
			p := newPagedFixture(t)
			page, err := p.service.ReadPage(context.Background(), p.input, knowledge.ReadPageOptions{})
			if err != nil || page.NextCursor == "" {
				t.Fatal(err)
			}
			options := knowledge.ReadPageOptions{Cursor: page.NextCursor}
			switch mode {
			case "subject":
				p.who.SubjectID = "other"
				p.scope.SubjectID = "other"
			case "issuer":
				p.who.Issuer = "other"
			case "client":
				p.who.ClientID = "other"
			case "policy":
				p.scope.PolicyVersion++
			case "publication":
				p.repo.state.Document.PublicationEpoch++
			case "approval":
				p.repo.state.PublicationReview.Approval.ID = "new-approval"
				p.repo.state.PublicationReview.Validity.ApprovalID = "new-approval"
			case "generation":
				g := p.repo.state.GenerationSet.Channels[contracts.KeywordChannel]
				g.ID = "new-gen"
				p.repo.state.GenerationSet.Channels[contracts.KeywordChannel] = g
			case "withdraw":
				p.repo.state.Document.ActiveRevisionID = nil
			case "store":
				p.repo.store = "replacement-store"
			case "revoke":
				p.deny = true
			case "selection":
				options.BlockIDs = []contracts.ID{"one", "two"}
			case "explicit-empty":
				options.BlockIDs = []contracts.ID{}
			case "tamper":
				options.Cursor += "!"
			}
			result, err := p.service.ReadPage(context.Background(), p.input, options)
			if err == nil || len(result.Record.Payload) != 0 || result.NextCursor != "" {
				t.Fatal("old cursor released content")
			}
		})
	}
}

func TestReadPageSelectionUsesFrozenOrderAndPreservesEmpty(t *testing.T) {
	p := newPagedFixture(t)
	options := knowledge.ReadPageOptions{BlockIDs: []contracts.ID{"three", "one"}}
	first, err := p.service.ReadPage(context.Background(), p.input, options)
	if err != nil || !reflect.DeepEqual(first.BlockIDs, []contracts.ID{"one"}) {
		t.Fatal(first, err)
	}
	options.BlockIDs = []contracts.ID{"one", "three"}
	options.Cursor = first.NextCursor
	last, err := p.service.ReadPage(context.Background(), p.input, options)
	if err != nil || !reflect.DeepEqual(last.BlockIDs, []contracts.ID{"three"}) || last.NextCursor != "" {
		t.Fatal(last, err)
	}
	empty, err := p.service.ReadPage(context.Background(), p.input, knowledge.ReadPageOptions{BlockIDs: []contracts.ID{}})
	if err != nil || empty.BlockIDs == nil || len(empty.BlockIDs) != 0 || empty.NextCursor != "" {
		t.Fatal("empty widened", err)
	}
	for _, ids := range [][]contracts.ID{{"one", "one"}, {"absent"}} {
		if _, err := p.service.ReadPage(context.Background(), p.input, knowledge.ReadPageOptions{BlockIDs: ids}); err == nil {
			t.Fatal("invalid selection admitted")
		}
	}
}
