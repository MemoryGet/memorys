package document_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

func candidateFixture(t *testing.T) (contracts.Draft, document.ParseCandidate) {
	t.Helper()
	blob := contracts.BlobRef{ID: "blob", SHA256: contracts.Digest(strings.Repeat("a", 64)), Size: 100}
	result := contracts.ParseResult{ParserVersion: "pypdf/6.19.0", Blocks: []contracts.ParsedBlock{{ID: "block", Content: contracts.Content{Kind: contracts.TextBlock, Text: "候选", Source: contracts.SourceAnchor{Kind: "original", BlobID: blob.ID, Locator: contracts.Locator{Precision: "coarse"}}}}}}
	candidate, err := document.CandidateFromParse(contracts.ParseRequest{DraftID: "draft", Input: blob, Request: contracts.RequestContext{ProfileDigest: contracts.Digest(strings.Repeat("b", 64))}}, 2, result)
	if err != nil {
		t.Fatal(err)
	}
	draft := contracts.Draft{ID: "draft", DocumentID: "doc", RowVersion: 2, State: "draft", Blobs: []contracts.BlobRef{blob}, Blocks: []contracts.DraftBlock{{ID: "block", DraftID: "draft", RowVersion: 7, Content: contracts.Content{Kind: contracts.TextBlock, Text: "old"}}}}
	return draft, candidate
}

func TestCandidateMergeKeepsCASAndIndependentOwnership(t *testing.T) {
	draft, candidate := candidateFixture(t)
	result, err := document.MergeParsedCandidate(draft, candidate)
	if err != nil || !result.Applied || result.Draft.RowVersion != 3 || result.Draft.Blocks[0].RowVersion != 8 {
		t.Fatalf("merge: %+v %v", result, err)
	}
	result.Draft.Blocks[0].Content.Text = "editor change"
	result.Draft.Blobs[0].Size = 200
	result.Candidate.Blocks[0].Content.Text = "retained candidate change"
	if draft.Blocks[0].Content.Text != "old" || candidate.Blocks[0].Content.Text != "候选" || draft.Blobs[0].Size != 100 {
		t.Fatal("aliased authoritative input")
	}
	if result.Draft.Blocks[0].Content.Text != "editor change" {
		t.Fatal("candidate aliases draft")
	}
}

func TestLateCandidateNeverOverwritesHumanChangesOrSubmittedDraft(t *testing.T) {
	for _, name := range []string{"edited", "submitted", "attachment replaced", "multiple originals"} {
		t.Run(name, func(t *testing.T) {
			draft, candidate := candidateFixture(t)
			switch name {
			case "edited":
				draft.RowVersion++
				draft.Blocks[0].Content.Text = "human edit"
			case "submitted":
				draft.State = "submitted"
			case "attachment replaced":
				draft.Blobs[0].SHA256 = contracts.Digest(strings.Repeat("c", 64))
			case "multiple originals":
				draft.Blobs = append(draft.Blobs, contracts.BlobRef{ID: "other"})
			}
			before := draft.Blocks[0].Content.Text
			result, err := document.MergeParsedCandidate(draft, candidate)
			if err != nil || result.Applied || result.Reason == "" || result.Draft.Blocks[0].Content.Text != before || !reflect.DeepEqual(result.Candidate, candidate) {
				t.Fatalf("late parser overwrote draft or lost candidate: %+v %v", result, err)
			}
		})
	}
}

func TestCandidateRejectsForgedOwnershipAndMissingTaskBinding(t *testing.T) {
	for _, name := range []string{"foreign draft", "foreign original", "published source", "profile", "block version", "order"} {
		t.Run(name, func(t *testing.T) {
			draft, candidate := candidateFixture(t)
			switch name {
			case "foreign draft":
				candidate.DraftID = "other"
			case "foreign original":
				candidate.Blocks[0].Content.Source.BlobID = "other"
			case "published source":
				candidate.Blocks[0].Content.Source.Kind = "revision"
			case "profile":
				candidate.ProfileDigest = ""
			case "block version":
				candidate.Blocks[0].RowVersion = 1
			case "order":
				candidate.Blocks[0].Order = 3
			}
			if result, err := document.MergeParsedCandidate(draft, candidate); err == nil || result.Applied {
				t.Fatalf("forged candidate admitted: %+v", result)
			}
		})
	}
}
