package postgres

import (
	"time"

	"github.com/MemoryGet/memorys/pkg/preparation"
)

// NewPublicationService composes PostgreSQL queued-job discovery with the
// generic preparation worker lifecycle. The caller still supplies the actual
// preparation Runner, including its task store and backend writer.
func NewPublicationService(runner preparation.Runner, queue PublicationQueue, interval time.Duration, onError func(error)) (*preparation.Service, error) {
	return preparation.NewService(runner, queue.Next, interval, onError)
}
