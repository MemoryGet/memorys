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

type grantFixture struct{}

func (grantFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	h := sha256.Sum256([]byte("profile"))
	return 1, contracts.Digest(hex.EncodeToString(h[:])), nil
}

func (grantFixture) SetGrant(_ context.Context, input contracts.SetGrantInput) (contracts.GrantResult, error) {
	return contracts.GrantResult{KnowledgeBaseID: input.KnowledgeBaseID, SubjectID: input.SubjectID, Role: input.Role, PolicyVersion: input.ExpectedPolicyVersion + 1}, nil
}

func (grantFixture) ListGrants(context.Context, contracts.GrantListInput) (contracts.GrantPage, error) {
	return contracts.GrantPage{Items: []contracts.GrantItem{{KnowledgeBaseID: "kb", SubjectID: "user", Role: "Viewer", PolicyVersion: 4}}}, nil
}

func TestGrantHandlerReturnsPolicyVersion(t *testing.T) {
	r := httptest.NewRequest(http.MethodPut, "/api/v1/knowledge-os/knowledge-bases/kb/grants/user", strings.NewReader(`{"role":"Viewer","expected_policy_version":4}`))
	r.SetPathValue("kb_id", "kb")
	r.SetPathValue("subject_id", "user")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "grant-1")
	w := httptest.NewRecorder()
	(GrantHandler{Service: grantFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"role":"Viewer"`) || !strings.Contains(w.Body.String(), `"policy_version":5`) {
		t.Fatalf("grant response: %d %s", w.Code, w.Body.String())
	}
}

func TestGrantListHandlerReturnsMembers(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/knowledge-bases/kb/grants?page_size=10", nil)
	r.SetPathValue("kb_id", "kb")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(GrantListHandler{Service: grantFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"subject_id":"user"`) || !strings.Contains(w.Body.String(), `"role":"Viewer"`) {
		t.Fatalf("grant list response: %d %s", w.Code, w.Body.String())
	}
}
