package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type DraftSourceApplication = contracts.DraftSourceApplication
type DraftSourceHandler struct {
	Service  DraftSourceApplication
	MaxBytes int64
}

func (h DraftSourceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := requestNonce(w)
	fail := func(status int, code, message string) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": message, "retryable": status == 503 || status == 504, "request_id": requestID})
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		fail(http.StatusMethodNotAllowed, "invalid_argument", "POST required")
		return
	}
	if h.Service == nil {
		fail(http.StatusServiceUnavailable, "capability_missing", "Draft source replacement not configured")
		return
	}
	draftID := contracts.ID(r.PathValue("draft_id"))
	if !partnerID(draftID) || r.URL.RawQuery != "" {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid draft ID")
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || len(headers[0]) > 16400 {
		fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
		return
	}
	scheme, token, ok := strings.Cut(headers[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || len(token) > 16384 || strings.ContainsAny(token, " \t\r\n,") {
		fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || len(keys[0]) == 0 || len(keys[0]) > 200 || strings.ContainsAny(keys[0], "\r\n") || !utf8.ValidString(keys[0]) {
		fail(http.StatusBadRequest, "invalid_argument", "Idempotency-Key required")
		return
	}
	limit := h.MaxBytes
	if limit <= 0 {
		limit = 64 << 20
	}
	if limit > 64<<20 {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Upload limit unavailable")
		return
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid multipart request")
		return
	}
	version, err := strconv.ParseUint(r.FormValue("expected_version"), 10, 64)
	if err != nil || version == 0 || version == ^uint64(0) {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid expected version")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(http.StatusBadRequest, "invalid_argument", "File required")
		return
	}
	defer file.Close()
	if header.Size < 0 || header.Size > limit {
		fail(http.StatusRequestEntityTooLarge, "invalid_argument", "File too large")
		return
	}
	mediaType := r.Header.Get("Content-Type")
	if kind, _, e := mime.ParseMediaType(header.Header.Get("Content-Type")); e == nil && kind != "" {
		mediaType = kind
	}
	if mediaType == "" || strings.HasPrefix(mediaType, "multipart/") {
		mediaType = "application/octet-stream"
	}
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) != header.Size || int64(len(body)) > limit {
		fail(http.StatusRequestEntityTooLarge, "invalid_argument", "File too large")
		return
	}
	sum := sha256.Sum256(body)
	digest := contracts.Digest(hex.EncodeToString(sum[:]))
	if _, _, err := mime.ParseMediaType(mediaType); err != nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid media type")
		return
	}
	profileVersion, profileDigest, err := h.Service.ProfileIdentity()
	decoded, digestErr := hex.DecodeString(string(profileDigest))
	if err != nil || profileVersion == 0 || digestErr != nil || len(decoded) != 32 || strings.ToLower(string(profileDigest)) != string(profileDigest) {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Source configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	result, err := h.Service.ReplaceDraftSource(ctx, contracts.DraftSourceInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: profileDigest, Deadline: deadline}, DraftID: draftID, ExpectedVersion: version, ExpectedSHA256: digest, Size: int64(len(body)), MediaType: mediaType, IdempotencyKey: keys[0]}, bytes.NewReader(body))
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Source replacement deadline exceeded")
		return
	}
	if err != nil {
		fail(http.StatusConflict, "conflict", "Draft source replacement preconditions changed")
		return
	}
	if result.Job.ID == "" || result.DocumentID == "" {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Parse job unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": result.Job.ID, "space_id": result.Job.SpaceID, "resource_id": result.DocumentID, "state": result.Job.State, "stage": result.Job.Stage, "attempt": result.Job.Attempt, "row_version": result.Job.RowVersion, "progress": result.Job.Progress})
}
