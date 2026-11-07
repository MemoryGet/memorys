package api

import (
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"time"
)

// MetricsHandler exposes a small dependency-free Prometheus surface. It only
// contains process/runtime data and never emits credentials, request payloads,
// database URLs, or user content.
type MetricsHandler struct {
	StartedAt time.Time
}

func (h *MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	started := h.StartedAt
	if started.IsZero() {
		started = time.Now().UTC()
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	uptime := time.Since(started).Seconds()
	if uptime < 0 {
		uptime = 0
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, "# HELP memorys_uptime_seconds Process uptime in seconds.\n# TYPE memorys_uptime_seconds gauge\nmemorys_uptime_seconds %s\n", strconv.FormatFloat(uptime, 'f', 3, 64))
	_, _ = fmt.Fprintf(w, "# HELP memorys_goroutines Current number of goroutines.\n# TYPE memorys_goroutines gauge\nmemorys_goroutines %d\n", runtime.NumGoroutine())
	_, _ = fmt.Fprintf(w, "# HELP memorys_heap_alloc_bytes Bytes allocated on the heap.\n# TYPE memorys_heap_alloc_bytes gauge\nmemorys_heap_alloc_bytes %d\n", memory.HeapAlloc)
	_, _ = fmt.Fprintf(w, "# HELP memorys_heap_objects Number of allocated heap objects.\n# TYPE memorys_heap_objects gauge\nmemorys_heap_objects %d\n", memory.HeapObjects)
	_, _ = fmt.Fprintf(w, "# HELP memorys_gc_cycles_total Completed garbage collection cycles.\n# TYPE memorys_gc_cycles_total counter\nmemorys_gc_cycles_total %d\n", memory.NumGC)
}
