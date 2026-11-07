package contracts

import (
	"context"
	"io"
)

type DraftSourceInput struct {
	Credential      Credential
	Request         RequestContext
	DraftID         ID
	ExpectedVersion uint64
	ExpectedSHA256  Digest
	Size            int64
	MediaType       string
	IdempotencyKey  string
}

type DraftSourceApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ReplaceDraftSource(context.Context, DraftSourceInput, io.Reader) (DraftParseResult, error)
}
