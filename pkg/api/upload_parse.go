package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/ingestion"
)

// UploadParsePreparation is returned by a trusted application callback. The
// callback owns authentication, authorization and document/task identity; no
// identity or task binding is accepted from multipart form fields.
type UploadParsePreparation struct {
	Write           contracts.BlobWriteRequest
	Principal       contracts.Principal
	DocumentID      contracts.ID
	DraftID         contracts.ID
	CandidateID     contracts.ID
	ExpectedVersion uint64
	Format          string
	Options         contracts.ParseOptions
	Job             contracts.Job
}

// UploadParseHandler accepts one original and enqueues its parser command.
// It deliberately returns queued rather than waiting for parsing to finish;
// callers use the returned job ID with the task-status application.
type UploadParseHandler struct {
	Service      ingestion.UploadParseService
	Prepare      func(*http.Request) (UploadParsePreparation, error)
	MaxBodyBytes int64
}

func (h UploadParseHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Prepare == nil || h.Service.Upload.Store == nil || h.Service.Creator == nil {
		http.Error(w, "upload parse not configured", http.StatusServiceUnavailable)
		return
	}
	limit := h.MaxBodyBytes
	if limit <= 0 {
		limit = MaxUploadBody
	}
	if limit > MaxUploadBody {
		http.Error(w, "upload limit invalid", http.StatusServiceUnavailable)
		return
	}
	if r.ContentLength > limit {
		http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
		return
	}
	body := &uploadBody{ReadCloser: http.MaxBytesReader(w, r.Body, limit)}
	r.Body = body
	prepared, err := h.Prepare(r)
	if err != nil {
		http.Error(w, "upload denied", http.StatusForbidden)
		return
	}
	if prepared.Write.Request.Deadline.IsZero() {
		http.Error(w, "upload unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), prepared.Write.Request.Deadline)
	defer cancel()
	if err := r.ParseMultipartForm(1 << 20); err != nil || body.exceeded {
		var tooLarge *http.MaxBytesError
		if body.exceeded || errors.As(err, &tooLarge) {
			http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid upload", http.StatusBadRequest)
		}
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if header.Size != prepared.Write.Size {
		http.Error(w, "file size mismatch", http.StatusBadRequest)
		return
	}
	blob, err := h.Service.UploadAndEnqueue(ctx, prepared.Write, file, prepared.Principal, prepared.DocumentID, prepared.DraftID, prepared.CandidateID, prepared.ExpectedVersion, prepared.Format, prepared.Options, prepared.Job)
	if err != nil {
		http.Error(w, "upload parse failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"job_id":      prepared.Job.ID,
		"state":       prepared.Job.State,
		"document_id": prepared.DocumentID,
		"draft_id":    prepared.DraftID,
		"blob":        map[string]any{"id": blob.ID, "sha256": blob.SHA256, "size": blob.Size, "media_type": blob.MediaType},
	})
}
