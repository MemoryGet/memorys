package contracts_test

import (
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestMemorySourceCategoriesRequireTheirEvidence(t *testing.T) {
	id := contracts.ID("source")
	citation := contracts.Citation{DocumentID: "document", RevisionID: "revision", BlockID: "block"}
	for _, p := range []contracts.MemoryProvenance{
		{Kind: contracts.MemoryUserConfirmed, ConfirmationID: &id},
		{Kind: contracts.MemoryTaskExperience, TaskID: &id},
		{Kind: contracts.MemoryTaskExperience, FeedbackID: &id},
		{Kind: contracts.MemoryKnowledgeSummary, Citations: []contracts.Citation{citation}},
		{Kind: contracts.MemoryTaskExperience, TaskID: &id, Citations: []contracts.Citation{citation}},
	} {
		if err := contracts.ValidateMemoryProvenance(p); err != nil {
			t.Fatal(p, err)
		}
	}
	for _, p := range []contracts.MemoryProvenance{
		{Kind: "model_inferred"},
		{Kind: contracts.MemoryUserConfirmed},
		{Kind: contracts.MemoryTaskExperience},
		{Kind: contracts.MemoryKnowledgeSummary},
		{Kind: contracts.MemoryUserConfirmed, ConfirmationID: &id, TaskID: &id},
		{Kind: contracts.MemoryTaskExperience, TaskID: &id, ConfirmationID: &id},
		{Kind: contracts.MemoryKnowledgeSummary, Citations: []contracts.Citation{citation}, FeedbackID: &id},
	} {
		if contracts.ValidateMemoryProvenance(p) == nil {
			t.Fatal("missing/mixed source accepted", p)
		}
	}
}

func TestMemorySourceRejectsUnboundedAndConflictingIdentifiers(t *testing.T) {
	id := contracts.ID("")
	if contracts.ValidateMemoryProvenance(contracts.MemoryProvenance{Kind: contracts.MemoryUserConfirmed, ConfirmationID: &id}) == nil {
		t.Fatal("empty confirmation")
	}
	p := contracts.MemoryProvenance{Kind: contracts.MemoryKnowledgeSummary, Citations: []contracts.Citation{{DocumentID: "d", RevisionID: "r", BlockID: "b"}, {DocumentID: "other", RevisionID: "r", BlockID: "b"}}}
	if contracts.ValidateMemoryProvenance(p) == nil {
		t.Fatal("revision has conflicting document owners")
	}
	p.Citations = []contracts.Citation{{DocumentID: "d", RevisionID: "r"}}
	if contracts.ValidateMemoryProvenance(p) == nil {
		t.Fatal("missing block")
	}
	p.Citations = make([]contracts.Citation, 257)
	if contracts.ValidateMemoryProvenance(p) == nil {
		t.Fatal("unbounded citations")
	}
}
