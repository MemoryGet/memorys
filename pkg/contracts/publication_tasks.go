package contracts

import "context"

type PublicationTaskControlRequest struct {
	Request            RequestContext
	Scope              AccessScope
	JobID              ID
	ExpectedJobVersion uint64
}

type PublicationTaskControl interface {
	CancelPublicationTask(context.Context, PublicationTaskControlRequest) (ResourceJob, error)
	PausePublicationTask(context.Context, PublicationTaskControlRequest) (ResourceJob, error)
}

type PublicationTaskRetryRequest struct {
	Request            RequestContext
	Scope              AccessScope
	Principal          Principal
	JobID              ID
	ExpectedJobVersion uint64
	IdempotencyKey     string
}

type PublicationTaskRetryResult struct {
	PreviousJobID ID
	Job           ResourceJob
}

type PublicationTaskRetrier interface {
	RetryPublicationTask(context.Context, PublicationTaskRetryRequest) (PublicationTaskRetryResult, error)
}
