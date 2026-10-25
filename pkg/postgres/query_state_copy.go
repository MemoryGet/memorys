package postgres

import (
	"slices"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// Only for r.states: these values were freshly decoded from the native view
// and admitted by takeState's serialized byte budget. Copy every mutable
// reference instead of repeatedly encoding/decoding the same immutable state.
// This does not admit caller values, share output ownership or cache authority
// beyond the current SQL view; other boundary DTOs retain their existing checks.
func copyAdmittedState(s contracts.CurrentDocumentState) contracts.CurrentDocumentState {
	s.Document.ActiveRevisionID = copyStatePointer(s.Document.ActiveRevisionID)
	s.Document.ActiveGenerationSetID = copyStatePointer(s.Document.ActiveGenerationSetID)
	s.Document.DeletedAt = copyStatePointer(s.Document.DeletedAt)
	s.PublicationReview = copyStatePointer(s.PublicationReview)
	s.GenerationSet = copyStatePointer(s.GenerationSet)
	if s.GenerationSet != nil && s.GenerationSet.Channels != nil {
		channels := make(map[contracts.Channel]contracts.IndexGeneration, len(s.GenerationSet.Channels))
		for channel, g := range s.GenerationSet.Channels {
			g.EmbeddingSpec = copyStatePointer(g.EmbeddingSpec)
			g.BackendBinding = copyStatePointer(g.BackendBinding)
			if g.BackendBinding != nil {
				g.BackendBinding.GenerationID = copyStatePointer(g.BackendBinding.GenerationID)
			}
			g.Readiness = copyStatePointer(g.Readiness)
			if g.Readiness != nil {
				g.Readiness.Generations = slices.Clone(g.Readiness.Generations)
				for n := range g.Readiness.Generations {
					r := &g.Readiness.Generations[n]
					r.EmbeddingSpecDigest = copyStatePointer(r.EmbeddingSpecDigest)
				}
			}
			channels[channel] = g
		}
		s.GenerationSet.Channels = channels
	}
	return s
}

func copyStatePointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
