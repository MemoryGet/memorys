package api

import (
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"

	"github.com/MemoryGet/memorys/pkg/ingestion"
)

const defaultBatchUploadFiles = 100

// BatchUploadParseHandler accepts multiple originals in one multipart request.
// Preparation remains host-owned for every file so the transport never trusts
// document, draft, candidate, or authorization fields from the form.
type BatchUploadParseHandler struct {
	Service      ingestion.UploadParseService
	Prepare      func(*http.Request, int, *multipart.FileHeader) (UploadParsePreparation, error)
	MaxFiles     int
	MaxBodyBytes int64
}

func (h BatchUploadParseHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Prepare == nil || h.Service.Upload.Store == nil || h.Service.Creator == nil {
		http.Error(w, "batch upload parse not configured", http.StatusServiceUnavailable)
		return
	}
	maxFiles := h.MaxFiles
	if maxFiles <= 0 {
		maxFiles = defaultBatchUploadFiles
	}
	if maxFiles > defaultBatchUploadFiles {
		http.Error(w, "batch upload file limit invalid", http.StatusServiceUnavailable)
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
	if err := r.ParseMultipartForm(1 << 20); err != nil || body.exceeded {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		var tooLarge *http.MaxBytesError
		if body.exceeded || errors.As(err, &tooLarge) {
			http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid upload", http.StatusBadRequest)
		}
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	if len(files) > maxFiles {
		http.Error(w, "too many files", http.StatusRequestEntityTooLarge)
		return
	}

	type item struct {
		Index      int            `json:"index"`
		Filename   string         `json:"filename"`
		JobID      string         `json:"job_id,omitempty"`
		State      string         `json:"state,omitempty"`
		DocumentID string         `json:"document_id,omitempty"`
		DraftID    string         `json:"draft_id,omitempty"`
		Blob       map[string]any `json:"blob,omitempty"`
		Error      string         `json:"error,omitempty"`
	}
	items := make([]item, 0, len(files))
	for index, header := range files {
		result := item{Index: index, Filename: header.Filename}
		prepared, err := h.Prepare(r, index, header)
		if err != nil {
			result.Error = "upload denied"
			items = append(items, result)
			continue
		}
		if prepared.Write.Request.Deadline.IsZero() {
			result.Error = "upload unavailable"
			items = append(items, result)
			continue
		}
		if header.Size != prepared.Write.Size {
			result.Error = "file size mismatch"
			items = append(items, result)
			continue
		}
		file, err := header.Open()
		if err != nil {
			result.Error = "file unavailable"
			items = append(items, result)
			continue
		}
		ctx, cancel := context.WithDeadline(r.Context(), prepared.Write.Request.Deadline)
		blob, uploadErr := h.Service.UploadAndEnqueue(ctx, prepared.Write, file, prepared.Principal, prepared.DocumentID, prepared.DraftID, prepared.CandidateID, prepared.ExpectedVersion, prepared.Format, prepared.Options, prepared.Job)
		cancel()
		_ = file.Close()
		if uploadErr != nil {
			result.Error = "upload parse failed"
			items = append(items, result)
			continue
		}
		result.JobID = string(prepared.Job.ID)
		result.State = prepared.Job.State
		result.DocumentID = string(prepared.DocumentID)
		result.DraftID = string(prepared.DraftID)
		result.Blob = map[string]any{"id": blob.ID, "sha256": blob.SHA256, "size": blob.Size, "media_type": blob.MediaType}
		items = append(items, result)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "count": len(items)})
}
