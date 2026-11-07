package localcli

import (
	"reflect"
	"testing"
)

func TestMergeTagsProducesStableSet(t *testing.T) {
	got, err := mergeTags([]string{"运维", "数据库"}, []string{" 数据库 ", "告警"}, []string{"运维"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"数据库", "告警"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeTags() = %#v, want %#v", got, want)
	}
}

func TestMergeTagsRejectsTooManyTags(t *testing.T) {
	existing := make([]string, 20)
	for i := range existing {
		existing[i] = string(rune('a' + i))
	}
	if _, err := mergeTags(existing, []string{"extra"}, nil); err == nil {
		t.Fatal("mergeTags accepted more than 20 tags")
	}
}

func TestMergeTagsRemovesMissingTagIdempotently(t *testing.T) {
	got, err := mergeTags([]string{"a"}, nil, []string{"missing"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("mergeTags() = %#v, want [a]", got)
	}
}
