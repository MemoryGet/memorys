// Package bootstrap owns the HTTP composition root. It wires only supplied
// capabilities and keeps absent production dependencies fail-closed.
package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/events"
)

var ErrBootstrap = errors.New("invalid server bootstrap")

type Config struct {
	Search          api.PartnerSearchApplication
	Read            api.CurrentReadApplication
	MCP             http.Handler
	Deletion        api.DocumentDeletionApplication
	DeletionProfile api.ProfileIdentitySource
	Jobs            api.JobStatusApplication
	Cancel          api.JobCancellationApplication
	Retry           api.JobRetryApplication
}

type Runtime struct {
	Handler http.Handler
	Runner  *events.Runner
}

func (r *Runtime) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req != nil && req.URL.Path == "/ready" {
		if req.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "GET required", http.StatusMethodNotAllowed)
			return
		}
		if err := r.Ready(); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
		return
	}
	if r == nil || r.Handler == nil {
		http.Error(w, "bootstrap unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Handler.ServeHTTP(w, req)
}

func (c Config) Validate() error {
	if c.Read != nil && c.Search == nil {
		return ErrBootstrap
	}
	if c.Deletion != nil && c.DeletionProfile == nil {
		return ErrBootstrap
	}
	return nil
}

func New(c Config) (http.Handler, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
	})
	mux.HandleFunc("/api/documents/upload", api.UploadHandler{}.Handle)
	mux.Handle("/api/knowledge/search", api.ApplicationSearchHandler{Service: c.Search})
	mux.Handle("/api/v1/knowledge-os/search", api.PartnerSearchHandler{Service: c.Search})
	mux.Handle("/api/v1/knowledge-os/read", api.PartnerReadHandler{Service: c.Read, Profile: c.Search})
	mux.Handle("/api/v1/knowledge-os/documents/{id}", api.DocumentDeleteHandler{Service: c.Deletion, Profile: c.DeletionProfile})
	mux.Handle("/api/v1/knowledge-os/jobs/{id}", api.JobStatusHandler{Service: c.Jobs})
	mux.Handle("/api/v1/knowledge-os/jobs/{id}/cancel", api.JobCancellationHandler{Service: c.Cancel})
	mux.Handle("/api/v1/knowledge-os/jobs/{id}/retry", api.JobRetryHandler{Service: c.Retry})
	if c.MCP == nil {
		mux.HandleFunc("/mcp", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "MCP not configured", http.StatusServiceUnavailable)
		})
	} else {
		mux.Handle("/mcp", c.MCP)
	}
	return mux, nil
}

func NewRuntime(c Config, runner *events.Runner) (*Runtime, error) {
	h, err := New(c)
	if err != nil {
		return nil, err
	}
	return &Runtime{Handler: h, Runner: runner}, nil
}

func (r *Runtime) Start(ctx context.Context) error {
	if r == nil || r.Handler == nil {
		return ErrBootstrap
	}
	if r.Runner == nil {
		return nil
	}
	return r.Runner.Start(ctx)
}

func (r *Runtime) Stop() error {
	if r == nil || r.Handler == nil {
		return ErrBootstrap
	}
	if r.Runner == nil {
		return nil
	}
	return r.Runner.Stop()
}

func (r *Runtime) Ready() error {
	if r == nil || r.Handler == nil {
		return ErrBootstrap
	}
	if r.Runner == nil {
		return nil
	}
	snapshot := r.Runner.Snapshot()
	if !snapshot.Started || snapshot.Stopped || snapshot.LastErr != nil {
		return ErrBootstrap
	}
	return nil
}
