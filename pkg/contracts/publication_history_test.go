package contracts_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func historyFixture() (contracts.PublicationHistory, contracts.CleanupPlan, contracts.GenerationSet) {
	revision, set, generation, epoch := contracts.ID("revision"), contracts.ID("set"), contracts.ID("generation"), contracts.ID("epoch")
	channel := contracts.KeywordChannel
	digest := contracts.Digest(strings.Repeat("a", 64))
	now := time.Now().UTC()
	h := contracts.PublicationHistory{ID: "history", OperationID: "operation", SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "doc", PreviousEpoch: 1, PublicationEpoch: 2, Action: "withdraw", PreviousRevisionID: &revision, PreviousGenerationSetID: &set, ActorID: "actor", Actor: contracts.Principal{SubjectID: "actor", SubjectType: "user", Issuer: "identity"}, Reason: "withdraw", OccurredAt: now, CommandDigest: digest}
	p := contracts.CleanupPlan{ID: "plan", HistoryID: h.ID, OperationID: h.OperationID, SpaceID: h.SpaceID, KnowledgeBaseID: h.KnowledgeBaseID, DocumentID: h.DocumentID, RevisionID: revision, GenerationSetID: set, PublicationEpoch: 1, ProfileDigest: digest, CreatedAt: now, Targets: []contracts.CleanupTarget{{ID: "target", Kind: "index", ModuleInstanceID: "engine", BindingReference: "frozen-binding", ProfileDigest: digest, GenerationID: &generation, BackendEpochID: &epoch, Channel: &channel}}}
	s := contracts.GenerationSet{ID: set, RevisionID: revision, BackendEpochID: epoch, Channels: map[contracts.Channel]contracts.IndexGeneration{channel: {ID: generation, KnowledgeBaseID: "kb", RevisionID: revision, BackendEpochID: epoch, Channel: channel, ProfileDigest: digest, BackendBinding: &contracts.BackendBinding{ModuleInstanceID: "engine", BackendID: "private-index"}}}}
	return h, p, s
}

func TestPublicationHistoryEpochActorAndRevisionPairs(t *testing.T) {
	h, _, _ := historyFixture()
	if err := contracts.ValidatePublicationHistory(h); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*contracts.PublicationHistory){func(h *contracts.PublicationHistory) { h.PreviousEpoch = math.MaxUint64; h.PublicationEpoch = 0 }, func(h *contracts.PublicationHistory) { h.PublicationEpoch = 4 }, func(h *contracts.PublicationHistory) { h.Actor.SubjectID = "other" }, func(h *contracts.PublicationHistory) { h.PreviousGenerationSetID = nil }, func(h *contracts.PublicationHistory) { h.Reason = "" }, func(h *contracts.PublicationHistory) { h.Action = "vendor" }, func(h *contracts.PublicationHistory) { h.OccurredAt = h.OccurredAt.In(time.FixedZone("local", 3600)) }, func(h *contracts.PublicationHistory) { h.OccurredAt = time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC) }, func(h *contracts.PublicationHistory) { h.OccurredAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }} {
		h, _, _ := historyFixture()
		change(&h)
		if contracts.ValidatePublicationHistory(h) == nil {
			t.Fatal("invalid history accepted")
		}
	}
	h, _, _ = historyFixture()
	h.Action = "publish"
	h.PreviousEpoch = 0
	h.PublicationEpoch = 1
	h.RevisionID = h.PreviousRevisionID
	h.GenerationSetID = h.PreviousGenerationSetID
	h.PreviousRevisionID = nil
	h.PreviousGenerationSetID = nil
	if err := contracts.ValidatePublicationHistory(h); err != nil {
		t.Fatal("first publication rejected", err)
	}
}

func TestWithdrawalCleanupBindsOldEpochAndImmutableTargets(t *testing.T) {
	h, p, s := historyFixture()
	if err := contracts.ValidateWithdrawalCleanup(p, h); err != nil {
		t.Fatal(err)
	}
	if err := contracts.ValidateCleanupGenerations(p, s); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*contracts.CleanupPlan){func(p *contracts.CleanupPlan) { p.PublicationEpoch = 2 }, func(p *contracts.CleanupPlan) { p.DocumentID = "other" }, func(p *contracts.CleanupPlan) { p.HistoryID = "other" }, func(p *contracts.CleanupPlan) { p.RevisionID = "new-version" }, func(p *contracts.CleanupPlan) { p.Targets = nil }, func(p *contracts.CleanupPlan) { p.Targets = append(p.Targets, p.Targets[0]) }, func(p *contracts.CleanupPlan) { p.Targets[0].BindingReference = "" }, func(p *contracts.CleanupPlan) { p.Targets[0].Kind = "blob" }, func(p *contracts.CleanupPlan) { p.Targets[0].BackendEpochID = nil }} {
		h, p, _ := historyFixture()
		change(&p)
		if contracts.ValidateWithdrawalCleanup(p, h) == nil {
			t.Fatal("unbound cleanup plan accepted")
		}
	}
}

func TestCleanupIndexCoverageRejectsWrongAndDuplicateChannels(t *testing.T) {
	for _, change := range []func(*contracts.CleanupPlan){func(p *contracts.CleanupPlan) { g := contracts.ID("other"); p.Targets[0].GenerationID = &g }, func(p *contracts.CleanupPlan) { p.Targets[0].ModuleInstanceID = "other" }, func(p *contracts.CleanupPlan) { p.Targets[0].ProfileDigest = contracts.Digest(strings.Repeat("b", 64)) }, func(p *contracts.CleanupPlan) { p.Targets = nil }, func(p *contracts.CleanupPlan) { t := p.Targets[0]; t.ID = "second"; p.Targets = append(p.Targets, t) }} {
		_, p, s := historyFixture()
		change(&p)
		if contracts.ValidateCleanupGenerations(p, s) == nil {
			t.Fatal("incorrect index target coverage accepted")
		}
	}
}
