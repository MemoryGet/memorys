package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type BlobContentApplication interface {
	OpenBlob(context.Context, contracts.Credential, contracts.RequestContext, contracts.ID) (contracts.BlobRef, io.ReadCloser, error)
}

// BlobContentHandler streams one authorized logical BlobRef. The application
// owns scope/ownership checks; this transport never accepts a physical path or
// public URL from the caller.
type BlobContentHandler struct{ Service BlobContentApplication }

func (h BlobContentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		http.Error(w, "request unavailable", http.StatusInternalServerError)
		return
	}
	requestID := hex.EncodeToString(nonce[:])
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	if h.Service == nil || r.URL.RawQuery != "" {
		http.Error(w, "Blob content unavailable", http.StatusServiceUnavailable)
		return
	}
	blobID := contracts.ID(r.PathValue("blob_id"))
	if !partnerID(blobID) {
		http.Error(w, "Invalid Blob request", http.StatusBadRequest)
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || len(headers[0]) > 16400 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	token, ok := strings.CutPrefix(headers[0], "Bearer ")
	if !ok || token == "" || strings.ContainsAny(token, " \t\r\n,") {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	ref, stream, err := h.Service.OpenBlob(ctx, credential, contracts.RequestContext{RequestID: contracts.ID(requestID), Deadline: deadline}, blobID)
	if err != nil || stream == nil || ref.ID != blobID || ref.Size < 0 || ref.Size > 64<<20 || ref.MediaType == "" {
		if stream != nil {
			_ = stream.Close()
		}
		http.Error(w, "Blob content unavailable", http.StatusNotFound)
		return
	}
	defer stream.Close()
	if _, _, err := mime.ParseMediaType(ref.MediaType); err != nil {
		http.Error(w, "Blob content unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", ref.MediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(ref.Size, 10))
	w.Header().Set("Content-Disposition", `attachment; filename="`+hex.EncodeToString([]byte(string(ref.ID)))+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	if _, err := io.CopyN(w, stream, ref.Size); err != nil && !errors.Is(err, io.EOF) {
		return
	}
}
