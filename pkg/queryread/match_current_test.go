package queryread_test

import (
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func currentMatchFixture() (queryread.CurrentExpectation, queryread.CurrentObservation) {
	digest := contracts.Digest(strings.Repeat("a", 64))
	set := contracts.GenerationSet{ID: "set", RevisionID: "revision", BackendEpochID: "epoch", State: "ready", Channels: map[contracts.Channel]contracts.IndexGeneration{contracts.KeywordChannel: {ID: "generation", KnowledgeBaseID: "kb", RevisionID: "revision", BackendEpochID: "epoch", Channel: contracts.KeywordChannel, ProfileDigest: digest, State: "ready"}}}
	fact := contracts.PublishedFact{DocumentID: "doc", SpaceID: "space", KnowledgeBaseID: "kb", RevisionID: "revision", RowVersion: 1, PublicationEpoch: 1, ApprovalID: "approval", ContentDigest: digest, GenerationSet: set}
	revision, id := contracts.ID("revision"), contracts.ID("set")
	doc := contracts.Document{ID: "doc", SpaceID: "space", KnowledgeBaseID: "kb", RowVersion: 1, PublicationEpoch: 1, ActiveRevisionID: &revision, ActiveGenerationSetID: &id}
	currentSet := set
	currentSet.Channels = map[contracts.Channel]contracts.IndexGeneration{contracts.KeywordChannel: set.Channels[contracts.KeywordChannel]}
	return queryread.CurrentExpectation{StoreEpochID: "store", ScopeVersion: 1, Published: []contracts.PublishedFact{fact}}, queryread.CurrentObservation{Identity: contracts.QueryReadIdentity{StoreEpochID: "store", ReadVersion: "current", Watermark: "watermark"}, Documents: []contracts.CurrentDocumentState{{Document: doc, ActiveBackendEpochID: "epoch", GenerationSet: &currentSet}}}
}

func TestCurrentMatchRejectsWithdrawVersionEpochAndProfileChanges(t *testing.T) {
	for _, change := range []func(*queryread.CurrentObservation){
		func(o *queryread.CurrentObservation) { o.Identity.StoreEpochID = "restored" },
		func(o *queryread.CurrentObservation) { o.Documents[0].Document.ActiveRevisionID = nil },
		func(o *queryread.CurrentObservation) {
			r := contracts.ID("new-revision")
			o.Documents[0].Document.ActiveRevisionID = &r
		},
		func(o *queryread.CurrentObservation) { o.Documents[0].Document.PublicationEpoch++ },
		func(o *queryread.CurrentObservation) { o.Documents[0].ActiveBackendEpochID = "new-epoch" },
		func(o *queryread.CurrentObservation) { n := time.Now(); o.Documents[0].Document.DeletedAt = &n },
		func(o *queryread.CurrentObservation) { o.Documents[0].GenerationSet.State = "building" },
		func(o *queryread.CurrentObservation) {
			g := o.Documents[0].GenerationSet.Channels[contracts.KeywordChannel]
			g.ProfileDigest = contracts.Digest(strings.Repeat("b", 64))
			o.Documents[0].GenerationSet.Channels[contracts.KeywordChannel] = g
		},
	} {
		expected, observed := currentMatchFixture()
		change(&observed)
		if queryread.MatchCurrent(currentScope(), expected, observed) != queryread.ErrStale {
			t.Fatal("stale metadata accepted")
		}
	}
}

func TestCurrentMatchAllowsBenignRowAdvanceButChecksScopeVersion(t *testing.T) {
	expected, observed := currentMatchFixture()
	observed.Documents[0].Document.RowVersion++
	if err := queryread.MatchCurrent(currentScope(), expected, observed); err != nil {
		t.Fatal(err)
	}
	scope := currentScope()
	scope.PolicyVersion++
	if queryread.MatchCurrent(scope, expected, observed) != queryread.ErrStale {
		t.Fatal("old policy snapshot reused")
	}
	observed.Documents[0].Document.RowVersion = 0
	if queryread.MatchCurrent(currentScope(), expected, observed) != queryread.ErrStale {
		t.Fatal("row version rollback accepted")
	}
}

func TestCurrentMatchRejectsMissingDuplicateAndUnpublishedExpectations(t *testing.T) {
	expected, observed := currentMatchFixture()
	observed.Documents = nil
	if queryread.MatchCurrent(currentScope(), expected, observed) != queryread.ErrInvalid {
		t.Fatal("incomplete current batch")
	}
	expected, observed = currentMatchFixture()
	expected.Published[0].ApprovalID = ""
	if queryread.MatchCurrent(currentScope(), expected, observed) != queryread.ErrInvalid {
		t.Fatal("unapproved expected fact")
	}
	expected, observed = currentMatchFixture()
	observed.Documents[0].Document.ID = "foreign"
	if queryread.MatchCurrent(currentScope(), expected, observed) != queryread.ErrInvalid {
		t.Fatal("foreign batch accepted")
	}
	expected, observed = currentMatchFixture()
	second := expected.Published[0]
	second.DocumentID, second.RevisionID, second.ApprovalID = "second-doc", "second-revision", "second-approval"
	second.GenerationSet.ID, second.GenerationSet.RevisionID = "second-set", second.RevisionID
	generation := second.GenerationSet.Channels[contracts.KeywordChannel]
	generation.ID, generation.RevisionID = "second-generation", second.RevisionID
	second.GenerationSet.Channels = map[contracts.Channel]contracts.IndexGeneration{contracts.KeywordChannel: generation}
	expected.Published = append(expected.Published, second)
	observed.Documents = append(observed.Documents, observed.Documents[0])
	if queryread.MatchCurrent(currentScope(), expected, observed) != queryread.ErrInvalid {
		t.Fatal("duplicate current document accepted")
	}
}
