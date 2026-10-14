package contracts

import (
	"math"
	"slices"
	"time"
)

type ProbeCheckMember struct {
	ProbeID, InvocationID ID
	Endpoint              StoreEndpointBinding
}

type StoreProbeCheck struct {
	ControlDomainID, CheckID, BindingID, ControllerID ID
	ExpectedBindingDigest                             Digest
	BindingVersion, FencingToken, RowVersion          uint64
	Members                                           []ProbeCheckMember // Immutable registered inventory, canonical by role.
	Deadline, UpdatedAt                               time.Time
	State                                             ProbeState // coverage_checked is not a Check state; checking uses registered.
}

func ValidateStoreProbeCheck(c StoreProbeCheck) error {
	for _, id := range []ID{c.ControlDomainID, c.CheckID, c.BindingID, c.ControllerID} {
		if !storeText(string(id)) {
			return invalidProbeAttempt()
		}
	}
	if !validDigest(c.ExpectedBindingDigest) || c.BindingVersion == 0 || c.FencingToken == 0 || c.RowVersion == 0 || c.Deadline.IsZero() || c.UpdatedAt.IsZero() || len(c.Members) < 2 || len(c.Members) > 5 {
		return invalidProbeAttempt()
	}
	if c.State != ProbeRegistered && c.State != ProbeAccepted && c.State != ProbeRejected && c.State != ProbeExpired && c.State != ProbeSuperseded {
		return invalidProbeAttempt()
	}
	roles := map[Capability]bool{}
	probes := map[ID]bool{}
	invocations := map[ID]bool{}
	var previous Capability
	for _, m := range c.Members {
		e := m.Endpoint
		if !storeText(string(m.ProbeID)) || !storeText(string(m.InvocationID)) || !storeText(string(e.ModuleInstanceID)) || !storeText(string(e.EndpointIdentity)) || roles[e.Role] || probes[m.ProbeID] || invocations[m.InvocationID] || e.Role <= previous {
			return invalidProbeAttempt()
		}
		if e.Role != Transactions && e.Role != QueryReading && e.Role != Metadata && e.Role != TaskFacts && e.Role != Audit {
			return invalidProbeAttempt()
		}
		roles[e.Role] = true
		probes[m.ProbeID] = true
		invocations[m.InvocationID] = true
		previous = e.Role
	}
	if !roles[Transactions] || !roles[QueryReading] {
		return invalidProbeAttempt()
	}
	return nil
}

// PrepareProbeCheckAcceptance returns candidate CAS records only. A real
// transaction must re-read authority/lease and commit Check, all Attempts and
// audit/outbox/journal together. It does not certify digest contents or readiness.
func PrepareProbeCheckAcceptance(c StoreProbeCheck, attempts []StoreProbeAttempt, e ProbeAdvanceExpectation) (StoreProbeCheck, []StoreProbeAttempt, error) {
	fail := func() (StoreProbeCheck, []StoreProbeAttempt, error) {
		return StoreProbeCheck{}, nil, invalidProbeAttempt()
	}
	if ValidateStoreProbeCheck(c) != nil || c.State != ProbeRegistered || c.RowVersion == math.MaxUint64 || len(attempts) != len(c.Members) || e.Now.IsZero() || e.Now.Before(c.UpdatedAt) || !e.Now.Before(c.Deadline) || e.CurrentBindingDigest != c.ExpectedBindingDigest || e.CurrentBindingVersion != c.BindingVersion {
		return fail()
	}
	l := e.Lease
	if l.ControlDomainID != c.ControlDomainID || l.CheckID != c.CheckID || l.FencingToken < c.FencingToken || l.FencingToken == 0 || l.RowVersion == 0 || !storeText(string(l.ControllerID)) || !e.Now.Before(l.ExpiresAt) || (l.ControllerID != c.ControllerID && l.FencingToken <= c.FencingToken) {
		return fail()
	}
	byProbe := map[ID]StoreProbeAttempt{}
	for _, a := range attempts {
		if _, duplicate := byProbe[a.Binding.ProbeID]; duplicate {
			return fail()
		}
		byProbe[a.Binding.ProbeID] = a
	}
	var candidates []StoreProbeAttempt
	for _, m := range c.Members {
		a, ok := byProbe[m.ProbeID]
		b := a.Binding
		if !ok || a.State != ProbeCoverageChecked || b.ControlDomainID != c.ControlDomainID || b.CheckID != c.CheckID || b.BindingID != c.BindingID || b.BindingVersion != c.BindingVersion || b.ExpectedBindingDigest != c.ExpectedBindingDigest || b.InvocationID != m.InvocationID || b.Endpoint != m.Endpoint || b.Deadline.After(c.Deadline) {
			return fail()
		}
		candidate := a
		candidate.RowVersion++
		candidate.ControllerID = l.ControllerID
		candidate.FencingToken = l.FencingToken
		candidate.State = ProbeAccepted
		candidate.UpdatedAt = e.Now
		if ValidateProbeAttemptAdvance(a, candidate, e) != nil {
			return fail()
		}
		candidates = append(candidates, candidate)
	}
	c.Members = slices.Clone(c.Members)
	c.RowVersion++
	c.ControllerID = l.ControllerID
	c.FencingToken = l.FencingToken
	c.State = ProbeAccepted
	c.UpdatedAt = e.Now
	return c, candidates, nil
}
