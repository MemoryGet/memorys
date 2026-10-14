package contracts

import (
	"context"
	"time"
)

// Declarative ports only. Implementations must authenticate the scope, enforce
// persistent uniqueness/CAS and atomically commit accounting with audit/outbox.
type MeterScope struct {
	ScopeID, SubjectID, SpaceID, AccountID ID
	PolicyVersion                          uint64
	ExpiresAt                              time.Time
}

type UsageLimit struct {
	Metric  UsageMetric
	Maximum uint64
}

type RequestAllowance struct {
	RequestID, AccountID               ID
	AdmissionDigest, RateProfileDigest Digest
	RateProfileID                      ID
	RateProfileVersion                 uint64
	Deadline                           time.Time
	Limits                             []UsageLimit
	MaximumCharge                      MoneyAmount
	Mode                               string // strict_bound or declared_estimate; never inferred from provider name.
}

type MetricBalance struct {
	Metric          UsageMetric
	Committed, Held uint64
}

type MeterBudgetSnapshot struct {
	Allowance                   RequestAllowance
	RowVersion                  uint64
	Balances                    []MetricBalance
	CommittedCharge, HeldCharge MoneyAmount
	Status                      string // open, stopped, reconciling, closed
	Overrun                     bool   // Actual external usage may exceed an estimated hold.
}

type MeterCommand struct {
	Request               RequestContext
	Scope                 MeterScope
	CommandID             ID
	CommandDigest         Digest
	ExpectedBudgetVersion uint64
}

type MeterOpenRequest struct {
	Command   MeterCommand
	Allowance RequestAllowance
}

type MeterReserveRequest struct {
	Command                                       MeterCommand
	ReservationID, InvocationID, ModuleInstanceID ID
	ParentInvocationID                            *ID
	Attempt                                       uint64
	ImplementationVersion, Operation, Stage       string
	Round                                         int
	PlanDigest, BoundEvidenceDigest               Digest
	MaximumUsage                                  []UsageLimit
	MaximumCharge                                 MoneyAmount
}

type MeterReservation struct {
	Plan          MeterReserveRequest
	RowVersion    uint64
	State         string // reserved, dispatched, unknown, settled, aborted_before_dispatch
	DispatchID    ID
	ReceiptID     ID
	ReceiptDigest Digest
	ActualCharge  *MoneyAmount // nil is unknown, not zero or the reserved amount.
	UpdatedAt     time.Time
}

type MeterBeginRequest struct {
	Command                    MeterCommand
	ReservationID, DispatchID  ID
	ExpectedReservationVersion uint64
}

type MeterReconcileRequest struct {
	Command                       MeterCommand
	ReservationID                 ID
	ExpectedReservationVersion    uint64
	Receipt                       UsageReceipt
	ReceiptDigest, EvidenceDigest Digest
	SupersedesReceiptDigest       Digest // Empty for first receipt; prior unknown receipt otherwise.
}

type MeterAbortRequest struct {
	Command                    MeterCommand
	ReservationID              ID
	ExpectedReservationVersion uint64
	NoDispatchEvidenceDigest   Digest
}

type MeterLoadRequest struct {
	Request       RequestContext
	Scope         MeterScope
	ReservationID *ID
}

type MeterResult struct {
	Budget      MeterBudgetSnapshot
	Reservation *MeterReservation
	Replayed    bool // Same persisted command result; not a second debit.
}

// Open establishes one immutable allowance. Other calls cannot reopen a request
// or change its account/rate snapshot. Load reads persisted state after uncertain
// commits; no call to an external provider is performed inside these methods.
type RequestMeter interface {
	Open(context.Context, MeterOpenRequest) (MeterResult, error)
	Reserve(context.Context, MeterReserveRequest) (MeterResult, error)
	Begin(context.Context, MeterBeginRequest) (MeterResult, error)
	Reconcile(context.Context, MeterReconcileRequest) (MeterResult, error)
	AbortBeforeDispatch(context.Context, MeterAbortRequest) (MeterResult, error)
	Load(context.Context, MeterLoadRequest) (MeterResult, error)
}
