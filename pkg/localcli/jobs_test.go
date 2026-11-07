package localcli

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestLastEventIDIsOnlyAReconnectSequence(t *testing.T) {
	request := httptest.NewRequest("GET", "/local/api/jobs/events", nil)
	if got := lastEventID(request); got != 0 {
		t.Fatalf("missing event id: %d", got)
	}
	request.Header.Set("Last-Event-ID", "41")
	if got := lastEventID(request); got != 41 {
		t.Fatalf("event id: %d", got)
	}
	request.Header.Set("Last-Event-ID", "forged-cursor")
	if got := lastEventID(request); got != 0 {
		t.Fatalf("invalid event id accepted: %d", got)
	}
}

func TestTaskListFilterIsStrictAndServerSide(t *testing.T) {
	request := httptest.NewRequest("GET", "/local/api/jobs?kind=parse&state=failed&document_id=doc", nil)
	filter, err := parseTaskListFilter(request)
	if err != nil || filter.Kind != "parse" || filter.State != "failed" || filter.DocumentID != "doc" {
		t.Fatalf("filter=%#v err=%v", filter, err)
	}
	tasks := []webTask{
		{Kind: "parse", DocumentID: "doc", Job: &webJob{State: "failed"}},
		{Kind: "parse", DocumentID: "doc", Job: &webJob{State: "completed"}},
		{Kind: "publication", DocumentID: "doc", Job: &webJob{State: "failed"}},
		{Kind: "parse", DocumentID: contracts.ID("other"), Job: &webJob{State: "failed"}},
	}
	filtered := filterTasks(tasks, filter)
	if len(filtered) != 1 || filtered[0].Kind != "parse" || filtered[0].Job.State != "failed" {
		t.Fatalf("filtered=%#v", filtered)
	}
	if _, err := parseTaskListFilter(httptest.NewRequest("GET", "/local/api/jobs?unknown=x", nil)); err == nil {
		t.Fatal("unknown task filter accepted")
	}
	if _, err := parseTaskListFilter(httptest.NewRequest("GET", "/local/api/jobs?state=bad", nil)); err == nil {
		t.Fatal("invalid task state accepted")
	}
	request = httptest.NewRequest("GET", "/local/api/jobs?state=failed&error_code=dependency_unavailable", nil)
	filter, err = parseTaskListFilter(request)
	if err != nil || filter.ErrorCode != "dependency_unavailable" {
		t.Fatalf("error filter=%#v err=%v", filter, err)
	}
	if _, err := parseTaskListFilter(httptest.NewRequest("GET", "/local/api/jobs?error_code=a%0Ab", nil)); err == nil {
		t.Fatal("invalid error code accepted")
	}
	request = httptest.NewRequest("GET", "/local/api/jobs?created_after=2026-10-01T00:00:00Z&created_before=2026-11-01T00:00:00Z", nil)
	filter, err = parseTaskListFilter(request)
	if err != nil || filter.CreatedAfter.IsZero() || filter.CreatedBefore.IsZero() {
		t.Fatalf("time filter=%#v err=%v", filter, err)
	}
	when := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	filtered = filterTasks([]webTask{{Kind: "parse", Job: &webJob{CreatedAt: when}}, {Kind: "parse", Job: &webJob{CreatedAt: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)}}}, filter)
	if len(filtered) != 1 || !filtered[0].Job.CreatedAt.Equal(when) {
		t.Fatalf("time filtered=%#v", filtered)
	}
}
