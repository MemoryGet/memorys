package main

import (
	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/bootstrap"
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
	return newMuxWithDeletion(search, read, mcpHandler, nil, search)
}

func newMuxWithDeletion(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource) http.Handler {
	return newMuxWithJobStatus(search, read, mcpHandler, deletion, deletionProfile, nil)
}

func newMuxWithJobStatus(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication) http.Handler {
	return newMuxWithJobControl(search, read, mcpHandler, deletion, deletionProfile, jobs, nil)
}

func newMuxWithJobControl(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication, cancel api.JobCancellationApplication) http.Handler {
	mux, err := bootstrap.New(bootstrap.Config{Search: search, Read: read, MCP: mcpHandler, Deletion: deletion, DeletionProfile: deletionProfile, Jobs: jobs, Cancel: cancel})
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "invalid bootstrap", http.StatusServiceUnavailable)
		})
	}
	return mux
}
func main() {
	server := &http.Server{Addr: "127.0.0.1:8080", Handler: newMux(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Println("memorys-server listening on 127.0.0.1:8080; business authorization not configured")
	log.Fatal(server.ListenAndServe())
}
