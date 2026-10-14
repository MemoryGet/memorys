package queryread

import (
	"errors"
	"reflect"
	"sort"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrStale = errors.New("publication snapshot is stale")

type CurrentExpectation struct {
	StoreEpochID contracts.ID
	ScopeVersion uint64
	Published    []contracts.PublishedFact
}

// MatchCurrent checks supplied metadata relationships. A match is not fresh
// authorization, approval validity, real index readiness or a send barrier.
func MatchCurrent(scope contracts.AccessScope, expected CurrentExpectation, observed CurrentObservation) error {
	if expected.StoreEpochID == "" || expected.ScopeVersion == 0 || len(expected.Published) == 0 || len(expected.Published) > 1000 || !scope.ExpiresAt.After(time.Now()) {
		return ErrInvalid
	}
	if observed.Identity.StoreEpochID != expected.StoreEpochID || scope.PolicyVersion != expected.ScopeVersion {
		return ErrStale
	}
	facts := append([]contracts.PublishedFact(nil), expected.Published...)
	sort.Slice(facts, func(i, j int) bool { return facts[i].DocumentID < facts[j].DocumentID })
	request := contracts.PublishedPageRequest{Identity: observed.Identity, KnowledgeBaseIDs: scope.KnowledgeBaseIDs, Limit: 1000}
	if contracts.ValidatePublishedPage(contracts.PublishedPage{Identity: observed.Identity, Facts: facts}, request, scope, "") != nil || len(observed.Documents) != len(facts) {
		return ErrInvalid
	}
	byID := make(map[contracts.ID]contracts.CurrentDocumentState, len(observed.Documents))
	for _, state := range observed.Documents {
		if _, duplicate := byID[state.Document.ID]; duplicate {
			return ErrInvalid
		}
		byID[state.Document.ID] = state
	}
	for _, fact := range facts {
		state, present := byID[fact.DocumentID]
		if !present {
			return ErrInvalid
		}
		d := state.Document
		if d.SpaceID != fact.SpaceID || d.KnowledgeBaseID != fact.KnowledgeBaseID || d.RowVersion < fact.RowVersion || d.DeletedAt != nil || d.PublicationEpoch != fact.PublicationEpoch || d.ActiveRevisionID == nil || *d.ActiveRevisionID != fact.RevisionID || d.ActiveGenerationSetID == nil || *d.ActiveGenerationSetID != fact.GenerationSet.ID || state.ActiveBackendEpochID != fact.GenerationSet.BackendEpochID || state.GenerationSet == nil {
			return ErrStale
		}
		set := *state.GenerationSet
		old := fact.GenerationSet
		if set.ID != old.ID || set.RevisionID != old.RevisionID || set.BackendEpochID != old.BackendEpochID || set.State != "ready" || len(set.Channels) != len(old.Channels) {
			return ErrStale
		}
		for channel, was := range old.Channels {
			now, present := set.Channels[channel]
			if !present || now.ID != was.ID || now.Channel != channel || now.KnowledgeBaseID != was.KnowledgeBaseID || now.RevisionID != was.RevisionID || now.BackendEpochID != was.BackendEpochID || now.ProfileDigest != was.ProfileDigest || now.State != "ready" || !reflect.DeepEqual(now.EmbeddingSpec, was.EmbeddingSpec) {
				return ErrStale
			}
		}
	}
	return nil
}
