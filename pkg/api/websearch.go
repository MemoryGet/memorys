package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/websearch"
)

type WebSearchApplication interface {
	Search(context.Context, websearch.SearchInput) (websearch.SearchResponse, error)
	Fetch(context.Context, websearch.FetchInput) (websearch.FetchResponse, error)
}

type WebSearchHandler struct{ Service WebSearchApplication }
type WebFetchHandler struct{ Service WebSearchApplication }

type webSearchRequest struct {
	Query            string `json:"query"`
	Count            *int   `json:"count"`
	OriginalQuestion string `json:"ori_question"`
	SpaceID          string `json:"space_id"`
	NeedContent      bool   `json:"need_content"`
}

type webFetchRequest struct {
	URL     string `json:"url"`
	SpaceID string `json:"space_id"`
}

func (h WebSearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Service == nil {
		http.Error(w, "web search not configured", http.StatusServiceUnavailable)
		return
	}
	token, ok := bearerToken(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	var input webSearchRequest
	if !decodeStrict(w, r, &input, 64<<10) || input.SpaceID == "" {
		http.Error(w, "invalid web search", http.StatusBadRequest)
		return
	}
	count := 10
	if input.Count != nil {
		count = *input.Count
	}
	if count < 1 || count > 50 {
		http.Error(w, "invalid web search", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	requestID := requestID()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	out, err := h.Service.Search(ctx, websearch.SearchInput{Credential: credential, RequestID: requestID, Deadline: deadline, SpaceID: contracts.ID(input.SpaceID), Query: input.Query, OriginalQuestion: input.OriginalQuestion, Count: count, NeedContent: input.NeedContent})
	if err != nil || ctx.Err() != nil {
		http.Error(w, "web search unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, out)
}

func (h WebSearchHandler) Fetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Service == nil {
		http.Error(w, "web fetch not configured", http.StatusServiceUnavailable)
		return
	}
	token, ok := bearerToken(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	var input webFetchRequest
	if !decodeStrict(w, r, &input, 64<<10) || input.URL == "" || input.SpaceID == "" {
		http.Error(w, "invalid web fetch", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	out, err := h.Service.Fetch(ctx, websearch.FetchInput{Credential: credential, RequestID: requestID(), Deadline: deadline, SpaceID: contracts.ID(input.SpaceID), URL: input.URL})
	if err != nil || ctx.Err() != nil {
		http.Error(w, "web fetch unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, out)
}

func (h WebFetchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	WebSearchHandler{Service: h.Service}.Fetch(w, r)
}

func bearerToken(r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || len(values[0]) > 16400 {
		return "", false
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || len(token) > 16384 || strings.ContainsAny(token, " \t\r\n,") {
		return "", false
	}
	return token, true
}

func decodeStrict(w http.ResponseWriter, r *http.Request, destination any, maxBytes int64) bool {
	if r.Body == nil {
		return false
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return false
	}
	var extra any
	return decoder.Decode(&extra) == io.EOF
}

func requestID() contracts.ID {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return contracts.ID(hex.EncodeToString(make([]byte, 16)))
	}
	return contracts.ID(hex.EncodeToString(nonce[:]))
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}
