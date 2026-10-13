// Package modulewire maps platform values to remote module DTOs.
// A valid DTO is not an authenticated invocation or a publication permission.
package modulewire

import (
	"reflect"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type Call struct{ InvocationID, ModuleInstanceID contracts.ID }
type wireContext struct {
	RequestID        contracts.ID
	InvocationID     contracts.ID
	ModuleInstanceID contracts.ID
	ContractVersion  string
	Deadline         time.Time
	ProfileDigest    contracts.Digest
	Scope            contracts.AccessScope
}
type parseInput struct {
	DraftID contracts.ID
	Input   contracts.BlobRef
	Format  string
	Options contracts.ParseOptions
}
type parseInvocation struct {
	Context wireContext
	Input   parseInput
}
type parseResponse struct {
	RequestID    contracts.ID
	InvocationID contracts.ID
	Result       contracts.ParseResult
}

type field struct {
	goName, wireName string
	optional         bool
}

var mappings = map[reflect.Type][]field{}

func register(value any, specification string) {
	var fields []field
	for _, item := range strings.Fields(specification) {
		parts := strings.Split(item, ":")
		fields = append(fields, field{parts[0], strings.TrimSuffix(parts[1], "?"), strings.HasSuffix(parts[1], "?")})
	}
	mappings[reflect.TypeOf(value)] = fields
}

func init() {
	register(wireContext{}, "RequestID:request_id InvocationID:invocation_id ModuleInstanceID:module_instance_id ContractVersion:contract_version Deadline:deadline ProfileDigest:profile_digest Scope:scope")
	register(parseInvocation{}, "Context:context Input:input")
	register(parseInput{}, "DraftID:draft_id Input:input Format:format Options:options")
	register(parseResponse{}, "RequestID:request_id InvocationID:invocation_id Result:result")
	register(contracts.AccessScope{}, "ScopeID:scope_id SubjectID:subject_id Action:action SpaceID:space_id KnowledgeBaseIDs:kb_ids PolicyVersion:policy_version ExpiresAt:expires_at")
	register(contracts.BlobRef{}, "ID:id SHA256:sha256 Size:size MediaType:media_type")
	register(contracts.ParseOptions{}, "Languages:languages EnableOCR:enable_ocr")
	register(contracts.ParseResult{}, "Blocks:blocks Diagnostics:diagnostics ParserVersion:parser_version")
	register(contracts.Diagnostic{}, "Code:code SafeMessage:safe_message Severity:severity")
	register(contracts.ParsedBlock{}, "ID:id Order:order Content:content")
	register(contracts.Content{}, "Kind:kind Text:text Source:source_anchor Table:table? Image:image? Audio:audio?")
	register(contracts.SourceAnchor{}, "Kind:kind BlobID:blob_id? RevisionBlockID:revision_block_id? Locator:locator DerivedFrom:derived_from?")
	register(contracts.OriginalAnchor{}, "BlobID:blob_id Locator:locator")
	register(contracts.Locator{}, "Precision:precision Page:page? Slide:slide? Worksheet:worksheet? CellRange:cell_range? ParagraphPath:paragraph_path? Characters:characters? Box:box?")
	register(contracts.CharacterRange{}, "Start:start End:end")
	register(contracts.BoundingBox{}, "Left:left Top:top Right:right Bottom:bottom")
	register(contracts.TableContent{}, "RowCount:row_count ColumnCount:column_count Cells:cells Caption:caption?")
	register(contracts.TableCell{}, "Row:row Column:column RowSpan:row_span ColumnSpan:column_span Text:text Role:role Formula:formula? NumberText:number_text? Source:source_anchor?")
	register(contracts.ImageContent{}, "Blob:blob Width:width? Height:height? Caption:caption? OCRText:ocr_text?")
	register(contracts.AudioContent{}, "Blob:blob DurationMS:duration_ms Segments:segments")
	register(contracts.AudioSegment{}, "StartMS:start_ms EndMS:end_ms Text:text SpeakerLabel:speaker_label?")
}
