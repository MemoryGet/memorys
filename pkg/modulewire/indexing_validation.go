package modulewire

import (
	"regexp"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var projectionVersion = regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+$`)

func validateTargets(scope contracts.AccessScope, setID, operationID, attemptID contracts.ID, fence uint64, targets []contracts.IndexGeneration, chunks []contracts.Chunk) error {
	if ensureIDs(setID, operationID, attemptID) != nil || fence == 0 || len(targets) == 0 || len(chunks) == 0 {
		return invalid()
	}
	first := targets[0]
	ids := make(map[contracts.ID]bool)
	channels := make(map[contracts.Channel]bool)
	for _, target := range targets {
		if target.ID == "" || target.State != "building" || ids[target.ID] || channels[target.Channel] ||
			target.KnowledgeBaseID != first.KnowledgeBaseID || target.RevisionID != first.RevisionID || target.BackendEpochID != first.BackendEpochID ||
			target.ProfileDigest != first.ProfileDigest || !contains(scope.KnowledgeBaseIDs, target.KnowledgeBaseID) {
			return invalid()
		}
		if target.Channel != contracts.KeywordChannel && target.Channel != contracts.VectorChannel && target.Channel != contracts.GraphChannel {
			return invalid()
		}
		if target.Channel == contracts.VectorChannel && (target.EmbeddingSpec == nil || target.EmbeddingSpec.Dimension < 1 || target.EmbeddingSpec.Dimension > 65536) {
			return invalid()
		}
		ids[target.ID] = true
		channels[target.Channel] = true
	}
	if ensureIDs(first.RevisionID, first.BackendEpochID) != nil || first.ProfileDigest == "" {
		return invalid()
	}
	seen := make(map[contracts.ID]bool)
	for _, chunk := range chunks {
		if chunk.ID == "" || seen[chunk.ID] || chunk.RevisionID != first.RevisionID || len(chunk.Spans) == 0 {
			return invalid()
		}
		seen[chunk.ID] = true
		for _, span := range chunk.Spans {
			if span.BlockID == "" || (span.Characters != nil && (span.Characters.Start < 0 || span.Characters.End < span.Characters.Start)) {
				return invalid()
			}
		}
	}
	return nil
}

func validateIndexInput(request contracts.IndexPrepareRequest) error {
	if err := validateTargets(request.Scope, request.GenerationSetID, request.OperationID, request.AttemptID, request.FencingToken, request.Generations, request.Chunks); err != nil {
		return err
	}
	dimension := 0
	for _, target := range request.Generations {
		if target.Channel == contracts.VectorChannel {
			dimension = target.EmbeddingSpec.Dimension
		}
	}
	if dimension == 0 {
		if len(request.Vectors) != 0 {
			return invalid()
		}
		return nil
	}
	if len(request.Vectors) != len(request.Chunks) {
		return invalid()
	}
	for _, vector := range request.Vectors {
		if len(vector) != dimension {
			return invalid()
		}
	}
	return nil
}

func validateCompositeInput(request contracts.CompositePrepareRequest) error {
	if !projectionVersion.MatchString(request.ProjectionFormatVersion) {
		return invalid()
	}
	return validateTargets(request.Scope, request.GenerationSetID, request.OperationID, request.AttemptID, request.FencingToken, request.Generations, request.Chunks)
}

func validatePrepareResult(result PrepareResult, expected PrepareExpectation) error {
	if !validPreparationExpectation(expected) {
		return invalid()
	}
	proof := result.Proof
	if expected.GenerationSetID == "" || expected.AttemptID == "" || expected.ContentDigest == "" || len(expected.Generations) == 0 ||
		proof.GenerationSetID != expected.GenerationSetID || proof.AttemptID != expected.AttemptID || proof.ContentDigest != expected.ContentDigest ||
		proof.ProfileDigest != expected.Generations[0].ProfileDigest || len(proof.Generations) != len(expected.Generations) || len(result.Bindings) != len(expected.Generations) {
		return invalid()
	}
	seen, bound := make(map[contracts.ID]bool), make(map[contracts.ID]bool)
	for _, readiness := range proof.Generations {
		if seen[readiness.GenerationID] || !matchingReadiness(readiness, expected) {
			return invalid()
		}
		seen[readiness.GenerationID] = true
	}
	for _, binding := range result.Bindings {
		if !seen[binding.GenerationID] || bound[binding.GenerationID] || binding.OpaqueBackendRef == "" {
			return invalid()
		}
		bound[binding.GenerationID] = true
	}
	return nil
}

func validPreparationExpectation(expected PrepareExpectation) bool {
	if len(expected.Generations) == 0 || len(expected.ChunkCounts) != len(expected.Generations) || !digestValue(expected.ContentDigest) {
		return false
	}
	first := expected.Generations[0]
	if ensureIDs(first.ID, first.RevisionID, first.KnowledgeBaseID, first.BackendEpochID) != nil || !digestValue(first.ProfileDigest) {
		return false
	}
	seen := make(map[contracts.ID]bool)
	for _, target := range expected.Generations {
		count, found := expected.ChunkCounts[target.ID]
		if target.ID == "" || seen[target.ID] || !found || count < 0 || target.RevisionID != first.RevisionID ||
			target.KnowledgeBaseID != first.KnowledgeBaseID || target.BackendEpochID != first.BackendEpochID || target.ProfileDigest != first.ProfileDigest {
			return false
		}
		if target.Channel == contracts.VectorChannel && !digestValue(expected.EmbeddingSpecDigests[target.ID]) {
			return false
		}
		seen[target.ID] = true
	}
	return true
}

func digestValue(value contracts.Digest) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func matchingReadiness(value contracts.GenerationReadiness, expected PrepareExpectation) bool {
	count, found := expected.ChunkCounts[value.GenerationID]
	if !found || count < 0 || value.ChunkCount != count {
		return false
	}
	for _, target := range expected.Generations {
		if value.GenerationID != target.ID {
			continue
		}
		if value.Channel != target.Channel || value.BackendEpochID != target.BackendEpochID {
			return false
		}
		if value.Channel == contracts.VectorChannel {
			digest, found := expected.EmbeddingSpecDigests[target.ID]
			return found && digest != "" && value.EmbeddingSpecDigest != nil && *value.EmbeddingSpecDigest == digest
		}
		return true
	}
	return false
}
