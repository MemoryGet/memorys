package contracts_test

import (
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func storeFixture() (contracts.StoreBindingExpectation, []contracts.StoreObservation) {
	id := contracts.MetadataStoreIdentity{DatasetID: "dataset", StoreEpochID: "store", JournalStreamID: "journal", BindingVersion: 2, ModelVersion: 1, SchemaDigest: contracts.Digest(strings.Repeat("a", 64))}
	expected := contracts.StoreBindingExpectation{BindingID: "binding", Identity: id, ActivationCursor: "required", Endpoints: []contracts.StoreEndpointBinding{{Role: contracts.Transactions, ModuleInstanceID: "writer", EndpointIdentity: "writer-endpoint"}, {Role: contracts.QueryReading, ModuleInstanceID: "reader", EndpointIdentity: "reader-endpoint"}}}
	var observations []contracts.StoreObservation
	for _, endpoint := range expected.Endpoints {
		observations = append(observations, contracts.StoreObservation{ObservationID: contracts.ID("observation-" + string(endpoint.Role)), BindingID: expected.BindingID, Identity: id, Endpoint: endpoint, CurrentCursor: "unverified-cursor"})
	}
	return expected, observations
}

func TestStoreIdentityAllowsDistinctReaderAndWriterInstances(t *testing.T) {
	e, o := storeFixture()
	if err := contracts.ValidateStoreObservations(e, o); err != nil {
		t.Fatal(err)
	}
	o[0], o[1] = o[1], o[0]
	if err := contracts.ValidateStoreObservations(e, o); err != nil {
		t.Fatal("receipt order affected identity", err)
	}
}

func TestStoreIdentityRejectsDatasetEpochModelAndBindingMismatch(t *testing.T) {
	for _, change := range []func(*contracts.StoreObservation){func(o *contracts.StoreObservation) { o.Identity.DatasetID = "clone" }, func(o *contracts.StoreObservation) { o.Identity.StoreEpochID = "backup" }, func(o *contracts.StoreObservation) { o.Identity.BindingVersion-- }, func(o *contracts.StoreObservation) { o.Identity.ModelVersion++ }, func(o *contracts.StoreObservation) {
		o.Identity.SchemaDigest = contracts.Digest(strings.Repeat("b", 64))
	}, func(o *contracts.StoreObservation) { o.Identity.JournalStreamID = "other" }, func(o *contracts.StoreObservation) { o.Endpoint.EndpointIdentity = "other" }, func(o *contracts.StoreObservation) { o.Endpoint.ModuleInstanceID = "other" }} {
		e, o := storeFixture()
		change(&o[1])
		if err := contracts.ValidateStoreObservations(e, o); err == nil {
			t.Fatal("mismatched store accepted")
		}
	}
}

func TestStoreIdentityRejectsMissingExtraAndReusedObservations(t *testing.T) {
	e, o := storeFixture()
	if contracts.ValidateStoreObservations(e, o[:1]) == nil {
		t.Fatal("missing reader accepted")
	}
	if contracts.ValidateStoreObservations(e, append(o, o[0])) == nil {
		t.Fatal("extra observation accepted")
	}
	o[1] = o[0]
	if contracts.ValidateStoreObservations(e, o) == nil {
		t.Fatal("duplicate role accepted")
	}
	e, o = storeFixture()
	o[1].ObservationID = o[0].ObservationID
	if contracts.ValidateStoreObservations(e, o) == nil {
		t.Fatal("receipt reused")
	}
}

func TestStoreIdentityDoesNotPretendToValidateCursorCoverage(t *testing.T) {
	e, o := storeFixture()
	o[0].CurrentCursor = "earlier-or-unknown"
	o[1].CurrentCursor = "lexically-z-but-unverified"
	if err := contracts.ValidateStoreObservations(e, o); err != nil {
		t.Fatal("identity checker improperly ordered cursors", err)
	}
	e.Identity.BindingVersion = 0
	if contracts.ValidateStoreObservations(e, o) == nil {
		t.Fatal("invalid expectation accepted")
	}
}
