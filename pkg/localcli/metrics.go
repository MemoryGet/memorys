package localcli

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

type webMetrics struct {
	Requests       atomic.Uint64
	Errors         atomic.Uint64
	StreamRequests atomic.Uint64
	LatencyNS      atomic.Uint64
	MaxLatencyNS   atomic.Uint64
}

func (m *webMetrics) observe(path string, err error, elapsed time.Duration) {
	m.Requests.Add(1)
	if err != nil {
		m.Errors.Add(1)
	}
	if path == "/local/api/search/stream" {
		m.StreamRequests.Add(1)
	}
	ns := uint64(elapsed)
	m.LatencyNS.Add(ns)
	for {
		old := m.MaxLatencyNS.Load()
		if ns <= old || m.MaxLatencyNS.CompareAndSwap(old, ns) {
			break
		}
	}
}

func (h *webHost) metricsList(_ context.Context, w http.ResponseWriter, _ *http.Request, _ *authority) error {
	requests := h.metrics.Requests.Load()
	average := float64(0)
	if requests > 0 {
		average = float64(h.metrics.LatencyNS.Load()) / float64(requests) / float64(time.Millisecond)
	}
	return webJSON(w, map[string]any{
		"requests":        requests,
		"errors":          h.metrics.Errors.Load(),
		"stream_requests": h.metrics.StreamRequests.Load(),
		"average_ms":      average,
		"max_ms":          float64(h.metrics.MaxLatencyNS.Load()) / float64(time.Millisecond),
	})
}

// metricsPrometheus exposes the same bounded process-local counters in the
// Prometheus text format. It intentionally contains no document IDs, query
// text, labels derived from user input, or credential material.
func (h *webHost) metricsPrometheus(_ context.Context, w http.ResponseWriter, _ *http.Request, _ *authority) error {
	requests := h.metrics.Requests.Load()
	errorsCount := h.metrics.Errors.Load()
	streams := h.metrics.StreamRequests.Load()
	latencySeconds := float64(h.metrics.LatencyNS.Load()) / float64(time.Second)
	maxLatencySeconds := float64(h.metrics.MaxLatencyNS.Load()) / float64(time.Second)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, err := fmt.Fprintf(w,
		"# HELP memorys_http_requests_total Total authenticated local web requests.\n"+
			"# TYPE memorys_http_requests_total counter\n"+
			"memorys_http_requests_total %s\n"+
			"# HELP memorys_http_errors_total Total authenticated local web requests that returned an error.\n"+
			"# TYPE memorys_http_errors_total counter\n"+
			"memorys_http_errors_total %s\n"+
			"# HELP memorys_http_stream_requests_total Total authenticated streaming answer requests.\n"+
			"# TYPE memorys_http_stream_requests_total counter\n"+
			"memorys_http_stream_requests_total %s\n"+
			"# HELP memorys_http_latency_seconds_sum Sum of authenticated local web request latency in seconds.\n"+
			"# TYPE memorys_http_latency_seconds_sum counter\n"+
			"memorys_http_latency_seconds_sum %s\n"+
			"# HELP memorys_http_latency_seconds_max Maximum authenticated local web request latency in seconds.\n"+
			"# TYPE memorys_http_latency_seconds_max gauge\n"+
			"memorys_http_latency_seconds_max %s\n",
		strconv.FormatUint(requests, 10), strconv.FormatUint(errorsCount, 10), strconv.FormatUint(streams, 10),
		strconv.FormatFloat(latencySeconds, 'f', 9, 64), strconv.FormatFloat(maxLatencySeconds, 'f', 9, 64))
	return err
}
