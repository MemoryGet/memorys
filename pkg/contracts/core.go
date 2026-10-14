// Package contracts defines provider-neutral, in-process architecture contracts.
// It is not an implementation of authentication, publication or retrieval.
// Wire serialization follows docs/architecture/openapi and is a transport concern.
package contracts

import "time"

type ID string
type Digest string
type Capability string
type Action string

const (
	Authentication         Capability = "authenticator"
	Authorization          Capability = "authorizer"
	BlobStorage            Capability = "blob_store"
	Parsing                Capability = "parser"
	Splitting              Capability = "splitter"
	Embedding              Capability = "embedder"
	IndexWriting           Capability = "index_writer"
	Retrieval              Capability = "retriever"
	CompositeRetrieval     Capability = "composite_ingest_search"
	Fusion                 Capability = "fuser"
	Reranking              Capability = "reranker"
	Generation             Capability = "generator"
	Ingestion              Capability = "ingestor"
	Publication            Capability = "publication"
	Reflection             Capability = "reflection"
	MemoryStorage          Capability = "memory"
	GraphStorage           Capability = "graph"
	GraphProjectionControl Capability = "graph_projection_manager"
	Metadata               Capability = "metadata_repository"
	Transactions           Capability = "metadata_uow"
	QueryReading           Capability = "query_read_unit"
	TaskFacts              Capability = "task_repository"
	TaskDelivery           Capability = "task_queue"
	Events                 Capability = "event_bus"
	Audit                  Capability = "audit"
	Evaluation             Capability = "evaluator"
	Observability          Capability = "observer"
)

type Code string

type PublicError struct {
	Code        Code
	SafeMessage string
	Retryable   bool
	RequestID   ID
}

func (e PublicError) Error() string { return e.SafeMessage }

type RequestContext struct {
	RequestID     ID
	ProfileDigest Digest
	Deadline      time.Time
}

type Principal struct {
	SubjectID   ID
	SubjectType string
	Issuer      string
	ClientID    ID
	Delegation  *Delegation
}

type Delegation struct {
	UserID         ID
	ClientID       ID
	ProofReference string
}

// AccessScope is authorization data, not proof of authorization by itself.
// Applications and remote adapters must validate its issuer/current policy.
// An empty KnowledgeBaseIDs slice must never mean unrestricted access.
type AccessScope struct {
	ScopeID          ID
	SubjectID        ID
	Action           Action
	SpaceID          ID
	KnowledgeBaseIDs []ID
	PolicyVersion    uint64
	ExpiresAt        time.Time
}

type Credential struct {
	Kind   string
	Secret []byte // Ephemeral; never include in configuration snapshots or logs.
}

type AuthorizeRequest struct {
	Principal        Principal
	Action           Action
	SpaceID          ID
	KnowledgeBaseIDs []ID
	ResourceIDs      []ID
}

type BlobRef struct {
	ID        ID
	SHA256    Digest
	Size      int64
	MediaType string
}

type BlobWriteRequest struct {
	Request        RequestContext
	Scope          AccessScope
	ExpectedSHA256 Digest
	Size           int64
	MediaType      string
	IdempotencyKey string
}

type BlobReadRequest struct {
	Request RequestContext
	Scope   AccessScope
	Blob    BlobRef
}
