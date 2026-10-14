package modulewire

import "github.com/MemoryGet/memorys/pkg/contracts"

func contains[T comparable](values []T, value T) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func checkRetrieve(request contracts.RetrieveRequest) error {
	filter := request.Publication
	if filter.SpaceID != request.Scope.SpaceID || len(filter.KnowledgeBaseIDs) == 0 || len(filter.RevisionIDs) == 0 ||
		len(filter.GenerationIDs) == 0 || len(request.Generations) == 0 || len(request.Channels) == 0 ||
		request.Limit < 1 || request.Limit > 1000 || request.BackendEpochID == "" || request.RouteID == "" {
		return invalid()
	}
	for _, kb := range filter.KnowledgeBaseIDs {
		if !contains(request.Scope.KnowledgeBaseIDs, kb) {
			return invalid()
		}
	}
	if (len(request.Query.Vector) > 0) != (request.Query.EmbeddingSpec != nil) {
		return invalid()
	}
	if request.Query.EmbeddingSpec != nil && len(request.Query.Vector) != request.Query.EmbeddingSpec.Dimension {
		return invalid()
	}
	ids := make(map[contracts.ID]bool)
	for _, generation := range request.Generations {
		if generation.ID == "" || ids[generation.ID] || !contains(filter.GenerationIDs, generation.ID) || !contains(filter.RevisionIDs, generation.RevisionID) ||
			!contains(filter.KnowledgeBaseIDs, generation.KnowledgeBaseID) || generation.BackendEpochID != request.BackendEpochID ||
			!contains(request.Channels, generation.Channel) {
			return invalid()
		}
		ids[generation.ID] = true
		if generation.Channel != contracts.VectorChannel && generation.Channel != contracts.KeywordChannel && generation.Channel != contracts.GraphChannel {
			return invalid()
		}
		if generation.Channel == contracts.VectorChannel && (generation.EmbeddingSpec == nil || generation.EmbeddingSpec.Dimension < 1) {
			return invalid()
		}
		if generation.Channel == contracts.VectorChannel && request.Query.EmbeddingSpec != nil &&
			(generation.EmbeddingSpec == nil || *generation.EmbeddingSpec != *request.Query.EmbeddingSpec) {
			return invalid()
		}
	}
	for _, channel := range request.Channels {
		found := false
		for _, generation := range request.Generations {
			if generation.Channel == channel {
				found = true
			}
		}
		if !found {
			return invalid()
		}
	}
	return nil
}

func checkCandidate(candidate contracts.Candidate, request contracts.RetrieveRequest) error {
	if candidate.FusionOrigins != nil {
		return invalid()
	} // Raw retrievers cannot invent platform fusion provenance.
	if candidate.RouteID != request.RouteID || candidate.Rank < 1 || candidate.ScoreKind == "" ||
		candidate.Citation.DocumentID == "" || candidate.Citation.BlockID == "" || len(candidate.GenerationIDs) == 0 ||
		!contains(request.Publication.KnowledgeBaseIDs, candidate.KnowledgeBaseID) ||
		!contains(request.Publication.RevisionIDs, candidate.Citation.RevisionID) {
		return invalid()
	}
	seen := make(map[contracts.ID]bool)
	for _, id := range candidate.GenerationIDs {
		found := false
		for _, generation := range request.Generations {
			if generation.ID == id && generation.KnowledgeBaseID == candidate.KnowledgeBaseID && generation.RevisionID == candidate.Citation.RevisionID {
				found = true
			}
		}
		if !found || seen[id] {
			return invalid()
		}
		seen[id] = true
	}
	return nil
}
