package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

type SearchApplication interface {
	Search(context.Context, knowledge.SearchInput) ([]contracts.Candidate, error)
}

// ApplicationSearchHandler accepts public search arguments only. Identity and
// publication filters are resolved inside the shared application service.
// This remains the provisional GET transport, not the partner OpenAPI handler.
type ApplicationSearchHandler struct{ Service SearchApplication }

func (h ApplicationSearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	if h.Service == nil {
		http.Error(w, "search not configured", 503)
		return
	}
	values := r.Header.Values("Authorization")
	if len(values) != 1 || len(values[0]) > 16400 {
		http.Error(w, "authentication required", 401)
		return
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || len(token) > 16384 || strings.ContainsAny(token, " \t\r\n,") {
		http.Error(w, "authentication required", 401)
		return
	}
	if len(r.URL.RawQuery) > 32768 {
		http.Error(w, "invalid search", 400)
		return
	}
	args, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		http.Error(w, "invalid search", 400)
		return
	}
	for k := range args {
		if k != "q" && k != "space_id" && k != "kb_id" && k != "limit" {
			http.Error(w, "invalid search", 400)
			return
		}
	}
	if len(args["q"]) != 1 || len(args["space_id"]) != 1 || len(args["limit"]) > 1 || len(args["kb_id"]) == 0 || len(args["kb_id"]) > 256 {
		http.Error(w, "invalid search", 400)
		return
	}
	count := 10
	if raw := args.Get("limit"); raw != "" {
		count, err = strconv.Atoi(raw)
	}
	if err != nil || count < 1 || count > 1000 {
		http.Error(w, "invalid search", 400)
		return
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		http.Error(w, "search unavailable", 503)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	ids := make([]contracts.ID, 0, len(args["kb_id"]))
	for _, kb := range args["kb_id"] {
		ids = append(ids, contracts.ID(kb))
	}
	results, err := h.Service.Search(ctx, knowledge.SearchInput{Credential: credential, RequestID: contracts.ID(hex.EncodeToString(nonce[:])), Deadline: deadline, SpaceID: contracts.ID(args.Get("space_id")), KnowledgeBaseIDs: ids, Text: args.Get("q"), Limit: count})
	if err != nil || ctx.Err() != nil {
		http.Error(w, "search unavailable", 503)
		return
	}
	output := make([]map[string]any, 0, len(results))
	for _, c := range results {
		if c.Citation.DocumentID == "" || c.Citation.RevisionID == "" || c.Citation.BlockID == "" {
			http.Error(w, "search unavailable", 503)
			return
		}
		output = append(output, map[string]any{"document_id": c.Citation.DocumentID, "revision_id": c.Citation.RevisionID, "block_id": c.Citation.BlockID, "text": c.Text, "rank": c.Rank, "source_anchor": sourceDTO(c.Citation.Source)})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"results": output})
}
