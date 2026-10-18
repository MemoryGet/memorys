package contracts

import (
	"context"
	"errors"
)

var ErrJobReadDenied = errors.New("job unavailable or hidden")

type ResourceJob struct {
	ResourceID      ID
	KnowledgeBaseID ID // internal binding used for management authorization; never projected on the wire
	Job             Job
}

// Principal is supplied by the authenticated application, never an HTTP field.
type JobReadRequest struct {
	Request   RequestContext
	Principal Principal
	JobID     ID
}

// Narrow read port; it is not the full TaskRepository or a queue consumer.
type ResourceJobReader interface {
	ReadResourceJob(context.Context, JobReadRequest) (ResourceJob, error)
}
