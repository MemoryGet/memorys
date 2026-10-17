package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

type PartnerSearchApplication interface {
	SearchApplication
	ProfileIdentity() (uint64, contracts.Digest, error)
}

// PartnerSearchHandler implements the partner POST /search wire mapping. The
// production bootstrap must supply the shared application service.
type PartnerSearchHandler struct{ Service PartnerSearchApplication }

func (h PartnerSearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var nonce [16]byte
	_, randomErr := rand.Read(nonce[:])
	requestID := hex.EncodeToString(nonce[:])
	fail := func(status int, code, message string) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": message, "retryable": status == 503 || status == 504, "request_id": requestID})
	}
	if randomErr != nil {
		fail(500, "internal", "Request unavailable")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(405, "invalid_argument", "POST required")
		return
	}
	if h.Service == nil {
		fail(503, "capability_missing", "Search not configured")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		fail(415, "invalid_argument", "JSON required")
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || len(headers[0]) > 16400 {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	scheme, token, ok := strings.Cut(headers[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || len(token) > 16384 || strings.ContainsAny(token, " \t\r\n,") {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		fail(400, "invalid_argument", "Invalid search request")
		return
	}
	if _, err := canonical.JSON(raw); err != nil {
		fail(400, "invalid_argument", "Invalid search request")
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		fail(400, "invalid_argument", "Invalid search request")
		return
	}
	for key := range fields {
		if key != "query" && key != "space_id" && key != "kb_ids" && key != "top_k" {
			fail(400, "invalid_argument", "Invalid search request")
			return
		}
	}
	var query string
	var space contracts.ID
	if json.Unmarshal(fields["query"], &query) != nil || json.Unmarshal(fields["space_id"], &space) != nil || !utf8.ValidString(query) || utf8.RuneCountInString(query) < 1 || utf8.RuneCountInString(query) > 10000 || !partnerID(space) {
		fail(400, "invalid_argument", "Invalid search request")
		return
	}
	var ids []contracts.ID
	if value, present := fields["kb_ids"]; present {
		if strings.TrimSpace(string(value)) == "null" || json.Unmarshal(value, &ids) != nil || ids == nil || len(ids) > 1000 {
			fail(400, "invalid_argument", "Invalid knowledge bases")
			return
		}
		seen := map[contracts.ID]bool{}
		for _, id := range ids {
			if !partnerID(id) || seen[id] {
				fail(400, "invalid_argument", "Invalid knowledge bases")
				return
			}
			seen[id] = true
		}
	}
	count := 10
	if value, present := fields["top_k"]; present {
		n, ok := new(big.Rat).SetString(string(value))
		if !ok || !n.IsInt() || !n.Num().IsInt64() || n.Num().Int64() < 1 || n.Num().Int64() > 100 {
			fail(400, "invalid_argument", "Invalid top K")
			return
		}
		count = int(n.Num().Int64())
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	started := time.Now()
	results, err := h.Service.Search(ctx, knowledge.SearchInput{Credential: credential, RequestID: contracts.ID(requestID), Deadline: deadline, SpaceID: space, KnowledgeBaseIDs: ids, Text: query, Limit: count})
	if ctx.Err() != nil {
		fail(504, "deadline_exceeded", "Search deadline exceeded")
		return
	}
	if errors.Is(err, knowledge.ErrUnauthenticated) {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	if err != nil {
		fail(503, "dependency_unavailable", "Search unavailable")
		return
	}
	version, digest, err := h.Service.ProfileIdentity()
	if err != nil || version == 0 || len(digest) != 64 || len(results) > count {
		fail(503, "dependency_unavailable", "Search unavailable")
		return
	}
	if decoded, err := hex.DecodeString(string(digest)); err != nil || len(decoded) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(503, "dependency_unavailable", "Search unavailable")
		return
	}
	evidence := make([]map[string]any, 0, len(results))
	for i, c := range results {
		if !partnerID(c.Citation.DocumentID) || !partnerID(c.Citation.RevisionID) || !partnerID(c.Citation.BlockID) || c.Rank != i+1 {
			fail(503, "dependency_unavailable", "Search unavailable")
			return
		}
		evidence = append(evidence, map[string]any{"citation": map[string]any{"document_id": c.Citation.DocumentID, "revision_id": c.Citation.RevisionID, "block_id": c.Citation.BlockID, "source_anchor": sourceDTO(c.Citation.Source)}, "source_excerpt": c.Text, "rank": c.Rank})
	}
	body := map[string]any{"request_id": requestID, "profile_version": version, "profile_digest": digest, "timings": map[string]any{"total_ms": float64(time.Since(started).Microseconds()) / 1000}, "degraded": []any{}, "evidence": evidence}
	encoded, err := json.Marshal(body)
	if err != nil {
		fail(503, "dependency_unavailable", "Search unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(encoded)
}
func partnerID(id contracts.ID) bool {
	return utf8.ValidString(string(id)) && utf8.RuneCountInString(string(id)) >= 1 && utf8.RuneCountInString(string(id)) <= 200
}
