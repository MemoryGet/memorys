package contracts_test

import (
	"github.com/MemoryGet/memorys/pkg/contracts"
	"math"
	"strings"
	"testing"
	"time"
)

func rate() contracts.UnitRate {
	return contracts.UnitRate{RateProfileID: "rate", RateProfileVersion: 1, RateProfileDigest: contracts.Digest(strings.Repeat("a", 64)), Metric: contracts.UsageOutputTokens, QuantityPerRate: 3, Price: contracts.MoneyAmount{Currency: "USD", Scale: 6, Units: 2}}
}
func TestMeterChargeExactCeilingAndZero(t *testing.T) {
	for _, c := range []struct{ quantity, want uint64 }{{0, 0}, {1, 1}, {2, 2}, {3, 2}, {4, 3}} {
		out, e := contracts.CalculateUsageCharge(c.quantity, rate())
		if e != nil || out.Units != c.want || out.Scale != 6 {
			t.Fatal(out, e)
		}
	}
}
func TestMeterChargeLargeIntermediateAndOverflow(t *testing.T) {
	r := rate()
	r.QuantityPerRate = 2
	out, e := contracts.CalculateUsageCharge(math.MaxUint64, r)
	if e != nil || out.Units != math.MaxUint64 {
		t.Fatal("intermediate uint64 product overflowed", out, e)
	}
	r.QuantityPerRate = 1
	if out, e := contracts.CalculateUsageCharge(math.MaxUint64, r); e == nil || out.Units != 0 {
		t.Fatal("unrepresentable charge accepted")
	}
}
func TestMeterMoneyMismatchAndAdditionOverflow(t *testing.T) {
	a := contracts.MoneyAmount{Currency: "USD", Scale: 6, Units: 1}
	for _, b := range []contracts.MoneyAmount{{Currency: "CNY", Scale: 6, Units: 1}, {Currency: "USD", Scale: 5, Units: 1}, {Currency: "USD", Scale: 6, Units: math.MaxUint64}} {
		if _, e := contracts.AddMoney(a, b); e == nil {
			t.Fatal("invalid money addition")
		}
	}
	r := rate()
	r.QuantityPerRate = 0
	if _, e := contracts.CalculateUsageCharge(1, r); e == nil {
		t.Fatal("zero denominator")
	}
}
func TestMeterCachedInputBucketsNeverDoubleCount(t *testing.T) {
	u, c, e := contracts.SplitInputTokenUsage(9007199254740993, 1)
	if e != nil || u+c != 9007199254740993 {
		t.Fatal(u, c, e)
	}
	if _, _, e := contracts.SplitInputTokenUsage(1, 2); e == nil {
		t.Fatal("cached exceeds total")
	}
}
func receipt() contracts.UsageReceipt {
	n := time.Unix(1900000000, 1)
	return contracts.UsageReceipt{ReceiptID: "receipt", RequestID: "request", InvocationID: "invocation", ModuleInstanceID: "model", ImplementationVersion: "1.0.0", Operation: "generate", Stage: "answer", Attempt: 1, StartedAt: n, FinishedAt: n.Add(time.Second), Status: "timeout", Complete: false, Quantities: []contracts.UsageQuantity{{Metric: contracts.UsageOutputTokens, Source: contracts.UsageUnknown}}}
}
func TestMeterUnknownUsageCannotBecomeZeroOrComplete(t *testing.T) {
	r := receipt()
	if e := contracts.ValidateUsageReceipt(r); e != nil {
		t.Fatal(e)
	}
	zero := uint64(0)
	r.Quantities[0].Quantity = &zero
	if contracts.ValidateUsageReceipt(r) == nil {
		t.Fatal("unknown quantity represented as zero")
	}
	r = receipt()
	r.Complete = true
	if contracts.ValidateUsageReceipt(r) == nil {
		t.Fatal("unknown declared complete")
	}
	r = receipt()
	r.Quantities = append(r.Quantities, r.Quantities[0])
	if contracts.ValidateUsageReceipt(r) == nil {
		t.Fatal("duplicate metric")
	}
}

func TestMeterEstimatedUsageAndMissingProviderReferenceCannotCertifyActual(t *testing.T) {
	r := receipt()
	n := uint64(10)
	r.Complete = true
	r.Status = "completed"
	r.Quantities = []contracts.UsageQuantity{{Metric: contracts.UsageOutputTokens, Source: contracts.UsageEstimated, Quantity: &n}}
	if contracts.ValidateUsageReceipt(r) == nil {
		t.Fatal("estimated usage marked complete")
	}
	r.Quantities[0].Source = contracts.UsageProviderReported
	if contracts.ValidateUsageReceipt(r) == nil {
		t.Fatal("reported usage without reference")
	}
	r.ProviderRecordReference = "provider-record"
	if err := contracts.ValidateUsageReceipt(r); err != nil {
		t.Fatal(err)
	}
}

func TestMeterReceiptInputPartitionsAgreeWithoutOverflow(t *testing.T) {
	quantity := func(metric contracts.UsageMetric, value uint64) contracts.UsageQuantity {
		return contracts.UsageQuantity{Metric: metric, Quantity: &value, Source: contracts.UsagePlatformMeasured}
	}
	for _, tc := range []struct {
		name                    string
		total, cached, uncached uint64
		valid                   bool
	}{
		{"exact", 10, 3, 7, true},
		{"all-zero", 0, 0, 0, true},
		{"maximum-exact", math.MaxUint64, 1, math.MaxUint64 - 1, true},
		{"uncached-exceeds-total", 10, 0, 11, false},
		{"sum-too-small", 10, 3, 6, false},
		{"sum-too-large", 10, 3, 8, false},
		{"sum-would-overflow", math.MaxUint64, 1, math.MaxUint64, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := receipt()
			r.Quantities = []contracts.UsageQuantity{quantity(contracts.UsageInputTokens, tc.total), quantity(contracts.UsageCachedInputTokens, tc.cached), quantity(contracts.UsageUncachedInputTokens, tc.uncached)}
			if err := contracts.ValidateUsageReceipt(r); (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}

func TestMeterReceiptPartialInputRequiresKnownTotal(t *testing.T) {
	n := uint64(7)
	uncached := contracts.UsageQuantity{Metric: contracts.UsageUncachedInputTokens, Quantity: &n, Source: contracts.UsagePlatformMeasured}
	r := receipt()
	r.Quantities = []contracts.UsageQuantity{uncached}
	if contracts.ValidateUsageReceipt(r) == nil {
		t.Fatal("uncached subset without total accepted")
	}
	r.Quantities = append(r.Quantities, contracts.UsageQuantity{Metric: contracts.UsageInputTokens, Source: contracts.UsageUnknown})
	if contracts.ValidateUsageReceipt(r) == nil {
		t.Fatal("uncached subset with unknown total accepted")
	}
	total := uint64(10)
	r.Quantities[1] = contracts.UsageQuantity{Metric: contracts.UsageInputTokens, Quantity: &total, Source: contracts.UsagePlatformMeasured}
	r.Quantities = append(r.Quantities, contracts.UsageQuantity{Metric: contracts.UsageCachedInputTokens, Source: contracts.UsageUnknown})
	if err := contracts.ValidateUsageReceipt(r); err != nil {
		t.Fatal("partial cached unknown must remain representable", err)
	}
}
