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

// BackgroundWorker is a host-owned durable worker lifecycle. Bootstrap does
// not know whether it uses PostgreSQL, Redis, or another queue.
type BackgroundWorker interface {
	Start(context.Context) error
	Stop() error
	Ready() error
}

var ErrBootstrap = errors.New("invalid server bootstrap")

type Config struct {
	BlobContent      api.BlobContentApplication
	UploadParse      *api.UploadParseHandler
	BatchUploadParse *api.BatchUploadParseHandler
	// Upload is an explicitly authorized multipart upload application. Bootstrap
	// never invents a BlobStore or request preparation callback.
	Upload                *api.UploadHandler
	Search                api.PartnerSearchApplication
	Read                  api.CurrentReadApplication
	Ask                   api.PartnerAskApplication
	Feedback              api.PartnerFeedbackApplication
	MCP                   http.Handler
	Deletion              api.DocumentDeletionApplication
	DeletionProfile       api.ProfileIdentitySource
	Jobs                  api.JobStatusApplication
	Cancel                api.JobCancellationApplication
	Retry                 api.JobRetryApplication
	ParseJobs             api.JobStatusApplication
	ParseCancel           api.JobCancellationApplication
	ParsePause            api.JobPauseApplication
	ParseRetry            api.JobRetryApplication
	PublicationJobs       api.JobStatusApplication
	PublicationCancel     api.JobCancellationApplication
	PublicationPause      api.JobPauseApplication
	PublicationRetry      api.PublicationJobRetryApplication
	Review                api.ReviewDecisionApplication
	ApprovalRequests      api.ApprovalRequestListApplication
	FeedbackList          api.FeedbackListApplication
	FeedbackProposals     api.FeedbackProposalApplication
	Grants                api.GrantApplication
	GrantList             api.GrantListApplication
	JobList               api.JobListApplication
	Drafts                api.DraftReadApplication
	DraftEdits            api.DraftEditApplication
	Revisions             api.RevisionReadApplication
	DraftSubmissions      api.DraftSubmitApplication
	PublicationStarts     api.PublicationStartApplication
	PublicationWithdraws  api.PublicationWithdrawApplication
	History               api.PublicationHistoryListApplication
	Documents             api.DocumentListApplication
	DocumentUploads       api.DocumentUploadApplication
	DocumentRead          api.DocumentReadApplication
	DraftParses           api.DraftParseApplication
	DraftCreates          api.DraftCreateApplication
	PublicationOperations api.PublicationOperationReadApplication
	DraftSources          api.DraftSourceApplication
	KnowledgeBases        api.KnowledgeBaseListApplication
	KnowledgeBaseCreates  api.KnowledgeBaseCreateApplication
	DraftCompare          api.DraftCompareApplication
	WebSearch             api.WebSearchApplication
	PublicationWorker     BackgroundWorker
	// ParseWorker is the host-owned durable parser loop. Upload endpoints only
	// enqueue parse commands; this dependency performs candidate generation.
	ParseWorker BackgroundWorker
	// ReadyCheck is an optional host-owned readiness dependency, such as a
	// database ping. It is evaluated on GET /ready and never on /health.
	ReadyCheck func(context.Context) error
}

type Runtime struct {
	Handler      http.Handler
	Runner       *events.Runner
	Runners      *events.RunnerGroup
	Worker       BackgroundWorker
	ParserWorker BackgroundWorker
	ready        func(context.Context) error
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
	mux.Handle("/metrics", &api.MetricsHandler{StartedAt: time.Now().UTC()})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if c.ReadyCheck != nil && c.ReadyCheck(r.Context()) != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	if c.UploadParse != nil {
		mux.HandleFunc("/api/documents/upload", c.UploadParse.Handle)
	} else if c.Upload == nil {
		mux.HandleFunc("/api/documents/upload", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "upload not configured", http.StatusServiceUnavailable)
		})
	} else {
		mux.HandleFunc("/api/documents/upload", c.Upload.Handle)
	}
	if c.BatchUploadParse != nil {
		mux.HandleFunc("/api/documents/batch-upload", c.BatchUploadParse.Handle)
	} else {
		mux.HandleFunc("/api/documents/batch-upload", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "batch upload not configured", http.StatusServiceUnavailable)
		})
	}
	mux.Handle("/api/knowledge/search", api.ApplicationSearchHandler{Service: c.Search})
	mux.Handle("/api/v1/knowledge-os/search", api.PartnerSearchHandler{Service: c.Search})
	mux.Handle("/api/v1/web-search", api.WebSearchHandler{Service: c.WebSearch})
	mux.Handle("/api/v1/web-fetch", api.WebFetchHandler{Service: c.WebSearch})
	mux.Handle("/api/v1/knowledge-os/read", api.PartnerReadHandler{Service: c.Read, Profile: c.Search})
	mux.Handle("/api/v1/knowledge-os/ask", api.PartnerAskHandler{Service: c.Ask})
	mux.Handle("/api/v1/knowledge-os/feedback", api.PartnerFeedbackHandler{Service: c.Feedback})
	mux.Handle("/api/v1/knowledge-os/blobs/{blob_id}/content", api.BlobContentHandler{Service: c.BlobContent})
	mux.Handle("/api/v1/knowledge-os/documents/{id}", api.DocumentDeleteHandler{Service: c.Deletion, Profile: c.DeletionProfile})
	mux.Handle("GET /api/v1/knowledge-os/documents/{id}", api.DocumentReadHandler{Service: c.DocumentRead})
	mux.Handle("/api/v1/knowledge-os/jobs/{id}", api.JobStatusHandler{Service: c.Jobs})
	mux.Handle("/api/v1/knowledge-os/jobs", api.JobListHandler{Service: c.JobList})
	mux.Handle("/api/v1/knowledge-os/jobs/{id}/events", api.JobEventsHandler{Service: c.Jobs})
	mux.Handle("/api/v1/knowledge-os/jobs/{id}/cancel", api.JobCancellationHandler{Service: c.Cancel})
	mux.Handle("/api/v1/knowledge-os/jobs/{id}/retry", api.JobRetryHandler{Service: c.Retry})
	mux.Handle("/api/v1/knowledge-os/parse-jobs/{id}", api.JobStatusHandler{Service: c.ParseJobs})
	mux.Handle("/api/v1/knowledge-os/parse-jobs/{id}/events", api.JobEventsHandler{Service: c.ParseJobs})
	mux.Handle("/api/v1/knowledge-os/parse-jobs/{id}/cancel", api.JobCancellationHandler{Service: c.ParseCancel})
	mux.Handle("/api/v1/knowledge-os/parse-jobs/{id}/pause", api.JobPauseHandler{Service: c.ParsePause})
	mux.Handle("/api/v1/knowledge-os/parse-jobs/{id}/retry", api.JobRetryHandler{Service: c.ParseRetry})
	mux.Handle("/api/v1/knowledge-os/publication-jobs/{id}", api.JobStatusHandler{Service: c.PublicationJobs})
	mux.Handle("/api/v1/knowledge-os/publication-jobs/{id}/events", api.JobEventsHandler{Service: c.PublicationJobs})
	mux.Handle("/api/v1/knowledge-os/publication-jobs/{id}/cancel", api.JobCancellationHandler{Service: c.PublicationCancel})
	mux.Handle("/api/v1/knowledge-os/publication-jobs/{id}/pause", api.JobPauseHandler{Service: c.PublicationPause})
	mux.Handle("/api/v1/knowledge-os/publication-jobs/{id}/retry", api.PublicationJobRetryHandler{Service: c.PublicationRetry})
	mux.Handle("/api/v1/knowledge-os/revisions/{revision_id}/approval-decisions", api.ReviewDecisionHandler{Service: c.Review})
	mux.Handle("/api/v1/knowledge-os/revisions:batch-approval-decisions", api.BatchReviewHandler{Service: c.Review})
	mux.Handle("/api/v1/knowledge-os/publication-operations:batch-start", api.BatchPublicationHandler{Service: c.PublicationStarts})
	mux.Handle("/api/v1/knowledge-os/knowledge-bases/{kb_id}/approval-requests", api.ApprovalRequestListHandler{Service: c.ApprovalRequests})
	mux.Handle("/api/v1/knowledge-os/knowledge-bases/{kb_id}/feedback", api.FeedbackListHandler{Service: c.FeedbackList})
	mux.Handle("/api/v1/knowledge-os/feedback/{id}/proposals", api.FeedbackProposalHandler{Service: c.FeedbackProposals})
	mux.Handle("/api/v1/knowledge-os/knowledge-bases/{kb_id}/grants/{subject_id}", api.GrantHandler{Service: c.Grants})
	mux.Handle("/api/v1/knowledge-os/knowledge-bases/{kb_id}/grants", api.GrantListHandler{Service: c.GrantList})
	mux.Handle("/api/v1/knowledge-os/drafts/{draft_id}", api.DraftHandler{Read: c.Drafts, Edit: c.DraftEdits})
	mux.Handle("/api/v1/knowledge-os/revisions/{revision_id}", api.RevisionReadHandler{Service: c.Revisions})
	mux.Handle("/api/v1/knowledge-os/drafts/{draft_id}/submit", api.DraftSubmitHandler{Service: c.DraftSubmissions})
	mux.Handle("/api/v1/knowledge-os/documents/{id}/publications", api.PublicationStartHandler{Service: c.PublicationStarts})
	mux.Handle("/api/v1/knowledge-os/documents/{id}/withdraw", api.PublicationWithdrawHandler{Service: c.PublicationWithdraws})
	mux.Handle("/api/v1/knowledge-os/documents/{id}/history", api.PublicationHistoryListHandler{Service: c.History})
	mux.Handle("/api/v1/knowledge-os/knowledge-bases/{kb_id}/documents", api.DocumentListHandler{Service: c.Documents})
	mux.Handle("POST /api/v1/knowledge-os/knowledge-bases/{kb_id}/documents", api.DocumentUploadHandler{Service: c.DocumentUploads})
	mux.Handle("/api/v1/knowledge-os/drafts/{draft_id}/parse", api.DraftParseHandler{Service: c.DraftParses})
	mux.Handle("/api/v1/knowledge-os/documents/{id}/drafts", api.DraftCreateHandler{Service: c.DraftCreates})
	mux.Handle("/api/v1/knowledge-os/publication-operations/{id}", api.PublicationOperationReadHandler{Service: c.PublicationOperations})
	mux.Handle("/api/v1/knowledge-os/drafts/{draft_id}/source", api.DraftSourceHandler{Service: c.DraftSources})
	mux.Handle("/api/v1/knowledge-os/spaces/{space_id}/knowledge-bases", api.KnowledgeBaseListHandler{Service: c.KnowledgeBases})
	mux.Handle("POST /api/v1/knowledge-os/spaces/{space_id}/knowledge-bases", api.KnowledgeBaseCreateHandler{Service: c.KnowledgeBaseCreates})
	mux.Handle("/api/v1/knowledge-os/drafts/{draft_id}/compare", api.DraftCompareHandler{Service: c.DraftCompare})
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
	return &Runtime{Handler: h, Runner: runner, Worker: c.PublicationWorker, ParserWorker: c.ParseWorker, ready: c.ReadyCheck}, nil
}

// NewRuntimeWithRunnerGroup binds several independently durable event
// consumers to the same HTTP lifecycle. A group is optional; NewRuntime
// remains the compatibility path for a single consumer.
func NewRuntimeWithRunnerGroup(c Config, runners *events.RunnerGroup) (*Runtime, error) {
	if runners == nil {
		return nil, ErrBootstrap
	}
	h, err := New(c)
	if err != nil {
		return nil, err
	}
	return &Runtime{Handler: h, Runners: runners, Worker: c.PublicationWorker, ParserWorker: c.ParseWorker, ready: c.ReadyCheck}, nil
}

func (r *Runtime) Start(ctx context.Context) error {
	if r == nil || r.Handler == nil {
		return ErrBootstrap
	}
	if r.Runners != nil {
		if err := r.Runners.Start(ctx); err != nil {
			return err
		}
	} else if r.Runner != nil {
		if err := r.Runner.Start(ctx); err != nil {
			return err
		}
	}
	if r.ParserWorker != nil {
		if err := r.ParserWorker.Start(ctx); err != nil {
			if r.Runners != nil {
				_ = r.Runners.Stop()
			} else if r.Runner != nil {
				_ = r.Runner.Stop()
			}
			return err
		}
	}
	if r.Worker != nil {
		if err := r.Worker.Start(ctx); err != nil {
			if r.ParserWorker != nil {
				_ = r.ParserWorker.Stop()
			}
			if r.Runners != nil {
				_ = r.Runners.Stop()
			} else if r.Runner != nil {
				_ = r.Runner.Stop()
			}
			return err
		}
	}
	return nil
}

func (r *Runtime) Stop() error {
	if r == nil || r.Handler == nil {
		return ErrBootstrap
	}
	var out error
	if r.Worker != nil {
		out = errors.Join(out, r.Worker.Stop())
	}
	if r.ParserWorker != nil {
		out = errors.Join(out, r.ParserWorker.Stop())
	}
	if r.Runners != nil {
		out = errors.Join(out, r.Runners.Stop())
	} else if r.Runner != nil {
		out = errors.Join(out, r.Runner.Stop())
	}
	return out
}

func (r *Runtime) Ready() error {
	if r == nil || r.Handler == nil {
		return ErrBootstrap
	}
	if r.ready != nil && r.ready(context.Background()) != nil {
		return ErrBootstrap
	}
	if r.Runners != nil {
		if r.Runners.Ready() != nil {
			return ErrBootstrap
		}
	}
	if r.Runner != nil {
		snapshot := r.Runner.Snapshot()
		if !snapshot.Started || snapshot.Stopped || snapshot.LastErr != nil {
			return ErrBootstrap
		}
	}
	if r.Worker != nil && r.Worker.Ready() != nil {
		return ErrBootstrap
	}
	if r.ParserWorker != nil && r.ParserWorker.Ready() != nil {
		return ErrBootstrap
	}
	return nil
}
