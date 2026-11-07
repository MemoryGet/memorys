package localcli

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsPrometheusFormat(t *testing.T) {
	h := &webHost{}
	h.metrics.observe("/local/api/search", nil, 12*time.Millisecond)
	h.metrics.observe("/local/api/search/stream", context.Canceled, 20*time.Millisecond)
	w := httptest.NewRecorder()
	if err := h.metricsPrometheus(context.Background(), w, httptest.NewRequest("GET", "/local/api/metrics/prometheus", nil), nil); err != nil {
		t.Fatal(err)
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain; version=0.0.4; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	body := w.Body.String()
	for _, metric := range []string{
		"memorys_http_requests_total 2",
		"memorys_http_errors_total 1",
		"memorys_http_stream_requests_total 1",
		"memorys_http_latency_seconds_sum 0.032000000",
		"memorys_http_latency_seconds_max 0.020000000",
	} {
		if !strings.Contains(body, metric) {
			t.Fatalf("missing metric %q in %s", metric, body)
		}
	}
}
