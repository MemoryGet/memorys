package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type FeedbackProposalApplication = contracts.FeedbackProposalApplication

type FeedbackProposalHandler struct{ Service FeedbackProposalApplication }

func (h FeedbackProposalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := requestNonce(w)
	fail := func(status int, code, message string) {
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": message, "retryable": status == 503 || status == 504, "request_id": requestID})
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		fail(http.StatusMethodNotAllowed, "invalid_argument", "POST required")
		return
	}
	if h.Service == nil {
		fail(http.StatusServiceUnavailable, "capability_missing", "Feedback proposal not configured")
		return
	}
	feedbackID := contracts.ID(r.PathValue("id"))
	if !partnerID(feedbackID) {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid feedback ID")
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || !partnerID(contracts.ID(keys[0])) || len(keys[0]) > 200 {
		fail(http.StatusBadRequest, "invalid_argument", "Idempotency-Key required")
		return
	}
	media := r.Header.Get("Content-Type")
	if media != "application/json" {
		fail(http.StatusUnsupportedMediaType, "invalid_argument", "JSON required")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid proposal request")
		return
	}
	if _, err = canonical.JSON(raw); err != nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid proposal request")
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid proposal request")
		return
	}
	for key := range fields {
		if key != "comment" {
			fail(http.StatusBadRequest, "invalid_argument", "Invalid proposal request")
			return
		}
	}
	var body struct {
		Comment string `json:"comment"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || !validProposalComment(body.Comment) {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid proposal comment")
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
	version, digest, err := h.Service.ProfileIdentity()
	decoded, digestErr := hex.DecodeString(string(digest))
	if err != nil || version == 0 || digestErr != nil || len(decoded) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Feedback configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	result, err := h.Service.ProposeFeedback(ctx, contracts.FeedbackProposalInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, FeedbackID: feedbackID, Comment: body.Comment, IdempotencyKey: keys[0]})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Feedback proposal deadline exceeded")
		return
	}
	if errors.Is(err, contracts.ErrFeedbackProposalConflict) {
		fail(http.StatusConflict, "conflict", "Feedback proposal idempotency conflict")
		return
	}
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Feedback proposal unavailable")
		return
	}
	if result.ID == "" || result.FeedbackID != feedbackID || result.DraftID == "" || result.State != "proposed" || result.CreatedAt.IsZero() {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Feedback proposal unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"proposal_id": result.ID, "feedback_id": result.FeedbackID, "draft_id": result.DraftID, "state": result.State, "created_at": result.CreatedAt.UTC().Format(time.RFC3339Nano), "request_id": requestID})
}

func validProposalComment(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 10000 && !strings.ContainsRune(value, 0)
}

func feedbackProposalDigest(feedbackID contracts.ID, comment, key string) string {
	sum := sha256.Sum256([]byte(string(feedbackID) + "\x00" + comment + "\x00" + key))
	return hex.EncodeToString(sum[:])
}
