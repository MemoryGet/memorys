package localcli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestEvaluationSetSelectionAndPersistence(t *testing.T) {
	records := []localFeedback{
		{ID: "f1", DocumentID: "doc-1", RevisionID: "rev-1", Kind: "incorrect", State: "open", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
		{ID: "f2", DocumentID: "doc-2", RevisionID: "rev-2", Kind: "helpful", State: "resolved", CreatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)},
	}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	tags := []string{"运维"}
	selected, err := selectEvaluationFeedback(records, "open", "incorrect", "doc-1", "rev-1", tags, map[contracts.ID][]string{"doc-1": {"运维"}}, from, to)
	if err != nil || len(selected) != 1 || selected[0].ID != "f1" {
		t.Fatalf("selected=%#v err=%v", selected, err)
	}
	root := filepath.Join(t.TempDir(), "runtime")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	set := localEvaluationSet{ID: "evalset-1", Title: "质量抽样", CreatedAt: time.Now().UTC(), Samples: selected}
	if err := saveEvaluationSets(root, []localEvaluationSet{set}); err != nil {
		t.Fatal(err)
	}
	loaded, err := readEvaluationSets(root)
	if err != nil || len(loaded) != 1 || loaded[0].Samples[0].ID != "f1" {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
}

func TestEvaluationSetTitleValidation(t *testing.T) {
	for _, value := range []string{"质量抽样", " evaluation "} {
		if !validEvaluationSetTitle(value) {
			t.Fatalf("valid title rejected: %q", value)
		}
	}
	for _, value := range []string{"", "  ", "bad\nname"} {
		if validEvaluationSetTitle(value) {
			t.Fatalf("invalid title accepted: %q", value)
		}
	}
}
