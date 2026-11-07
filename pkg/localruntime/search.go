// Package localruntime assembles the single-database local application host.
// It reuses the authenticated knowledge application and native PG adapters;
// it never creates identities, policies, publications or default grants.
package localruntime

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
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
	"github.com/MemoryGet/memorys/pkg/storage"
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
	blobs     contracts.BlobStore
	reranker  contracts.Reranker
	tags      map[contracts.ID][]string
}

// OriginalFactory is invocation-scoped: its grants must authenticate this
// credential and resolve each currently approved original independently.
type OriginalFactory func(contracts.Authenticator, contracts.Credential, contracts.Principal) (contracts.BlobStore, contentverify.OriginalReadControl, contentverify.OriginalTextProjector, error)

func (s *Search) WithOriginals(factory OriginalFactory) *Search { s.originals = factory; return s }

// WithSourceTags binds the local manifest's user-owned document tags to
// source metadata responses. The map is copied so callers cannot mutate an
// active search runtime after assembly.
func (s *Search) WithSourceTags(tags map[contracts.ID][]string) *Search {
	if s == nil {
		return s
	}
	s.tags = make(map[contracts.ID][]string, len(tags))
	for documentID, values := range tags {
		s.tags[documentID] = append([]string(nil), values...)
	}
	return s
}

// WithBlobStore binds the host's logical original store for management source
// replacement. The runtime remains fail-closed when no store is supplied.
func (s *Search) WithBlobStore(store contracts.BlobStore) *Search {
	if s != nil {
		s.blobs = store
	}
	return s
}

// ParserBlobStore exposes the already-authorized original store to a host
// parser assembly. It returns the logical capability only; callers cannot
// bypass the per-request scope checks embedded by the store.
func (s *Search) ParserBlobStore() contracts.BlobStore {
	if s == nil {
		return nil
	}
	return s.blobs
}

// WithLocalBlobRoot enables the explicitly configured local original store
// for the management upload path. Authorization is still performed by the
// application service; the store callback rechecks the current subject grant
// and policy head before touching bytes.
func (s *Search) WithLocalBlobRoot(root string) *Search {
	if s == nil || s.db == nil || strings.TrimSpace(root) == "" {
		return s
	}
	s.blobs = storage.LocalBlobStore{Root: root, MaxBytes: 64 << 20, Authorize: func(ctx context.Context, scope contracts.AccessScope, _ contracts.BlobRef) error {
		if ctx == nil || scope.SubjectID == "" || scope.SpaceID == "" || len(scope.KnowledgeBaseIDs) != 1 || scope.PolicyVersion == 0 || !scope.ExpiresAt.After(time.Now()) || scope.Action != "document.parse" {
			return errors.New("blob scope rejected")
		}
		var allowed bool
		if s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.access_policy_heads h JOIN akos_withdraw_v1.access_policy_grants g ON g.space_id=h.space_id AND g.version=h.version WHERE h.space_id=$1 AND h.version=$2 AND g.subject_id=$3 AND g.kb_id=$4 AND g.action=$5)`, string(scope.SpaceID), scope.PolicyVersion, string(scope.SubjectID), string(scope.KnowledgeBaseIDs[0]), string(scope.Action)).Scan(&allowed) != nil || !allowed {
			return errors.New("blob scope unauthorized")
		}
		return nil
	}}
	return s
}

// WithReranker replaces the bounded lexical fallback during host startup.
// The injected implementation remains behind contracts.Reranker and cannot
// add evidence or bypass the final currentness checks.
func (s *Search) WithReranker(reranker contracts.Reranker) *Search {
	if s != nil && reranker != nil {
		s.reranker = reranker
	}
	return s
}

// Authenticator returns the authenticator bound to this search instance for
// sibling protocol adapters such as the MCP gateway. The caller receives the
// same immutable service dependency; it cannot mint identities or bypass the
// search service's per-request authorization checks.
func (s *Search) Authenticator() contracts.Authenticator {
	if s == nil {
		return nil
	}
	return s.auth
}

// NewAnswer binds a host-supplied generator to this authenticated search
// runtime. The generator never receives unrestricted content; AnswerService
// supplies only current, citation-bound search evidence and revalidates it
// after generation.
func (s *Search) NewAnswer(generator contracts.Generator) (*knowledge.AnswerService, error) {
	if s == nil || s.db == nil || generator == nil {
		return nil, knowledge.ErrAnswer
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	return knowledge.NewAnswer(knowledge.AnswerDependencies{Search: s, Auth: s.auth, Authorizer: authorizer, KnowledgeBases: authorizer, Generator: generator})
}

// NewFeedback binds the durable PostgreSQL feedback store and citation
// resolver to this authenticated runtime. The incremental feedback_v1
// migration must be applied before the returned service is used.
func (s *Search) NewFeedback() (*knowledge.FeedbackService, error) {
	if s == nil || s.db == nil {
		return nil, knowledge.ErrFeedback
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	return knowledge.NewFeedback(knowledge.FeedbackDependencies{Auth: s.auth, Authorizer: authorizer, Targets: postgres.FeedbackTargets{DB: s.db}, Store: postgres.FeedbackStore{DB: s.db}})
}

// ReadCurrent exposes the same current-publication read path used by the
// partner API. It is intentionally separate from Search so MCP/read callers
// cannot reuse a search result as an authorization decision.
func (s *Search) ReadCurrent(ctx context.Context, in knowledge.CurrentReadInput) (contracts.FrozenContentRecord, error) {
	var empty contracts.FrozenContentRecord
	if s == nil || s.db == nil || ctx == nil || ctx.Err() != nil {
		return empty, knowledge.ErrRead
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return empty, knowledge.ErrRead
	}
	_, err := configprofile.Decode(s.profile)
	if err != nil || s.checkClock(ctx) != nil {
		return empty, knowledge.ErrRead
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	reads := postgres.QueryReads{DB: s.db, Verify: func(c context.Context, scope contracts.AccessScope) error {
		if s.checkClock(c) != nil {
			return knowledge.ErrRead
		}
		credential := contracts.Credential{Kind: in.Credential.Kind, Secret: append([]byte(nil), in.Credential.Secret...)}
		defer clear(credential.Secret)
		who, authErr := s.auth.Authenticate(c, credential)
		if authErr != nil {
			return knowledge.ErrRead
		}
		fresh, authErr := authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: who, Action: scope.Action, SpaceID: scope.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)})
		if authErr != nil || queryread.MatchRefreshedScope(c, who, scope, who, fresh, scope.KnowledgeBaseIDs) != nil {
			return knowledge.ErrRead
		}
		return nil
	}}
	reader := postgres.PublishedFrozenReader{Reads: reads}
	service, err := knowledge.NewCurrentRead(knowledge.CurrentReadDependencies{
		Auth: s.auth, Locator: authorizer, Reads: reads, Content: reader,
		Facts: postgres.QueryFacts{DB: s.db}, Verifier: contentverify.TextReadVerifier{Reader: reader},
	})
	if err != nil {
		return empty, knowledge.ErrRead
	}
	return service.ReadCurrent(ctx, in)
}

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
	if p.Index.Mode != "independent" || len(p.Index.Routes) != 1 || len(p.Index.Routes[0].Channels) != 1 || p.Index.Routes[0].Channels[0] != contracts.KeywordChannel || len(p.Index.Policy.RequiredChannels) != 1 || p.Index.Policy.RequiredChannels[0] != contracts.KeywordChannel || len(p.Query.Routes) != 1 || p.Query.Policy.EnableGeneration || p.Query.Reflection.Enabled || len(p.Query.Bindings) != 1 {
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
	reranker := s.reranker
	if reranker == nil {
		reranker = knowledge.LexicalReranker{}
	}
	plans, err = knowledge.NewConfiguredPlans(knowledge.ConfiguredPlansConfig{
		Profile: profile, Authorizer: authorizer, Facts: facts, Reads: reads,
		Limits:   queryread.Limits{PageSize: 100, MaxPages: 10, MaxFacts: 1000, MaxGenerations: 3000},
		Reranker: reranker,
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
	// The native publication projection is intentionally scoped to one KB per
	// invocation. Federated callers still receive one authorized result set by
	// running that same path independently for each selected KB, then merging
	// the bounded evidence locally. A failure in any selected partition fails
	// closed rather than returning a partial, misleading answer.
	if in.KnowledgeBaseIDs == nil {
		resolved, resolveErr := authorizer.ListKnowledgeBases(ctx, who, in.SpaceID)
		if resolveErr != nil {
			return nil, knowledge.ErrSearch
		}
		in.KnowledgeBaseIDs = append([]contracts.ID(nil), resolved...)
	}
	var results []contracts.Candidate
	if len(in.KnowledgeBaseIDs) > 1 {
		for _, kb := range in.KnowledgeBaseIDs {
			partition := in
			partition.KnowledgeBaseIDs = []contracts.ID{kb}
			items, searchErr := service.Search(ctx, partition)
			if searchErr != nil {
				return nil, searchErr
			}
			results = append(results, items...)
		}
		results = mergeFederatedCandidates(results, in.Limit)
	} else {
		results, err = service.Search(ctx, in)
	}
	if err != nil {
		return nil, err
	}
	if s.checkClock(ctx) != nil {
		return nil, knowledge.ErrSearch
	}
	return results, nil
}

func (s *Search) SearchSourceMetadata(ctx context.Context, results []contracts.Candidate) (map[contracts.ID]contracts.SearchSourceMetadata, error) {
	out := map[contracts.ID]contracts.SearchSourceMetadata{}
	if s == nil || s.db == nil || len(results) == 0 {
		return out, nil
	}
	for index := range results {
		candidate := results[index]
		var epoch, title string
		var deleted bool
		if err := s.db.QueryRowContext(ctx, `SELECT d.publication_epoch::text, d.deleted, COALESCE(m.title,d.id) FROM akos_withdraw_v1.documents d LEFT JOIN akos_withdraw_v1.document_metadata m ON m.space_id=d.space_id AND m.kb_id=d.kb_id AND m.document_id=d.id WHERE d.id=$1::text AND d.kb_id=$2::text`, string(candidate.Citation.DocumentID), string(candidate.KnowledgeBaseID)).Scan(&epoch, &deleted, &title); err != nil {
			// Older databases can lack the optional metadata table. The current
			// document fact is still authoritative for status and epoch.
			if err = s.db.QueryRowContext(ctx, `SELECT publication_epoch::text,deleted,id FROM akos_withdraw_v1.documents WHERE id=$1::text AND kb_id=$2::text`, string(candidate.Citation.DocumentID), string(candidate.KnowledgeBaseID)).Scan(&epoch, &deleted, &title); err != nil {
				return nil, err
			}
		}
		status := "published"
		if deleted {
			status = "deleted"
		}
		updatedAt := ""
		var updated sql.NullTime
		if s.db.QueryRowContext(ctx, `SELECT max(occurred_at) FROM akos_withdraw_v1.publication_history WHERE document_id=$1::text AND kb_id=$2::text`, string(candidate.Citation.DocumentID), string(candidate.KnowledgeBaseID)).Scan(&updated) == nil && updated.Valid {
			updatedAt = updated.Time.UTC().Format(time.RFC3339Nano)
		}
		out[candidate.Citation.DocumentID] = contracts.SearchSourceMetadata{Title: title, UpdatedAt: updatedAt, PublicationStatus: status, PublicationEpoch: parseUint64(epoch), Tags: append([]string(nil), s.tags[candidate.Citation.DocumentID]...)}
	}
	return out, nil
}
