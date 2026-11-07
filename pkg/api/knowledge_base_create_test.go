package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type knowledgeBaseCreateFixture struct{}

func (knowledgeBaseCreateFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	h := sha256.Sum256([]byte("profile"))
	return 1, contracts.Digest(hex.EncodeToString(h[:])), nil
}
func (knowledgeBaseCreateFixture) CreateKnowledgeBase(context.Context, contracts.KnowledgeBaseCreateInput) (contracts.KnowledgeBase, error) {
	return contracts.KnowledgeBase{ID: "kb1", SpaceID: "space1", Name: "资料库", RowVersion: 1, PolicyVersion: 1}, nil
}

func TestKnowledgeBaseCreateHandlerAcceptsStrictJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/spaces/space1/knowledge-bases", strings.NewReader(`{"name":"资料库"}`))
	r.SetPathValue("space_id", "space1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "create-key")
	w := httptest.NewRecorder()
	(KnowledgeBaseCreateHandler{Service: knowledgeBaseCreateFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"id":"kb1"`) {
		t.Fatalf("create response: %d %s", w.Code, w.Body.String())
	}
}
