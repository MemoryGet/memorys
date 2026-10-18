package knowledge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"sync/atomic"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/publicationprojection"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// ConfiguredPlans assembles one configured route over one authorized KB per
// call, matching the native retrieval projection contract. Route orchestration
// remains with SearchService; it is not a multi-KB federation implementation.
type ConfiguredPlansConfig struct {
	Profile    configprofile.Snapshot
	Authorizer contracts.Authorizer
	Facts      queryread.CurrentFactsSource
	Reads      contracts.QueryReadUnit
	Limits     queryread.Limits
	Resolve    func(context.Context, contracts.ModuleBinding) (contracts.Retriever, error)
	Reranker   contracts.Reranker
}
type ConfiguredPlans struct {
	profile []byte
	config  ConfiguredPlansConfig
	closed  atomic.Bool
}

func NewConfiguredPlans(c ConfiguredPlansConfig) (*ConfiguredPlans, error) {
	raw, e := configprofile.Encode(c.Profile)
	if e != nil || missing(c.Authorizer) || missing(c.Facts) || missing(c.Reads) || c.Resolve == nil || c.Limits.PageSize < 1 || c.Limits.PageSize > 1000 || c.Limits.MaxPages < 1 || c.Limits.MaxPages > 1000 || c.Limits.MaxFacts < 1 || c.Limits.MaxFacts > 1000 || c.Limits.MaxGenerations < 1 || c.Limits.MaxGenerations > 3000 || c.Profile.Query.Policy.EnableRerank && missing(c.Reranker) {
		return nil, ErrSearch
	}
	c.Profile = configprofile.Snapshot{}
	return &ConfiguredPlans{profile: raw, config: c}, nil
}
func (s *ConfiguredPlans) Close() {
	if s != nil {
		s.closed.Store(true)
	}
}

func (s *ConfiguredPlans) refresh(ctx context.Context, who contracts.Principal, q contracts.RetrieveRequest, documents []contracts.ID) (queryread.ControlFacts, error) {
	var empty queryread.ControlFacts
	if s == nil || s.closed.Load() || ctx == nil || ctx.Err() != nil {
		return empty, ErrSearch
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrSearch
	}
	actor, e := copyValue(who)
	if e != nil {
		return empty, e
	}
	request := contracts.AuthorizeRequest{Principal: actor, Action: "knowledge.search", SpaceID: q.Scope.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), q.Scope.KnowledgeBaseIDs...), ResourceIDs: append([]contracts.ID(nil), documents...)}
	grant, e := s.config.Authorizer.Authorize(ctx, request)
	grant, owned := copyValue(grant)
	if e != nil || owned != nil || queryread.MatchRefreshedScope(ctx, who, q.Scope, who, grant, q.Scope.KnowledgeBaseIDs) != nil {
		return empty, ErrSearch
	}
	scope, e := copyValue(q.Scope)
	if e != nil {
		return empty, e
	}
	facts, e := s.config.Facts.LoadCurrentFacts(ctx, q.Request, scope)
	facts, owned = copyValue(facts)
	if e != nil || owned != nil || facts.ProfileDigest != q.Request.ProfileDigest || facts.ReviewPolicies[q.Scope.KnowledgeBaseIDs[0]] == 0 || ctx.Err() != nil || s.closed.Load() {
		return empty, ErrSearch
	}
	return facts, nil
}

type plannedInventory struct {
	identity contracts.QueryReadIdentity
	epoch    contracts.BackendEpoch
	facts    []contracts.PublishedFact
}

func (s *ConfiguredPlans) inventory(ctx context.Context, q contracts.RetrieveRequest, rules map[contracts.ID]uint64) (plannedInventory, error) {
	var out plannedInventory
	unit, e := queryread.ProtectUnit(s.config.Reads)
	if e != nil {
		return out, ErrSearch
	}
	e = unit.WithinRead(ctx, q.Scope, func(c context.Context, identity contracts.QueryReadIdentity, repo contracts.QueryReadRepository) error {
		if !id(identity.StoreEpochID) || identity.ReadVersion == "" || identity.Watermark == "" {
			return ErrSearch
		}
		out.identity = identity
		var e error
		out.epoch, e = repo.LoadActiveEpoch(c, q.Scope.KnowledgeBaseIDs[0])
		if e != nil || out.epoch.KnowledgeBaseID != q.Scope.KnowledgeBaseIDs[0] || !id(out.epoch.ID) || out.epoch.State != "active" {
			return ErrSearch
		}
		out.facts, e = queryread.Collect(c, repo, q.Scope, identity, q.Scope.KnowledgeBaseIDs, s.config.Limits)
		if e != nil {
			return e
		}
		out.facts, e = copyValue(out.facts)
		if e != nil {
			return e
		}
		observation := queryread.CurrentObservation{Identity: identity}
		remaining := 4 << 20
		for _, fact := range out.facts {
			if fact.GenerationSet.BackendEpochID != out.epoch.ID {
				return ErrSearch
			}
			state, e := repo.LoadCurrentDocument(c, fact.DocumentID)
			if e != nil {
				return e
			}
			raw, e := json.Marshal(state)
			if e != nil || len(raw) > remaining {
				return ErrSearch
			}
			remaining -= len(raw)
			state, e = copyValue(state)
			if e != nil {
				return e
			}
			observation.Documents = append(observation.Documents, state)
		}
		if len(out.facts) > 0 {
			return queryread.MatchCurrentReview(q.Scope, queryread.CurrentExpectation{StoreEpochID: identity.StoreEpochID, ScopeVersion: q.Scope.PolicyVersion, Published: out.facts}, observation, rules)
		}
		return c.Err()
	})
	if e != nil {
		return plannedInventory{}, ErrSearch
	}
	return out, nil
}

// All numbers in the internal parent commitment are decimal strings, preserving
// uint64 identity independently of canonical JSON's floating-number encoding.
func parentNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		return x.String()
	case []any:
		for i := range x {
			x[i] = parentNumbers(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = parentNumbers(x[k])
		}
	}
	return v
}
func parentInventoryDigest(q contracts.RetrieveRequest, in plannedInventory) (contracts.Digest, error) {
	raw, e := json.Marshal(map[string]any{"format": "akos.search-parent/1", "scope": q.Scope, "identity": in.identity, "epoch": in.epoch, "published": in.facts})
	if e != nil || len(raw) > 4<<20 {
		return "", ErrSearch
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return "", ErrSearch
	}
	raw, e = json.Marshal(parentNumbers(value))
	if e != nil {
		return "", ErrSearch
	}
	raw, e = canonical.JSON(raw)
	if e != nil {
		return "", ErrSearch
	}
	sum := sha256.Sum256(append([]byte("akos.search-parent/1\n"), raw...))
	return contracts.Digest(hex.EncodeToString(sum[:])), nil
}
func newPlanID() (contracts.ID, error) {
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return "", ErrSearch
	}
	return contracts.ID(hex.EncodeToString(nonce[:])), nil
}

func (s *ConfiguredPlans) Prepare(ctx context.Context, principal contracts.Principal, input contracts.RetrieveRequest) (SearchPlan, error) {
	var empty SearchPlan
	if s == nil || s.closed.Load() || ctx == nil || ctx.Err() != nil {
		return empty, ErrSearch
	}
	q, e := copyValue(input)
	who, owned := copyValue(principal)
	if e != nil || owned != nil || len(q.Scope.KnowledgeBaseIDs) != 1 || q.Scope.Action != "knowledge.search" || !id(q.Request.RequestID) || q.Request.Deadline.IsZero() || !q.Request.Deadline.After(time.Now()) || q.Request.Deadline.After(q.Scope.ExpiresAt) || q.BackendEpochID != "" || len(q.Generations) != 0 || !reflect.DeepEqual(q.Publication, contracts.PublicationFilter{}) {
		return empty, ErrSearch
	}
	profile, e := configprofile.Decode(s.profile)
	if e != nil || q.Request.ProfileDigest != profile.Pipeline.Digest {
		return empty, ErrSearch
	}
	var route *contracts.RetrievalRoute
	for _, candidate := range profile.Query.Routes {
		if candidate.ID == q.RouteID {
			owned := candidate
			route = &owned
		}
	}
	if route == nil || !reflect.DeepEqual(route.Channels, q.Channels) || q.Limit < 1 || q.Limit > route.CandidateLimit {
		return empty, ErrSearch
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrSearch
	}
	ctx, cancel := context.WithDeadline(ctx, q.Request.Deadline)
	defer cancel()
	initial, e := s.refresh(ctx, who, q, nil)
	if e != nil {
		return empty, e
	}
	inventory, e := s.inventory(ctx, q, initial.ReviewPolicies)
	if e != nil {
		return empty, e
	}
	parent, e := parentInventoryDigest(q, inventory)
	if e != nil {
		return empty, e
	}
	projectionID, e := newPlanID()
	if e != nil {
		return empty, e
	}
	invocationID, e := newPlanID()
	if e != nil {
		return empty, e
	}
	record := publicationprojection.Record{SchemaVersion: 1, ProjectionID: projectionID, InvocationID: invocationID, RequestID: q.Request.RequestID, ParentScopeID: q.Scope.ScopeID, ChildScopeID: q.Scope.ScopeID, SpaceID: q.Scope.SpaceID, KnowledgeBaseID: q.Scope.KnowledgeBaseIDs[0], BackendEpochID: inventory.epoch.ID, RouteID: q.RouteID, ModuleInstanceID: route.Binding.InstanceID, ParentPublicationDigest: parent, QueryProfileDigest: profile.Query.Digest, PipelineDigest: profile.Pipeline.Digest, PolicyVersion: q.Scope.PolicyVersion, Channels: append([]contracts.Channel(nil), q.Channels...), ExpiresAtUnixNS: q.Request.Deadline.UnixNano()}
	q.BackendEpochID = inventory.epoch.ID
	q.Publication = contracts.PublicationFilter{SpaceID: q.Scope.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), q.Scope.KnowledgeBaseIDs...)}
	documents := []contracts.ID{}
	revisions := map[contracts.ID]bool{}
	for _, fact := range inventory.facts {
		documents = append(documents, fact.DocumentID)
		for _, channel := range route.Channels {
			g, ok := fact.GenerationSet.Channels[channel]
			if !ok {
				continue
			}
			q.Generations = append(q.Generations, g)
			q.Publication.GenerationIDs = append(q.Publication.GenerationIDs, g.ID)
			revisions[fact.RevisionID] = true
			record.Entries = append(record.Entries, publicationprojection.Entry{DocumentID: fact.DocumentID, RevisionID: fact.RevisionID, PublicationVersion: fact.PublicationEpoch, GenerationSetID: fact.GenerationSet.ID, GenerationID: g.ID, Channel: channel, IndexProfileDigest: g.ProfileDigest})
		}
	}
	for revision := range revisions {
		q.Publication.RevisionIDs = append(q.Publication.RevisionIDs, revision)
	}
	sort.Slice(q.Publication.RevisionIDs, func(i, j int) bool { return q.Publication.RevisionIDs[i] < q.Publication.RevisionIDs[j] })
	sort.Slice(q.Publication.GenerationIDs, func(i, j int) bool { return q.Publication.GenerationIDs[i] < q.Publication.GenerationIDs[j] })
	// Projection ordering is byte lexical, independent of map/channel iteration.
	sort.Slice(record.Entries, func(i, j int) bool {
		a, b := record.Entries[i], record.Entries[j]
		av, bv := []string{string(a.DocumentID), string(a.RevisionID), string(a.Channel), string(a.GenerationID)}, []string{string(b.DocumentID), string(b.RevisionID), string(b.Channel), string(b.GenerationID)}
		for k := range av {
			if av[k] != bv[k] {
				return av[k] < bv[k]
			}
		}
		return false
	})
	if len(inventory.facts) > 0 && len(record.Entries) == 0 {
		// A configured route that cannot describe any current generation is a
		// stale/incomplete route, not an empty search result.
		return empty, ErrSearch
	}
	var raw []byte
	if len(record.Entries) > 0 {
		raw, e = json.Marshal(record)
		if e != nil {
			return empty, e
		}
		_, q.Publication.SnapshotDigest, e = publicationprojection.ValidatedDigest(raw)
		if e != nil {
			return empty, ErrSearch
		}
	} else {
		q.Publication.SnapshotDigest = parent
	}
	last, e := s.refresh(ctx, who, q, documents)
	if e != nil || !reflect.DeepEqual(initial.ReviewPolicies, last.ReviewPolicies) {
		return empty, ErrSearch
	}
	binding, e := copyValue(route.Binding)
	if e != nil {
		return empty, e
	}
	inner, e := s.config.Resolve(ctx, binding)
	if e != nil || missing(inner) || s.closed.Load() || ctx.Err() != nil {
		return empty, ErrSearch
	}
	fixed, e := copyValue(q)
	if e != nil {
		return empty, e
	}
	expected, e := copyValue(queryread.CurrentExpectation{StoreEpochID: inventory.identity.StoreEpochID, ScopeVersion: q.Scope.PolicyVersion, Published: inventory.facts})
	if e != nil {
		return empty, e
	}
	control := &preparedPlan{source: s, who: who, request: fixed, raw: append([]byte(nil), raw...), call: publicationprojection.CallIdentity{InvocationID: invocationID, ModuleInstanceID: binding.InstanceID, Limit: q.Limit}, documents: append([]contracts.ID(nil), documents...), expectation: expected, rules: initial.ReviewPolicies, inner: inner}
	requestOut, _ := copyValue(fixed)
	expectationOut, _ := copyValue(expected)
	return SearchPlan{Request: requestOut, Expectation: expectationOut, Retriever: control, Reranker: s.config.Reranker}, nil
}

type projectionContextKey struct{}
type projectionCapability struct {
	plan   *preparedPlan
	active atomic.Bool
}
type preparedPlan struct {
	source      *ConfiguredPlans
	who         contracts.Principal
	request     contracts.RetrieveRequest
	raw         []byte
	call        publicationprojection.CallIdentity
	documents   []contracts.ID
	expectation queryread.CurrentExpectation
	rules       map[contracts.ID]uint64
	inner       contracts.Retriever
}

func (p *preparedPlan) Search(ctx context.Context, q contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	if ctx == nil || !reflect.DeepEqual(q, p.request) {
		return nil, ErrSearch
	}
	facts, e := p.source.refresh(ctx, p.who, p.request, p.documents)
	if e != nil || !reflect.DeepEqual(facts.ReviewPolicies, p.rules) {
		return nil, ErrSearch
	}
	if len(p.raw) == 0 {
		inventory, e := p.source.inventory(ctx, p.request, p.rules)
		if e != nil || inventory.identity.StoreEpochID != p.expectation.StoreEpochID {
			return nil, ErrSearch
		}
		for _, fact := range inventory.facts {
			for _, channel := range p.request.Channels {
				if _, ok := fact.GenerationSet.Channels[channel]; ok {
					return nil, ErrSearch
				}
			}
		}
		return []contracts.Candidate{}, nil
	}
	cap := &projectionCapability{plan: p}
	cap.active.Store(true)
	defer cap.active.Store(false)
	call, e := copyValue(q)
	if e != nil {
		return nil, e
	}
	results, e := p.inner.Search(context.WithValue(ctx, projectionContextKey{}, cap), call)
	if e != nil || ctx.Err() != nil || p.source.closed.Load() {
		return nil, ErrSearch
	}
	facts, e = p.source.refresh(ctx, p.who, p.request, p.documents)
	if e != nil || !reflect.DeepEqual(facts.ReviewPolicies, p.rules) {
		return nil, ErrSearch
	}
	return copyValue(results)
}

// AuthorizeProjection is a callback for a configured local retriever. It accepts
// only a live, private prepared invocation context, never a client digest alone.
// Caller authentication and final evidence checks still belong to SearchService.
func (s *ConfiguredPlans) AuthorizeProjection(ctx context.Context, q contracts.RetrieveRequest, module contracts.ID) ([]byte, publicationprojection.CallIdentity, error) {
	var empty publicationprojection.CallIdentity
	if ctx == nil || s == nil || s.closed.Load() {
		return nil, empty, ErrSearch
	}
	cap, ok := ctx.Value(projectionContextKey{}).(*projectionCapability)
	if !ok || !cap.active.Load() || cap.plan.source != s || cap.plan.call.ModuleInstanceID != module || !reflect.DeepEqual(q, cap.plan.request) {
		return nil, empty, ErrSearch
	}
	facts, e := s.refresh(ctx, cap.plan.who, cap.plan.request, cap.plan.documents)
	if e != nil || !reflect.DeepEqual(facts.ReviewPolicies, cap.plan.rules) || !cap.active.Load() || publicationprojection.ValidateRequest(cap.plan.raw, cap.plan.call, q) != nil {
		return nil, empty, ErrSearch
	}
	return append([]byte(nil), cap.plan.raw...), cap.plan.call, nil
}
