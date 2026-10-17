package mcpgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/mcpgateway"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerTransport struct{ base http.RoundTripper }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer alice")
	return b.base.RoundTrip(r)
}

// Exercise SDK client negotiation and notifications, rather than only manually
// constructed JSON-RPC. The application is a fixture; this is transport evidence.
func TestOfficialClientStreamableSearch(t *testing.T) {
	g, err := mcpgateway.New(config(t, application(sampleApplication)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(g)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "akos-interoperability-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: server.URL, HTTPClient: &http.Client{Transport: bearerTransport{http.DefaultTransport}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, &mcp.ClientSessionOptions{ProtocolVersion: mcpgateway.ProtocolVersion})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "knowledge_search" {
		t.Fatalf("tool listing: %v %v", tools, err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "knowledge_search", Arguments: map[string]any{
		"query": "中文", "space_id": "space", "kb_ids": []string{"kb"}, "top_k": 1,
	}})
	if err != nil || result.IsError || len(result.Content) != 1 {
		t.Fatalf("tool call: %v %v", result, err)
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("missing text result")
	}
	var text any
	if json.Unmarshal([]byte(content.Text), &text) != nil {
		t.Fatal("invalid text JSON")
	}
	a, _ := json.Marshal(text)
	b, err := json.Marshal(result.StructuredContent)
	if err != nil || string(a) != string(b) || !strings.Contains(string(a), "source-alice") {
		t.Fatal("client response representations differ")
	}
}

type auth struct{}

type readApplication func(context.Context, knowledge.CurrentReadInput) (contracts.FrozenContentRecord, error)

func (f readApplication) ReadCurrent(c context.Context, q knowledge.CurrentReadInput) (contracts.FrozenContentRecord, error) {
	return f(c, q)
}

func TestOfficialClientCurrentRead(t *testing.T) {
	frozen, err := document.BuildFrozenContent(contracts.Draft{ID: "draft", DocumentID: "doc", State: "draft", RowVersion: 1, Blocks: []contracts.DraftBlock{{ID: "block", DraftID: "draft", RowVersion: 1, Content: contracts.Content{Kind: contracts.TextBlock, Text: "已验证正文", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "coarse"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	c := config(t, application(sampleApplication))
	c.Read = readApplication(func(ctx context.Context, q knowledge.CurrentReadInput) (contracts.FrozenContentRecord, error) {
		if string(q.Credential.Secret) != "alice" || q.DocumentID != "doc" || ctx.Err() != nil {
			t.Fatal("read identity lost")
		}
		return contracts.FrozenContentRecord{DocumentID: "doc", RevisionID: "rev", Payload: frozen.Payload}, nil
	})
	g, err := mcpgateway.New(c)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(g)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "read-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: &http.Client{Transport: bearerTransport{http.DefaultTransport}}, DisableStandaloneSSE: true, MaxRetries: -1}, &mcp.ClientSessionOptions{ProtocolVersion: mcpgateway.ProtocolVersion})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 2 {
		t.Fatal(tools, err)
	}
	for _, args := range []map[string]any{{"document_id": "doc"}, {"document_id": "doc", "cursor": "unsupported"}, {"document_id": "doc", "subject_id": "forged"}} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "knowledge_read", Arguments: args})
		if err != nil || len(result.Content) != 1 {
			t.Fatal(result, err)
		}
		wantError := len(args) > 1
		if result.IsError != wantError {
			t.Fatal("read success/error mismatch", result)
		}
		text, ok := result.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatal("missing JSON text")
		}
		var value any
		if json.Unmarshal([]byte(text.Text), &value) != nil {
			t.Fatal("invalid JSON")
		}
		a, _ := json.Marshal(value)
		b, _ := json.Marshal(result.StructuredContent)
		if string(a) != string(b) {
			t.Fatal("read formats differ")
		}
	}
}

func (auth) Authenticate(_ context.Context, c contracts.Credential) (contracts.Principal, error) {
	token := string(c.Secret)
	if token != "alice" && token != "bob" {
		return contracts.Principal{}, errors.New("private authentication detail")
	}
	return contracts.Principal{SubjectID: contracts.ID(token), SubjectType: "user", Issuer: "test", ClientID: "client"}, nil
}

type application func(context.Context, knowledge.SearchInput) ([]contracts.Candidate, error)

func (f application) Search(c context.Context, in knowledge.SearchInput) ([]contracts.Candidate, error) {
	return f(c, in)
}
func (application) ProfileIdentity() (uint64, contracts.Digest, error) {
	return 1, contracts.Digest(strings.Repeat("a", 64)), nil
}
func config(t *testing.T, app api.PartnerSearchApplication) mcpgateway.Config {
	t.Helper()
	raw, err := os.ReadFile("../../docs/architecture/mcp/tools.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return mcpgateway.Config{Auth: auth{}, Search: app, Catalog: raw}
}
func sampleApplication(_ context.Context, in knowledge.SearchInput) ([]contracts.Candidate, error) {
	return []contracts.Candidate{{Text: "source-" + string(in.Credential.Secret), Rank: 1, Citation: contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "coarse"}}}}}, nil
}
func rpc(g http.Handler, token, method string, params any, version string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	r := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Authorization", "Bearer "+token)
	if version != "" {
		r.Header.Set("Mcp-Protocol-Version", version)
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}
func TestSDKInitializationListAndSearchResults(t *testing.T) {
	g, err := mcpgateway.New(config(t, application(sampleApplication)))
	if err != nil {
		t.Fatal(err)
	}
	w := rpc(g, "alice", "initialize", map[string]any{"protocolVersion": mcpgateway.ProtocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}}, "")
	var init struct {
		Result struct {
			Version string `json:"protocolVersion"`
		} `json:"result"`
	}
	if json.Unmarshal(w.Body.Bytes(), &init) != nil || w.Code != 200 || init.Result.Version != mcpgateway.ProtocolVersion || w.Header().Get("Mcp-Session-Id") != "" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = rpc(g, "alice", "tools/list", map[string]any{}, mcpgateway.ProtocolVersion)
	var list struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Result.Tools) != 1 || list.Result.Tools[0].Name != "knowledge_search" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = rpc(g, "alice", "tools/call", map[string]any{"name": "knowledge_search", "arguments": map[string]any{"query": "中文", "space_id": "space", "kb_ids": []string{"kb"}, "top_k": 1}}, mcpgateway.ProtocolVersion)
	var result struct {
		Result struct {
			Error      bool           `json:"isError"`
			Structured map[string]any `json:"structuredContent"`
			Content    []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || w.Code != 200 || result.Result.Error || len(result.Result.Content) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	var text map[string]any
	if json.Unmarshal([]byte(result.Result.Content[0].Text), &text) != nil || len(result.Result.Structured) == 0 {
		t.Fatal("missing dual representation")
	}
	a, _ := json.Marshal(text)
	b, _ := json.Marshal(result.Result.Structured)
	if string(a) != string(b) || !strings.Contains(string(a), "source-alice") {
		t.Fatal("structured/text identity lost", w.Body.String())
	}
}
func TestGatewayRejectsAuthenticationVersionOriginAndUnknownArguments(t *testing.T) {
	calls := 0
	g, err := mcpgateway.New(config(t, application(func(c context.Context, in knowledge.SearchInput) ([]contracts.Candidate, error) {
		calls++
		return sampleApplication(c, in)
	})))
	if err != nil {
		t.Fatal(err)
	}
	if w := rpc(g, "revoked", "tools/list", map[string]any{}, mcpgateway.ProtocolVersion); w.Code != 401 || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := rpc(g, "alice", "tools/list", map[string]any{}, "2026-07-28"); w.Code != 400 {
		t.Fatal("version accepted", w.Code)
	}
	r := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("origin accepted")
	}
	w = rpc(g, "alice", "tools/call", map[string]any{"name": "knowledge_search", "arguments": map[string]any{"query": "x", "space_id": "space", "user_id": "bob"}}, mcpgateway.ProtocolVersion)
	var out struct {
		Result struct {
			Error      bool           `json:"isError"`
			Structured map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || !out.Result.Error || out.Result.Structured["code"] != "invalid_argument" || calls != 0 {
		t.Fatal("invalid arguments reached business", w.Code, w.Body.String())
	}
}
func TestConcurrentRequestsDoNotShareCredential(t *testing.T) {
	g, err := mcpgateway.New(config(t, application(sampleApplication)))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, token := range []string{"alice", "bob"} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			w := rpc(g, token, "tools/call", map[string]any{"name": "knowledge_search", "arguments": map[string]any{"query": "x", "space_id": "space", "kb_ids": []string{"kb"}, "top_k": 1}}, mcpgateway.ProtocolVersion)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "source-"+token) {
				t.Errorf("credential cross-talk: %s", w.Body.String())
			}
		}(token)
	}
	wg.Wait()
}
func TestRequestCancellationReachesApplication(t *testing.T) {
	started, finished := make(chan struct{}), make(chan struct{})
	app := application(func(ctx context.Context, in knowledge.SearchInput) ([]contracts.Candidate, error) {
		close(started)
		<-ctx.Done()
		close(finished)
		return []contracts.Candidate{{Text: "unverified-private"}}, nil
	})
	g, err := mcpgateway.New(config(t, app))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	raw := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knowledge_search","arguments":{"query":"x","space_id":"space","kb_ids":["kb"]}}}`
	r := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", strings.NewReader(raw)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Authorization", "Bearer alice")
	r.Header.Set("Mcp-Protocol-Version", mcpgateway.ProtocolVersion)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { g.ServeHTTP(w, r); close(done) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("application not entered")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("application cancellation lost")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway did not finish")
	}
	if strings.Contains(w.Body.String(), "unverified-private") {
		t.Fatal("cancelled content escaped")
	}
}

func TestFrozenCatalogAndDefaultEmbedding(t *testing.T) {
	c := config(t, application(sampleApplication))
	c.Catalog = nil
	if _, err := mcpgateway.New(c); err != nil {
		t.Fatal("embedded contract unavailable", err)
	}
	c = config(t, application(sampleApplication))
	c.Catalog = []byte(strings.Replace(string(c.Catalog), "Search authorized published evidence", "Changed business contract", 1))
	if _, err := mcpgateway.New(c); err == nil {
		t.Fatal("contract drift accepted")
	}
	c = config(t, application(nil))
	if _, err := mcpgateway.New(c); err == nil {
		t.Fatal("typed nil application accepted")
	}
}

func TestProtocolMetadataAliasSessionAndTaskRejected(t *testing.T) {
	g, err := mcpgateway.New(config(t, application(sampleApplication)))
	if err != nil {
		t.Fatal(err)
	}
	for _, params := range []map[string]any{{"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28"}}, {"_Meta": map[string]any{}}, {"task": map[string]any{}}} {
		if w := rpc(g, "alice", "tools/list", params, mcpgateway.ProtocolVersion); w.Code != 400 {
			t.Fatal("protocol bypass accepted", params, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer alice")
	r.Header.Set("Mcp-Session-Id", "not-a-credential")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("session header accepted", w.Code)
	}
	r = httptest.NewRequest(http.MethodGet, "http://localhost/mcp", nil)
	r.Header.Set("Authorization", "Bearer alice")
	w = httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 405 || w.Header().Get("Allow") != "POST" {
		t.Fatal("stateless GET was not rejected")
	}
}
