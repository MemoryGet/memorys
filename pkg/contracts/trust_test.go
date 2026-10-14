package contracts_test

import (
	"github.com/MemoryGet/memorys/pkg/contracts"
	"strings"
	"testing"
)

func trustHeads() (contracts.TrustHead, contracts.TrustHead) {
	a := contracts.TrustHead{DomainID: "domain", IssuerID: "issuer", Active: contracts.TrustSnapshotIdentity{ID: "snapshot-1", Version: 9007199254740993, Digest: contracts.Digest(strings.Repeat("a", 64))}, BuildLockDigest: contracts.Digest(strings.Repeat("b", 64)), HighWaterVersion: 9007199254740993, RowVersion: 5}
	b := a
	b.Active.ID = "snapshot-2"
	b.Active.Version++
	b.Active.Digest = contracts.Digest(strings.Repeat("c", 64))
	b.HighWaterVersion++
	b.RowVersion++
	return a, b
}

func TestTrustHeadAdvanceExactVersion(t *testing.T) {
	a, b := trustHeads()
	if err := contracts.ValidateTrustHeadAdvance(a, b); err != nil {
		t.Fatal(err)
	}
}
func TestTrustHeadRejectsRollbackSameVersionAndCASMismatch(t *testing.T) {
	a, b := trustHeads()
	for _, mutate := range []func(*contracts.TrustHead){func(h *contracts.TrustHead) {
		h.Active.Version = a.Active.Version
		h.HighWaterVersion = a.HighWaterVersion
	}, func(h *contracts.TrustHead) {
		h.Active.Version = a.Active.Version - 1
		h.HighWaterVersion = a.HighWaterVersion - 1
	}, func(h *contracts.TrustHead) { h.RowVersion = a.RowVersion }, func(h *contracts.TrustHead) { h.HighWaterVersion++ }} {
		h := b
		mutate(&h)
		if err := contracts.ValidateTrustHeadAdvance(a, h); err == nil {
			t.Fatal("invalid advance accepted")
		}
	}
}
func TestTrustHeadRejectsDomainDigestAndCounterOverflow(t *testing.T) {
	a, b := trustHeads()
	for _, mutate := range []func(*contracts.TrustHead){func(h *contracts.TrustHead) { h.DomainID = "other" }, func(h *contracts.TrustHead) { h.IssuerID = "other" }, func(h *contracts.TrustHead) { h.Active.Digest = "bad" }, func(h *contracts.TrustHead) { h.BuildLockDigest = "bad" }, func(h *contracts.TrustHead) { h.Active.ID = "" }} {
		h := b
		mutate(&h)
		if err := contracts.ValidateTrustHeadAdvance(a, h); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
	a.RowVersion = ^uint64(0)
	b.RowVersion = 0
	if err := contracts.ValidateTrustHeadAdvance(a, b); err == nil {
		t.Fatal("row version overflow accepted")
	}
}
