package contracts

import "context"

// These are draft in-process transaction ports, not a frozen wire protocol or
// a complete PublicationService/UnitOfWork implementation.
type WithdrawalCommand struct {
	OperationID, DocumentID            ID
	Key                                string
	ActorSemanticDigest, CommandDigest Digest
}
type CommandReservation struct {
	Created                bool
	OperationID, HistoryID ID
	PublicationEpoch       uint64
}
type WithdrawalCompletion struct {
	PlanID                      ID
	ExpectedRowVersion          uint64
	EventID, AuditID, RequestID ID
}

// A narrow head projection avoids manufacturing missing Document metadata.
type WithdrawalSnapshot struct {
	SpaceID, KnowledgeBaseID, DocumentID, RevisionID, GenerationSetID ID
	PublicationEpoch, RowVersion                                      uint64
	Targets                                                           []CleanupTarget
}
type WithdrawalPorts struct {
	History     PublicationHistoryRepository
	Plans       CleanupPlanRepository
	Reserve     func(context.Context, AccessScope, WithdrawalCommand) (CommandReservation, error)
	LoadCurrent func(context.Context, AccessScope, ID) (WithdrawalSnapshot, error)
	Complete    func(context.Context, AccessScope, WithdrawalCompletion) error
}
type WithdrawalUnit interface {
	WithinWithdrawal(context.Context, AccessScope, func(context.Context, WithdrawalPorts) error) error
}
