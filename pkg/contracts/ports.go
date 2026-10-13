package contracts

import (
	"context"
	"io"
)

type Authenticator interface {
	Authenticate(context.Context, Credential) (Principal, error)
}

type Authorizer interface {
	Authorize(context.Context, AuthorizeRequest) (AccessScope, error)
}

type BlobStore interface {
	Put(context.Context, BlobWriteRequest, io.Reader) (BlobRef, error)
	Open(context.Context, BlobReadRequest) (io.ReadCloser, error)
	Delete(context.Context, BlobReadRequest) error
}

type Parser interface {
	Parse(context.Context, ParseRequest) (ParseResult, error)
}

type Splitter interface {
	Split(context.Context, SplitRequest) ([]Chunk, error)
}

type Embedder interface {
	Embed(context.Context, EmbedRequest) (EmbedResult, error)
}

// IndexWriter prepares isolated attempt output; proof is not publication.
// Verify must test visibility/filters, not merely that a write was accepted.
type IndexWriter interface {
	Prepare(context.Context, IndexPrepareRequest) (ReadinessProof, error)
	Verify(context.Context, ReadinessProof) error
	Delete(context.Context, IndexDeleteRequest) error
}

// Retriever must enforce every supplied scope and publication filter before
// candidate selection. Its results are rechecked by the application service.
type Retriever interface {
	Search(context.Context, RetrieveRequest) ([]Candidate, error)
}

// CompositeEngine owns projection ingestion, internal embedding and retrieval
// as one replacement unit. It never owns authoritative content or publication.
// Query vectors are optional only when the verified backend embeds Query.Text.
type CompositeEngine interface {
	PrepareProjection(context.Context, CompositePrepareRequest) (ReadinessProof, error)
	Verify(context.Context, ReadinessProof) error
	Delete(context.Context, IndexDeleteRequest) error
	Search(context.Context, RetrieveRequest) ([]Candidate, error)
}

type Fuser interface {
	Fuse(context.Context, FuseRequest) ([]Candidate, error)
}

type Reranker interface {
	Rerank(context.Context, RerankRequest) ([]Evidence, error)
}
