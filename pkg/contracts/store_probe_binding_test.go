package contracts_test

import (
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func probeFixture() (contracts.BoundStoreProbeRequest, contracts.StoreBindingExpectation, contracts.BoundStoreObservation, time.Time) {
	e, observations := storeFixture()
	now := time.Unix(1900000000, 123)
	b := contracts.StoreProbeBinding{CheckID: "check", ProbeID: "probe", InvocationID: "invoke", RequestID: "request", BindingID: e.BindingID, ControlDomainID: "domain", ScopeID: "scope", SubjectID: "operator", BindingVersion: e.Identity.BindingVersion, PolicyVersion: 1, ExpectedBindingDigest: contracts.Digest(strings.Repeat("b", 64)), RequestProfileDigest: contracts.Digest(strings.Repeat("c", 64)), Endpoint: e.Endpoints[0], IssuedAt: now.Add(-time.Second), Deadline: now.Add(time.Second)}
	r := contracts.BoundStoreProbeRequest{Request: contracts.RequestContext{RequestID: b.RequestID, ProfileDigest: b.RequestProfileDigest, Deadline: b.Deadline}, Scope: contracts.DeploymentScope{ID: b.ScopeID, SubjectID: b.SubjectID, DomainID: b.ControlDomainID, Action: "metadata.store.observe", PolicyVersion: 1, ExpiresAt: b.Deadline}, Binding: b}
	return r, e, contracts.BoundStoreObservation{Binding: b, Observation: observations[0], ObservedAt: now}, now
}

func TestBoundProbeAcceptsGivenBindingAndRejectsOldInvocation(t *testing.T) {
	r, e, o, n := probeFixture()
	if err := contracts.ValidateBoundStoreObservation(r, e, o, n); err != nil {
		t.Fatal(err)
	}
	r.Binding.ProbeID = "new-probe"
	r.Binding.InvocationID = "new-invocation"
	if contracts.ValidateBoundStoreObservation(r, e, o, n) == nil {
		t.Fatal("old observation reused for new probe")
	}
}

func TestBoundProbeRejectsEndpointDigestAndBindingMismatch(t *testing.T) {
	for _, change := range []func(*contracts.BoundStoreObservation){func(o *contracts.BoundStoreObservation) {
		o.Binding.ExpectedBindingDigest = contracts.Digest(strings.Repeat("d", 64))
	}, func(o *contracts.BoundStoreObservation) { o.Binding.Endpoint.ModuleInstanceID = "wrong" }, func(o *contracts.BoundStoreObservation) { o.Binding.BindingVersion-- }, func(o *contracts.BoundStoreObservation) { o.Observation.Identity.StoreEpochID = "backup" }} {
		r, e, o, n := probeFixture()
		change(&o)
		if contracts.ValidateBoundStoreObservation(r, e, o, n) == nil {
			t.Fatal("invalid bound observation accepted")
		}
	}
}

func TestBoundProbeRejectsScopeAndRequestMutation(t *testing.T) {
	for _, change := range []func(*contracts.BoundStoreProbeRequest){func(r *contracts.BoundStoreProbeRequest) { r.Scope.Action = "knowledge.search" }, func(r *contracts.BoundStoreProbeRequest) { r.Scope.DomainID = "other" }, func(r *contracts.BoundStoreProbeRequest) { r.Scope.PolicyVersion++ }, func(r *contracts.BoundStoreProbeRequest) {
		r.Request.Deadline = r.Request.Deadline.Add(time.Nanosecond)
	}, func(r *contracts.BoundStoreProbeRequest) {
		r.Request.ProfileDigest = contracts.Digest(strings.Repeat("e", 64))
	}} {
		r, e, o, n := probeFixture()
		change(&r)
		if contracts.ValidateBoundStoreObservation(r, e, o, n) == nil {
			t.Fatal("mutated request accepted")
		}
	}
}

func TestBoundProbeExactTemporalWindow(t *testing.T) {
	r, e, o, n := probeFixture()
	if contracts.ValidateBoundStoreObservation(r, e, o, r.Binding.Deadline) == nil {
		t.Fatal("deadline equality allowed")
	}
	o.ObservedAt = r.Binding.IssuedAt.Add(-time.Nanosecond)
	if contracts.ValidateBoundStoreObservation(r, e, o, n) == nil {
		t.Fatal("pre-request observation accepted")
	}
	o.ObservedAt = n.Add(time.Nanosecond)
	if contracts.ValidateBoundStoreObservation(r, e, o, n) == nil {
		t.Fatal("future observation accepted")
	}
}
