package contracts

import (
	"context"
	"time"
)

type ExecutionBudget struct {
	Deadline        time.Time
	MaxOutputTokens int
	MaxExtraRounds  int
}

type GenerateRequest struct {
	Request  RequestContext
	Scope    AccessScope
	Question string
	Evidence []Evidence
	Budget   ExecutionBudget
}

type AnswerResult struct {
	RequestID      ID
	Answer         string
	Citations      []Citation
	Status         string
	ProfileVersion uint64
	ProfileDigest  Digest
	TimingsMS      map[string]float64
	Degraded       []Degradation
}

type Degradation struct {
	Capability Capability
	Reason     string
}

// In-process union; the transport maps this to SSE or a final MCP result.
type AnswerEvent struct {
	Kind     string
	Text     string
	Citation *Citation
	Result   *AnswerResult
	Error    *PublicError
}

type AnswerSink func(AnswerEvent) error

type Generator interface {
	Generate(context.Context, GenerateRequest, AnswerSink) error
}

type ReflectionRequest struct {
	Request  RequestContext
	Scope    AccessScope
	Question string
	Evidence []Evidence
	Answer   *AnswerResult
	Budget   ExecutionBudget
}

type Assessment struct {
	Sufficient bool
	Reasons    []Diagnostic
}

type PlannedQuery struct {
	Text     string
	RouteIDs []ID
}

type QueryPlan struct{ Queries []PlannedQuery }

type DraftProposal struct {
	ID             ID
	DocumentID     ID
	BaseRevisionID *ID
	Blocks         []ParsedBlock
	Evidence       []Citation
	Reason         string
}

// Suggestions do not mutate authoritative content or grant additional scope.
type Reflector interface {
	Assess(context.Context, ReflectionRequest) (Assessment, error)
	Refine(context.Context, ReflectionRequest) (QueryPlan, error)
	Propose(context.Context, ReflectionRequest) (DraftProposal, error)
}

type MemoryProvenance struct {
	Kind       string
	TaskID     *ID
	FeedbackID *ID
	Citations  []Citation
}

type MemoryRecord struct {
	ID              ID
	OwnerID         ID
	SpaceID         ID
	KnowledgeBaseID *ID
	TaskID          *ID
	Content         string
	Provenance      MemoryProvenance
	CreatedAt       time.Time
	ExpiresAt       *time.Time
	Version         uint64
}

type MemoryWriteRequest struct {
	Command         CommandContext
	Record          MemoryRecord
	ExpectedVersion uint64
}

type MemorySearchRequest struct {
	Request RequestContext
	Scope   AccessScope
	OwnerID ID
	TaskID  *ID
	Query   string
	Limit   int
}

type MemoryDeleteRequest struct {
	Command         CommandContext
	RecordID        ID
	ExpectedVersion uint64
}

type Memory interface {
	Save(context.Context, MemoryWriteRequest) (MemoryRecord, error)
	Search(context.Context, MemorySearchRequest) ([]MemoryRecord, error)
	Update(context.Context, MemoryWriteRequest) (MemoryRecord, error)
	Delete(context.Context, MemoryDeleteRequest) error
}

type Entity struct {
	ID       ID
	Kind     string
	Name     string
	Evidence []Citation
}

type Relation struct {
	ID       ID
	FromID   ID
	ToID     ID
	Kind     string
	Evidence []Citation
}

type GraphResult struct {
	Entities  []Entity
	Relations []Relation
	Evidence  []Evidence
}

type GraphExtractRequest struct {
	Request       RequestContext
	Scope         AccessScope
	Evidence      []Evidence
	ProfileDigest Digest
}

type GraphQueryRequest struct {
	Request     RequestContext
	Scope       AccessScope
	Publication PublicationFilter
	Query       string
	Limit       int
}

type Graph interface {
	Extract(context.Context, GraphExtractRequest) (GraphResult, error)
	Query(context.Context, GraphQueryRequest) (GraphResult, error)
}
