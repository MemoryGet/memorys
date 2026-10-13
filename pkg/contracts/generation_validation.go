package contracts

// GenerationExpectation must come from authoritative revision/profile/epoch
// records, not the engine result or a client's request. This checker validates
// relationships only; readiness must also be verified against the live backend.
type GenerationExpectation struct {
	KnowledgeBaseID     ID
	RevisionID          ID
	BackendEpochID      ID
	ProfileDigest       Digest
	ContentDigest       Digest
	RequiredChannels    []Channel
	EmbeddingSpec       *EmbeddingSpec
	EmbeddingSpecDigest *Digest
}

func invalidGeneration(message string) error {
	return PublicError{Code: "validation_failed", SafeMessage: message}
}

func validDigest(value Digest) bool {
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

func knownChannel(channel Channel) bool {
	return channel == KeywordChannel || channel == VectorChannel || channel == GraphChannel
}

// ValidateReadyGenerationSet does not approve, authorize or publish anything.
// It must run before the application's transactional publication CAS, which
// separately checks current authorization, approvals and worker fencing.
func ValidateReadyGenerationSet(set GenerationSet, expected GenerationExpectation) error {
	if expected.KnowledgeBaseID == "" || expected.RevisionID == "" || expected.BackendEpochID == "" ||
		!validDigest(expected.ProfileDigest) || !validDigest(expected.ContentDigest) || len(expected.RequiredChannels) == 0 {
		return invalidGeneration("Missing authoritative generation expectation")
	}
	if set.ID == "" || set.State != "ready" || set.RevisionID != expected.RevisionID || set.BackendEpochID != expected.BackendEpochID {
		return invalidGeneration("Generation set is not ready for the expected revision and backend epoch")
	}
	required := make(map[Channel]bool)
	for _, channel := range expected.RequiredChannels {
		if !knownChannel(channel) || required[channel] {
			return invalidGeneration("Invalid required channel declaration")
		}
		required[channel] = true
		if _, found := set.Channels[channel]; !found {
			return invalidGeneration("Required index channel is missing")
		}
	}
	seenIDs := make(map[ID]bool)
	for channel, generation := range set.Channels {
		if !knownChannel(channel) || generation.Channel != channel || generation.ID == "" || seenIDs[generation.ID] {
			return invalidGeneration("Index generation identity or channel is invalid")
		}
		seenIDs[generation.ID] = true
		if err := validateGeneration(generation, set, expected); err != nil {
			return err
		}
	}
	return nil
}

func validateGeneration(generation IndexGeneration, set GenerationSet, expected GenerationExpectation) error {
	if generation.State != "ready" || generation.KnowledgeBaseID != expected.KnowledgeBaseID ||
		generation.RevisionID != expected.RevisionID || generation.BackendEpochID != expected.BackendEpochID || generation.ProfileDigest != expected.ProfileDigest {
		return invalidGeneration("Index generation does not match the ready publication target")
	}
	binding := generation.BackendBinding
	if binding == nil || binding.ModuleInstanceID == "" || binding.BackendID == "" || binding.GenerationID == nil || *binding.GenerationID != generation.ID {
		return invalidGeneration("Generation backend binding is missing or inconsistent")
	}
	if generation.Channel == VectorChannel {
		if expected.EmbeddingSpec == nil || generation.EmbeddingSpec == nil || *expected.EmbeddingSpec != *generation.EmbeddingSpec ||
			expected.EmbeddingSpec.Dimension < 1 || expected.EmbeddingSpec.ProviderModel == "" || expected.EmbeddingSpec.ModelRevision == "" ||
			expected.EmbeddingSpecDigest == nil || !validDigest(*expected.EmbeddingSpecDigest) {
			return invalidGeneration("Vector generation embedding specification does not match")
		}
	}
	return validateGenerationProof(generation, set, expected)
}

func validateGenerationProof(generation IndexGeneration, set GenerationSet, expected GenerationExpectation) error {
	proof := generation.Readiness
	if proof == nil || proof.GenerationSetID != set.ID || proof.AttemptID == "" || proof.ProfileDigest != expected.ProfileDigest || proof.ContentDigest != expected.ContentDigest {
		return invalidGeneration("Readiness proof does not match the generation set and content")
	}
	found := false
	seen := make(map[ID]bool)
	for _, member := range proof.Generations {
		mapped, exists := set.Channels[member.Channel]
		if !exists || mapped.ID != member.GenerationID || seen[member.GenerationID] || member.BackendEpochID != expected.BackendEpochID ||
			member.ChunkCount < 0 || (member.Channel != GraphChannel && member.ChunkCount == 0) {
			return invalidGeneration("Readiness proof contains an invalid channel generation")
		}
		seen[member.GenerationID] = true
		if member.Channel == VectorChannel && (member.EmbeddingSpecDigest == nil || expected.EmbeddingSpecDigest == nil || *member.EmbeddingSpecDigest != *expected.EmbeddingSpecDigest) {
			return invalidGeneration("Readiness proof embedding digest does not match")
		}
		if member.GenerationID == generation.ID {
			found = true
		}
	}
	if !found {
		return invalidGeneration("Readiness proof does not include this index generation")
	}
	return nil
}
