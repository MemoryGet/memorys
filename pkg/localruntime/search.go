// Package localruntime assembles the single-database local application host.
// It reuses the authenticated knowledge application and native PG adapters;
// it never creates identities, policies, publications or default grants.
package localruntime

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/clockhealth"
	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/publicationprojection"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

var ErrConfiguration = errors.New("local keyword search configuration rejected")

type Search struct {
	db        *sql.DB
	auth      contracts.Authenticator
	profile   []byte
	clockMu   sync.Mutex
	clock     *clockhealth.Tracker
	started   time.Time
	slots     chan struct{}
	originals OriginalFactory
}

// OriginalFactory is invocation-scoped: its grants must authenticate this
// credential and resolve each currently approved original independently.
type OriginalFactory func(contracts.Authenticator, contracts.Credential, contracts.Principal) (contracts.BlobStore, contentverify.OriginalReadControl, contentverify.OriginalTextProjector, error)

func (s *Search) WithOriginals(factory OriginalFactory) *Search { s.originals = factory; return s }

// NewSearch admits exactly one native keyword route. The supplied authenticator
// must check current token status, in addition to validating signatures.
func NewSearch(db *sql.DB, auth contracts.Authenticator, profile configprofile.Snapshot) (*Search, error) {
	raw, err := configprofile.Encode(profile)
	if err != nil || db == nil || auth == nil || reflect.ValueOf(auth).Kind() == reflect.Pointer && reflect.ValueOf(auth).IsNil() || !supported(profile) {
		return nil, ErrConfiguration
	}
	clock, err := clockhealth.NewTracker(250 * time.Millisecond)
	if err != nil {
		return nil, ErrConfiguration
	}
	parallel := 4
	if limit := db.Stats().MaxOpenConnections; limit > 0 {
		if limit < 2 {
			return nil, ErrConfiguration
		}
		if limit/2 < parallel {
			parallel = limit / 2
		}
	}
	return &Search{db: db, auth: auth, profile: raw, clock: clock, started: time.Now(), slots: make(chan struct{}, parallel)}, nil
}

func supported(p configprofile.Snapshot) bool {
	if p.Index.Mode != "independent" || len(p.Index.Routes) != 1 || len(p.Index.Routes[0].Channels) != 1 || p.Index.Routes[0].Channels[0] != contracts.KeywordChannel || len(p.Index.Policy.RequiredChannels) != 1 || p.Index.Policy.RequiredChannels[0] != contracts.KeywordChannel || len(p.Query.Routes) != 1 || p.Query.Policy.EnableRerank || p.Query.Policy.EnableGeneration || p.Query.Reflection.Enabled || len(p.Query.Bindings) != 1 {
		return false
	}
	r := p.Query.Routes[0]
	return len(r.Channels) == 1 && r.Channels[0] == contracts.KeywordChannel && r.Binding.Capability == contracts.Retrieval && r.Binding.ContractVersion == "1.0" && len(r.Binding.RequiredFeatures) == 0 && reflect.DeepEqual(p.Query.Bindings[contracts.Retrieval], r.Binding)
}

func (s *Search) checkClock(ctx context.Context) error {
	if s == nil || ctx == nil || ctx.Err() != nil {
		return knowledge.ErrSearch
	}
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	now := time.Now()
	if s.clock.Observe(now, now.Sub(s.started)) != nil || !s.clock.Healthy() {
		return knowledge.ErrSearch
	}
	return nil
}

func (s *Search) ProfileIdentity() (uint64, contracts.Digest, error) {
	if s == nil {
		return 0, "", knowledge.ErrSearch
	}
	p, err := configprofile.Decode(s.profile)
	if err != nil {
		return 0, "", knowledge.ErrSearch
	}
	return p.Pipeline.Version, p.Pipeline.Digest, nil
}

// scopeVerifier reauthenticates the request credential at every SQL read
// boundary and obtains a fresh policy decision. An AccessScope alone is never
// permission. This closure exists only for the lifetime of one Search call.
func scopeVerifier(auth contracts.Authenticator, authorizer contracts.Authorizer, secret contracts.Credential, who contracts.Principal) postgres.VerifyScope {
	return func(ctx context.Context, before contracts.AccessScope) error {
		credential := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
		defer clear(credential.Secret)
		current, err := auth.Authenticate(ctx, credential)
		if err != nil {
			return knowledge.ErrSearch
		}
		fresh, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: current, Action: before.Action, SpaceID: before.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), before.KnowledgeBaseIDs...)})
		if err != nil || queryread.MatchRefreshedScope(ctx, who, before, current, fresh, before.KnowledgeBaseIDs) != nil {
			return knowledge.ErrSearch
		}
		return nil
	}
}

func (s *Search) Search(ctx context.Context, in knowledge.SearchInput) ([]contracts.Candidate, error) {
	if s.checkClock(ctx) != nil || in.Deadline.IsZero() || !in.Deadline.After(time.Now()) || in.Credential.Kind != "access_token" || len(in.Credential.Secret) == 0 || len(in.Credential.Secret) > 16384 || len(in.Text) > 4096 {
		return nil, knowledge.ErrSearch
	}
	ctx, cancel := context.WithDeadline(ctx, in.Deadline)
	defer cancel()
	ctx, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	// Each active request can hold a native read transaction while its current
	// identity/policy check borrows another connection. Reserve both before
	// work; callers sharing this pool must not lower its limit at runtime.
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return nil, knowledge.ErrSearch
	}
	secret := contracts.Credential{Kind: in.Credential.Kind, Secret: append([]byte(nil), in.Credential.Secret...)}
	defer clear(secret.Secret)
	credential := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
	who, err := s.auth.Authenticate(ctx, credential)
	clear(credential.Secret)
	if err != nil {
		return nil, knowledge.ErrUnauthenticated
	}
	profile, err := configprofile.Decode(s.profile)
	if err != nil {
		return nil, knowledge.ErrSearch
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	verify := scopeVerifier(s.auth, authorizer, secret, who)
	reads := postgres.QueryReads{DB: s.db, Verify: func(c context.Context, scope contracts.AccessScope) error {
		if s.checkClock(c) != nil {
			return knowledge.ErrSearch
		}
		return verify(c, scope)
	}}
	facts := postgres.QueryFacts{DB: s.db}
	var plans *knowledge.ConfiguredPlans
	plans, err = knowledge.NewConfiguredPlans(knowledge.ConfiguredPlansConfig{
		Profile: profile, Authorizer: authorizer, Facts: facts, Reads: reads,
		Limits: queryread.Limits{PageSize: 100, MaxPages: 10, MaxFacts: 1000, MaxGenerations: 3000},
		Resolve: func(_ context.Context, binding contracts.ModuleBinding) (contracts.Retriever, error) {
			if binding.InstanceID != profile.Query.Routes[0].Binding.InstanceID || binding.Capability != contracts.Retrieval {
				return nil, knowledge.ErrSearch
			}
			return postgres.KeywordRetriever{Reads: reads, ModuleInstanceID: binding.InstanceID, Authorize: func(c context.Context, q contracts.RetrieveRequest) ([]byte, publicationprojection.CallIdentity, error) {
				return plans.AuthorizeProjection(c, q, binding.InstanceID)
			}}, nil
		},
	})
	if err != nil {
		return nil, knowledge.ErrSearch
	}
	defer plans.Close()
	// Original-file evidence deliberately has no provider until an independent
	// original-read grant source is configured. Native verification rejects it.
	backend := postgres.EvidenceBackend{Reads: reads}
	if s.originals != nil {
		backend.Originals, backend.OriginalControl, backend.Projector, err = s.originals(s.auth, secret, who)
		if err != nil {
			return nil, knowledge.ErrSearch
		}
	}
	service, err := knowledge.NewSearch(profile, knowledge.Dependencies{KnowledgeBases: authorizer, Auth: s.auth, Authorizer: authorizer, Plans: plans, Facts: facts, Reads: reads, Backend: backend})
	if err != nil {
		return nil, knowledge.ErrSearch
	}
	results, err := service.Search(ctx, in)
	if err != nil {
		return nil, err
	}
	if s.checkClock(ctx) != nil {
		return nil, knowledge.ErrSearch
	}
	return results, nil
}
