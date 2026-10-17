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
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/mcpgateway"
)

func verifyPartnerReadGateway(ctx context.Context, db *sql.DB, auth contracts.Authenticator, profile api.PartnerSearchApplication, read api.CurrentReadApplication, credential contracts.Credential, doc contracts.ID) error {
	gateway, err := mcpgateway.New(mcpgateway.Config{Auth: auth, Search: profile, Read: read})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/read", api.PartnerReadHandler{Service: read, Profile: profile})
	mux.Handle("/mcp", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()
	post := func(path string, value any) (json.RawMessage, error) {
		raw, _ := json.Marshal(value)
		r, err := http.NewRequestWithContext(ctx, "POST", server.URL+path, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("Authorization", "Bearer "+string(credential.Secret))
		r.Header.Set("Mcp-Protocol-Version", mcpgateway.ProtocolVersion)
		response, err := server.Client().Do(r)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
		if err != nil || response.StatusCode != 200 || len(data) > 4<<20 {
			return nil, errors.New("native read HTTP failed")
		}
		return data, nil
	}
	args := map[string]any{"document_id": doc}
	rest, err := post("/read", args)
	if err != nil {
		return err
	}
	rpc := func(method string, params any) (json.RawMessage, error) {
		return post("/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	}
	initial, err := rpc("initialize", map[string]any{"protocolVersion": mcpgateway.ProtocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "native-read-probe", "version": "1"}})
	if err != nil {
		return err
	}
	listed, err := rpc("tools/list", map[string]any{})
	if err != nil {
		return err
	}
	result, err := rpc("tools/call", map[string]any{"name": "knowledge_read", "arguments": args})
	if err != nil {
		return err
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
		return errors.New("native MCP read failed")
	}
	var restValue, mcpValue, textValue map[string]any
	if json.Unmarshal(rest, &restValue) != nil || json.Unmarshal(response.Result.Structured, &mcpValue) != nil || json.Unmarshal([]byte(response.Result.Content[0].Text), &textValue) != nil {
		return errors.New("native read format missing")
	}
	a, _ := json.Marshal(mcpValue)
	b, _ := json.Marshal(textValue)
	if !bytes.Equal(a, b) {
		return errors.New("native MCP read formats differ")
	}
	for _, key := range []string{"document_id", "revision_id", "blocks", "citations", "profile_digest", "profile_version"} {
		a, _ := json.Marshal(restValue[key])
		b, _ := json.Marshal(mcpValue[key])
		if !bytes.Equal(a, b) {
			return errors.New("native REST/MCP read differs")
		}
	}
	if len(currentReadSamples) != 1 {
		return errors.New("native read sample missing")
	}
	sample := currentReadSamples[0]
	sample["rest_request"], sample["rest_result"], sample["mcp_result"] = args, rest, result
	sample["mcp_initialize"], sample["mcp_tools_list"] = initial, listed
	// Continue a REST cursor through MCP to prove both use the same application
	// and signing module. The fixture has two actual approved original blocks.
	cursor, ok := restValue["next_cursor"].(string)
	if !ok || cursor == "" {
		return errors.New("native read paging cursor missing")
	}
	nextArgs := map[string]any{"document_id": doc, "cursor": cursor}
	secondRest, err := post("/read", nextArgs)
	if err != nil {
		return err
	}
	secondMCP, err := rpc("tools/call", map[string]any{"name": "knowledge_read", "arguments": nextArgs})
	if err != nil {
		return err
	}
	var secondValue map[string]any
	if json.Unmarshal(secondRest, &secondValue) != nil || len(secondValue["blocks"].([]any)) != 1 || secondValue["next_cursor"] != nil {
		return errors.New("native read page completion differs")
	}
	if restValue["blocks"].([]any)[0].(map[string]any)["id"] == secondValue["blocks"].([]any)[0].(map[string]any)["id"] {
		return errors.New("native read pages repeated block")
	}
	sample["rest_pages"] = []json.RawMessage{rest, secondRest}
	sample["mcp_pages"] = []json.RawMessage{result, secondMCP}
	sample["page_requests"] = []map[string]any{args, nextArgs}
	if paged, ok := read.(api.PagedReadApplication); ok {
		version, digest, e := profile.ProfileIdentity()
		if e != nil || version == 0 {
			return errors.New("native paging profile missing")
		}
		deadline, _ := ctx.Deadline()
		in := knowledge.CurrentReadInput{Credential: credential, DocumentID: doc, Request: contracts.RequestContext{RequestID: "native-tampered-cursor", ProfileDigest: digest, Deadline: deadline}}
		if output, e := paged.ReadPage(ctx, in, knowledge.ReadPageOptions{Cursor: cursor + "!"}); !errors.Is(e, knowledge.ErrReadCursor) || len(output.Record.Payload) != 0 {
			return errors.New("native tampered cursor released content")
		}
		sample["tampered_cursor_rejected"] = true
		if _, e := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=true WHERE token_id='identity-token'`); e != nil {
			return e
		}
		output, pageErr := paged.ReadPage(ctx, in, knowledge.ReadPageOptions{Cursor: cursor})
		if _, e := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=false WHERE token_id='identity-token'`); e != nil {
			return e
		}
		if pageErr == nil || len(output.Record.Payload) != 0 {
			return errors.New("native revoked identity continued page")
		}
		sample["revoked_page_identity_rejected"] = true
	} else {
		return errors.New("native paged application missing")
	}
	return nil
}
