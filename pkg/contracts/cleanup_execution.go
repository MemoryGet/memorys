package contracts

import "context"

// Draft execution contracts. These do not establish provider identity or prove
// a backend effect; independent binding/receipt verification remains required.
type CleanupOperation struct {
	ID                                                     ID
	Digest                                                 Digest
	SpaceID, KnowledgeBaseID, DocumentID, PlanID, TargetID ID
	RevisionID, GenerationSetID                            ID
	PublicationEpoch                                       uint64
	Target                                                 CleanupTarget
}
type CleanupReceipt struct {
	OperationID     ID
	RequestDigest   Digest
	State           string // absent, succeeded, pending, unknown
	ResultReference ID
}
type CleanupExecutor interface {
	Lookup(context.Context, CleanupOperation) (CleanupReceipt, error)
	Execute(context.Context, CleanupOperation) (CleanupReceipt, error)
}
