package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type DraftParseApplication = contracts.DraftParseApplication

type DraftParseHandler struct{ Service DraftParseApplication }

func (h DraftParseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		fail(http.StatusServiceUnavailable, "capability_missing", "Draft parsing not configured")
		return
	}
	draftID := contracts.ID(r.PathValue("draft_id"))
	if !partnerID(draftID) || r.URL.RawQuery != "" {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid draft ID")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		fail(http.StatusUnsupportedMediaType, "invalid_argument", "JSON required")
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
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<10))
	if err != nil || len(raw) == 0 || len(raw) > 32<<10 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid parse request")
		return
	}
	if _, err := canonical.JSON(raw); err != nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid parse request")
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid parse request")
		return
	}
	for key := range fields {
		if key != "expected_version" && key != "pipeline_profile_id" && key != "pipeline_profile_version" {
			fail(http.StatusBadRequest, "invalid_argument", "Invalid parse request")
			return
		}
	}
	var body struct {
		ExpectedVersion        uint64       `json:"expected_version"`
		PipelineProfileID      contracts.ID `json:"pipeline_profile_id"`
		PipelineProfileVersion uint64       `json:"pipeline_profile_version"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || body.ExpectedVersion == 0 || body.ExpectedVersion == math.MaxUint64 || !partnerID(body.PipelineProfileID) || body.PipelineProfileVersion == 0 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid parse request")
		return
	}
	version, digest, err := h.Service.ProfileIdentity()
	decoded, digestErr := hex.DecodeString(string(digest))
	if err != nil || version == 0 || digestErr != nil || len(decoded) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Parse configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	result, err := h.Service.ParseDraft(ctx, contracts.DraftParseInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, DraftID: draftID, ExpectedVersion: body.ExpectedVersion, PipelineProfileID: body.PipelineProfileID, PipelineProfileVersion: body.PipelineProfileVersion, IdempotencyKey: keys[0]})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Parse deadline exceeded")
		return
	}
	if err != nil {
		fail(http.StatusConflict, "conflict", "Draft parse preconditions changed")
		return
	}
	job := result.Job
	if job.ID == "" || result.DocumentID == "" || job.State != "queued" || job.Stage != "document.parse" || job.RowVersion == 0 {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Parse job unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": job.ID, "space_id": job.SpaceID, "resource_id": result.DocumentID, "state": job.State, "stage": job.Stage, "attempt": job.Attempt, "row_version": job.RowVersion, "progress": job.Progress})
}
