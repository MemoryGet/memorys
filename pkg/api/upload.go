package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/ingestion"
	"io"
	"net/http"
)

const MaxUploadBody int64 = 65 << 20

type UploadHandler struct {
	Service      ingestion.UploadService
	Prepare      func(*http.Request) (contracts.BlobWriteRequest, error)
	MaxBodyBytes int64
}

func (h UploadHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if h.Prepare == nil || h.Service.Store == nil {
		http.Error(w, "upload not configured", 503)
		return
	}
	limit := h.MaxBodyBytes
	if limit <= 0 {
		limit = MaxUploadBody
	}
	if limit > MaxUploadBody {
		http.Error(w, "upload limit invalid", 503)
		return
	}
	if r.ContentLength > limit {
		http.Error(w, "upload too large", 413)
		return
	}
	body := &uploadBody{ReadCloser: http.MaxBytesReader(w, r.Body, limit)}
	r.Body = body
	req, err := h.Prepare(r)
	if err != nil {
		http.Error(w, "upload denied", 403)
		return
	}
	if req.Request.Deadline.IsZero() {
		http.Error(w, "upload unavailable", 503)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), req.Request.Deadline)
	defer cancel()
	err = r.ParseMultipartForm(1 << 20)
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if err != nil || body.exceeded {
		var tooLarge *http.MaxBytesError
		if body.exceeded || errors.As(err, &tooLarge) {
			http.Error(w, "upload too large", 413)
		} else {
			http.Error(w, "invalid upload", 400)
		}
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", 400)
		return
	}
	defer file.Close()
	if header.Size != req.Size {
		http.Error(w, "file size mismatch", 400)
		return
	}
	blob, err := h.Service.Upload(ctx, req, file)
	if err != nil {
		http.Error(w, "upload failed", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"blob": map[string]any{"id": blob.ID, "sha256": blob.SHA256, "size": blob.Size, "media_type": blob.MediaType}})
}

// Multipart parsing can replace the underlying read error; preserve the
// MaxBytesReader signal for chunked requests without a Content-Length.
type uploadBody struct {
	io.ReadCloser
	exceeded bool
}

func (b *uploadBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	var large *http.MaxBytesError
	if errors.As(err, &large) {
		b.exceeded = true
	}
	return n, err
}
