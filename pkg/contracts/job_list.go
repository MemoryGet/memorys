package contracts

import (
	"context"
	"time"
)

type JobListInput struct {
	Credential    Credential
	Request       RequestContext
	Kind          string
	State         string
	DocumentID    ID
	CreatedAfter  time.Time
	CreatedBefore time.Time
	PageSize      int
	Cursor        string
}

type JobListItem struct {
	Kind string
	ResourceJob
}

type JobPage struct {
	Items      []JobListItem
	NextCursor string
}

type JobListApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ListJobs(context.Context, JobListInput) (JobPage, error)
}
