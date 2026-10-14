package contracts

import (
	"context"
	"math"
	"time"
)

type ProbeState string

const (
	ProbeRegistered      ProbeState = "registered"
	ProbeObserved        ProbeState = "observed"
	ProbeCoverageChecked ProbeState = "coverage_checked"
	ProbeAccepted        ProbeState = "accepted"
	ProbeRejected        ProbeState = "rejected"
	ProbeExpired         ProbeState = "expired"
	ProbeSuperseded      ProbeState = "superseded"
)

type StoreProbeAttempt struct {
	Binding                           StoreProbeBinding
	RequestDigest                     Digest
	ControllerID                      ID
	FencingToken, RowVersion          uint64
	State                             ProbeState
	ObservationDigest, CoverageDigest Digest // Empty until the corresponding stage.
	TerminalReason                    Code
	UpdatedAt                         time.Time
}

type ProbeControlLease struct {
	ControlDomainID, CheckID, ControllerID ID
	FencingToken, RowVersion               uint64
	ExpiresAt                              time.Time
}

type ProbeAdvanceExpectation struct {
	Lease                 ProbeControlLease
	CurrentBindingDigest  Digest
	CurrentBindingVersion uint64
	Now                   time.Time
}

// All methods are scoped to the deployment-domain transaction callback. They
// must enforce unique Probe/Invocation IDs, CAS and real lease fencing.
type StoreProbeLedgerRepository interface {
	LoadCheck(context.Context, ID) (StoreProbeCheck, error)
	CreateCheck(context.Context, StoreProbeCheck) error
	CompareAndSwapCheck(context.Context, StoreProbeCheck, uint64) error
	LoadAttempt(context.Context, ID) (StoreProbeAttempt, error)
	CreateAttempt(context.Context, StoreProbeAttempt) error
	CompareAndSwapAttempt(context.Context, StoreProbeAttempt, uint64) error
	LoadLease(context.Context, ID) (ProbeControlLease, error)
	CompareAndSwapLease(context.Context, ProbeControlLease, uint64) error
	AppendAudit(context.Context, DeploymentAuditRecord) error
	AppendOutbox(context.Context, DeploymentEvent) error
	AppendJournal(context.Context, MigrationJournalRecord) error
}

// Attempt/lease and audit/outbox/journal commit together or roll back. Handles
// cannot escape; network verification is outside this transaction.
type StoreProbeLedgerUnit interface {
	WithinProbeLedger(context.Context, DeploymentScope, func(context.Context, StoreProbeLedgerRepository) error) error
}

func invalidProbeAttempt() error {
	return PublicError{Code: "validation_failed", SafeMessage: "Invalid probe attempt transition"}
}
func terminalProbe(s ProbeState) bool {
	return s == ProbeAccepted || s == ProbeRejected || s == ProbeExpired || s == ProbeSuperseded
}

// Shape only; does not validate request hashes, source authority or real coverage.
func ValidateProbeAttempt(a StoreProbeAttempt) error {
	b := a.Binding
	for _, id := range []ID{b.CheckID, b.ProbeID, b.InvocationID, b.RequestID, b.BindingID, b.ControlDomainID, b.ScopeID, b.SubjectID, b.Endpoint.ModuleInstanceID, b.Endpoint.EndpointIdentity, a.ControllerID} {
		if !storeText(string(id)) {
			return invalidProbeAttempt()
		}
	}
	if b.Endpoint.Role != Transactions && b.Endpoint.Role != QueryReading && b.Endpoint.Role != Metadata && b.Endpoint.Role != TaskFacts && b.Endpoint.Role != Audit {
		return invalidProbeAttempt()
	}
	if b.BindingVersion == 0 || b.PolicyVersion == 0 || a.FencingToken == 0 || a.RowVersion == 0 || !validDigest(b.ExpectedBindingDigest) || !validDigest(b.RequestProfileDigest) || !validDigest(a.RequestDigest) || b.IssuedAt.IsZero() || b.Deadline.IsZero() || !b.IssuedAt.Before(b.Deadline) || a.UpdatedAt.IsZero() || a.UpdatedAt.Before(b.IssuedAt) {
		return invalidProbeAttempt()
	}
	obs, cov := a.ObservationDigest, a.CoverageDigest
	if (obs != "" && !validDigest(obs)) || (cov != "" && !validDigest(cov)) || (cov != "" && obs == "") {
		return invalidProbeAttempt()
	}
	switch a.State {
	case ProbeRegistered:
		if obs != "" || cov != "" {
			return invalidProbeAttempt()
		}
	case ProbeObserved:
		if obs == "" || cov != "" {
			return invalidProbeAttempt()
		}
	case ProbeCoverageChecked, ProbeAccepted:
		if obs == "" || cov == "" {
			return invalidProbeAttempt()
		}
	case ProbeRejected, ProbeExpired, ProbeSuperseded:
		if !storeText(string(a.TerminalReason)) {
			return invalidProbeAttempt()
		}
	default:
		return invalidProbeAttempt()
	}
	if a.State != ProbeRejected && a.State != ProbeExpired && a.State != ProbeSuperseded && a.TerminalReason != "" {
		return invalidProbeAttempt()
	}
	return nil
}

// Local transition validation, not a database CAS, current authorization,
// trusted lease check or collective CheckAttempt acceptance proof.
func ValidateProbeAttemptAdvance(before, after StoreProbeAttempt, e ProbeAdvanceExpectation) error {
	if ValidateProbeAttempt(before) != nil || ValidateProbeAttempt(after) != nil || terminalProbe(before.State) || before.RowVersion == math.MaxUint64 || after.RowVersion != before.RowVersion+1 || !sameStoreProbeBinding(before.Binding, after.Binding) || before.RequestDigest != after.RequestDigest {
		return invalidProbeAttempt()
	}
	l, b := e.Lease, before.Binding
	if !storeText(string(l.ControllerID)) || l.ControlDomainID != b.ControlDomainID || l.CheckID != b.CheckID || l.FencingToken == 0 || l.RowVersion == 0 || l.FencingToken < before.FencingToken || after.FencingToken != l.FencingToken || after.ControllerID != l.ControllerID || e.Now.IsZero() || !e.Now.Before(l.ExpiresAt) || !after.UpdatedAt.Equal(e.Now) || e.Now.Before(before.UpdatedAt) || !validDigest(e.CurrentBindingDigest) || e.CurrentBindingVersion == 0 {
		return invalidProbeAttempt()
	}
	if (before.ObservationDigest != "" && after.ObservationDigest != before.ObservationDigest) || (before.CoverageDigest != "" && after.CoverageDigest != before.CoverageDigest) {
		return invalidProbeAttempt()
	}
	if after.ControllerID != before.ControllerID && after.FencingToken <= before.FencingToken {
		return invalidProbeAttempt()
	}
	changed := e.CurrentBindingDigest != b.ExpectedBindingDigest || e.CurrentBindingVersion != b.BindingVersion
	switch after.State {
	case ProbeExpired:
		if e.Now.Before(b.Deadline) || after.TerminalReason != "deadline_exceeded" || after.ObservationDigest != before.ObservationDigest || after.CoverageDigest != before.CoverageDigest {
			return invalidProbeAttempt()
		}
	case ProbeSuperseded:
		if !changed || after.TerminalReason != "binding_changed" || after.ObservationDigest != before.ObservationDigest || after.CoverageDigest != before.CoverageDigest {
			return invalidProbeAttempt()
		}
	default:
		if changed || !e.Now.Before(b.Deadline) {
			return invalidProbeAttempt()
		}
		switch after.State {
		case ProbeObserved:
			if before.State != ProbeRegistered {
				return invalidProbeAttempt()
			}
		case ProbeCoverageChecked:
			if before.State != ProbeObserved {
				return invalidProbeAttempt()
			}
		case ProbeAccepted:
			if before.State != ProbeCoverageChecked {
				return invalidProbeAttempt()
			}
		case ProbeRejected:
			if after.ObservationDigest != before.ObservationDigest || after.CoverageDigest != before.CoverageDigest {
				return invalidProbeAttempt()
			}
		default:
			return invalidProbeAttempt()
		}
	}
	return nil
}
