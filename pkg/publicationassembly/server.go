// Package publicationassembly owns host configuration for the publication
// preparation worker. It keeps credentials and concrete queue wiring out of
// the HTTP and domain packages.
package publicationassembly

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/localruntime"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

// Build creates the optional PostgreSQL publication preparation service. The
// worker is enabled only with an explicit flag and a private service token.
func Build(ctx context.Context, native *localruntime.Search) (*preparation.Service, error) {
	if native == nil || ctx == nil || strings.TrimSpace(os.Getenv("MEMORYS_PUBLICATION_ENABLED")) != "1" {
		return nil, nil
	}
	space := contracts.ID(strings.TrimSpace(os.Getenv("MEMORYS_PUBLICATION_SPACE_ID")))
	kb := contracts.ID(strings.TrimSpace(os.Getenv("MEMORYS_PUBLICATION_KB_ID")))
	workerID := contracts.ID(strings.TrimSpace(os.Getenv("MEMORYS_PUBLICATION_WORKER_ID")))
	token := strings.TrimSpace(os.Getenv("MEMORYS_PUBLICATION_WORKER_TOKEN"))
	if space == "" || kb == "" || workerID == "" || token == "" {
		return nil, errors.New("publication worker configuration rejected")
	}
	return native.NewPublicationWorker(ctx, contracts.Credential{Kind: "access_token", Secret: []byte(token)}, space, kb, workerID, 500*time.Millisecond, nil)
}
