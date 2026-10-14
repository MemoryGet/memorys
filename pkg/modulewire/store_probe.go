package modulewire

import (
	"github.com/MemoryGet/memorys/pkg/contracts"
	"time"
)

type probeBindingRecord struct {
	CheckID, ProbeID, InvocationID, RequestID, BindingID contracts.ID
	ControlDomainID, ScopeID, SubjectID                  contracts.ID
	BindingVersion, PolicyVersion                        uint64
	ExpectedBindingDigest, RequestProfileDigest          contracts.Digest
	Endpoint                                             contracts.StoreEndpointBinding
	IssuedAtNS, DeadlineNS                               int64
}
type probeRequestContext struct {
	RequestID     contracts.ID
	ProfileDigest contracts.Digest
	DeadlineNS    int64
}
type probeScopeRecord struct {
	ID, SubjectID, DomainID contracts.ID
	Action                  contracts.Action
	PolicyVersion           uint64
	ExpiresAtNS             int64
}
type probeRequestRecord struct {
	Request probeRequestContext
	Scope   probeScopeRecord
	Binding probeBindingRecord
}
type probeObservationRecord struct {
	Binding      probeBindingRecord
	Observation  contracts.StoreObservation
	ObservedAtNS int64
}
type probeReviewRecord struct {
	SchemaVersion int
	Expectation   contracts.StoreBindingExpectation
	Request       probeRequestRecord
	Observation   probeObservationRecord
}

func init() {
	register(probeBindingRecord{}, "CheckID:check_id ProbeID:probe_id InvocationID:invocation_id RequestID:request_id BindingID:binding_id ControlDomainID:control_domain_id ScopeID:scope_id SubjectID:subject_id BindingVersion:binding_version PolicyVersion:policy_version ExpectedBindingDigest:expected_binding_digest RequestProfileDigest:request_profile_digest Endpoint:endpoint IssuedAtNS:issued_at_ns DeadlineNS:deadline_ns")
	register(probeRequestContext{}, "RequestID:request_id ProfileDigest:profile_digest DeadlineNS:deadline_ns")
	register(probeScopeRecord{}, "ID:id SubjectID:subject_id DomainID:domain_id Action:action PolicyVersion:policy_version ExpiresAtNS:expires_at_ns")
	register(probeRequestRecord{}, "Request:request Scope:scope Binding:binding")
	register(probeObservationRecord{}, "Binding:binding Observation:observation ObservedAtNS:observed_at_ns")
	register(probeReviewRecord{}, "SchemaVersion:schema_version Expectation:expectation Request:request Observation:observation")
}

func positiveNS(v time.Time) bool { n := v.UnixNano(); return n > 0 && time.Unix(0, n).Equal(v) }
func bindingToRecord(b contracts.StoreProbeBinding) probeBindingRecord {
	return probeBindingRecord{b.CheckID, b.ProbeID, b.InvocationID, b.RequestID, b.BindingID, b.ControlDomainID, b.ScopeID, b.SubjectID, b.BindingVersion, b.PolicyVersion, b.ExpectedBindingDigest, b.RequestProfileDigest, b.Endpoint, b.IssuedAt.UnixNano(), b.Deadline.UnixNano()}
}
func bindingFromRecord(b probeBindingRecord) contracts.StoreProbeBinding {
	return contracts.StoreProbeBinding{CheckID: b.CheckID, ProbeID: b.ProbeID, InvocationID: b.InvocationID, RequestID: b.RequestID, BindingID: b.BindingID, ControlDomainID: b.ControlDomainID, ScopeID: b.ScopeID, SubjectID: b.SubjectID, BindingVersion: b.BindingVersion, PolicyVersion: b.PolicyVersion, ExpectedBindingDigest: b.ExpectedBindingDigest, RequestProfileDigest: b.RequestProfileDigest, Endpoint: b.Endpoint, IssuedAt: time.Unix(0, b.IssuedAtNS), Deadline: time.Unix(0, b.DeadlineNS)}
}

// Offline bound-probe codecs do not authenticate the clock, expectation or peer.
func EncodeBoundStoreProbeReview(expected contracts.StoreBindingExpectation, r contracts.BoundStoreProbeRequest, a contracts.BoundStoreObservation, now time.Time) ([]byte, error) {
	for _, value := range []time.Time{r.Request.Deadline, r.Scope.ExpiresAt, r.Binding.IssuedAt, r.Binding.Deadline, a.Binding.IssuedAt, a.Binding.Deadline, a.ObservedAt, now} {
		if !positiveNS(value) {
			return nil, invalid()
		}
	}
	if contracts.ValidateBoundStoreObservation(r, expected, a, now) != nil {
		return nil, invalid()
	}
	request := probeRequestRecord{probeRequestContext{r.Request.RequestID, r.Request.ProfileDigest, r.Request.Deadline.UnixNano()}, probeScopeRecord{r.Scope.ID, r.Scope.SubjectID, r.Scope.DomainID, r.Scope.Action, r.Scope.PolicyVersion, r.Scope.ExpiresAt.UnixNano()}, bindingToRecord(r.Binding)}
	observation := probeObservationRecord{bindingToRecord(a.Binding), a.Observation, a.ObservedAt.UnixNano()}
	return encode(probeReviewRecord{1, expected, request, observation})
}

func DecodeBoundStoreProbeReview(raw []byte, now time.Time) (contracts.StoreBindingExpectation, contracts.BoundStoreProbeRequest, contracts.BoundStoreObservation, error) {
	var wire probeReviewRecord
	fail := func() (contracts.StoreBindingExpectation, contracts.BoundStoreProbeRequest, contracts.BoundStoreObservation, error) {
		return contracts.StoreBindingExpectation{}, contracts.BoundStoreProbeRequest{}, contracts.BoundStoreObservation{}, invalid()
	}
	if decode(raw, &wire) != nil || wire.SchemaVersion != 1 || !strictStoreIntegers(raw) {
		return fail()
	}
	for _, n := range []int64{wire.Request.Request.DeadlineNS, wire.Request.Scope.ExpiresAtNS, wire.Request.Binding.IssuedAtNS, wire.Request.Binding.DeadlineNS, wire.Observation.Binding.IssuedAtNS, wire.Observation.Binding.DeadlineNS, wire.Observation.ObservedAtNS} {
		if n <= 0 {
			return fail()
		}
	}
	r := contracts.BoundStoreProbeRequest{Request: contracts.RequestContext{RequestID: wire.Request.Request.RequestID, ProfileDigest: wire.Request.Request.ProfileDigest, Deadline: time.Unix(0, wire.Request.Request.DeadlineNS)}, Scope: contracts.DeploymentScope{ID: wire.Request.Scope.ID, SubjectID: wire.Request.Scope.SubjectID, DomainID: wire.Request.Scope.DomainID, Action: wire.Request.Scope.Action, PolicyVersion: wire.Request.Scope.PolicyVersion, ExpiresAt: time.Unix(0, wire.Request.Scope.ExpiresAtNS)}, Binding: bindingFromRecord(wire.Request.Binding)}
	a := contracts.BoundStoreObservation{Binding: bindingFromRecord(wire.Observation.Binding), Observation: wire.Observation.Observation, ObservedAt: time.Unix(0, wire.Observation.ObservedAtNS)}
	if _, err := EncodeBoundStoreProbeReview(wire.Expectation, r, a, now); err != nil {
		return fail()
	}
	return wire.Expectation, r, a, nil
}
