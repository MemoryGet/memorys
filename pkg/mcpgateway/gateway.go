// Package mcpgateway isolates the official MCP SDK from application contracts.
package mcpgateway

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const ProtocolVersion = "2025-11-25"

//go:embed tools.v1.json
var builtinCatalog []byte

var ErrConfiguration = errors.New("MCP configuration rejected")

type Config struct {
	Auth           contracts.Authenticator
	Search         api.PartnerSearchApplication
	Read           api.CurrentReadApplication
	Catalog        []byte
	AllowedOrigins []string
	MaxConcurrent  int
}
type Gateway struct {
	auth      contracts.Authenticator
	tools     []runtimeTool
	origins   map[string]bool
	slots     chan struct{}
	transport http.Handler
}
type runtimeTool struct {
	tool          *mcp.Tool
	input, output *jsonschema.Resolved
	handler       http.Handler
	path          string
}
type definition struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Input       json.RawMessage      `json:"inputSchema"`
	Output      json.RawMessage      `json:"outputSchema"`
	Annotations *mcp.ToolAnnotations `json:"annotations"`
	Execution   json.RawMessage      `json:"execution"`
}

func New(c Config) (*Gateway, error) {
	if absent(c.Auth) || absent(c.Search) || len(c.Catalog) > 1<<20 {
		return nil, ErrConfiguration
	}
	if len(c.Catalog) == 0 {
		c.Catalog = builtinCatalog
	}
	expected, err := canonical.JSON(builtinCatalog)
	if err != nil {
		return nil, ErrConfiguration
	}
	actual, err := canonical.JSON(c.Catalog)
	if err != nil || !bytes.Equal(expected, actual) {
		return nil, ErrConfiguration
	}
	if _, err := canonical.JSON(c.Catalog); err != nil {
		return nil, ErrConfiguration
	}
	var catalog struct {
		Tools []definition `json:"tools"`
	}
	d := json.NewDecoder(bytes.NewReader(c.Catalog))
	d.DisallowUnknownFields()
	if d.Decode(&catalog) != nil || len(catalog.Tools) != 4 {
		return nil, ErrConfiguration
	}
	var enabled []definition
	seen := map[string]bool{}
	for i := range catalog.Tools {
		t := &catalog.Tools[i]
		if seen[t.Name] || (t.Name != "knowledge_search" && t.Name != "knowledge_read" && t.Name != "knowledge_ask" && t.Name != "knowledge_feedback") {
			return nil, ErrConfiguration
		}
		seen[t.Name] = true
		if t.Name == "knowledge_search" || t.Name == "knowledge_read" && !absent(c.Read) {
			enabled = append(enabled, *t)
		}
	}
	if !seen["knowledge_search"] || len(enabled) == 0 {
		return nil, ErrConfiguration
	}
	var runtimeTools []runtimeTool
	for _, chosen := range enabled {
		var in, out jsonschema.Schema
		if json.Unmarshal(chosen.Input, &in) != nil || json.Unmarshal(chosen.Output, &out) != nil {
			return nil, ErrConfiguration
		}
		input, err := in.Resolve(nil)
		if err != nil {
			return nil, ErrConfiguration
		}
		output, err := out.Resolve(nil)
		if err != nil {
			return nil, ErrConfiguration
		}
		var handler http.Handler = api.PartnerSearchHandler{Service: c.Search}
		path := "/api/v1/knowledge-os/search"
		if chosen.Name == "knowledge_read" {
			handler = api.PartnerReadHandler{Service: c.Read, Profile: c.Search}
			path = "/api/v1/knowledge-os/read"
		}
		runtimeTools = append(runtimeTools, runtimeTool{tool: &mcp.Tool{Name: chosen.Name, Description: chosen.Description, InputSchema: chosen.Input, OutputSchema: chosen.Output, Annotations: chosen.Annotations}, input: input, output: output, handler: handler, path: path})
	}
	max := c.MaxConcurrent
	if max == 0 {
		max = 8
	}
	if max < 1 || max > 64 {
		return nil, ErrConfiguration
	}
	g := &Gateway{auth: c.Auth, tools: runtimeTools, origins: map[string]bool{}, slots: make(chan struct{}, max)}
	for _, origin := range c.AllowedOrigins {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || g.origins[origin] {
			return nil, ErrConfiguration
		}
		g.origins[origin] = true
	}
	g.transport = mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		server := mcp.NewServer(&mcp.Implementation{Name: "ai-knowledge-os", Version: "0.1.0"}, nil)
		for _, tool := range g.tools {
			server.AddTool(tool.tool, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				callCtx, cancel := context.WithCancel(ctx)
				defer cancel()
				stop := context.AfterFunc(r.Context(), cancel)
				defer stop()
				if deadline, ok := r.Context().Deadline(); ok {
					var end context.CancelFunc
					callCtx, end = context.WithDeadline(callCtx, deadline)
					defer end()
				}
				return g.call(callCtx, request, tool), nil
			})
		}
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, PropagateRequestCancellation: true})
	return g, nil
}
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if g == nil {
		http.Error(w, "MCP unavailable", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	origin := r.Header.Values("Origin")
	if len(origin) > 1 || len(origin) == 1 && !g.origins[origin[0]] {
		http.Error(w, "Origin denied", 403)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || len(headers[0]) > 16400 {
		unauthenticated(w)
		return
	}
	scheme, token, ok := strings.Cut(headers[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || len(token) > 16384 || strings.ContainsAny(token, " \t\r\n,") {
		unauthenticated(w)
		return
	}
	secret := []byte(token)
	who, err := g.auth.Authenticate(ctx, contracts.Credential{Kind: "access_token", Secret: secret})
	clear(secret)
	if err != nil || who.SubjectID == "" || who.Issuer == "" || ctx.Err() != nil {
		unauthenticated(w)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST required", 405)
		return
	}
	if r.Header.Get("Mcp-Session-Id") != "" {
		http.Error(w, "Sessions unsupported", 400)
		return
	}
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		http.Error(w, "MCP busy", 429)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		http.Error(w, "Invalid MCP request", 400)
		return
	}
	if _, err := canonical.JSON(raw); err != nil {
		http.Error(w, "Invalid MCP request", 400)
		return
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil || envelope == nil {
		http.Error(w, "Invalid MCP request", 400)
		return
	}
	for key := range envelope {
		if key != "jsonrpc" && key != "method" && key != "params" && key != "id" {
			http.Error(w, "Invalid MCP request", 400)
			return
		}
	}
	var message struct {
		Method string                     `json:"method"`
		Params map[string]json.RawMessage `json:"params"`
	}
	if json.Unmarshal(raw, &message) != nil {
		http.Error(w, "Invalid MCP request", 400)
		return
	}
	var allowed map[string]bool
	switch message.Method {
	case "initialize":
		allowed = map[string]bool{"protocolVersion": true, "capabilities": true, "clientInfo": true, "_meta": true}
	case "tools/list":
		allowed = map[string]bool{"cursor": true, "_meta": true}
	case "tools/call":
		allowed = map[string]bool{"name": true, "arguments": true, "_meta": true}
	}
	if allowed != nil {
		for key := range message.Params {
			if !allowed[key] {
				http.Error(w, "Invalid MCP parameters", 400)
				return
			}
		}
	}
	versions := r.Header.Values("Mcp-Protocol-Version")
	if len(versions) > 1 || len(versions) == 1 && versions[0] != ProtocolVersion || message.Method != "initialize" && len(versions) != 1 {
		http.Error(w, "Unsupported MCP version", 400)
		return
	}
	if message.Method == "initialize" {
		var version string
		if json.Unmarshal(message.Params["protocolVersion"], &version) != nil || version != ProtocolVersion {
			http.Error(w, "Unsupported MCP version", 400)
			return
		}
	}
	if _, ok := message.Params["task"]; ok {
		http.Error(w, "Tasks unsupported", 400)
		return
	}
	if meta, ok := message.Params["_meta"]; ok {
		var m map[string]json.RawMessage
		if json.Unmarshal(meta, &m) != nil {
			http.Error(w, "Invalid metadata", 400)
			return
		}
		if value, ok := m["io.modelcontextprotocol/protocolVersion"]; ok {
			var version string
			if json.Unmarshal(value, &version) != nil || version != ProtocolVersion {
				http.Error(w, "Unsupported MCP version", 400)
				return
			}
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	g.transport.ServeHTTP(w, r)
}
func unauthenticated(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "Authentication required", 401)
}

type response struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	overflow bool
}

func (r *response) Header() http.Header { return r.header }
func (r *response) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}
func (r *response) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	if r.body.Len()+len(p) > 4<<20 {
		r.overflow = true
		return 0, errors.New("result too large")
	}
	return r.body.Write(p)
}
func (g *Gateway) call(ctx context.Context, request *mcp.CallToolRequest, tool runtimeTool) *mcp.CallToolResult {
	if request == nil || request.Params == nil || request.Extra == nil {
		return toolError("dependency_unavailable", "Tool unavailable")
	}
	raw := request.Params.Arguments
	var input any
	if _, err := canonical.JSON(raw); err != nil || json.Unmarshal(raw, &input) != nil || tool.input.Validate(input) != nil {
		return toolError("invalid_argument", "Invalid tool arguments")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://internal"+tool.path, bytes.NewReader(raw))
	if err != nil {
		return toolError("internal", "Tool unavailable")
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", request.Extra.Header.Get("Authorization"))
	result := &response{header: make(http.Header)}
	tool.handler.ServeHTTP(result, r)
	body := result.body.Bytes()
	var output any
	if result.overflow || json.Unmarshal(body, &output) != nil || tool.output.Validate(output) != nil {
		return toolError("internal", "Tool unavailable")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}, StructuredContent: json.RawMessage(append([]byte(nil), body...)), IsError: result.status >= 400}
}
func toolError(code, message string) *mcp.CallToolResult {
	var nonce [16]byte
	_, _ = rand.Read(nonce[:])
	raw, _ := json.Marshal(map[string]any{"code": code, "safe_message": message, "retryable": false, "request_id": hex.EncodeToString(nonce[:])})
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}, StructuredContent: json.RawMessage(raw), IsError: true}
}

func absent(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}
