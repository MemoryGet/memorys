package localruntime

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

type testAuthenticator struct {
	who contracts.Principal
	err error
}

func (a *testAuthenticator) Authenticate(_ context.Context, c contracts.Credential) (contracts.Principal, error) {
	clear(c.Secret) // A provider owns its temporary copy, not the request bytes.
	return a.who, a.err
}

type testAuthorizer struct{ scope contracts.AccessScope }

func (a testAuthorizer) Authorize(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error) {
	return a.scope, nil
}

func testProfile(t *testing.T) configprofile.Snapshot {
	t.Helper()
	r := contracts.ModuleBinding{InstanceID: "keyword-reader", Capability: contracts.Retrieval, ContractVersion: "1.0"}
	w := contracts.ModuleBinding{InstanceID: "keyword-writer", Capability: contracts.IndexWriting, ContractVersion: "1.0"}
	p, err := configprofile.Seal(configprofile.Snapshot{Format: "akos.profile-snapshot/1", Pipeline: contracts.PipelineProfile{ID: "pipeline", Version: 1, IndexProfile: contracts.ProfileReference{ID: "index", Version: 1}, QueryProfile: contracts.ProfileReference{ID: "query", Version: 1}}, Index: contracts.IndexProfile{ID: "index", Version: 1, Mode: "independent", Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.IndexWriting: w}, Routes: []contracts.IndexRoute{{ID: "keyword", Channels: []contracts.Channel{contracts.KeywordChannel}, Binding: w}}, Policy: contracts.IndexPolicy{SchemaVersion: 1, Chunking: contracts.ChunkPolicy{Strategy: "characters", MaxCodepoints: 500}, RequiredChannels: []contracts.Channel{contracts.KeywordChannel}, ProjectionFormatVersion: "akos.chunks/1"}}, Query: contracts.QueryProfile{ID: "query", Version: 1, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.Retrieval: r}, Routes: []contracts.RetrievalRoute{{ID: "keyword", Binding: r, Channels: []contracts.Channel{contracts.KeywordChannel}, CandidateLimit: 10, Required: true}}, Policy: contracts.QueryPolicy{SchemaVersion: 1, CandidateLimit: 10, AnswerEvidenceLimit: 5, FusionMethod: "rrf", RankConstant: 60, RetrievalTimeoutMS: 1000}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNativeHostRejectsUnsupportedRoutes(t *testing.T) {
	for _, modify := range []func(*configprofile.Snapshot){
		func(p *configprofile.Snapshot) { p.Query.Routes[0].Channels[0] = contracts.VectorChannel },
		func(p *configprofile.Snapshot) {
			p.Query.Routes[0].Binding.RequiredFeatures = []string{"unsupported.feature"}
		},
		func(p *configprofile.Snapshot) {
			p.Query.Bindings[contracts.Retrieval] = contracts.ModuleBinding{Capability: contracts.Retrieval, InstanceID: "different-reader", ContractVersion: "1.0"}
		},
	} {
		p := testProfile(t)
		modify(&p)
		p, err := configprofile.Seal(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewSearch(&sql.DB{}, &testAuthenticator{}, p); !errors.Is(err, ErrConfiguration) {
			t.Fatal("unsupported native configuration admitted", err)
		}
	}
}

func TestSQLBoundaryReauthenticatesAndRejectsPolicyOrActorChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	who := contracts.Principal{SubjectID: "reader", SubjectType: "user", Issuer: "issuer", ClientID: "client"}
	before := contracts.AccessScope{ScopeID: "scope", SubjectID: "reader", SpaceID: "space", Action: "knowledge.search", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}
	auth := &testAuthenticator{who: who}
	secret := contracts.Credential{Kind: "access_token", Secret: []byte("private-token")}
	verify := scopeVerifier(auth, testAuthorizer{before}, secret, who)
	if err := verify(ctx, before); err != nil || string(secret.Secret) != "private-token" {
		t.Fatal("valid refresh or credential ownership failed", err)
	}
	auth.err = errors.New("revoked token")
	if verify(ctx, before) == nil {
		t.Fatal("revocation accepted")
	}
	auth.err = nil
	auth.who.Issuer = "other-issuer"
	if verify(ctx, before) == nil {
		t.Fatal("issuer switch accepted")
	}
	auth.who = who
	fresh := before
	fresh.PolicyVersion++
	if scopeVerifier(auth, testAuthorizer{fresh}, secret, who)(ctx, before) == nil {
		t.Fatal("policy switch accepted")
	}
}

func TestInvalidCredentialDeniedBeforeAnySQL(t *testing.T) {
	auth := &testAuthenticator{err: errors.New("private-provider-detail")}
	s, err := NewSearch(&sql.DB{}, auth, testProfile(t))
	if err != nil {
		t.Fatal(err)
	}
	in := knowledge.SearchInput{Credential: contracts.Credential{Kind: "access_token", Secret: []byte("private-token")}, Deadline: time.Now().Add(time.Second), Text: "keyword"}
	if _, err := s.Search(context.Background(), in); !errors.Is(err, knowledge.ErrUnauthenticated) {
		t.Fatal("invalid token did not produce safe authentication error", err)
	}
	if string(in.Credential.Secret) != "private-token" {
		t.Fatal("caller credential was modified")
	}
}

func TestPoolReservationAndQueuedCancellation(t *testing.T) {
	db := &sql.DB{}
	db.SetMaxOpenConns(1)
	if _, err := NewSearch(db, &testAuthenticator{}, testProfile(t)); err == nil {
		t.Fatal("one-connection pool cannot service nested current checks")
	}
	db.SetMaxOpenConns(2)
	s, err := NewSearch(db, &testAuthenticator{}, testProfile(t))
	if err != nil {
		t.Fatal(err)
	}
	s.slots <- struct{}{}
	defer func() { <-s.slots }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	in := knowledge.SearchInput{Credential: contracts.Credential{Kind: "access_token", Secret: []byte("token")}, Deadline: time.Now().Add(time.Second), Text: "word"}
	if results, err := s.Search(ctx, in); err == nil || len(results) != 0 {
		t.Fatal("cancelled queued request continued")
	}
}
