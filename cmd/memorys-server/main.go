package main

import (
	"context"
	"database/sql"
	"errors"
	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/bootstrap"
	"github.com/MemoryGet/memorys/pkg/localruntime"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Business routes require trusted application authorization/preparation hooks.
// Bootstrap does not invent identities or publish uploaded content.
func newMux() http.Handler { return newMuxWithSearch(nil) }

func newMuxWithSearch(search api.PartnerSearchApplication) http.Handler {
	return newMuxWithServicesAndReady(search, nil, nil)
}

func newMuxWithServices(search api.PartnerSearchApplication, mcpHandler http.Handler) http.Handler {
	return newMuxWithServicesAndReady(search, mcpHandler, nil)
}

func newMuxWithServicesAndReady(search api.PartnerSearchApplication, mcpHandler http.Handler, ready func(context.Context) error) http.Handler {
	return newMuxWithReadAndReady(search, nil, mcpHandler, ready)
}

func newMuxWithRead(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler) http.Handler {
	return newMuxWithReadAndReady(search, read, mcpHandler, nil)
}

func newMuxWithReadAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, ready func(context.Context) error) http.Handler {
	return newMuxWithDeletionAndReady(search, read, mcpHandler, nil, search, ready)
}

func newMuxWithDeletion(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource) http.Handler {
	return newMuxWithDeletionAndReady(search, read, mcpHandler, deletion, deletionProfile, nil)
}

func newMuxWithDeletionAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, ready func(context.Context) error) http.Handler {
	return newMuxWithJobStatusAndReady(search, read, mcpHandler, deletion, deletionProfile, nil, ready)
}

func newMuxWithJobStatus(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication) http.Handler {
	return newMuxWithJobStatusAndReady(search, read, mcpHandler, deletion, deletionProfile, jobs, nil)
}

func newMuxWithJobStatusAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication, ready func(context.Context) error) http.Handler {
	return newMuxWithJobControlAndReady(search, read, mcpHandler, deletion, deletionProfile, jobs, nil, ready)
}

func newMuxWithJobControl(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication, cancel api.JobCancellationApplication) http.Handler {
	return newMuxWithJobControlAndReady(search, read, mcpHandler, deletion, deletionProfile, jobs, cancel, nil)
}

func newMuxWithJobControlAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication, cancel api.JobCancellationApplication, ready func(context.Context) error) http.Handler {
	mux, err := bootstrap.New(bootstrap.Config{Search: search, Read: read, MCP: mcpHandler, Deletion: deletion, DeletionProfile: deletionProfile, Jobs: jobs, Cancel: cancel, ReadyCheck: ready})
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "invalid bootstrap", http.StatusServiceUnavailable)
		})
	}
	return mux
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	settings, err := loadSearchSettings(os.LookupEnv)
	if err != nil {
		return err
	}
	var db *sql.DB
	var ready func(context.Context) error
	var search api.PartnerSearchApplication
	if raw := strings.TrimSpace(os.Getenv("MEMORYS_DATABASE_URL")); raw != "" {
		db, err = sql.Open("pgx", raw)
		if err != nil {
			return errors.New("database configuration rejected")
		}
		defer db.Close()
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
		check, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = db.PingContext(check)
		cancel()
		if err != nil {
			return errors.New("database unavailable")
		}
		ready = func(c context.Context) error {
			c, cancel := context.WithTimeout(c, 2*time.Second)
			defer cancel()
			return db.PingContext(c)
		}
		log.Println("memorys-server database connected")
	}
	if settings.Enabled {
		search, err = assembleSearch(db, settings)
		if err != nil {
			return err
		}
		log.Println("memorys-server authenticated PostgreSQL keyword search configured (revision text only)")
	}
	server := &http.Server{Addr: "127.0.0.1:8080", Handler: newMuxWithServicesAndReady(search, nil, ready), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	stopped := make(chan struct{})
	shutdownDone := make(chan struct{})
	defer close(stopped)
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if server.Shutdown(shutdown) != nil {
				_ = server.Close()
			}
		case <-stopped:
		}
	}()
	log.Println("memorys-server listening on 127.0.0.1:8080")
	err = server.ListenAndServe()
	if ctx.Err() != nil {
		<-shutdownDone // Drain handlers before the deferred database pool close.
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errors.New("HTTP listener unavailable")
	}
	return nil
}

func assembleSearch(db *sql.DB, settings searchSettings) (*localruntime.Search, error) {
	return localruntime.NewSearchFromFiles(db, settings.Issuer, settings.Audience, settings.SubjectType, settings.JWKSFile, settings.ProfileFile, settings.MaxTokenLifetime)
}
