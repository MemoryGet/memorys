package contracts

import "context"

type DraftCompareInput struct {
	Credential  Credential
	Request     RequestContext
	DraftID     ID
	FromVersion uint64
}
type DraftCompareChange struct {
	BlockID       ID
	Kind          string
	BeforeVersion uint64
	AfterVersion  uint64
}
type DraftCompareResult struct {
	DraftID     ID
	FromVersion uint64
	ToVersion   uint64
	Changes     []DraftCompareChange
}

type DraftCompareApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	CompareDraft(context.Context, DraftCompareInput) (DraftCompareResult, error)
}
