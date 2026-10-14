package contracts

import (
	"math"
	"math/big"
	"time"
)

type MoneyAmount struct {
	Currency string
	Scale    uint32
	Units    uint64
}
type UsageMetric string

const (
	UsageRetrievalCalls      UsageMetric = "retrieval_calls"
	UsageEmbeddingCalls      UsageMetric = "embedding_calls"
	UsageModelCalls          UsageMetric = "model_calls"
	UsageInputTokens         UsageMetric = "input_tokens_total"
	UsageCachedInputTokens   UsageMetric = "input_tokens_cached"
	UsageUncachedInputTokens UsageMetric = "input_tokens_uncached"
	UsageOutputTokens        UsageMetric = "output_tokens_total"
	UsageEmbeddingTokens     UsageMetric = "embedding_input_tokens"
	UsageCandidateBytes      UsageMetric = "candidate_bytes"
	UsageBlobBytes           UsageMetric = "blob_bytes"
	UsageOCRPages            UsageMetric = "ocr_pages"
)

type UsageSource string

const (
	UsageProviderReported UsageSource = "provider_reported"
	UsagePlatformMeasured UsageSource = "platform_measured"
	UsageEstimated        UsageSource = "estimated"
	UsageUnknown          UsageSource = "unknown"
)

type UsageQuantity struct {
	Metric   UsageMetric
	Quantity *uint64
	Source   UsageSource
}
type UsageReceipt struct {
	ReceiptID, RequestID, InvocationID, ModuleInstanceID ID
	ParentInvocationID                                   *ID
	ImplementationVersion, Operation, Stage              string
	Attempt                                              uint64
	Round                                                int
	StartedAt, FinishedAt                                time.Time
	Status                                               string
	Complete                                             bool
	Quantities                                           []UsageQuantity
	ProviderRecordReference                              ID
}

type UnitRate struct {
	RateProfileID      ID
	RateProfileVersion uint64
	RateProfileDigest  Digest
	Metric             UsageMetric
	QuantityPerRate    uint64
	Price              MoneyAmount
}

func invalidMetering() error {
	return PublicError{Code: "validation_failed", SafeMessage: "Invalid metering value"}
}
func validMoney(m MoneyAmount) bool {
	if len(m.Currency) != 3 || m.Scale > 9 {
		return false
	}
	for _, c := range m.Currency {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}
func knownUsageMetric(m UsageMetric) bool {
	switch m {
	case UsageRetrievalCalls, UsageEmbeddingCalls, UsageModelCalls, UsageInputTokens, UsageCachedInputTokens, UsageUncachedInputTokens, UsageOutputTokens, UsageEmbeddingTokens, UsageCandidateBytes, UsageBlobBytes, UsageOCRPages:
		return true
	}
	return false
}

// Integer ceil(quantity * price units / quantity-per-rate). It does not
// authenticate a rate catalog, predict real bills or debit a budget.
func CalculateUsageCharge(quantity uint64, rate UnitRate) (MoneyAmount, error) {
	if !storeText(string(rate.RateProfileID)) || rate.RateProfileVersion == 0 || !validDigest(rate.RateProfileDigest) || !knownUsageMetric(rate.Metric) || rate.QuantityPerRate == 0 || !validMoney(rate.Price) {
		return MoneyAmount{}, invalidMetering()
	}
	product := new(big.Int).Mul(new(big.Int).SetUint64(quantity), new(big.Int).SetUint64(rate.Price.Units))
	divisor := new(big.Int).SetUint64(rate.QuantityPerRate)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(product, divisor, remainder)
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsUint64() {
		return MoneyAmount{}, invalidMetering()
	}
	out := rate.Price
	out.Units = quotient.Uint64()
	return out, nil
}

func AddMoney(a, b MoneyAmount) (MoneyAmount, error) {
	if !validMoney(a) || !validMoney(b) || a.Currency != b.Currency || a.Scale != b.Scale || a.Units > math.MaxUint64-b.Units {
		return MoneyAmount{}, invalidMetering()
	}
	a.Units += b.Units
	return a, nil
}

// Cached is a subset of input total; bill these two buckets, not total+cached.
func SplitInputTokenUsage(total, cached uint64) (uncached uint64, cachedBucket uint64, err error) {
	if cached > total {
		return 0, 0, invalidMetering()
	}
	return total - cached, cached, nil
}

// Receipt shape/relations only, not provider authenticity or billing completeness.
func ValidateUsageReceipt(r UsageReceipt) error {
	for _, id := range []ID{r.ReceiptID, r.RequestID, r.InvocationID, r.ModuleInstanceID} {
		if !storeText(string(id)) {
			return invalidMetering()
		}
	}
	if r.ParentInvocationID != nil && !storeText(string(*r.ParentInvocationID)) {
		return invalidMetering()
	}
	if !storeText(r.ImplementationVersion) || !storeText(r.Operation) || !storeText(r.Stage) || r.Attempt == 0 || r.Round < 0 || r.Round > 2 || r.StartedAt.IsZero() || r.FinishedAt.Before(r.StartedAt) || len(r.Quantities) > 11 {
		return invalidMetering()
	}
	if r.Status != "completed" && r.Status != "error" && r.Status != "timeout" && r.Status != "canceled" && r.Status != "unknown" {
		return invalidMetering()
	}
	if r.Complete && (r.Status == "unknown" || len(r.Quantities) == 0) {
		return invalidMetering()
	}
	metrics := map[UsageMetric]UsageQuantity{}
	for _, q := range r.Quantities {
		if !knownUsageMetric(q.Metric) {
			return invalidMetering()
		}
		if _, exists := metrics[q.Metric]; exists {
			return invalidMetering()
		}
		metrics[q.Metric] = q
		switch q.Source {
		case UsageUnknown:
			if q.Quantity != nil || r.Complete {
				return invalidMetering()
			}
		case UsageProviderReported, UsagePlatformMeasured, UsageEstimated:
			if q.Quantity == nil {
				return invalidMetering()
			}
			if q.Source == UsageEstimated && r.Complete {
				return invalidMetering()
			}
			if q.Source == UsageProviderReported && !storeText(string(r.ProviderRecordReference)) {
				return invalidMetering()
			}
		default:
			return invalidMetering()
		}
	}
	if cached, ok := metrics[UsageCachedInputTokens]; ok && cached.Quantity != nil {
		total, present := metrics[UsageInputTokens]
		if !present || total.Quantity == nil || *cached.Quantity > *total.Quantity {
			return invalidMetering()
		}
	}
	if uncached, ok := metrics[UsageUncachedInputTokens]; ok && uncached.Quantity != nil {
		total, present := metrics[UsageInputTokens]
		if !present || total.Quantity == nil || *uncached.Quantity > *total.Quantity {
			return invalidMetering()
		}
		if cached, present := metrics[UsageCachedInputTokens]; present && cached.Quantity != nil {
			// Subtract only after subset checks; never overflow by summing buckets.
			if *uncached.Quantity != *total.Quantity-*cached.Quantity {
				return invalidMetering()
			}
		}
	}
	return nil
}
