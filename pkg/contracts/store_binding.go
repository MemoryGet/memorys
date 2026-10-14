package contracts

import (
	"context"
	"unicode/utf8"
)

type MetadataStoreIdentity struct {
	DatasetID, StoreEpochID, JournalStreamID ID
	BindingVersion, ModelVersion             uint64
	SchemaDigest                             Digest
}

type StoreEndpointBinding struct {
	Role                               Capability
	ModuleInstanceID, EndpointIdentity ID
}

// Expectation must come from an independent trusted deployment control plane.
// A copied record from the database being inspected is not an expectation.
type StoreBindingExpectation struct {
	BindingID        ID
	Identity         MetadataStoreIdentity
	ActivationCursor string
	Endpoints        []StoreEndpointBinding
}

type StoreObservation struct {
	ObservationID, BindingID ID
	Identity                 MetadataStoreIdentity
	Endpoint                 StoreEndpointBinding
	CurrentCursor            string
}

type StoreProbeRequest struct {
	Request   RequestContext
	Scope     DeploymentScope
	BindingID ID
	Endpoint  StoreEndpointBinding
}

type StoreIdentityReader interface {
	ObserveStore(context.Context, StoreProbeRequest) (StoreObservation, error)
}

type StoreCoverageRequest struct {
	Request     RequestContext
	Scope       DeploymentScope
	Expectation StoreBindingExpectation
	Observation StoreObservation
}

type StoreCoverageReceipt struct {
	ObservationID, JournalStreamID ID
	RequiredCursor, ObservedCursor string
	VerificationReference          ID
}

// This port must inspect real sealed commits and lineage. It may not implement
// coverage by comparing cursor strings or treating matching identities as proof.
type StoreCoverageVerifier interface {
	VerifyStoreCoverage(context.Context, StoreCoverageRequest) (StoreCoverageReceipt, error)
}

func storeText(v string) bool { return v != "" && len(v) <= 4096 && utf8.ValidString(v) }

// ValidateStoreBindingExpectation checks local shape, not independent origin.
func ValidateStoreBindingExpectation(expected StoreBindingExpectation) error {
	invalid := func() error { return PublicError{Code: "validation_failed", SafeMessage: "Invalid store observation"} }
	id := expected.Identity
	if !storeText(string(expected.BindingID)) || !storeText(string(id.DatasetID)) || !storeText(string(id.StoreEpochID)) || !storeText(string(id.JournalStreamID)) || id.BindingVersion == 0 || id.ModelVersion == 0 || !validDigest(id.SchemaDigest) || !storeText(expected.ActivationCursor) || len(expected.Endpoints) < 2 || len(expected.Endpoints) > 5 {
		return invalid()
	}
	bindings := map[Capability]StoreEndpointBinding{}
	for _, endpoint := range expected.Endpoints {
		if endpoint.Role != Transactions && endpoint.Role != QueryReading && endpoint.Role != Metadata && endpoint.Role != TaskFacts && endpoint.Role != Audit {
			return invalid()
		}
		if _, duplicate := bindings[endpoint.Role]; duplicate || !storeText(string(endpoint.ModuleInstanceID)) || !storeText(string(endpoint.EndpointIdentity)) {
			return invalid()
		}
		bindings[endpoint.Role] = endpoint
	}
	if _, ok := bindings[Transactions]; !ok {
		return invalid()
	}
	if _, ok := bindings[QueryReading]; !ok {
		return invalid()
	}
	return nil
}

// ValidateStoreObservations checks identity and exact endpoint inventory only.
// It never checks provenance/freshness or cursor coverage, and grants no readiness.
func ValidateStoreObservations(expected StoreBindingExpectation, observations []StoreObservation) error {
	invalid := func() error { return PublicError{Code: "validation_failed", SafeMessage: "Invalid store observation"} }
	if ValidateStoreBindingExpectation(expected) != nil || len(observations) != len(expected.Endpoints) {
		return invalid()
	}
	id := expected.Identity
	bindings := map[Capability]StoreEndpointBinding{}
	for _, endpoint := range expected.Endpoints {
		bindings[endpoint.Role] = endpoint
	}
	roles := map[Capability]bool{}
	receipts := map[ID]bool{}
	for _, observation := range observations {
		binding, ok := bindings[observation.Endpoint.Role]
		if !ok || roles[observation.Endpoint.Role] || observation.Endpoint != binding || observation.BindingID != expected.BindingID || observation.Identity != id || !storeText(string(observation.ObservationID)) || receipts[observation.ObservationID] || !storeText(observation.CurrentCursor) {
			return invalid()
		}
		roles[observation.Endpoint.Role] = true
		receipts[observation.ObservationID] = true
	}
	return nil
}
