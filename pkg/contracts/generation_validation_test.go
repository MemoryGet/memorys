package contracts_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func readySet() (contracts.GenerationSet, contracts.GenerationExpectation) {
	spec := contracts.EmbeddingSpec{ProviderModel: "fixture", ModelRevision: "1", Dimension: 128, Distance: "cosine", Normalization: "l2", PreprocessVersion: "1"}
	specDigest := contracts.Digest(strings.Repeat("a", 64))
	expect := contracts.GenerationExpectation{KnowledgeBaseID: "kb", RevisionID: "rev", BackendEpochID: "epoch", ProfileDigest: contracts.Digest(strings.Repeat("b", 64)),
		ContentDigest: contracts.Digest(strings.Repeat("c", 64)), RequiredChannels: []contracts.Channel{contracts.KeywordChannel, contracts.VectorChannel}, EmbeddingSpec: &spec, EmbeddingSpecDigest: &specDigest}
	set := contracts.GenerationSet{ID: "set", RevisionID: "rev", BackendEpochID: "epoch", State: "ready", Channels: map[contracts.Channel]contracts.IndexGeneration{}}
	proof := &contracts.ReadinessProof{GenerationSetID: "set", AttemptID: "attempt", ContentDigest: expect.ContentDigest, ProfileDigest: expect.ProfileDigest,
		Generations: []contracts.GenerationReadiness{{GenerationID: "keyword", Channel: contracts.KeywordChannel, BackendEpochID: "epoch", ChunkCount: 2},
			{GenerationID: "vector", Channel: contracts.VectorChannel, BackendEpochID: "epoch", ChunkCount: 2, EmbeddingSpecDigest: &specDigest}}}
	for channel, id := range map[contracts.Channel]contracts.ID{contracts.KeywordChannel: "keyword", contracts.VectorChannel: "vector"} {
		generation := contracts.IndexGeneration{ID: id, KnowledgeBaseID: "kb", RevisionID: "rev", BackendEpochID: "epoch", Channel: channel, State: "ready", ProfileDigest: expect.ProfileDigest,
			BackendBinding: &contracts.BackendBinding{ModuleInstanceID: "fixture", BackendID: string(id), GenerationID: &id}, Readiness: proof}
		if channel == contracts.VectorChannel {
			generation.EmbeddingSpec = &spec
		}
		set.Channels[channel] = generation
	}
	return set, expect
}

func change(set *contracts.GenerationSet, channel contracts.Channel, edit func(*contracts.IndexGeneration)) {
	value := set.Channels[channel]
	edit(&value)
	set.Channels[channel] = value
}

func TestReadyGenerationSetAcceptsConsistentCompositeProof(t *testing.T) {
	set, expected := readySet()
	if err := contracts.ValidateReadyGenerationSet(set, expected); err != nil {
		t.Fatal(err)
	}
}

func TestReadyGenerationSetAcceptsIndependentChannelProofs(t *testing.T) {
	set, expected := readySet()
	for channel, generation := range set.Channels {
		proof := *generation.Readiness
		proof.AttemptID = contracts.ID("attempt-" + string(channel))
		for _, member := range proof.Generations {
			if member.Channel == channel {
				proof.Generations = []contracts.GenerationReadiness{member}
				break
			}
		}
		generation.Readiness = &proof
		set.Channels[channel] = generation
	}
	if err := contracts.ValidateReadyGenerationSet(set, expected); err != nil {
		t.Fatal(err)
	}
}

func TestReadyGenerationSetRejectsUnsafePublicationTargets(t *testing.T) {
	tests := []struct {
		name string
		edit func(*contracts.GenerationSet, *contracts.GenerationExpectation)
	}{
		{"missing channel", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) {
			delete(s.Channels, contracts.VectorChannel)
		}},
		{"draft revision", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) { s.RevisionID = "draft" }},
		{"epoch changed", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) { s.BackendEpochID = "old" }},
		{"not ready", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) { s.State = "building" }},
		{"foreign KB", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) {
			change(s, contracts.VectorChannel, func(g *contracts.IndexGeneration) { g.KnowledgeBaseID = "other" })
		}},
		{"mixed revision", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) {
			change(s, contracts.VectorChannel, func(g *contracts.IndexGeneration) { g.RevisionID = "other" })
		}},
		{"wrong dimension", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) {
			change(s, contracts.VectorChannel, func(g *contracts.IndexGeneration) {
				spec := *g.EmbeddingSpec
				spec.Dimension = 256
				g.EmbeddingSpec = &spec
			})
		}},
		{"no backend mapping", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) {
			change(s, contracts.VectorChannel, func(g *contracts.IndexGeneration) { g.BackendBinding = nil })
		}},
		{"wrong channel key", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) {
			change(s, contracts.VectorChannel, func(g *contracts.IndexGeneration) { g.Channel = contracts.KeywordChannel })
		}},
		{"missing proof", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) {
			change(s, contracts.VectorChannel, func(g *contracts.IndexGeneration) { g.Readiness = nil })
		}},
		{"different set proof", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) {
			s.Channels[contracts.VectorChannel].Readiness.GenerationSetID = "other"
		}},
		{"changed content", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) {
			s.Channels[contracts.VectorChannel].Readiness.ContentDigest = contracts.Digest(strings.Repeat("d", 64))
		}},
		{"missing member proof", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) {
			s.Channels[contracts.VectorChannel].Readiness.Generations = s.Channels[contracts.VectorChannel].Readiness.Generations[:1]
		}},
		{"empty vector index", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) {
			s.Channels[contracts.VectorChannel].Readiness.Generations[1].ChunkCount = 0
		}},
		{"spec digest changed", func(s *contracts.GenerationSet, _ *contracts.GenerationExpectation) {
			digest := contracts.Digest(strings.Repeat("d", 64))
			s.Channels[contracts.VectorChannel].Readiness.Generations[1].EmbeddingSpecDigest = &digest
		}},
		{"untrusted empty expectation", func(_ *contracts.GenerationSet, e *contracts.GenerationExpectation) { e.RequiredChannels = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set, expected := readySet()
			test.edit(&set, &expected)
			err := contracts.ValidateReadyGenerationSet(set, expected)
			var failure contracts.PublicError
			if err == nil || !errors.As(err, &failure) || failure.Code != "validation_failed" {
				t.Fatalf("expected safe contract rejection, got %v", err)
			}
		})
	}
}
