package contracts_test

import (
	"github.com/MemoryGet/memorys/pkg/contracts"
	"math"
	"strings"
	"testing"
	"time"
)

func ledgerFixture() (contracts.StoreProbeAttempt, contracts.ProbeAdvanceExpectation) {
	r, _, _, n := probeFixture()
	return contracts.StoreProbeAttempt{Binding: r.Binding, RequestDigest: contracts.Digest(strings.Repeat("d", 64)), ControllerID: "controller", FencingToken: 1, RowVersion: 1, State: contracts.ProbeRegistered, UpdatedAt: r.Binding.IssuedAt}, contracts.ProbeAdvanceExpectation{Lease: contracts.ProbeControlLease{ControlDomainID: r.Binding.ControlDomainID, CheckID: r.Binding.CheckID, ControllerID: "controller", FencingToken: 1, RowVersion: 1, ExpiresAt: n.Add(time.Minute)}, CurrentBindingDigest: r.Binding.ExpectedBindingDigest, CurrentBindingVersion: r.Binding.BindingVersion, Now: n}
}
func observed(a contracts.StoreProbeAttempt, e contracts.ProbeAdvanceExpectation) contracts.StoreProbeAttempt {
	a.RowVersion++
	a.State = contracts.ProbeObserved
	a.ObservationDigest = contracts.Digest(strings.Repeat("e", 64))
	a.UpdatedAt = e.Now
	return a
}

func TestProbeLedgerOrderedStagesAndTerminalCannotRevive(t *testing.T) {
	a, e := ledgerFixture()
	b := observed(a, e)
	if err := contracts.ValidateProbeAttemptAdvance(a, b, e); err != nil {
		t.Fatal(err)
	}
	a = b
	b.RowVersion++
	b.State = contracts.ProbeCoverageChecked
	b.CoverageDigest = contracts.Digest(strings.Repeat("f", 64))
	if err := contracts.ValidateProbeAttemptAdvance(a, b, e); err != nil {
		t.Fatal(err)
	}
	a = b
	b.RowVersion++
	b.State = contracts.ProbeAccepted
	if err := contracts.ValidateProbeAttemptAdvance(a, b, e); err != nil {
		t.Fatal(err)
	}
	a = b
	b.RowVersion++
	b.State = contracts.ProbeObserved
	b.CoverageDigest = ""
	if contracts.ValidateProbeAttemptAdvance(a, b, e) == nil {
		t.Fatal("terminal revived")
	}
}

func TestProbeLedgerRejectsSkipMutationAndDigestOverwrite(t *testing.T) {
	for _, change := range []func(*contracts.StoreProbeAttempt){func(b *contracts.StoreProbeAttempt) {
		b.State = contracts.ProbeAccepted
		b.CoverageDigest = contracts.Digest(strings.Repeat("f", 64))
	}, func(b *contracts.StoreProbeAttempt) { b.Binding.ProbeID = "new" }, func(b *contracts.StoreProbeAttempt) { b.RequestDigest = contracts.Digest(strings.Repeat("a", 64)) }, func(b *contracts.StoreProbeAttempt) { b.RowVersion++ }} {
		a, e := ledgerFixture()
		b := observed(a, e)
		change(&b)
		if contracts.ValidateProbeAttemptAdvance(a, b, e) == nil {
			t.Fatal("bad advance accepted")
		}
	}
	a, e := ledgerFixture()
	a = observed(a, e)
	b := a
	b.RowVersion++
	b.State = contracts.ProbeCoverageChecked
	b.CoverageDigest = contracts.Digest(strings.Repeat("f", 64))
	b.ObservationDigest = contracts.Digest(strings.Repeat("a", 64))
	if contracts.ValidateProbeAttemptAdvance(a, b, e) == nil {
		t.Fatal("received response overwritten")
	}
}

func TestProbeLedgerRejectsOldFenceAndAllowsCurrentController(t *testing.T) {
	a, e := ledgerFixture()
	b := observed(a, e)
	e.Lease.ControllerID = "replacement"
	b.ControllerID = "replacement"
	if contracts.ValidateProbeAttemptAdvance(a, b, e) == nil {
		t.Fatal("controller changed without advancing fence")
	}
	b.ControllerID = a.ControllerID
	e.Lease.FencingToken = 2
	if contracts.ValidateProbeAttemptAdvance(a, b, e) == nil {
		t.Fatal("old controller committed")
	}
	b.ControllerID = e.Lease.ControllerID
	b.FencingToken = 2
	if err := contracts.ValidateProbeAttemptAdvance(a, b, e); err != nil {
		t.Fatal("valid takeover rejected", err)
	}
	e.Lease.ExpiresAt = e.Now
	if contracts.ValidateProbeAttemptAdvance(a, b, e) == nil {
		t.Fatal("expired lease accepted")
	}
}

func TestProbeLedgerExpiryAndChangedBindingTerminalPaths(t *testing.T) {
	a, e := ledgerFixture()
	b := a
	b.RowVersion++
	b.State = contracts.ProbeExpired
	b.TerminalReason = "deadline_exceeded"
	e.Now = a.Binding.Deadline
	b.UpdatedAt = e.Now
	if err := contracts.ValidateProbeAttemptAdvance(a, b, e); err != nil {
		t.Fatal(err)
	}
	a, e = ledgerFixture()
	b = a
	b.RowVersion++
	b.State = contracts.ProbeSuperseded
	b.TerminalReason = "binding_changed"
	b.UpdatedAt = e.Now
	if contracts.ValidateProbeAttemptAdvance(a, b, e) == nil {
		t.Fatal("unchanged binding superseded")
	}
	e.CurrentBindingVersion++
	if err := contracts.ValidateProbeAttemptAdvance(a, b, e); err != nil {
		t.Fatal(err)
	}
}

func TestProbeLedgerVersionOverflowAndMissingStateEvidence(t *testing.T) {
	a, e := ledgerFixture()
	a.RowVersion = math.MaxUint64
	b := observed(a, e)
	if contracts.ValidateProbeAttemptAdvance(a, b, e) == nil {
		t.Fatal("row version overflow accepted")
	}
	a, e = ledgerFixture()
	b = observed(a, e)
	b.ObservationDigest = ""
	if contracts.ValidateProbeAttemptAdvance(a, b, e) == nil {
		t.Fatal("missing observation evidence")
	}
}
