package contracts

import "context"

// ParseTaskControlRequest is issued only after the application has resolved
// and reauthorized the task's document/knowledge-base binding.
type ParseTaskControlRequest struct {
	Request            RequestContext
	Scope              AccessScope
	JobID              ID
	ExpectedJobVersion uint64
}

type ParseTaskControl interface {
	CancelParseTask(context.Context, ParseTaskControlRequest) (ResourceJob, error)
	PauseParseTask(context.Context, ParseTaskControlRequest) (ResourceJob, error)
	RetryParseTask(context.Context, ParseTaskControlRequest) (ResourceJob, error)
}
