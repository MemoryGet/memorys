package queryread

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type CurrentLimits struct{ MaxDocuments, MaxBytes int }
type CurrentObservation struct {
	Identity  contracts.QueryReadIdentity
	Documents []contracts.CurrentDocumentState
}

// ObserveCurrent always opens a new callback; it cannot accept an old repository
// handle. Real freshness/ACL/consistency require an admitted read unit and fresh
// authorization. Returned state is not a permission or publication proof.
func ObserveCurrent(ctx context.Context, unit contracts.QueryReadUnit, scope contracts.AccessScope, documentIDs []contracts.ID, limits CurrentLimits) (CurrentObservation, error) {
	if ctx == nil || unit == nil || limits.MaxDocuments < 1 || limits.MaxDocuments > 1000 || limits.MaxBytes < 1 || limits.MaxBytes > 4<<20 || len(documentIDs) == 0 || len(documentIDs) > limits.MaxDocuments {
		return CurrentObservation{}, ErrInvalid
	}
	ids := append([]contracts.ID(nil), documentIDs...)
	seen := make(map[contracts.ID]bool, len(ids))
	for _, id := range ids {
		if id == "" || len(id) > 4096 || !utf8.ValidString(string(id)) || seen[id] {
			return CurrentObservation{}, ErrInvalid
		}
		seen[id] = true
	}
	guarded, err := ProtectUnit(unit)
	if err != nil {
		return CurrentObservation{}, err
	}
	var result CurrentObservation
	err = guarded.WithinRead(ctx, scope, func(c context.Context, identity contracts.QueryReadIdentity, repo contracts.QueryReadRepository) error {
		for _, value := range []string{string(identity.StoreEpochID), identity.ReadVersion, identity.Watermark} {
			if value == "" || len(value) > 4096 || !utf8.ValidString(value) {
				return ErrInvalid
			}
		}
		result.Identity = identity
		encodedIdentity, err := json.Marshal(identity)
		if err != nil {
			return ErrInvalid
		}
		if len(encodedIdentity) > limits.MaxBytes {
			return ErrLimit
		}
		remaining := limits.MaxBytes - len(encodedIdentity)
		states, err := LoadCurrentDocuments(c, repo, ids)
		if err != nil {
			return err
		}
		for _, state := range states {
			raw, err := json.Marshal(state)
			if err != nil {
				return ErrInvalid
			}
			if len(raw) > remaining {
				return ErrLimit
			}
			remaining -= len(raw)
			result.Documents = append(result.Documents, state)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return ErrInvalid
		}
		if len(encoded) > limits.MaxBytes {
			return ErrLimit
		}
		return nil
	})
	if err != nil {
		return CurrentObservation{}, err
	}
	return result, nil
}
