package contracts_test

import (
	"github.com/MemoryGet/memorys/pkg/contracts"
	"strings"
	"testing"
)

func checkFixture() (contracts.StoreProbeCheck, []contracts.StoreProbeAttempt, contracts.ProbeAdvanceExpectation) {
	a, e := ledgerFixture()
	a = observed(a, e)
	a.RowVersion++
	a.State = contracts.ProbeCoverageChecked
	a.CoverageDigest = contracts.Digest(strings.Repeat("f", 64))
	b := a
	b.Binding.ProbeID = "reader-probe"
	b.Binding.InvocationID = "reader-invocation"
	b.Binding.Endpoint = contracts.StoreEndpointBinding{Role: contracts.QueryReading, ModuleInstanceID: "reader", EndpointIdentity: "reader-endpoint"}
	c := contracts.StoreProbeCheck{ControlDomainID: a.Binding.ControlDomainID, CheckID: a.Binding.CheckID, BindingID: a.Binding.BindingID, ControllerID: a.ControllerID, ExpectedBindingDigest: a.Binding.ExpectedBindingDigest, BindingVersion: a.Binding.BindingVersion, FencingToken: 1, RowVersion: 1, Deadline: a.Binding.Deadline, UpdatedAt: a.Binding.IssuedAt, State: contracts.ProbeRegistered, Members: []contracts.ProbeCheckMember{{ProbeID: a.Binding.ProbeID, InvocationID: a.Binding.InvocationID, Endpoint: a.Binding.Endpoint}, {ProbeID: b.Binding.ProbeID, InvocationID: b.Binding.InvocationID, Endpoint: b.Binding.Endpoint}}}
	return c, []contracts.StoreProbeAttempt{a, b}, e
}

func TestProbeCheckPreparesAllMembersAndCopiesInventory(t *testing.T) {
	c, a, e := checkFixture()
	after, proposals, err := contracts.PrepareProbeCheckAcceptance(c, a, e)
	if err != nil || len(proposals) != 2 || after.State != contracts.ProbeAccepted || after.RowVersion != 2 {
		t.Fatal(err)
	}
	for i, p := range proposals {
		if p.State != contracts.ProbeAccepted || p.RowVersion != a[i].RowVersion+1 {
			t.Fatal("missing attempt proposal")
		}
	}
	after.Members[0].ProbeID = "changed"
	if c.Members[0].ProbeID == "changed" {
		t.Fatal("check proposal aliased input")
	}
	a[0], a[1] = a[1], a[0]
	if _, _, err := contracts.PrepareProbeCheckAcceptance(c, a, e); err != nil {
		t.Fatal("input order changed acceptance", err)
	}
}

func TestProbeCheckRejectsMissingExtraDuplicateAndMixedCheck(t *testing.T) {
	for _, change := range []func(*[]contracts.StoreProbeAttempt){func(a *[]contracts.StoreProbeAttempt) { *a = (*a)[:1] }, func(a *[]contracts.StoreProbeAttempt) { *a = append(*a, (*a)[0]) }, func(a *[]contracts.StoreProbeAttempt) { (*a)[1] = (*a)[0] }, func(a *[]contracts.StoreProbeAttempt) { (*a)[1].Binding.CheckID = "other" }} {
		c, a, e := checkFixture()
		change(&a)
		out, p, err := contracts.PrepareProbeCheckAcceptance(c, a, e)
		if err == nil || p != nil || out.CheckID != "" {
			t.Fatal("partial acceptance escaped")
		}
	}
}

func TestProbeCheckRejectsIncompleteCoverageAndWrongEndpoint(t *testing.T) {
	for _, change := range []func(*contracts.StoreProbeAttempt){func(a *contracts.StoreProbeAttempt) { a.State = contracts.ProbeObserved; a.CoverageDigest = "" }, func(a *contracts.StoreProbeAttempt) { a.Binding.Endpoint.EndpointIdentity = "other" }, func(a *contracts.StoreProbeAttempt) {
		a.Binding.ExpectedBindingDigest = contracts.Digest(strings.Repeat("a", 64))
	}, func(a *contracts.StoreProbeAttempt) { a.State = contracts.ProbeAccepted }} {
		c, a, e := checkFixture()
		change(&a[1])
		if _, _, err := contracts.PrepareProbeCheckAcceptance(c, a, e); err == nil {
			t.Fatal("invalid member accepted")
		}
	}
}

func TestProbeCheckRejectsOldLeaseChangedBindingAndExpiredWindow(t *testing.T) {
	c, a, e := checkFixture()
	e.Lease.ControllerID = "new"
	if _, _, err := contracts.PrepareProbeCheckAcceptance(c, a, e); err == nil {
		t.Fatal("controller takeover without fence advance")
	}
	e.Lease.FencingToken = 2
	if _, _, err := contracts.PrepareProbeCheckAcceptance(c, a, e); err != nil {
		t.Fatal("valid takeover proposal rejected", err)
	}
	e.CurrentBindingVersion++
	if _, _, err := contracts.PrepareProbeCheckAcceptance(c, a, e); err == nil {
		t.Fatal("changed binding accepted")
	}
	c, a, e = checkFixture()
	e.Now = c.Deadline
	if _, _, err := contracts.PrepareProbeCheckAcceptance(c, a, e); err == nil {
		t.Fatal("expired Check accepted")
	}
}

func TestProbeCheckRejectsInvalidOrTerminalInventory(t *testing.T) {
	c, a, e := checkFixture()
	c.Members[1].ProbeID = c.Members[0].ProbeID
	if _, _, err := contracts.PrepareProbeCheckAcceptance(c, a, e); err == nil {
		t.Fatal("duplicate registered inventory")
	}
	c, a, e = checkFixture()
	c.State = contracts.ProbeAccepted
	if _, _, err := contracts.PrepareProbeCheckAcceptance(c, a, e); err == nil {
		t.Fatal("terminal Check reaccepted")
	}
}
