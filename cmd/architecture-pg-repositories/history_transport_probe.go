package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/mcpgateway"
)

var historicalSearchService api.PartnerSearchApplication
var startHistoricalTransport func(context.Context) (*historicalTransport, error)

// A trusted fixture snapshot; search is not invoked by this read-only probe.
type lifecycleProfile struct {
	api.PartnerSearchApplication
	version uint64
	digest  contracts.Digest
}

func (p lifecycleProfile) ProfileIdentity() (uint64, contracts.Digest, error) {
	return p.version, p.digest, nil
}

type historicalTransport struct {
	ctx        context.Context
	server     *httptest.Server
	credential contracts.Credential
	samples    []map[string]any
}

func newHistoricalTransport(ctx context.Context, db *sql.DB, auth contracts.Authenticator, read api.CurrentReadApplication, credential contracts.Credential) (*historicalTransport, error) {
	var raw []byte
	err := db.QueryRowContext(ctx, `SELECT c.payload FROM akos_withdraw_v1.query_profile_heads h JOIN akos_withdraw_v1.query_profile_bindings b USING(space_id,kb_id,version) JOIN akos_withdraw_v1.query_profile_catalog c ON c.profile_digest=b.profile_digest WHERE h.space_id='space' AND h.kb_id='kb'`).Scan(&raw)
	if err != nil {
		return nil, err
	}
	snapshot, err := configprofile.Decode(raw)
	if err != nil {
		return nil, err
	}
	profile := lifecycleProfile{PartnerSearchApplication: historicalSearchService, version: snapshot.Pipeline.Version, digest: snapshot.Pipeline.Digest}
	gateway, err := mcpgateway.New(mcpgateway.Config{Auth: auth, Search: profile, Read: read})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/api/v1/knowledge-os/read", api.PartnerReadHandler{Service: read, Profile: profile})
	mux.Handle("/mcp", gateway)
	probe := &historicalTransport{ctx: ctx, server: httptest.NewServer(mux), credential: credential}
	status, _, err := probe.rpc("initialize", map[string]any{"protocolVersion": mcpgateway.ProtocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "history-lifecycle", "version": "1"}})
	if err != nil || status != 200 {
		probe.server.Close()
		return nil, errors.New("history lifecycle MCP initialization failed")
	}
	return probe, nil
}

func (p *historicalTransport) post(path string, value any) (int, json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, nil, err
	}
	r, err := http.NewRequestWithContext(p.ctx, "POST", p.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	r.Header.Set("Authorization", "Bearer "+string(p.credential.Secret))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Mcp-Protocol-Version", mcpgateway.ProtocolVersion)
	response, err := p.server.Client().Do(r)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return 0, nil, errors.New("history lifecycle HTTP body unavailable")
	}
	return response.StatusCode, body, nil
}

func (p *historicalTransport) rpc(method string, params any) (int, json.RawMessage, error) {
	return p.post("/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
}

func (p *historicalTransport) args(cursor string) map[string]any {
	args := map[string]any{"document_id": historicalTarget.Document, "revision_id": historicalTarget.NewRevision}
	if cursor != "" {
		args["cursor"] = cursor
	}
	return args
}

func (p *historicalTransport) pair(phase string) (string, error) {
	args := p.args("")
	status, first, err := p.post("/api/v1/knowledge-os/read", args)
	var page struct {
		Cursor string `json:"next_cursor"`
	}
	if err != nil || status != 200 || json.Unmarshal(first, &page) != nil || page.Cursor == "" {
		return "", errors.New("history lifecycle REST first page failed")
	}
	continued := p.args(page.Cursor)
	status, last, err := p.rpc("tools/call", map[string]any{"name": "knowledge_read", "arguments": continued})
	var result struct {
		Result struct {
			IsError    bool           `json:"isError"`
			Structured map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	if err != nil || status != 200 || json.Unmarshal(last, &result) != nil || result.Result.IsError || result.Result.Structured["next_cursor"] != nil || result.Result.Structured["revision_id"] != string(historicalTarget.NewRevision) {
		return "", errors.New("history lifecycle MCP continuation failed")
	}
	p.samples = append(p.samples, map[string]any{"phase": phase, "request": args, "continuation": continued, "rest_status": 200, "rest_result": first, "mcp_status": status, "mcp_result": last})
	return page.Cursor, nil
}

func safeLifecycleError(raw json.RawMessage) bool {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil || len(value) != 4 {
		return false
	}
	for _, key := range []string{"code", "safe_message", "request_id", "retryable"} {
		if _, ok := value[key]; !ok {
			return false
		}
	}
	return true
}

func (p *historicalTransport) deny(phase, cursor string, conflict bool) error {
	args := p.args(cursor)
	status, rest, err := p.post("/api/v1/knowledge-os/read", args)
	if err != nil || status < 400 || !safeLifecycleError(rest) || conflict && status != 409 {
		return errors.New("history lifecycle REST denial failed")
	}
	mcpStatus, mcp, err := p.rpc("tools/call", map[string]any{"name": "knowledge_read", "arguments": args})
	var result struct {
		Result struct {
			IsError    bool            `json:"isError"`
			Structured json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	if err != nil || mcpStatus != 200 || json.Unmarshal(mcp, &result) != nil || !result.Result.IsError || !safeLifecycleError(result.Result.Structured) {
		return errors.New("history lifecycle MCP denial failed")
	}
	p.samples = append(p.samples, map[string]any{"phase": phase, "request": args, "rest_status": status, "rest_result": rest, "mcp_status": mcpStatus, "mcp_result": mcp})
	return nil
}
