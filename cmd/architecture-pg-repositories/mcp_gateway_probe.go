package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/mcpgateway"
)

var mcpGatewaySamples []map[string]any

func verifyMCPGateway(ctx context.Context, db *sql.DB, auth contracts.Authenticator, service *knowledge.SearchService, credential contracts.Credential, query string, space contracts.ID, partner json.RawMessage) error {
	root := os.Getenv("AKOS_TEST_ROOT")
	if root == "" {
		return errors.New("test workspace root missing")
	}
	catalog, err := os.ReadFile(filepath.Join(root, "docs", "architecture", "mcp", "tools.v1.json"))
	if err != nil {
		return err
	}
	gateway, err := mcpgateway.New(mcpgateway.Config{Auth: auth, Search: service, Catalog: catalog})
	if err != nil {
		return err
	}
	server := httptest.NewServer(gateway)
	defer server.Close()
	client := &http.Client{}
	call := func(method string, params any, version string) (int, json.RawMessage, error) {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/mcp", strings.NewReader(string(body)))
		if err != nil {
			return 0, nil, err
		}
		r.Header.Set("Authorization", "Bearer "+string(credential.Secret))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		if version != "" {
			r.Header.Set("Mcp-Protocol-Version", version)
		}
		response, err := client.Do(r)
		if err != nil {
			return 0, nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.Header.Get("Mcp-Session-Id") != "" {
			return 0, nil, errors.New("MCP response read failed")
		}
		return response.StatusCode, json.RawMessage(data), nil
	}
	status, initial, err := call("initialize", map[string]any{"protocolVersion": mcpgateway.ProtocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "native-probe", "version": "1"}}, "")
	if err != nil || status != 200 {
		return errors.New("native MCP initialization failed")
	}
	var init struct {
		Result struct {
			Version string `json:"protocolVersion"`
		} `json:"result"`
	}
	if json.Unmarshal(initial, &init) != nil || init.Result.Version != mcpgateway.ProtocolVersion {
		return errors.New("MCP version negotiation differs")
	}
	status, tools, err := call("tools/list", map[string]any{}, mcpgateway.ProtocolVersion)
	if err != nil || status != 200 {
		return errors.New("native MCP listing failed")
	}
	var list struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if json.Unmarshal(tools, &list) != nil || len(list.Result.Tools) != 1 || list.Result.Tools[0].Name != "knowledge_search" {
		return errors.New("MCP advertised unimplemented tool")
	}
	args := map[string]any{"query": query, "space_id": space, "top_k": 1}
	status, result, err := call("tools/call", map[string]any{"name": "knowledge_search", "arguments": args}, mcpgateway.ProtocolVersion)
	if err != nil || status != 200 {
		return errors.New("native MCP search failed")
	}
	var response struct {
		Result struct {
			Error      bool            `json:"isError"`
			Structured json.RawMessage `json:"structuredContent"`
			Content    []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(result, &response) != nil || response.Result.Error || len(response.Result.Content) != 1 {
		var failure struct {
			Code string `json:"code"`
			Safe string `json:"safe_message"`
		}
		_ = json.Unmarshal(response.Result.Structured, &failure)
		remaining := time.Duration(0)
		if deadline, ok := ctx.Deadline(); ok {
			remaining = time.Until(deadline)
		}
		return fmt.Errorf("native MCP tool output failed: code=%s remaining=%s context=%v", failure.Code, remaining, ctx.Err())
	}
	var a, b map[string]any
	if json.Unmarshal(response.Result.Structured, &a) != nil || json.Unmarshal([]byte(response.Result.Content[0].Text), &b) != nil {
		return errors.New("MCP structured/text missing")
	}
	ar, _ := json.Marshal(a)
	br, _ := json.Marshal(b)
	if string(ar) != string(br) {
		return errors.New("MCP dual representations differ")
	}
	var rest map[string]any
	if json.Unmarshal(partner, &rest) != nil {
		return errors.New("REST reference missing")
	}
	expected, _ := json.Marshal(rest["evidence"])
	actual, _ := json.Marshal(a["evidence"])
	if string(expected) != string(actual) || a["profile_digest"] != rest["profile_digest"] {
		return errors.New("REST/MCP evidence parity failed")
	}
	badArgs := map[string]any{"query": query, "space_id": space, "user_id": "forged"}
	status, invalid, err := call("tools/call", map[string]any{"name": "knowledge_search", "arguments": badArgs}, mcpgateway.ProtocolVersion)
	if err != nil || status != 200 {
		return errors.New("MCP invalid arguments response failed")
	}
	var invalidResult struct {
		Result struct {
			Error      bool           `json:"isError"`
			Structured map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	if json.Unmarshal(invalid, &invalidResult) != nil || !invalidResult.Result.Error || invalidResult.Result.Structured["code"] != "invalid_argument" {
		return errors.New("MCP forged actor was accepted")
	}
	if status, _, err := call("tools/list", map[string]any{}, "2026-07-28"); err != nil || status != 400 {
		return errors.New("MCP protocol whitelist failed")
	}
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=true WHERE token_id='identity-token'`); err != nil {
		return err
	}
	revokedStatus, revokedBody, revokedErr := call("tools/list", map[string]any{}, mcpgateway.ProtocolVersion)
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=false WHERE token_id='identity-token'`); err != nil {
		return err
	}
	if revokedErr != nil || revokedStatus != 401 || strings.Contains(string(revokedBody), string(credential.Secret)) {
		return errors.New("MCP revoked token reached protocol dispatch")
	}
	mcpGatewaySamples = append(mcpGatewaySamples, map[string]any{"sdk": "github.com/modelcontextprotocol/go-sdk@v1.8.0", "protocol": mcpgateway.ProtocolVersion, "initialize": initial, "tools_list": tools, "arguments": args, "search_result": result, "invalid_result": invalid, "rest_result": partner, "mode": "controlled-private-bearer", "unsupported_version_rejected": true, "revoked_credential_rejected": true})
	return nil
}
