package modulewire

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type storeBindingReview struct {
	SchemaVersion int
	Expectation   contracts.StoreBindingExpectation
	Observations  []contracts.StoreObservation
}

func init() {
	register(contracts.MetadataStoreIdentity{}, "DatasetID:dataset_id StoreEpochID:metadata_store_epoch_id JournalStreamID:journal_stream_id BindingVersion:binding_version ModelVersion:metadata_model_version SchemaDigest:schema_digest")
	register(contracts.StoreEndpointBinding{}, "Role:role ModuleInstanceID:module_instance_id EndpointIdentity:endpoint_identity")
	register(contracts.StoreBindingExpectation{}, "BindingID:binding_id Identity:identity ActivationCursor:activation_cursor Endpoints:endpoints")
	register(contracts.StoreObservation{}, "ObservationID:observation_id BindingID:binding_id Identity:identity Endpoint:endpoint CurrentCursor:current_cursor")
	register(storeBindingReview{}, "SchemaVersion:schema_version Expectation:expectation Observations:observations")
}

// These codecs validate a supplied offline review, not its independent origin,
// current store state, authenticated service or journal coverage.
func EncodeStoreBindingReview(expected contracts.StoreBindingExpectation, observations []contracts.StoreObservation) ([]byte, error) {
	if contracts.ValidateStoreObservations(expected, observations) != nil {
		return nil, invalid()
	}
	return encode(storeBindingReview{1, expected, observations})
}

func strictStoreIntegers(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var root map[string]any
	if d.Decode(&root) != nil {
		return false
	}
	var check func(any) bool
	check = func(v any) bool {
		switch value := v.(type) {
		case json.Number:
			return !strings.ContainsAny(string(value), ".eE")
		case map[string]any:
			for _, item := range value {
				if !check(item) {
					return false
				}
			}
		case []any:
			for _, item := range value {
				if !check(item) {
					return false
				}
			}
		}
		return true
	}
	return check(root)
}

func DecodeStoreBindingReview(raw []byte) (contracts.StoreBindingExpectation, []contracts.StoreObservation, error) {
	var review storeBindingReview
	if decode(raw, &review) != nil || review.SchemaVersion != 1 || !strictStoreIntegers(raw) {
		return contracts.StoreBindingExpectation{}, nil, invalid()
	}
	if contracts.ValidateStoreObservations(review.Expectation, review.Observations) != nil {
		return contracts.StoreBindingExpectation{}, nil, invalid()
	}
	return review.Expectation, review.Observations, nil
}
