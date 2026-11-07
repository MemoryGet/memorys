package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type DraftEditApplication = contracts.DraftEditApplication

type draftBlockEditDTO struct {
	ID                contracts.ID            `json:"id"`
	Kind              contracts.BlockKind     `json:"kind"`
	Text              string                  `json:"text"`
	Source            feedbackSourceAnchorDTO `json:"source_anchor"`
	DraftID           contracts.ID            `json:"draft_id"`
	RowVersion        uint64                  `json:"row_version"`
	StructuredContent json.RawMessage         `json:"structured_content"`
}

type editDraftBody struct {
	ExpectedVersion uint64              `json:"expected_version"`
	Blocks          []draftBlockEditDTO `json:"blocks"`
}

type draftBlobDTO struct {
	ID        contracts.ID     `json:"id"`
	SHA256    contracts.Digest `json:"sha256"`
	Size      int64            `json:"size"`
	MediaType string           `json:"media_type"`
}

type draftTableCellDTO struct {
	Row          int                      `json:"row"`
	Column       int                      `json:"column"`
	RowSpan      int                      `json:"row_span"`
	ColumnSpan   int                      `json:"column_span"`
	Text         string                   `json:"text"`
	Role         string                   `json:"role"`
	Formula      *string                  `json:"formula"`
	NumberText   *string                  `json:"number_text"`
	SourceAnchor *feedbackSourceAnchorDTO `json:"source_anchor"`
}

type draftTableDTO struct {
	Type        string              `json:"type"`
	RowCount    int                 `json:"row_count"`
	ColumnCount int                 `json:"column_count"`
	Cells       []draftTableCellDTO `json:"cells"`
	Caption     *string             `json:"caption"`
}

type draftImageDTO struct {
	Type    string       `json:"type"`
	Blob    draftBlobDTO `json:"blob"`
	Width   *int         `json:"width"`
	Height  *int         `json:"height"`
	Caption *string      `json:"caption"`
	OCRText *string      `json:"ocr_text"`
}

type draftAudioSegmentDTO struct {
	StartMS      int64   `json:"start_ms"`
	EndMS        int64   `json:"end_ms"`
	Text         string  `json:"text"`
	SpeakerLabel *string `json:"speaker_label"`
}

type draftAudioDTO struct {
	Type       string                 `json:"type"`
	Blob       draftBlobDTO           `json:"blob"`
	DurationMS int64                  `json:"duration_ms"`
	Segments   []draftAudioSegmentDTO `json:"segments"`
}

func (s feedbackSourceAnchorDTO) validShape() bool {
	return (s.Kind == "original" && s.BlobID != "" && s.RevisionBlockID == "") || (s.Kind == "revision" && s.BlobID == "" && s.RevisionBlockID != "")
}

func (s feedbackSourceAnchorDTO) contract() contracts.SourceAnchor {
	derived := make([]contracts.OriginalAnchor, 0, len(s.DerivedFrom))
	for _, item := range s.DerivedFrom {
		derived = append(derived, contracts.OriginalAnchor{BlobID: contracts.ID(item.BlobID), Locator: item.Locator.contract()})
	}
	return contracts.SourceAnchor{Kind: s.Kind, BlobID: contracts.ID(s.BlobID), RevisionBlockID: contracts.ID(s.RevisionBlockID), Locator: s.Locator.contract(), DerivedFrom: derived}
}

func strictJSON(raw []byte, value any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("trailing json")
	}
	return nil
}

func parseDraftContent(kind contracts.BlockKind, text string, source feedbackSourceAnchorDTO, raw json.RawMessage) (contracts.Content, error) {
	if !source.validShape() || source.Kind == "" || source.Locator.Precision == "" {
		return contracts.Content{}, errors.New("invalid source")
	}
	content := contracts.Content{Kind: kind, Text: text, Source: source.contract()}
	if kind == contracts.TextBlock {
		if raw != nil {
			return contracts.Content{}, errors.New("text content payload")
		}
		return content, nil
	}
	if len(raw) == 0 || string(raw) == "null" {
		return contracts.Content{}, errors.New("missing structured content")
	}
	switch kind {
	case contracts.TableBlock:
		var input draftTableDTO
		if strictJSON(raw, &input) != nil || input.Type != "table" {
			return contracts.Content{}, errors.New("invalid table")
		}
		table := &contracts.TableContent{RowCount: input.RowCount, ColumnCount: input.ColumnCount, Caption: input.Caption}
		for _, cell := range input.Cells {
			var sourceAnchor *contracts.SourceAnchor
			if cell.SourceAnchor != nil {
				if !cell.SourceAnchor.validShape() {
					return contracts.Content{}, errors.New("invalid table source")
				}
				value := cell.SourceAnchor.contract()
				sourceAnchor = &value
			}
			table.Cells = append(table.Cells, contracts.TableCell{Row: cell.Row, Column: cell.Column, RowSpan: cell.RowSpan, ColumnSpan: cell.ColumnSpan, Text: cell.Text, Role: cell.Role, Formula: cell.Formula, NumberText: cell.NumberText, Source: sourceAnchor})
		}
		content.Table = table
	case contracts.ImageBlock:
		var input draftImageDTO
		if strictJSON(raw, &input) != nil || input.Type != "image" {
			return contracts.Content{}, errors.New("invalid image")
		}
		content.Image = &contracts.ImageContent{Blob: contracts.BlobRef{ID: input.Blob.ID, SHA256: input.Blob.SHA256, Size: input.Blob.Size, MediaType: input.Blob.MediaType}, Width: input.Width, Height: input.Height, Caption: input.Caption, OCRText: input.OCRText}
	case contracts.AudioBlock:
		var input draftAudioDTO
		if strictJSON(raw, &input) != nil || input.Type != "audio" {
			return contracts.Content{}, errors.New("invalid audio")
		}
		content.Audio = &contracts.AudioContent{Blob: contracts.BlobRef{ID: input.Blob.ID, SHA256: input.Blob.SHA256, Size: input.Blob.Size, MediaType: input.Blob.MediaType}, DurationMS: input.DurationMS}
		for _, segment := range input.Segments {
			content.Audio.Segments = append(content.Audio.Segments, contracts.AudioSegment{StartMS: segment.StartMS, EndMS: segment.EndMS, Text: segment.Text, SpeakerLabel: segment.SpeakerLabel})
		}
	default:
		return contracts.Content{}, errors.New("invalid block kind")
	}
	return content, nil
}

func decodeDraftEdit(raw []byte, draftID contracts.ID) (contracts.DraftEditInput, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return contracts.DraftEditInput{}, errors.New("invalid body")
	}
	for key := range fields {
		if key != "expected_version" && key != "blocks" {
			return contracts.DraftEditInput{}, errors.New("unknown field")
		}
	}
	var body editDraftBody
	if strictJSON(raw, &body) != nil || body.ExpectedVersion == 0 || body.ExpectedVersion == math.MaxUint64 || body.Blocks == nil || len(body.Blocks) > 100000 {
		return contracts.DraftEditInput{}, errors.New("invalid body")
	}
	blocks := make([]contracts.DraftBlock, 0, len(body.Blocks))
	for index, item := range body.Blocks {
		if !partnerID(item.ID) || !partnerID(item.DraftID) || item.DraftID != draftID || item.RowVersion == 0 || item.Kind == "" || !utf8.ValidString(item.Text) {
			return contracts.DraftEditInput{}, errors.New("invalid block")
		}
		content, err := parseDraftContent(item.Kind, item.Text, item.Source, item.StructuredContent)
		if err != nil {
			return contracts.DraftEditInput{}, err
		}
		blocks = append(blocks, contracts.DraftBlock{ID: item.ID, DraftID: item.DraftID, RowVersion: item.RowVersion, Order: index, Content: content})
	}
	return contracts.DraftEditInput{DraftID: draftID, ExpectedVersion: body.ExpectedVersion, Blocks: blocks}, nil
}

type DraftEditHandler struct{ Service DraftEditApplication }

// DraftHandler multiplexes the stable draft resource path while keeping read
// and edit capabilities independently injectable at the composition root.
type DraftHandler struct {
	Read DraftReadApplication
	Edit DraftEditApplication
}

func (h DraftHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		(DraftReadHandler{Service: h.Read}).ServeHTTP(w, r)
		return
	}
	(DraftEditHandler{Service: h.Edit}).ServeHTTP(w, r)
}

func (h DraftEditHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := requestNonce(w)
	fail := func(status int, code, message string) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": message, "retryable": status == 503 || status == 504, "request_id": requestID})
	}
	if r.Method != http.MethodPatch {
		w.Header().Set("Allow", http.MethodPatch)
		fail(http.StatusMethodNotAllowed, "invalid_argument", "PATCH required")
		return
	}
	if h.Service == nil {
		fail(http.StatusServiceUnavailable, "capability_missing", "Draft edits not configured")
		return
	}
	draftID := contracts.ID(r.PathValue("draft_id"))
	if !partnerID(draftID) || r.URL.RawQuery != "" {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid draft ID")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		fail(http.StatusUnsupportedMediaType, "invalid_argument", "JSON required")
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || len(headers[0]) > 16400 {
		fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
		return
	}
	scheme, token, ok := strings.Cut(headers[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || len(token) > 16384 || strings.ContainsAny(token, " \t\r\n,") {
		fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || len(keys[0]) == 0 || len(keys[0]) > 200 || strings.ContainsAny(keys[0], "\r\n") || !utf8.ValidString(keys[0]) {
		fail(http.StatusBadRequest, "invalid_argument", "Idempotency-Key required")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (8<<20)+1))
	if err != nil || len(raw) == 0 || len(raw) > 8<<20 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid draft edit")
		return
	}
	if _, err := canonical.JSON(raw); err != nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid draft edit")
		return
	}
	input, err := decodeDraftEdit(raw, draftID)
	if err != nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid draft edit")
		return
	}
	version, digest, err := h.Service.ProfileIdentity()
	if err != nil || version == 0 || len(digest) != 64 {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Draft configuration unavailable")
		return
	}
	decoded, err := hex.DecodeString(string(digest))
	if err != nil || len(decoded) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Draft configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	input.Credential = credential
	input.IdempotencyKey = keys[0]
	input.Request = contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}
	draft, err := h.Service.EditDraft(ctx, input)
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Draft edit deadline exceeded")
		return
	}
	if errors.Is(err, contracts.ErrDraftEditConflict) {
		fail(http.StatusConflict, "conflict", "Draft changed; reload before editing")
		return
	}
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Draft edit unavailable")
		return
	}
	body, err := projectDraft(draft)
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Draft result unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
