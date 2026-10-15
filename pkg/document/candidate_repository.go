package document

import (
	"context"
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

// A transport or commit boundary may not know whether effects were committed.
// Reconcile or retry the identical candidate, never invent a new candidate ID.
var ErrCandidateCommitUnknown = errors.New("candidate commit outcome requires reconciliation")

// ErrCandidateRejected certifies that this invocation committed no effects.
// Adapters must not use it for connection loss or uncertain commit outcomes.
var ErrCandidateRejected = errors.New("candidate write rejected without committed effects")

// CandidateCommand is a platform-owned, synchronous candidate write contract.
// It does not complete a background job or confer a lease/fencing authority.
type CandidateCommand struct {
	Request     contracts.RequestContext
	Scope       contracts.AccessScope
	DocumentID  contracts.ID
	CandidateID contracts.ID
	Candidate   ParseCandidate
}

type CandidateReceipt struct {
	CandidateID  contracts.ID
	DraftID      contracts.ID
	DocumentID   contracts.ID
	Applied      bool
	Reason       string
	DraftVersion uint64
}

// CommitCandidate atomically stores the immutable candidate, optional draft CAS,
// receipt, audit and outbox. An ID replay must match all candidate/actor bindings.
// Unknown commit outcomes require replay/reconciliation with the same ID.
type CandidateRepository interface {
	CommitCandidate(context.Context, CandidateCommand) (CandidateReceipt, error)
}

type CandidateReadRequest struct {
	Request                          contracts.RequestContext
	Scope                            contracts.AccessScope
	DocumentID, DraftID, CandidateID contracts.ID
}

type CandidateRecord struct {
	Candidate ParseCandidate
	Receipt   CandidateReceipt
	ActorID   contracts.ID
}

// Reads require their own resource grant. A missing/denied record does not prove
// that an earlier uncertain write failed: its transaction may still be pending.
type CandidateReader interface {
	ReadCandidate(context.Context, CandidateReadRequest) (CandidateRecord, error)
}

var ErrCandidateRead = errors.New("candidate unavailable or denied")
