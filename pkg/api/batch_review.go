package api

import (
	"context"
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

type BatchReviewHandler struct{ Service ReviewDecisionApplication }

type batchReviewItem struct {
	RevisionID              contracts.ID `json:"revision_id"`
	Decision                string       `json:"decision"`
	Reason                  string       `json:"reason"`
	ExpectedApprovalVersion uint64       `json:"expected_approval_version"`
	IdempotencyKey          string       `json:"idempotency_key"`
}

func (h BatchReviewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		fail(http.StatusServiceUnavailable, "capability_missing", "Batch review not configured")
		return
	}
	if media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || media != "application/json" {
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
	raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil || len(raw) == 0 || len(raw) > 2<<20 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid batch review request")
		return
	}
	var body struct {
		Items []batchReviewItem `json:"items"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || len(body.Items) < 1 || len(body.Items) > 100 {
		fail(http.StatusBadRequest, "invalid_argument", "Batch must contain 1 to 100 items")
		return
	}
	version, digest, err := h.Service.ProfileIdentity()
	decoded, digestErr := hex.DecodeString(string(digest))
	if err != nil || version == 0 || digestErr != nil || len(decoded) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Review configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	out := make([]map[string]any, 0, len(body.Items))
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	for index, item := range body.Items {
		if !partnerID(item.RevisionID) || (item.Decision != "approve" && item.Decision != "reject" && item.Decision != "revoke") || !utf8.ValidString(item.Reason) || utf8.RuneCountInString(item.Reason) > 10000 || item.IdempotencyKey == "" || len(item.IdempotencyKey) > 200 {
			out = append(out, map[string]any{"revision_id": item.RevisionID, "status": "invalid_argument"})
			continue
		}
		approval, err := h.Service.DecideReview(ctx, contracts.ReviewDecisionInput{Credential: credential, Request: contracts.RequestContext{RequestID: contracts.ID(string(requestID) + "-" + strconv.Itoa(index)), ProfileDigest: digest, Deadline: deadline}, RevisionID: item.RevisionID, Decision: item.Decision, Reason: item.Reason, ExpectedApprovalVersion: item.ExpectedApprovalVersion, IdempotencyKey: item.IdempotencyKey})
		if err != nil {
			out = append(out, map[string]any{"revision_id": item.RevisionID, "status": "failed"})
			continue
		}
		out = append(out, map[string]any{"revision_id": approval.RevisionID, "status": "committed", "approval_id": approval.ID, "decision": approval.Decision, "row_version": approval.RowVersion})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": out})
}
