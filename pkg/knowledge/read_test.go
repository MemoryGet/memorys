package knowledge_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type locateRead func(context.Context, contracts.Principal, contracts.ID) (contracts.ReadableDocument, error)

func (f locateRead) LocateReadableDocument(c context.Context, p contracts.Principal, id contracts.ID) (contracts.ReadableDocument, error) {
	return f(c, p, id)
}

type frozenRead func(context.Context, contracts.FrozenContentReadRequest) (contracts.FrozenContentRecord, error)

type verifyRead func(context.Context, contracts.FrozenContentReadRequest, contracts.FrozenRevisionFact, contracts.FrozenContentRecord) error

func (f verifyRead) VerifyReadContent(c context.Context, q contracts.FrozenContentReadRequest, expected contracts.FrozenRevisionFact, r contracts.FrozenContentRecord) error {
	return f(c, q, expected, r)
}

func (f frozenRead) ReadFrozenContent(c context.Context, q contracts.FrozenContentReadRequest) (contracts.FrozenContentRecord, error) {
	return f(c, q)
}

type readFixture struct {
	fact  contracts.PublishedFact
	state contracts.CurrentDocumentState
	store contracts.ID
}

func (r *readFixture) WithinRead(c context.Context, _ contracts.AccessScope, cb func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
	store := r.store
	if store == "" {
		store = "store"
	}
	return cb(c, contracts.QueryReadIdentity{StoreEpochID: store, ReadVersion: "fresh", Watermark: "current"}, r)
}
func (r *readFixture) ReadPublishedPage(_ context.Context, q contracts.PublishedPageRequest) (contracts.PublishedPage, error) {
	return contracts.PublishedPage{}, errors.New("read must never scan the knowledge base")
}
func (r *readFixture) LoadCurrentDocument(context.Context, contracts.ID) (contracts.CurrentDocumentState, error) {
	return r.state, nil
}
func (r *readFixture) LoadActiveEpoch(context.Context, contracts.ID) (contracts.BackendEpoch, error) {
	return contracts.BackendEpoch{}, errors.New("unused")
}

func TestReadCurrentVerifiesContentAndRechecksControl(t *testing.T) {
	for _, mode := range []string{"ok", "corrupt", "wrong-record", "revoke", "withdraw", "scope-mutation", "historical", "verify-failure", "verify-withdraw", "verify-mutation", "missing-verifier"} {
		t.Run(mode, func(t *testing.T) {
			frozen, err := document.BuildFrozenContent(contracts.Draft{ID: "draft", DocumentID: "doc", State: "draft", RowVersion: 1, Blocks: []contracts.DraftBlock{{ID: "block", DraftID: "draft", RowVersion: 1, Order: 0, Content: contracts.Content{Kind: contracts.TextBlock, Text: "中文正文", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "coarse"}}}}}})
			if err != nil {
				t.Fatal(err)
			}
			profile := contracts.Digest(strings.Repeat("a", 64))
			rev, setID := contracts.ID("rev"), contracts.ID("set")
			set := contracts.GenerationSet{ID: setID, RevisionID: rev, BackendEpochID: "backend", State: "ready", Channels: map[contracts.Channel]contracts.IndexGeneration{contracts.KeywordChannel: {ID: "gen", Channel: contracts.KeywordChannel, RevisionID: rev, KnowledgeBaseID: "kb", BackendEpochID: "backend", State: "ready", ProfileDigest: profile}}}
			r := &readFixture{fact: contracts.PublishedFact{DocumentID: "doc", RevisionID: rev, SpaceID: "space", KnowledgeBaseID: "kb", RowVersion: 1, PublicationEpoch: 1, ApprovalID: "approval", ContentDigest: frozen.ContentHash, GenerationSet: set}, state: contracts.CurrentDocumentState{Document: contracts.Document{ID: "doc", SpaceID: "space", KnowledgeBaseID: "kb", RowVersion: 1, PublicationEpoch: 1, ActiveRevisionID: &rev, ActiveGenerationSetID: &setID}, ActiveBackendEpochID: "backend", GenerationSet: &set, PublicationReview: &contracts.CurrentPublicationReview{Revision: contracts.FrozenRevisionFact{ID: rev, DocumentID: "doc", ContentHash: frozen.ContentHash, BlocksHash: frozen.BlocksHash, HashFormat: frozen.HashFormat}, Approval: contracts.Approval{ID: "approval", RevisionID: rev, ContentHash: frozen.ContentHash, Decision: "approve", PolicyVersion: 1, RowVersion: 1, ActorID: "reviewer", DecidedAt: time.Now()}, Validity: contracts.ApprovalValidityFact{ApprovalID: "approval", ApprovalRowVersion: 1, ReviewPolicyVersion: 1, State: "effective"}}}}
			who := contracts.Principal{SubjectID: "reader", SubjectType: "user", Issuer: "issuer", ClientID: "client"}
			scope := contracts.AccessScope{ScopeID: "scope", SubjectID: "reader", Action: "knowledge.read", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}
			authCalls, contentCalls := 0, 0
			var credentialBytes []byte
			d := knowledge.CurrentReadDependencies{Auth: auth(func(_ context.Context, c contracts.Credential) (contracts.Principal, error) {
				authCalls++
				credentialBytes = c.Secret
				if mode == "revoke" && authCalls > 1 {
					return contracts.Principal{}, errors.New("secret")
				}
				return who, nil
			}), Locator: locateRead(func(context.Context, contracts.Principal, contracts.ID) (contracts.ReadableDocument, error) {
				return contracts.ReadableDocument{DocumentID: "doc", SpaceID: "space", KnowledgeBaseID: "kb", Scope: scope}, nil
			}), Reads: r, Facts: facts(func(context.Context, contracts.RequestContext, contracts.AccessScope) (queryread.ControlFacts, error) {
				return queryread.ControlFacts{ProfileDigest: profile, ReviewPolicies: map[contracts.ID]uint64{"kb": 1}}, nil
			}), Content: frozenRead(func(_ context.Context, q contracts.FrozenContentReadRequest) (contracts.FrozenContentRecord, error) {
				contentCalls++
				record := contracts.FrozenContentRecord{DocumentID: "doc", RevisionID: rev, Payload: append([]byte(nil), frozen.Payload...)}
				switch mode {
				case "corrupt":
					record.Payload = []byte(`{}`)
				case "wrong-record":
					record.RevisionID = "other"
				case "withdraw":
					r.state.Document.ActiveRevisionID = nil
				case "scope-mutation":
					q.Scope.KnowledgeBaseIDs[0] = "attacker"
				}
				return record, nil
			})}
			verifyCalls := 0
			d.Verifier = verifyRead(func(_ context.Context, q contracts.FrozenContentReadRequest, expected contracts.FrozenRevisionFact, record contracts.FrozenContentRecord) error {
				verifyCalls++
				if q.Scope.Action != "knowledge.read" || q.Scope.KnowledgeBaseIDs[0] != "kb" || expected.ContentHash != frozen.ContentHash || expected.BlocksHash != frozen.BlocksHash || string(record.Payload) != string(frozen.Payload) {
					t.Fatal("source verification lost approved read binding")
				}
				switch mode {
				case "verify-failure":
					return errors.New("private provider error")
				case "verify-withdraw":
					r.state.Document.ActiveRevisionID = nil
				case "verify-mutation":
					q.Scope.KnowledgeBaseIDs[0] = "attacker"
					record.Payload[0] = '!'
				}
				return nil
			})
			if mode == "missing-verifier" {
				d.Verifier = nil
				if _, err := knowledge.NewCurrentRead(d); err != knowledge.ErrRead {
					t.Fatal("missing verifier accepted")
				}
				return
			}
			svc, err := knowledge.NewCurrentRead(d)
			if err != nil {
				t.Fatal(err)
			}
			in := knowledge.CurrentReadInput{Credential: contracts.Credential{Kind: "access_token", Secret: []byte("token")}, DocumentID: "doc", Request: contracts.RequestContext{RequestID: "request", ProfileDigest: profile, Deadline: time.Now().Add(time.Minute)}}
			if mode == "historical" {
				in.RevisionID = "old"
			}
			result, err := svc.ReadCurrent(context.Background(), in)
			good := mode == "ok" || mode == "scope-mutation" || mode == "verify-mutation"
			if good && (err != nil || string(result.Payload) != string(frozen.Payload) || authCalls != 2) {
				t.Fatalf("successful read rejected: %v", err)
			}
			if !good && (err != knowledge.ErrRead || len(result.Payload) != 0) {
				t.Fatalf("failure released content: %v", err)
			}
			if mode == "historical" && contentCalls != 0 {
				t.Fatal("historical request reached content")
			}
			if good && verifyCalls != 1 {
				t.Fatal("output bypassed source verification")
			}
			if string(in.Credential.Secret) != "token" || scope.KnowledgeBaseIDs[0] != "kb" {
				t.Fatal("caller values mutated")
			}
			for _, b := range credentialBytes {
				if b != 0 {
					t.Fatal("authentication secret retained")
				}
			}
		})
	}
}
