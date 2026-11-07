package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type JobPauseApplication interface {
	PauseJob(context.Context, document.JobCancellationInput) (contracts.ResourceJob, error)
}

// JobPauseHandler reuses the strict authentication, idempotency and CAS wire
// validation of JobCancellationHandler while mapping pause-specific service
// errors to the same safe transport classes.
type JobPauseHandler struct{ Service JobPauseApplication }

func (h JobPauseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Service == nil {
		JobCancellationHandler{}.ServeHTTP(w, r)
		return
	}
	JobCancellationHandler{Service: pauseAdapter{service: h.Service}}.ServeHTTP(w, r)
}

type pauseAdapter struct{ service JobPauseApplication }

func (a pauseAdapter) CancelJob(ctx context.Context, in document.JobCancellationInput) (contracts.ResourceJob, error) {
	result, err := a.service.PauseJob(ctx, in)
	if errors.Is(err, document.ErrJobPauseUnauthenticated) {
		return result, document.ErrJobCancellationUnauthenticated
	}
	if errors.Is(err, document.ErrJobPauseDenied) {
		return result, document.ErrJobCancellationDenied
	}
	if errors.Is(err, document.ErrJobPause) {
		return result, document.ErrJobCancellation
	}
	return result, err
}
