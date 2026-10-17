package main

import (
	"encoding/json"
	"github.com/MemoryGet/memorys/pkg/api"
	"log"
	"net/http"
	"time"
)

// Business routes require trusted application authorization/preparation hooks.
// Bootstrap does not invent identities or publish uploaded content.
func newMux() http.Handler { return newMuxWithSearch(nil) }

func newMuxWithSearch(search api.PartnerSearchApplication) http.Handler {
	return newMuxWithServices(search, nil)
}

func newMuxWithServices(search api.PartnerSearchApplication, mcpHandler http.Handler) http.Handler {
	return newMuxWithRead(search, nil, mcpHandler)
}

func newMuxWithRead(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
	})
	mux.HandleFunc("/api/documents/upload", (api.UploadHandler{}).Handle)
	mux.Handle("/api/knowledge/search", api.ApplicationSearchHandler{Service: search})
	mux.Handle("/api/v1/knowledge-os/search", api.PartnerSearchHandler{Service: search})
	mux.Handle("/api/v1/knowledge-os/read", api.PartnerReadHandler{Service: read, Profile: search})
	if mcpHandler == nil {
		mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "MCP not configured", 503) })
	} else {
		mux.Handle("/mcp", mcpHandler)
	}
	return mux
}
func main() {
	server := &http.Server{Addr: "127.0.0.1:8080", Handler: newMux(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Println("memorys-server listening on 127.0.0.1:8080; business authorization not configured")
	log.Fatal(server.ListenAndServe())
}
