package knowledge_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type auth func(context.Context, contracts.Credential) (contracts.Principal, error)

func (f auth) Authenticate(c context.Context, q contracts.Credential) (contracts.Principal, error) {
	return f(c, q)
}

type authorize func(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error)

func (f authorize) Authorize(c context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
	return f(c, q)
}

type plans func(context.Context, contracts.Principal, contracts.RetrieveRequest) (knowledge.SearchPlan, error)

func (f plans) Prepare(c context.Context, p contracts.Principal, q contracts.RetrieveRequest) (knowledge.SearchPlan, error) {
	return f(c, p, q)
}

type facts func(context.Context, contracts.RequestContext, contracts.AccessScope) (queryread.ControlFacts, error)

func (f facts) LoadCurrentFacts(c context.Context, r contracts.RequestContext, s contracts.AccessScope) (queryread.ControlFacts, error) {
	return f(c, r, s)
}

type retrieve func(context.Context, contracts.RetrieveRequest) ([]contracts.Candidate, error)

func (f retrieve) Search(c context.Context, q contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	return f(c, q)
}

type unusedReads struct{}

func (unusedReads) WithinRead(context.Context, contracts.AccessScope, func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
	return errors.New("unexpected read")
}

type unusedBackend struct{}

func (unusedBackend) Verify(context.Context, contracts.RequestContext, contracts.AccessScope, queryread.CurrentObservation, []contracts.Evidence) error {
	return errors.New("unexpected backend")
}
func fixture(t *testing.T) (configprofile.Snapshot, knowledge.Dependencies, knowledge.SearchInput) {
	t.Helper()
	binding := contracts.ModuleBinding{InstanceID: "reader", Capability: contracts.Retrieval, ContractVersion: "1.0"}
	profile, err := configprofile.Seal(configprofile.Snapshot{Format: "akos.profile-snapshot/1", Pipeline: contracts.PipelineProfile{ID: "pipeline", Version: 1, IndexProfile: contracts.ProfileReference{ID: "index", Version: 1}, QueryProfile: contracts.ProfileReference{ID: "query", Version: 1}}, Index: contracts.IndexProfile{ID: "index", Version: 1, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.IndexWriting: {InstanceID: "writer", Capability: contracts.IndexWriting, ContractVersion: "1.0"}}}, Query: contracts.QueryProfile{ID: "query", Version: 1, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.Retrieval: binding}, Routes: []contracts.RetrievalRoute{{ID: "route", Binding: binding, Channels: []contracts.Channel{contracts.KeywordChannel}, CandidateLimit: 10}}, Policy: contracts.QueryPolicy{SchemaVersion: 1, CandidateLimit: 10, AnswerEvidenceLimit: 5, FusionMethod: "rrf", RankConstant: 60, RetrievalTimeoutMS: 1000}}})
	if err != nil {
		t.Fatal(err)
	}
	who := contracts.Principal{SubjectID: "reader", SubjectType: "user", Issuer: "issuer", ClientID: "client"}
	scope := contracts.AccessScope{ScopeID: "scope", SubjectID: "reader", Action: "knowledge.search", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}
	d := knowledge.Dependencies{Auth: auth(func(context.Context, contracts.Credential) (contracts.Principal, error) { return who, nil }), Authorizer: authorize(func(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error) { return scope, nil }), Plans: plans(func(_ context.Context, _ contracts.Principal, q contracts.RetrieveRequest) (knowledge.SearchPlan, error) {
		return knowledge.SearchPlan{Request: q, Expectation: queryread.CurrentExpectation{StoreEpochID: "store", ScopeVersion: 1}, Retriever: retrieve(func(context.Context, contracts.RetrieveRequest) ([]contracts.Candidate, error) { return nil, nil })}, nil
	}), Facts: facts(func(context.Context, contracts.RequestContext, contracts.AccessScope) (queryread.ControlFacts, error) {
		return queryread.ControlFacts{ProfileDigest: profile.Pipeline.Digest, ReviewPolicies: map[contracts.ID]uint64{"kb": 1}}, nil
	}), Reads: unusedReads{}, Backend: unusedBackend{}}
	return profile, d, knowledge.SearchInput{Credential: contracts.Credential{Kind: "access_token", Secret: []byte("private")}, RequestID: "request", Deadline: time.Now().Add(time.Minute), SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Text: "中文", Limit: 5}
}
func TestSearchReauthenticatesAndOwnsCredentials(t *testing.T) {
	p, d, in := fixture(t)
	a, z, f := 0, 0, 0
	oldAuth, oldAuthorize, oldFacts := d.Auth, d.Authorizer, d.Facts
	var scratch []byte
	d.Auth = auth(func(c context.Context, q contracts.Credential) (contracts.Principal, error) {
		a++
		scratch = q.Secret
		return oldAuth.Authenticate(c, q)
	})
	d.Authorizer = authorize(func(c context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
		z++
		return oldAuthorize.Authorize(c, q)
	})
	d.Facts = facts(func(c context.Context, r contracts.RequestContext, s contracts.AccessScope) (queryread.ControlFacts, error) {
		f++
		return oldFacts.LoadCurrentFacts(c, r, s)
	})
	service, err := knowledge.NewSearch(p, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Search(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if a != 3 || z != 3 || f != 2 || string(in.Credential.Secret) != "private" {
		t.Fatal("required refresh or credential ownership missing", a, z, f)
	}
	for _, b := range scratch {
		if b != 0 {
			t.Fatal("temporary credential not cleared")
		}
	}
}
func TestSearchRejectsPlannerEscalationAndUnverifiedOutput(t *testing.T) {
	for _, kind := range []string{"scope", "query", "deadline", "unbound-evidence", "final-revocation"} {
		t.Run(kind, func(t *testing.T) {
			p, d, in := fixture(t)
			base := d.Plans
			d.Plans = plans(func(c context.Context, who contracts.Principal, q contracts.RetrieveRequest) (knowledge.SearchPlan, error) {
				plan, err := base.Prepare(c, who, q)
				switch kind {
				case "scope":
					plan.Request.Scope.KnowledgeBaseIDs = []contracts.ID{"other"}
				case "query":
					plan.Request.Query.Text = "changed"
				case "deadline":
					plan.Request.Request.Deadline = time.Now().Add(time.Hour)
				case "unbound-evidence":
					plan.Retriever = retrieve(func(context.Context, contracts.RetrieveRequest) ([]contracts.Candidate, error) {
						return []contracts.Candidate{{Text: "protected", Rank: 1}}, nil
					})
				}
				return plan, err
			})
			if kind == "final-revocation" {
				calls := 0
				original := d.Auth
				d.Auth = auth(func(c context.Context, q contracts.Credential) (contracts.Principal, error) {
					calls++
					if calls == 3 {
						return contracts.Principal{}, errors.New("private revocation detail")
					}
					return original.Authenticate(c, q)
				})
			}
			service, err := knowledge.NewSearch(p, d)
			if err != nil {
				t.Fatal(err)
			}
			if results, err := service.Search(context.Background(), in); err == nil || results != nil || err.Error() != "knowledge search unavailable" {
				t.Fatal("unverified output escaped", results, err)
			}
		})
	}
}

func TestSearchRejectsMalformedIdentityBeforeAuthorization(t *testing.T) {
	p, d, in := fixture(t)
	d.Auth = auth(func(context.Context, contracts.Credential) (contracts.Principal, error) {
		return contracts.Principal{SubjectID: contracts.ID(string([]byte{0xff})), SubjectType: "user", Issuer: "issuer", ClientID: "client"}, nil
	})
	called := false
	d.Authorizer = authorize(func(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error) {
		called = true
		return contracts.AccessScope{}, nil
	})
	service, err := knowledge.NewSearch(p, d)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := service.Search(context.Background(), in); err == nil || result != nil || called {
		t.Fatal("malformed identity was repaired and authorized")
	}
}

type rerank func(context.Context, contracts.RerankRequest) ([]contracts.Evidence, error)

func (f rerank) Rerank(c context.Context, r contracts.RerankRequest) ([]contracts.Evidence, error) {
	return f(c, r)
}

func TestSearchRequiresConfiguredRerankerAndVerifiesBeforeCallingIt(t *testing.T) {
	p, d, in := fixture(t)
	p.Query.Policy.EnableRerank = true
	p.Query.Policy.RerankTimeoutMS = 1000
	p.Query.Bindings[contracts.Reranking] = contracts.ModuleBinding{InstanceID: "reranker", Capability: contracts.Reranking, ContractVersion: "1.0"}
	p, err := configprofile.Seal(p)
	if err != nil {
		t.Fatal(err)
	}
	service, err := knowledge.NewSearch(p, d)
	if err != nil {
		t.Fatal(err)
	}
	if results, err := service.Search(context.Background(), in); err == nil || results != nil {
		t.Fatal("missing configured reranker silently ignored")
	}
	base := d.Plans
	calls := 0
	d.Plans = plans(func(c context.Context, who contracts.Principal, q contracts.RetrieveRequest) (knowledge.SearchPlan, error) {
		plan, err := base.Prepare(c, who, q)
		plan.Reranker = rerank(func(context.Context, contracts.RerankRequest) ([]contracts.Evidence, error) { calls++; return nil, nil })
		plan.Retriever = retrieve(func(context.Context, contracts.RetrieveRequest) ([]contracts.Candidate, error) {
			return []contracts.Candidate{{Text: "protected", Rank: 1, Citation: contracts.Citation{DocumentID: "unbound", RevisionID: "rev", BlockID: "block"}}}, nil
		})
		return plan, err
	})
	service, err = knowledge.NewSearch(p, d)
	if err != nil {
		t.Fatal(err)
	}
	if results, err := service.Search(context.Background(), in); err == nil || results != nil || calls != 0 {
		t.Fatal("unverified content sent to reranker")
	}
}

func TestSearchRejectsReflectionUntilExecutorSupportsIt(t *testing.T) {
	p, d, _ := fixture(t)
	p.Query.Reflection = contracts.ReflectionPolicy{Enabled: true, MaxExtraRounds: 2, MaxPlanQueries: 2, TotalBudgetMS: 30000}
	p, err := configprofile.Seal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := knowledge.NewSearch(p, d); err == nil {
		t.Fatal("unsupported reflection silently accepted")
	}
}

func TestSearchRejectsMissingExplicitFusionBinding(t *testing.T) {
	p, d, _ := fixture(t)
	p.Query.Bindings[contracts.Fusion] = contracts.ModuleBinding{InstanceID: "configured-fuser", Capability: contracts.Fusion, ContractVersion: "1.0"}
	p, err := configprofile.Seal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := knowledge.NewSearch(p, d); err == nil {
		t.Fatal("explicit fusion module silently replaced by default")
	}
}

func TestRerankCandidateBudgetIsIndependentOfRequestedTopK(t *testing.T) {
	p, d, in := fixture(t)
	in.Limit = 1
	p.Query.Policy.EnableRerank = true
	p.Query.Policy.RerankTimeoutMS = 1000
	p.Query.Bindings[contracts.Reranking] = contracts.ModuleBinding{InstanceID: "reranker", Capability: contracts.Reranking, ContractVersion: "1.0"}
	p, err := configprofile.Seal(p)
	if err != nil {
		t.Fatal(err)
	}
	d.Facts = facts(func(context.Context, contracts.RequestContext, contracts.AccessScope) (queryread.ControlFacts, error) {
		return queryread.ControlFacts{ProfileDigest: p.Pipeline.Digest, ReviewPolicies: map[contracts.ID]uint64{"kb": 1}}, nil
	})
	original := d.Plans
	observed := 0
	d.Plans = plans(func(c context.Context, who contracts.Principal, q contracts.RetrieveRequest) (knowledge.SearchPlan, error) {
		observed = q.Limit
		plan, err := original.Prepare(c, who, q)
		plan.Reranker = rerank(func(context.Context, contracts.RerankRequest) ([]contracts.Evidence, error) {
			t.Fatal("empty pool should not call reranker")
			return nil, nil
		})
		return plan, err
	})
	service, err := knowledge.NewSearch(p, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Search(context.Background(), in); err != nil || observed != 10 {
		t.Fatal("requested top K truncated configured candidate pool", observed, err)
	}
}

type kbDirectory func(context.Context, contracts.Principal, contracts.ID) ([]contracts.ID, error)

func (f kbDirectory) ListKnowledgeBases(c context.Context, p contracts.Principal, s contracts.ID) ([]contracts.ID, error) {
	return f(c, p, s)
}
func TestSearchResolvesOmittedKBsButKeepsExplicitEmpty(t *testing.T) {
	for _, omitted := range []bool{true, false} {
		p, d, in := fixture(t)
		if omitted {
			in.KnowledgeBaseIDs = nil
		} else {
			in.KnowledgeBaseIDs = []contracts.ID{}
		}
		resolved, authorized := 0, 0
		original := d.Authorizer
		d.KnowledgeBases = kbDirectory(func(_ context.Context, who contracts.Principal, space contracts.ID) ([]contracts.ID, error) {
			resolved++
			if who.SubjectID != "reader" || space != "space" {
				t.Fatal("unbound KB lookup")
			}
			return []contracts.ID{"kb"}, nil
		})
		d.Authorizer = authorize(func(c context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
			authorized++
			return original.Authorize(c, q)
		})
		service, err := knowledge.NewSearch(p, d)
		if err != nil {
			t.Fatal(err)
		}
		out, err := service.Search(context.Background(), in)
		if err != nil || len(out) != 0 {
			t.Fatal(err)
		}
		if omitted {
			if resolved != 1 || authorized != 3 {
				t.Fatal("omitted scope was not resolved and reauthorized", resolved, authorized)
			}
		} else if resolved != 0 || authorized != 0 {
			t.Fatal("explicit empty expanded scope")
		}
	}
}

func TestEmptySearchRechecksCredentialAfterDirectoryResolution(t *testing.T) {
	p, d, in := fixture(t)
	in.KnowledgeBaseIDs = nil
	revoked := false
	original := d.Auth
	d.KnowledgeBases = kbDirectory(func(context.Context, contracts.Principal, contracts.ID) ([]contracts.ID, error) {
		revoked = true
		return []contracts.ID{}, nil
	})
	d.Auth = auth(func(c context.Context, q contracts.Credential) (contracts.Principal, error) {
		if revoked {
			return contracts.Principal{}, errors.New("revoked during directory read")
		}
		return original.Authenticate(c, q)
	})
	service, err := knowledge.NewSearch(p, d)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := service.Search(context.Background(), in); err == nil || out != nil {
		t.Fatal("revoked credential returned successful empty result")
	}
}

func TestInitialAuthenticationFailureKeepsSafeTypedError(t *testing.T) {
	p, d, in := fixture(t)
	d.Auth = auth(func(context.Context, contracts.Credential) (contracts.Principal, error) {
		return contracts.Principal{}, errors.New("private signature detail")
	})
	service, err := knowledge.NewSearch(p, d)
	if err != nil {
		t.Fatal(err)
	}
	out, err := service.Search(context.Background(), in)
	if out != nil || !errors.Is(err, knowledge.ErrSearch) || !errors.Is(err, knowledge.ErrUnauthenticated) || err.Error() != "knowledge search unavailable" {
		t.Fatal("authentication classification leaked details or lost generic contract", err)
	}
}
