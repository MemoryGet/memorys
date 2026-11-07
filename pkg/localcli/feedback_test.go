package localcli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestValidFeedbackKind(t *testing.T) {
	for _, value := range []string{"helpful", "incorrect", "missing", "outdated", "other"} {
		if !validFeedbackKind(value) {
			t.Errorf("valid feedback kind %q rejected", value)
		}
	}
	for _, value := range []string{"", "HELPFUL", "unknown"} {
		if validFeedbackKind(value) {
			t.Errorf("invalid feedback kind %q accepted", value)
		}
	}
}

func TestValidFeedbackText(t *testing.T) {
	if !validFeedbackText("引用的第二段与问题无关") {
		t.Fatal("expected normal feedback text to be accepted")
	}
	for _, value := range []string{"", "   "} {
		if validFeedbackText(value) {
			t.Errorf("blank feedback text %q accepted", value)
		}
	}
	if validFeedbackText(string(make([]byte, 10001))) {
		t.Fatal("oversized feedback text accepted")
	}
}

func TestValidFeedbackQueryAllowsOmittedAndBoundsInput(t *testing.T) {
	if !validFeedbackQuery("") || !validFeedbackQuery("数据库如何恢复？") {
		t.Fatal("valid feedback query rejected")
	}
	if validFeedbackQuery(strings.Repeat("q", 4097)) || validFeedbackQuery("bad\x00query") {
		t.Fatal("invalid feedback query accepted")
	}
}

func TestReadFeedbackNormalizesLegacyRecords(t *testing.T) {
	root := t.TempDir()
	raw, err := json.Marshal([]localFeedback{{ID: "feedback-1", Kind: "helpful", Comment: "ok"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "feedback.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	records, err := readFeedback(root)
	if err != nil || len(records) != 1 || records[0].State != "open" {
		t.Fatalf("legacy feedback was not normalized: %#v %v", records, err)
	}
}

func TestFilterFeedbackValidatesAndSelectsExportRows(t *testing.T) {
	records := []localFeedback{
		{ID: "one", DocumentID: "doc-1", Kind: "helpful", State: "open"},
		{ID: "two", DocumentID: "doc-2", Kind: "incorrect", State: "resolved"},
	}
	filtered, err := filterFeedback(records, "resolved", "incorrect", "doc-2")
	if err != nil || len(filtered) != 1 || filtered[0].ID != "two" {
		t.Fatalf("filtered feedback = %#v, err=%v", filtered, err)
	}
	for _, invalid := range []struct{ state, kind, document string }{{"waiting", "", ""}, {"", "bad", ""}, {"", "", "line\nfeed"}} {
		if _, err := filterFeedback(records, invalid.state, invalid.kind, invalid.document); err == nil {
			t.Fatalf("invalid feedback filter accepted: %#v", invalid)
		}
	}
}

func TestFeedbackTimeBoundsValidateOrderedRFC3339Range(t *testing.T) {
	from, to, err := feedbackTimeBounds("2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z")
	if err != nil || !from.Before(to) {
		t.Fatalf("bounds=%v %v err=%v", from, to, err)
	}
	if _, _, err := feedbackTimeBounds("bad", ""); err == nil {
		t.Fatal("invalid from accepted")
	}
	if _, _, err := feedbackTimeBounds("2026-02-01T00:00:00Z", "2026-01-01T00:00:00Z"); err == nil {
		t.Fatal("reversed range accepted")
	}
	if from.Equal(time.Time{}) || to.Equal(time.Time{}) {
		t.Fatal("valid bounds unexpectedly zero")
	}
}

func TestFilterFeedbackTimeUsesHalfOpenRange(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	records := []localFeedback{{ID: "before", CreatedAt: start.Add(-time.Second)}, {ID: "inside", CreatedAt: start.Add(time.Hour)}, {ID: "end", CreatedAt: end}}
	filtered := filterFeedbackTime(records, start, end)
	if len(filtered) != 1 || filtered[0].ID != "inside" {
		t.Fatalf("filtered=%#v", filtered)
	}
}

func TestFeedbackScopeFilters(t *testing.T) {
	records := []localFeedback{{ID: "f1", DocumentID: "doc-1", RevisionID: "rev-1"}, {ID: "f2", DocumentID: "doc-2", RevisionID: "rev-2"}}
	filtered, err := filterFeedbackRevision(records, "rev-1")
	if err != nil || len(filtered) != 1 || filtered[0].ID != "f1" {
		t.Fatalf("revision filter: %#v %v", filtered, err)
	}
	filtered = filterFeedbackTags(records, []string{"运维"}, map[contracts.ID][]string{"doc-1": {"数据库", "运维"}, "doc-2": {"产品"}})
	if len(filtered) != 1 || filtered[0].ID != "f1" {
		t.Fatalf("tag filter: %#v", filtered)
	}
	if _, err := filterFeedbackRevision(records, "bad\nrevision"); err == nil {
		t.Fatal("invalid revision accepted")
	}
}

func TestCanonicalOriginalFormatAvoidsJPEGAmbiguity(t *testing.T) {
	if canonicalOriginalFormat("jpeg") != "jpg" || !supportedOriginalFormat("jpeg") || mediaFormats[canonicalOriginalFormat("jpeg")] != "image/jpeg" {
		t.Fatal("jpeg was not mapped to the canonical image format")
	}
}

func TestOriginalHeaderValidation(t *testing.T) {
	if !originalHeaderValid("png", []byte("\x89PNG\r\n\x1a\ncontent")) || !originalHeaderValid("jpg", []byte{0xff, 0xd8, 0xff, 0xe0}) || originalHeaderValid("png", []byte("PKzip")) {
		t.Fatal("image header validation is not strict")
	}
}

func TestNormalizeTagsAndMatch(t *testing.T) {
	tags, err := normalizeTags([]string{" 运维 ", "数据库", "运维"})
	if err != nil || len(tags) != 2 || !tagsMatch(tags, []string{"运维"}) || tagsMatch(tags, []string{"缺失"}) {
		t.Fatalf("tags: %#v %v", tags, err)
	}
}

func TestTagsPersistInManifest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runtime")
	s, err := createState(root)
	if err != nil {
		t.Fatal(err)
	}
	s.m.Tags = map[contracts.ID][]string{"doc-1": {"数据库", "运维"}}
	if err := writeAtomic(root, "manifest.json", encode(s.m)); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadState(root, false)
	if err != nil || !reflect.DeepEqual(loaded.m.Tags, s.m.Tags) {
		t.Fatalf("tags did not survive manifest reload: %#v %v", loaded.m.Tags, err)
	}
}
