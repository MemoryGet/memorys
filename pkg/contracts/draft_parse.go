package contracts

import "context"

type DraftParseInput struct {
	Credential             Credential
	Request                RequestContext
	DraftID                ID
	ExpectedVersion        uint64
	PipelineProfileID      ID
	PipelineProfileVersion uint64
	IdempotencyKey         string
}

type DraftParseResult struct {
	Job        Job
	DocumentID ID
}

type DraftParseApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ParseDraft(context.Context, DraftParseInput) (DraftParseResult, error)
}
