package contracts

type Channel string

const (
	KeywordChannel Channel = "keyword"
	VectorChannel  Channel = "vector"
	GraphChannel   Channel = "graph"
)

type EmbeddingSpec struct {
	ProviderModel     string
	ModelRevision     string
	Dimension         int
	Distance          string
	Normalization     string
	PreprocessVersion string
}

type Vector []float32

type EmbedRequest struct {
	Request RequestContext
	Texts   []string
	Spec    EmbeddingSpec
}

type EmbedResult struct {
	Vectors []Vector
	Spec    EmbeddingSpec
}

type IndexGeneration struct {
	ID              ID
	KnowledgeBaseID ID
	RevisionID      ID
	Channel         Channel
	ProfileDigest   Digest
	EmbeddingSpec   *EmbeddingSpec
	BackendEpochID  ID
	State           string
	BackendBinding  *BackendBinding
	Readiness       *ReadinessProof
}

type PublicationFilter struct {
	SpaceID          ID
	KnowledgeBaseIDs []ID
	RevisionIDs      []ID
	GenerationIDs    []ID
	SnapshotDigest   Digest
}

type Query struct {
	Text          string
	Vector        Vector
	EmbeddingSpec *EmbeddingSpec
}

type RetrieveRequest struct {
	Request        RequestContext
	Query          Query
	Scope          AccessScope
	Publication    PublicationFilter
	Generations    []IndexGeneration
	BackendEpochID ID
	RouteID        ID
	Channels       []Channel
	Limit          int
}

type Citation struct {
	DocumentID ID
	RevisionID ID
	BlockID    ID
	Source     SourceAnchor
}

type Candidate struct {
	Citation        Citation
	ChunkID         ID
	RouteID         ID
	RetrieverID     ID
	KnowledgeBaseID ID
	Rank            int
	Score           float64
	ScoreKind       string
	GenerationIDs   []ID
	Text            string
	FusionOrigins   []FusionOrigin
}

// FusionOrigin records one route's contribution, independent of vendor score.
type FusionOrigin struct {
	RouteID       ID
	RetrieverID   ID
	Rank          int
	GenerationIDs []ID
}

type Evidence struct {
	Citation      Citation
	SourceExcerpt string
	Rank          int
}

type RankedList struct {
	RouteID    ID
	Candidates []Candidate
}

type FuseRequest struct {
	Request      RequestContext
	Lists        []RankedList
	RankConstant int
	Limit        int
}

type RerankRequest struct {
	Request    RequestContext
	Query      string
	Candidates []Candidate
	Limit      int
}

type IndexPrepareRequest struct {
	Request         RequestContext
	Scope           AccessScope
	GenerationSetID ID
	Generations     []IndexGeneration
	OperationID     ID
	AttemptID       ID
	FencingToken    uint64
	Chunks          []Chunk
	Vectors         []Vector
}

// Chunks are approved platform projections with stable source spans. Provider
// internal re-chunking must retain a verified mapping back to those spans.
// Backend embedding specifications still have to match target Generations.
type CompositePrepareRequest struct {
	Request                 RequestContext
	Scope                   AccessScope
	GenerationSetID         ID
	Generations             []IndexGeneration
	OperationID             ID
	AttemptID               ID
	FencingToken            uint64
	Chunks                  []Chunk
	ProjectionFormatVersion string
}

type ReadinessProof struct {
	GenerationSetID ID
	AttemptID       ID
	ContentDigest   Digest
	ProfileDigest   Digest
	Generations     []GenerationReadiness
}

type GenerationReadiness struct {
	GenerationID        ID
	Channel             Channel
	BackendEpochID      ID
	ChunkCount          int
	EmbeddingSpecDigest *Digest
}

type IndexDeleteRequest struct {
	Request      RequestContext
	Scope        AccessScope
	GenerationID ID
	ChunkIDs     []ID
}
