package localruntime

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// Corpus holds an invocation's private validation capability. It cannot be
// serialized or reused after Close. Callers validate before and after model I/O.
type Corpus struct {
	Candidates []contracts.Candidate
	Request    contracts.RequestContext
	Scope      contracts.AccessScope
	mu         sync.Mutex
	validate   func(context.Context, []contracts.Candidate) error
	close      func()
}

func (c *Corpus) Validate(ctx context.Context, items []contracts.Candidate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.validate == nil {
		return knowledge.ErrSearch
	}
	return c.validate(ctx, items)
}
func (c *Corpus) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.close != nil {
		c.close()
		c.close = nil
		c.validate = nil
	}
}

func (s *Search) OpenCorpus(ctx context.Context, in knowledge.SearchInput) (*Corpus, error) {
	if s.checkClock(ctx) != nil || in.RequestID == "" || in.Deadline.IsZero() || !in.Deadline.After(time.Now()) || len(in.KnowledgeBaseIDs) != 1 || in.Credential.Kind != "access_token" || len(in.Credential.Secret) == 0 {
		return nil, knowledge.ErrSearch
	}
	ctx, cancel := context.WithDeadline(ctx, in.Deadline)
	memo := &postgres.ArtifactDigestMemoScope{}
	ctx = memo.Context(ctx)
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		cancel()
		return nil, knowledge.ErrSearch
	}
	secret := contracts.Credential{Kind: in.Credential.Kind, Secret: append([]byte(nil), in.Credential.Secret...)}
	cleanup := func() { clear(secret.Secret); cancel(); <-s.slots }
	failed := true
	defer func() {
		if failed {
			cleanup()
		}
	}()
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return nil, knowledge.ErrUnauthenticated
	}
	profile, err := configprofile.Decode(s.profile)
	if err != nil {
		return nil, knowledge.ErrSearch
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: 2 * time.Minute}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "knowledge.search", SpaceID: in.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), in.KnowledgeBaseIDs...)})
	if err != nil {
		return nil, knowledge.ErrSearch
	}
	verify := scopeVerifier(s.auth, authorizer, secret, who)
	reads := postgres.QueryReads{DB: s.db, Verify: func(c context.Context, q contracts.AccessScope) error {
		if s.checkClock(c) != nil {
			return knowledge.ErrSearch
		}
		return verify(c, q)
	}}
	request := contracts.RequestContext{RequestID: in.RequestID, ProfileDigest: profile.Pipeline.Digest, Deadline: in.Deadline}
	control := postgres.QueryFacts{DB: s.db}
	facts, err := control.LoadCurrentFacts(ctx, request, scope)
	if err != nil || facts.ProfileDigest != profile.Pipeline.Digest {
		return nil, knowledge.ErrSearch
	}
	loaded, err := reads.LoadPublishedCorpus(ctx, scope)
	if err != nil {
		return nil, err
	}
	backend := postgres.EvidenceBackend{Reads: reads}
	if s.originals != nil {
		backend.Originals, backend.OriginalControl, backend.Projector, err = s.originals(s.auth, secret, who)
		if err != nil {
			return nil, err
		}
	}
	// Keep independent deep copies: an embedder/ranker cannot rewrite the
	// baseline and make altered text or citations validate against themselves.
	raw, _ := json.Marshal(loaded.Candidates)
	var exposed, baseline []contracts.Candidate
	if json.Unmarshal(raw, &exposed) != nil || json.Unmarshal(raw, &baseline) != nil {
		return nil, knowledge.ErrSearch
	}
	known := map[contracts.ID]contracts.Candidate{}
	for _, item := range baseline {
		known[item.ChunkID] = item
	}
	exposedScope := scope
	exposedScope.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	result := &Corpus{Candidates: exposed, Request: request, Scope: exposedScope, close: cleanup}
	result.validate = func(c context.Context, selected []contracts.Candidate) error {
		c = memo.Context(c)
		if ctx.Err() != nil || c.Err() != nil || verify(c, scope) != nil || s.checkClock(c) != nil {
			return knowledge.ErrSearch
		}
		current, err := control.LoadCurrentFacts(c, request, scope)
		if err != nil || !reflect.DeepEqual(facts, current) {
			return knowledge.ErrSearch
		}
		evidence := make([]contracts.Evidence, 0, len(selected))
		seen := map[contracts.ID]bool{}
		for _, item := range selected {
			old, ok := known[item.ChunkID]
			if !ok || seen[item.ChunkID] || !reflect.DeepEqual(old.Citation, item.Citation) || !reflect.DeepEqual(old.GenerationIDs, item.GenerationIDs) || old.Text != item.Text || old.KnowledgeBaseID != item.KnowledgeBaseID {
				return knowledge.ErrSearch
			}
			seen[item.ChunkID] = true
			evidence = append(evidence, contracts.Evidence{Citation: item.Citation, SourceExcerpt: item.Text, Rank: len(evidence) + 1})
		}
		if len(loaded.Facts) > 0 {
			ids := make([]contracts.ID, 0, len(loaded.Facts))
			for _, f := range loaded.Facts {
				ids = append(ids, f.DocumentID)
			}
			observed, err := queryread.ObserveCurrent(c, reads, scope, ids, queryread.CurrentLimits{MaxDocuments: 1000, MaxBytes: 4 << 20})
			if err != nil {
				return knowledge.ErrSearch
			}
			expected := queryread.CurrentExpectation{StoreEpochID: loaded.Observation.Identity.StoreEpochID, ScopeVersion: scope.PolicyVersion, Published: loaded.Facts}
			if queryread.MatchCurrentReview(scope, expected, observed, current.ReviewPolicies) != nil || backend.Verify(c, request, scope, observed, evidence) != nil {
				return knowledge.ErrSearch
			}
		}
		return verify(c, scope)
	}
	if err = result.Validate(ctx, exposed); err != nil {
		return nil, err
	}
	failed = false
	return result, nil
}
