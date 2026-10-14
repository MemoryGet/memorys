package contracts

import (
	"context"
	"time"
)

// Separate draft from the unbound offline-review format. The controller must
// allocate unique invocation/probe IDs and validate authorization independently.
type StoreProbeBinding struct {
	CheckID, ProbeID, InvocationID, RequestID, BindingID ID
	ControlDomainID, ScopeID, SubjectID                  ID
	BindingVersion, PolicyVersion                        uint64
	ExpectedBindingDigest, RequestProfileDigest          Digest
	Endpoint                                             StoreEndpointBinding
	IssuedAt, Deadline                                   time.Time
}

type BoundStoreProbeRequest struct {
	Request RequestContext
	Scope   DeploymentScope
	Binding StoreProbeBinding
}

type BoundStoreObservation struct {
	Binding     StoreProbeBinding
	Observation StoreObservation
	ObservedAt  time.Time
}

type BoundStoreIdentityReader interface {
	ObserveBoundStore(context.Context, BoundStoreProbeRequest) (BoundStoreObservation, error)
}

func sameStoreProbeBinding(a, b StoreProbeBinding) bool {
	if !a.IssuedAt.Equal(b.IssuedAt) || !a.Deadline.Equal(b.Deadline) {
		return false
	}
	a.IssuedAt = time.Time{}
	a.Deadline = time.Time{}
	b.IssuedAt = time.Time{}
	b.Deadline = time.Time{}
	return a == b
}

// ValidateBoundStoreObservation checks supplied binding/identity/window only.
// It does not authenticate the service, prove current policy or journal coverage,
// recompute the independent expectation digest, or consume a replay ledger.
func ValidateBoundStoreObservation(request BoundStoreProbeRequest, expected StoreBindingExpectation, actual BoundStoreObservation, now time.Time) error {
	invalid := func() error {
		return PublicError{Code: "validation_failed", SafeMessage: "Invalid bound store observation"}
	}
	if ValidateStoreBindingExpectation(expected) != nil {
		return invalid()
	}
	b := request.Binding
	for _, id := range []ID{b.CheckID, b.ProbeID, b.InvocationID, b.RequestID, b.BindingID, b.ControlDomainID, b.ScopeID, b.SubjectID} {
		if !storeText(string(id)) {
			return invalid()
		}
	}
	if !validDigest(b.ExpectedBindingDigest) || !validDigest(b.RequestProfileDigest) || b.BindingID != expected.BindingID || b.BindingVersion != expected.Identity.BindingVersion || b.PolicyVersion == 0 {
		return invalid()
	}
	if now.IsZero() || b.IssuedAt.IsZero() || b.Deadline.IsZero() || b.IssuedAt.After(now) || !b.IssuedAt.Before(b.Deadline) || !now.Before(b.Deadline) {
		return invalid()
	}
	if request.Request.RequestID != b.RequestID || request.Request.ProfileDigest != b.RequestProfileDigest || !request.Request.Deadline.Equal(b.Deadline) {
		return invalid()
	}
	s := request.Scope
	if s.ID != b.ScopeID || s.SubjectID != b.SubjectID || s.DomainID != b.ControlDomainID || s.PolicyVersion != b.PolicyVersion || s.Action != "metadata.store.observe" || s.ExpiresAt.Before(b.Deadline) {
		return invalid()
	}
	allowed := false
	for _, endpoint := range expected.Endpoints {
		if endpoint == b.Endpoint {
			allowed = true
		}
	}
	if !allowed {
		return invalid()
	}
	if !sameStoreProbeBinding(b, actual.Binding) || actual.ObservedAt.IsZero() || actual.ObservedAt.Before(b.IssuedAt) || actual.ObservedAt.After(now) {
		return invalid()
	}
	o := actual.Observation
	if o.BindingID != expected.BindingID || o.Identity != expected.Identity || o.Endpoint != b.Endpoint || !storeText(string(o.ObservationID)) || !storeText(o.CurrentCursor) {
		return invalid()
	}
	return nil
}
