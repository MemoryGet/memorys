package localruntime

import (
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestJobListCursorBindsAllFilters(t *testing.T) {
	after := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	before := after.Add(24 * time.Hour)
	input := contracts.JobListInput{Kind: "parse", State: "paused", DocumentID: "doc", CreatedAfter: after, CreatedBefore: before}
	raw, err := encodeJobListCursor(jobListCursor{Subject: "user", After: "job-1", Kind: input.Kind, State: input.State, DocumentID: string(input.DocumentID), CreatedAfter: jobTimeText(after), CreatedBefore: jobTimeText(before)})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := decodeJobListCursor(raw, "user", input); err != nil || got != "job-1" {
		t.Fatalf("cursor did not decode: %q %v", got, err)
	}
	input.State = "failed"
	if _, err := decodeJobListCursor(raw, "user", input); err == nil {
		t.Fatal("cursor accepted changed state filter")
	}
}
