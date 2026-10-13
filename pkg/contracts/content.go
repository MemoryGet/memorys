package contracts

type BlockKind string

const (
	TextBlock  BlockKind = "text"
	TableBlock BlockKind = "table"
	ImageBlock BlockKind = "image"
	AudioBlock BlockKind = "audio"
)

type CharacterRange struct{ Start, End int }
type BoundingBox struct{ Left, Top, Right, Bottom float64 }

type Locator struct {
	Precision     string
	Page          *int
	Slide         *int
	Worksheet     string
	CellRange     string
	ParagraphPath []int
	Characters    *CharacterRange
	Box           *BoundingBox
}

type OriginalAnchor struct {
	BlobID  ID
	Locator Locator
}

type SourceAnchor struct {
	Kind            string
	BlobID          ID
	RevisionBlockID ID
	Locator         Locator
	DerivedFrom     []OriginalAnchor
}

type TableCell struct {
	Row        int
	Column     int
	RowSpan    int
	ColumnSpan int
	Text       string
	Role       string
	Formula    *string // Preserved text only; must never be executed.
	NumberText *string
	Source     *SourceAnchor
}

type TableContent struct {
	RowCount    int
	ColumnCount int
	Cells       []TableCell
	Caption     *string
}

type ImageContent struct {
	Blob    BlobRef
	Width   *int
	Height  *int
	Caption *string
	OCRText *string
}

type AudioSegment struct {
	StartMS      int64
	EndMS        int64
	Text         string
	SpeakerLabel *string
}

type AudioContent struct {
	Blob       BlobRef
	DurationMS int64
	Segments   []AudioSegment
}

// Content is a typed in-process union. Exactly one payload matches Kind;
// text blocks have no payload. Boundary validation is required, not implicit.
// It is deliberately not the JSON layout of StructuredContent in OpenAPI.
type Content struct {
	Kind   BlockKind
	Text   string
	Source SourceAnchor
	Table  *TableContent
	Image  *ImageContent
	Audio  *AudioContent
}

type ParsedBlock struct {
	ID      ID
	Order   int
	Content Content
}

type Block struct {
	ID         ID
	RevisionID ID
	Order      int
	Content    Content
}

type DraftBlock struct {
	ID         ID
	DraftID    ID
	RowVersion uint64
	Order      int
	Content    Content
}

type ParseOptions struct {
	Languages []string
	EnableOCR bool
}

type ParseRequest struct {
	Request RequestContext
	Scope   AccessScope
	DraftID ID
	Input   BlobRef
	Format  string
	Options ParseOptions
}

type Diagnostic struct {
	Code        string
	SafeMessage string
	Severity    string
}

type ParseResult struct {
	Blocks        []ParsedBlock
	Diagnostics   []Diagnostic
	ParserVersion string
}

type BlockSpan struct {
	BlockID    ID
	Characters *CharacterRange
}

type Chunk struct {
	ID            ID
	RevisionID    ID
	Spans         []BlockSpan
	Text          string
	ProfileDigest Digest
	ContentHash   Digest
}

type ChunkPolicy struct {
	Strategy            string
	MaxCodepoints       int
	OverlapCodepoints   int
	ParentMaxCodepoints *int
}

type SplitRequest struct {
	Request    RequestContext
	Scope      AccessScope
	RevisionID ID
	Blocks     []Block
	Policy     ChunkPolicy
}
