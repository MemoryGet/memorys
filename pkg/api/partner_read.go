package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

type CurrentReadApplication interface {
	ReadCurrent(context.Context, knowledge.CurrentReadInput) (contracts.FrozenContentRecord, error)
}

type PagedReadApplication interface {
	ReadPage(context.Context, knowledge.CurrentReadInput, knowledge.ReadPageOptions) (knowledge.ReadPageResult, error)
}

type ProfileIdentitySource interface {
	ProfileIdentity() (uint64, contracts.Digest, error)
}

// PartnerReadHandler exposes the current, source-verified read application.
// Profile is a trusted immutable configuration identity, never a caller field.
// Cursor support is supplied by the application's optional paging interface.
// Historical versions require the service's separately authorized history ports.
type PartnerReadHandler struct {
	Service CurrentReadApplication
	Profile ProfileIdentitySource
}

func (h PartnerReadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var nonce [16]byte
	_, randomErr := rand.Read(nonce[:])
	requestID := hex.EncodeToString(nonce[:])
	fail := func(status int, code, message string) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if status == 401 {
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
	if h.Service == nil || h.Profile == nil {
		fail(503, "capability_missing", "Read not configured")
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
		fail(400, "invalid_argument", "Invalid read request")
		return
	}
	if _, err := canonical.JSON(raw); err != nil {
		fail(400, "invalid_argument", "Invalid read request")
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		fail(400, "invalid_argument", "Invalid read request")
		return
	}
	for key := range fields {
		if key != "document_id" && key != "revision_id" && key != "block_ids" && key != "cursor" {
			fail(400, "invalid_argument", "Invalid read request")
			return
		}
	}
	var documentID, revisionID contracts.ID
	if json.Unmarshal(fields["document_id"], &documentID) != nil || !partnerID(documentID) {
		fail(400, "invalid_argument", "Invalid document")
		return
	}
	if value, present := fields["revision_id"]; present {
		if json.Unmarshal(value, &revisionID) != nil || !partnerID(revisionID) {
			fail(400, "invalid_argument", "Invalid revision")
			return
		}
	}
	var ids []contracts.ID
	if value, present := fields["block_ids"]; present {
		if json.Unmarshal(value, &ids) != nil || ids == nil || len(ids) > 1000 {
			fail(400, "invalid_argument", "Invalid blocks")
			return
		}
		seen := map[contracts.ID]bool{}
		for _, id := range ids {
			if !partnerID(id) || seen[id] {
				fail(400, "invalid_argument", "Invalid blocks")
				return
			}
			seen[id] = true
		}
	}
	var cursor string
	if value, present := fields["cursor"]; present {
		if json.Unmarshal(value, &cursor) != nil || cursor == "" || len(cursor) > 4096 {
			fail(400, "invalid_argument", "Invalid cursor")
			return
		}
		if _, ok := h.Service.(PagedReadApplication); !ok {
			fail(503, "capability_missing", "Read pagination unavailable")
			return
		}
	}
	version, digest, err := h.Profile.ProfileIdentity()
	decoded, digestErr := hex.DecodeString(string(digest))
	if err != nil || version == 0 || digestErr != nil || len(decoded) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(503, "dependency_unavailable", "Read unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	started := time.Now()
	in := knowledge.CurrentReadInput{Credential: credential, Request: contracts.RequestContext{RequestID: contracts.ID(requestID), ProfileDigest: digest, Deadline: deadline}, DocumentID: documentID, RevisionID: revisionID}
	var record contracts.FrozenContentRecord
	var nextCursor string
	if paged, ok := h.Service.(PagedReadApplication); ok {
		var page knowledge.ReadPageResult
		page, err = paged.ReadPage(ctx, in, knowledge.ReadPageOptions{BlockIDs: ids, Cursor: cursor})
		record, ids, nextCursor = page.Record, page.BlockIDs, page.NextCursor
	} else {
		record, err = h.Service.ReadCurrent(ctx, in)
	}
	if ctx.Err() != nil {
		fail(504, "deadline_exceeded", "Read deadline exceeded")
		return
	}
	if errors.Is(err, knowledge.ErrReadCursor) {
		fail(409, "conflict", "Read snapshot unavailable")
		return
	}
	if errors.Is(err, knowledge.ErrReadPagination) {
		fail(503, "capability_missing", "Read pagination unavailable")
		return
	}
	if err != nil || record.DocumentID != documentID || revisionID != "" && record.RevisionID != revisionID {
		fail(503, "dependency_unavailable", "Read unavailable")
		return
	}
	body, err := ProjectReadContent(record, ids)
	if err != nil {
		fail(503, "dependency_unavailable", "Read unavailable")
		return
	}
	body["request_id"], body["profile_version"], body["profile_digest"] = requestID, version, digest
	body["timings"] = map[string]any{"total_ms": float64(time.Since(started).Microseconds()) / 1000}
	body["degraded"] = []any{}
	if nextCursor != "" {
		body["next_cursor"] = nextCursor
	}
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > 4<<20 {
		fail(503, "dependency_unavailable", "Read unavailable")
		return
	}
	if ctx.Err() != nil {
		fail(504, "deadline_exceeded", "Read deadline exceeded")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(encoded)
}
