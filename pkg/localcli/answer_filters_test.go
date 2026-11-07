package localcli

import "testing"

func TestParseAnswerFilterNormalizesAndBounds(t *testing.T) {
	filter, err := parseAnswerFilter("revision", "published", "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z", []string{" 运维 ", "运维"})
	if err != nil || filter.Source != "revision" || filter.Status != "published" || len(filter.Tags) != 1 || filter.Tags[0] != "运维" || filter.UpdatedAfter.IsZero() || filter.UpdatedBefore.IsZero() {
		t.Fatalf("filter=%+v err=%v", filter, err)
	}
	for _, value := range []struct {
		name   string
		source string
		status string
		after  string
		before string
	}{
		{"source", "private", "", "", ""},
		{"status", "", "draft", "", ""},
		{"range", "", "", "2026-11-01T00:00:00Z", "2026-10-01T00:00:00Z"},
	} {
		if _, err := parseAnswerFilter(value.source, value.status, value.after, value.before, nil); err == nil {
			t.Fatalf("accepted invalid %s filter", value.name)
		}
	}
}
