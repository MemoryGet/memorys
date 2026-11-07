package contracts

import (
	"context"
	"errors"
	"io"
)

var ErrDocumentUploadConflict = errors.New("document upload idempotency conflict")

type DocumentUploadInput struct {
	Credential      Credential
	Request         RequestContext
	KnowledgeBaseID ID
	Title           string
	Size            int64
	MediaType       string
	ExpectedSHA256  Digest
	IdempotencyKey  string
}

type DocumentUploadResult struct {
	Document Document
	Draft    Draft
	Job      Job
	Blob     BlobRef
}

type DocumentUploadApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	UploadDocument(context.Context, DocumentUploadInput, io.Reader) (DocumentUploadResult, error)
}
