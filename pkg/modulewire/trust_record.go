package modulewire

import "github.com/MemoryGet/memorys/pkg/contracts"

func init() {
	register(trustIdentityRecord{}, "ID:id Version:version Digest:digest")
	register(trustHeadRecord{}, "DomainID:domain_id IssuerID:issuer_id Active:active BuildLockDigest:build_lock_digest HighWaterVersion:high_water_version RowVersion:row_version")
}

type trustIdentityRecord struct {
	ID      contracts.ID     `json:"id"`
	Version uint64           `json:"version"`
	Digest  contracts.Digest `json:"digest"`
}
type trustHeadRecord struct {
	DomainID         contracts.ID        `json:"domain_id"`
	IssuerID         contracts.ID        `json:"issuer_id"`
	Active           trustIdentityRecord `json:"active"`
	BuildLockDigest  contracts.Digest    `json:"build_lock_digest"`
	HighWaterVersion uint64              `json:"high_water_version"`
	RowVersion       uint64              `json:"row_version"`
}

// EncodeTrustHeadRecord is the metadata payload codec, not an authority proof.
func EncodeTrustHeadRecord(value contracts.TrustHead) ([]byte, error) {
	if err := contracts.ValidateTrustHeadRecord(value); err != nil {
		return nil, err
	}
	if err := ensureIDs(value.DomainID, value.IssuerID, value.Active.ID); err != nil {
		return nil, err
	}
	for _, id := range []contracts.ID{value.DomainID, value.IssuerID, value.Active.ID} {
		if len([]rune(id)) > 200 {
			return nil, invalid()
		}
	}
	return encode(trustHeadRecord{value.DomainID, value.IssuerID, trustIdentityRecord{value.Active.ID, value.Active.Version, value.Active.Digest}, value.BuildLockDigest, value.HighWaterVersion, value.RowVersion})
}

func DecodeTrustHeadRecord(raw []byte) (contracts.TrustHead, error) {
	var row trustHeadRecord
	if err := decode(raw, &row); err != nil {
		return contracts.TrustHead{}, err
	}
	value := contracts.TrustHead{DomainID: row.DomainID, IssuerID: row.IssuerID, Active: contracts.TrustSnapshotIdentity{ID: row.Active.ID, Version: row.Active.Version, Digest: row.Active.Digest}, BuildLockDigest: row.BuildLockDigest, HighWaterVersion: row.HighWaterVersion, RowVersion: row.RowVersion}
	if _, err := EncodeTrustHeadRecord(value); err != nil {
		return contracts.TrustHead{}, err
	}
	return value, nil
}
