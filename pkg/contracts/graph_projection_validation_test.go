package contracts_test

import (
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func graphTarget() contracts.GraphProjectionTarget {
	d := contracts.Digest(strings.Repeat("a", 64))
	return contracts.GraphProjectionTarget{SpaceID: "space", KnowledgeBaseID: "kb", BackendEpochID: "epoch", GenerationID: "generation", IndexProfileDigest: d, ExtractionProfileDigest: d, OntologyDigest: d, SourceSnapshotDigest: d}
}

func TestGraphTargetNeedsAllBindings(t *testing.T) {
	r := graphTarget()
	if err := contracts.ValidateGraphProjectionTarget(r); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*contracts.GraphProjectionTarget){
		func(r *contracts.GraphProjectionTarget) { r.SpaceID = "" },
		func(r *contracts.GraphProjectionTarget) { r.BackendEpochID = "" },
		func(r *contracts.GraphProjectionTarget) { r.GenerationID = "" },
		func(r *contracts.GraphProjectionTarget) { r.SourceSnapshotDigest = "not-a-digest" },
	} {
		bad := r
		change(&bad)
		if contracts.ValidateGraphProjectionTarget(bad) == nil {
			t.Fatal("missing binding accepted")
		}
	}
}

func TestGraphProofCannotRebindTargetAttemptOrInput(t *testing.T) {
	target := graphTarget()
	input := contracts.Digest(strings.Repeat("b", 64))
	p := contracts.GraphProjectionProof{ProofID: "proof", AttemptID: "attempt", Target: target, InputDigest: input, State: "verified"}
	if err := contracts.ValidateGraphProjectionProof(target, "attempt", input, "verified", p); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*contracts.GraphProjectionProof){
		func(p *contracts.GraphProjectionProof) { p.Target.SpaceID = "other" },
		func(p *contracts.GraphProjectionProof) { p.Target.KnowledgeBaseID = "other" },
		func(p *contracts.GraphProjectionProof) { p.Target.BackendEpochID = "other" },
		func(p *contracts.GraphProjectionProof) { p.Target.GenerationID = "other" },
		func(p *contracts.GraphProjectionProof) { p.Target.IndexProfileDigest = input },
		func(p *contracts.GraphProjectionProof) { p.Target.ExtractionProfileDigest = input },
		func(p *contracts.GraphProjectionProof) { p.Target.OntologyDigest = input },
		func(p *contracts.GraphProjectionProof) { p.Target.SourceSnapshotDigest = input },
		func(p *contracts.GraphProjectionProof) { p.AttemptID = "other" },
		func(p *contracts.GraphProjectionProof) { p.InputDigest = target.IndexProfileDigest },
		func(p *contracts.GraphProjectionProof) { p.ProofID = "" },
		func(p *contracts.GraphProjectionProof) { p.State = "prepared" },
	} {
		bad := p
		change(&bad)
		if contracts.ValidateGraphProjectionProof(target, "attempt", input, "verified", bad) == nil {
			t.Fatal("rebound result accepted", bad)
		}
	}
	if contracts.ValidateGraphProjectionProof(target, "attempt", input, "active", p) == nil {
		t.Fatal("activation state accepted")
	}
}

func TestGraphCleanupCompletionRequiresEvidenceAndTime(t *testing.T) {
	target := graphTarget()
	now := time.Unix(1900000000, 1)
	p := contracts.GraphCleanupReceipt{OperationID: "operation", OperationKind: "source_invalidation", RequestDigest: graphCleanupExpectation(target).RequestDigest, Target: target, State: "completed", CompletedAt: &now, ProviderRecordReference: "evidence"}
	if err := contracts.ValidateGraphCleanupReceipt(graphCleanupExpectation(target), p); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*contracts.GraphCleanupReceipt){
		func(p *contracts.GraphCleanupReceipt) { p.CompletedAt = nil },
		func(p *contracts.GraphCleanupReceipt) { p.ProviderRecordReference = "" },
		func(p *contracts.GraphCleanupReceipt) { p.OperationID = "other" },
		func(p *contracts.GraphCleanupReceipt) { p.Target.GenerationID = "other" },
		func(p *contracts.GraphCleanupReceipt) { p.State = "released" },
	} {
		bad := p
		change(&bad)
		if contracts.ValidateGraphCleanupReceipt(graphCleanupExpectation(target), bad) == nil {
			t.Fatal("false completion accepted")
		}
	}
}

func TestGraphUnknownCleanupCannotClaimCompletion(t *testing.T) {
	target := graphTarget()
	now := time.Unix(1900000000, 1)
	for _, state := range []string{"queued", "running", "failed", "unknown"} {
		p := contracts.GraphCleanupReceipt{OperationID: "operation", OperationKind: "source_invalidation", RequestDigest: graphCleanupExpectation(target).RequestDigest, Target: target, State: state}
		if err := contracts.ValidateGraphCleanupReceipt(graphCleanupExpectation(target), p); err != nil {
			t.Fatal(err)
		}
		p.CompletedAt = &now
		if contracts.ValidateGraphCleanupReceipt(graphCleanupExpectation(target), p) == nil {
			t.Fatal("pending cleanup declared completed")
		}
	}
}

func graphCleanupExpectation(target contracts.GraphProjectionTarget) contracts.GraphCleanupExpectation {
	return contracts.GraphCleanupExpectation{Target: target, OperationID: "operation", OperationKind: "source_invalidation", RequestDigest: contracts.Digest(strings.Repeat("c", 64))}
}

func TestGraphCleanupBindsOperationKindAndFrozenRequest(t *testing.T) {
	expected := graphCleanupExpectation(graphTarget())
	now := time.Unix(1900000000, 1)
	receipt := contracts.GraphCleanupReceipt{OperationID: expected.OperationID, OperationKind: expected.OperationKind, RequestDigest: expected.RequestDigest, Target: expected.Target, State: "completed", CompletedAt: &now, ProviderRecordReference: "evidence"}
	if err := contracts.ValidateGraphCleanupReceipt(expected, receipt); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*contracts.GraphCleanupReceipt){
		func(r *contracts.GraphCleanupReceipt) { r.OperationKind = "generation_deletion" },
		func(r *contracts.GraphCleanupReceipt) { r.RequestDigest = contracts.Digest(strings.Repeat("d", 64)) },
		func(r *contracts.GraphCleanupReceipt) { r.RequestDigest = "" },
	} {
		bad := receipt
		change(&bad)
		if contracts.ValidateGraphCleanupReceipt(expected, bad) == nil {
			t.Fatal("foreign operation receipt accepted")
		}
	}
	for _, kind := range []string{"", "unknown", "source_invalidation"} {
		bad := expected
		bad.OperationKind = kind
		if kind == "source_invalidation" {
			bad.RequestDigest = ""
		}
		if contracts.ValidateGraphCleanupReceipt(bad, receipt) == nil {
			t.Fatal("invalid expectation accepted")
		}
	}
	expected.OperationKind = "generation_deletion"
	receipt.OperationKind = "generation_deletion"
	if err := contracts.ValidateGraphCleanupReceipt(expected, receipt); err != nil {
		t.Fatal(err)
	}
}
