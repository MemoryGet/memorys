package contracts

// These values describe platform snapshots, not vendor runtime objects.
// Implementations must freeze/copy maps and slices when publishing a snapshot.
type ModuleBinding struct {
	InstanceID       ID
	Capability       Capability
	ContractVersion  string
	RequiredFeatures []string
}

type ProfileReference struct {
	ID      ID
	Version uint64
}

type IndexRoute struct {
	ID       ID
	Channels []Channel
	Binding  ModuleBinding
}

type RetrievalRoute struct {
	ID             ID
	Channels       []Channel
	Binding        ModuleBinding
	CandidateLimit int
	Required       bool
}

type IndexPolicy struct {
	SchemaVersion           uint64
	Chunking                ChunkPolicy
	EmbeddingSpec           EmbeddingSpec
	RequiredChannels        []Channel
	ProjectionFormatVersion string
}

type IndexProfile struct {
	ID       ID
	Version  uint64
	Mode     string
	Bindings map[Capability]ModuleBinding
	Routes   []IndexRoute
	Policy   IndexPolicy
	Digest   Digest
}

type QueryPolicy struct {
	SchemaVersion        uint64
	CandidateLimit       int
	AnswerEvidenceLimit  int
	FusionMethod         string
	RankConstant         int
	EnableRerank         bool
	EnableGeneration     bool
	MaxOutputTokens      int
	RetrievalTimeoutMS   int64
	RerankTimeoutMS      int64
	GenerationTimeoutMS  int64
	OptionalCapabilities []Capability
}

type ReflectionPolicy struct {
	Enabled        bool
	MaxExtraRounds int
	MaxPlanQueries int
	TotalBudgetMS  int64
}

type QueryProfile struct {
	ID         ID
	Version    uint64
	Bindings   map[Capability]ModuleBinding
	Routes     []RetrievalRoute
	Reflection ReflectionPolicy
	Policy     QueryPolicy
	Digest     Digest
}

type PipelineProfile struct {
	ID           ID
	Version      uint64
	IndexProfile ProfileReference
	QueryProfile ProfileReference
	Digest       Digest
}

type BackendBinding struct {
	PlatformResourceType string
	PlatformResourceID   ID
	ModuleInstanceID     ID
	BackendID            string // Adapter-private; must not appear in external citations.
	GenerationID         *ID
	Purpose              string
}

type GenerationSet struct {
	ID             ID
	RevisionID     ID
	BackendEpochID ID
	Channels       map[Channel]IndexGeneration
	State          string
}

type BackendEpoch struct {
	ID                     ID
	KnowledgeBaseID        ID
	Number                 uint64
	IndexProfile           ProfileReference
	ProfileDigest          Digest
	State                  string
	RevisionGenerationSets map[ID]ID
	CutoverWatermark       string
}

type QuerySnapshot struct {
	ScopeVersion            uint64
	KnowledgeBaseEpochs     map[ID]ID
	PublicationFilterDigest Digest
	GenerationSets          []GenerationSet
	QueryProfileDigest      Digest
}
