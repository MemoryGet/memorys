package contracts

import (
	"context"
	"errors"
	"time"
)

// DeploymentScope is separate from KB AccessScope. It is authorization data,
// not proof: governance must validate issuer, current policy and operator.
type DeploymentScope struct {
	ID            ID
	SubjectID     ID
	DomainID      ID
	Action        Action
	PolicyVersion uint64
	ExpiresAt     time.Time
}

type DeploymentAuthorizeRequest struct {
	Principal   Principal
	DomainID    ID
	Action      Action
	ResourceIDs []ID
}
type DeploymentAuthorizer interface {
	AuthorizeDeployment(context.Context, DeploymentAuthorizeRequest) (DeploymentScope, error)
}

type TrustDomain struct {
	ID                       ID
	IssuerID                 ID
	PublisherPolicyReference ID
	RowVersion               uint64
}
type TrustSnapshotIdentity struct {
	ID      ID
	Version uint64
	Digest  Digest
}
type TrustHead struct {
	DomainID         ID
	IssuerID         ID
	Active           TrustSnapshotIdentity
	BuildLockDigest  Digest
	HighWaterVersion uint64
	RowVersion       uint64
}
type TrustSnapshotRecord struct {
	DomainID            ID
	Identity            TrustSnapshotIdentity
	PublicConfig        BlobRef
	ProvenanceReference ID
	ApprovalReference   ID
}
type KeyRetirement struct {
	DomainID             ID
	IssuerID             ID
	KeyID                ID
	PublicKeyFingerprint Digest
	Reason               string
	Permanent            bool
	OperationID          ID
	RecordedAt           time.Time
}
type TrustActivation struct {
	ID                       ID
	RowVersion               uint64
	DomainID                 ID
	ActorID                  ID
	ExpectedHeadRowVersion   uint64
	ExpectedHighWaterVersion uint64
	Candidate                TrustSnapshotIdentity
	BuildLockDigest          Digest
	RequestDigest            Digest
	State                    string
}

// Deployment records intentionally carry DomainID rather than a fake SpaceID.
type DeploymentAuditRecord struct {
	ID          ID
	DomainID    ID
	ActorID     ID
	OperationID ID
	Action      Action
	ResourceIDs []ID
	OccurredAt  time.Time
}
type DeploymentEvent struct {
	ID             ID
	DomainID       ID
	Type           string
	OperationID    ID
	HeadRowVersion uint64
	Snapshot       TrustSnapshotIdentity
	OccurredAt     time.Time
}

// All writes are transaction-scoped. Repository methods do not authorize a
// caller, verify provenance or prove external distribution. Only governance
// orchestration can change TrustHead; retired identities are append-only.
type TrustRepository interface {
	LoadDomain(context.Context, ID) (TrustDomain, error)
	LoadHead(context.Context, ID) (TrustHead, error)
	LoadSnapshot(context.Context, ID, TrustSnapshotIdentity) (TrustSnapshotRecord, error)
	ListRetirements(context.Context, ID) ([]KeyRetirement, error)
	LoadActivation(context.Context, ID, ID) (TrustActivation, error)
	CreateSnapshot(context.Context, TrustSnapshotRecord) error
	SaveActivation(context.Context, TrustActivation, uint64) error
	CompareAndSwapHead(context.Context, TrustHead, uint64) error
	AppendRetirement(context.Context, KeyRetirement) error
	AppendAudit(context.Context, DeploymentAuditRecord) error
	AppendOutbox(context.Context, DeploymentEvent) error
	AppendJournal(context.Context, MigrationJournalRecord) error
}

// Within commits head/activation/retirement/audit/outbox/journal atomically or
// rolls them all back. Callback repositories cannot escape; no network calls
// belong in this transaction. This is a port, not a database implementation.
type TrustUnitOfWork interface {
	WithinTrust(context.Context, DeploymentScope, func(context.Context, TrustRepository) error) error
}

// ValidateTrustHeadAdvance checks record relationships only. It does not
// execute a database CAS, authenticate provenance or inspect retired keys.
func ValidateTrustHeadAdvance(before, after TrustHead) error {
	invalid := errors.New("invalid trust head advance")
	if ValidateTrustHeadRecord(before) != nil || ValidateTrustHeadRecord(after) != nil {
		return invalid
	}
	if before.DomainID == "" || before.IssuerID == "" || before.DomainID != after.DomainID || before.IssuerID != after.IssuerID {
		return invalid
	}
	if before.RowVersion == 0 || before.RowVersion == ^uint64(0) || after.RowVersion != before.RowVersion+1 {
		return invalid
	}
	if before.HighWaterVersion != before.Active.Version || after.HighWaterVersion != after.Active.Version || after.HighWaterVersion <= before.HighWaterVersion {
		return invalid
	}
	return nil
}

// ValidateTrustHeadRecord validates a single authority record's relationships,
// not the provenance, current policy, storage transaction or recovery anchor.
func ValidateTrustHeadRecord(value TrustHead) error {
	if value.DomainID == "" || value.IssuerID == "" || value.Active.ID == "" || value.Active.Version == 0 || value.RowVersion == 0 || value.HighWaterVersion != value.Active.Version || !trustDigest(value.Active.Digest) || !trustDigest(value.BuildLockDigest) {
		return errors.New("invalid trust head record")
	}
	return nil
}

func trustDigest(value Digest) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
