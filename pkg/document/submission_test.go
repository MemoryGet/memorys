package document_test

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

func TestSubmissionPreparesIndependentVersionAndPendingReview(t *testing.T) {
	draft := frozenDraftFixture()
	at := time.Date(2026, 10, 15, 9, 1, 2, 3, time.FixedZone("test", 8*3600))
	p, err := document.PrepareSubmission(draft, 1, "revision", "review", "actor", 7, at)
	if err != nil {
		t.Fatal(err)
	}
	if p.Draft.RowVersion != 2 || p.Draft.State != "submitted" || p.Revision.Blocks[0].RevisionID != "revision" || p.Approval.State != "submitted" || p.Approval.RowVersion != 0 || p.Approval.PolicyVersion != 7 || p.Approval.ContentHash != p.Revision.ContentHash || !p.Revision.CreatedAt.Equal(at) || p.Revision.CreatedAt.Location() != time.UTC {
		t.Fatal("submission facts inconsistent")
	}
	if draft.State != "draft" || draft.RowVersion != 1 {
		t.Fatal("input draft changed")
	}
	want := p.Revision.Blocks[0].Content.Text
	p.Draft.Blocks[0].Content.Text = "edited"
	p.Draft.Blobs[0].MediaType = "changed"
	draft.Blocks[0].Content.Text = "caller edit"
	if p.Revision.Blocks[0].Content.Text != want || p.Revision.Blobs[0].MediaType != "text/plain" {
		t.Fatal("revision content aliased")
	}
	reconstructed := frozenDraftFixture()
	reconstructed.Blocks = nil
	for _, block := range p.Revision.Blocks {
		reconstructed.Blocks = append(reconstructed.Blocks, contracts.DraftBlock{ID: block.ID, DraftID: reconstructed.ID, RowVersion: 1, Order: block.Order, Content: block.Content})
	}
	reconstructed.Blobs = p.Revision.Blobs
	frozen, err := document.BuildFrozenContent(reconstructed)
	if err != nil || !bytes.Equal(frozen.Payload, p.Frozen.Payload) || frozen.ContentHash != p.Revision.ContentHash {
		t.Fatal("revision cannot reproduce frozen identity", err)
	}
}

func TestSubmissionRejectsStaleSubmittedAndOverflowDraft(t *testing.T) {
	for _, change := range []func(*contracts.Draft){
		func(d *contracts.Draft) { d.State = "submitted" },
		func(d *contracts.Draft) { d.RowVersion = 2 },
		func(d *contracts.Draft) { d.RowVersion = math.MaxUint64 },
		func(d *contracts.Draft) { d.Blocks = nil },
		func(d *contracts.Draft) { id := contracts.ID("revision"); d.BaseRevisionID = &id },
	} {
		d := frozenDraftFixture()
		change(&d)
		before, _ := json.Marshal(d)
		if _, err := document.PrepareSubmission(d, 1, "revision", "review", "actor", 1, time.Now()); err == nil {
			t.Fatal("invalid submission accepted")
		}
		after, _ := json.Marshal(d)
		if !bytes.Equal(before, after) {
			t.Fatal("failed submission changed input")
		}
	}
}
