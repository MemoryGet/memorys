package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type PartnerFeedbackInput struct {
	Credential      contracts.Credential
	Request         contracts.RequestContext
	SpaceID         contracts.ID
	Citation        *contracts.Citation
	TargetRequestID *contracts.ID
	Kind            string
	Comment         string
	IdempotencyKey  string
}

type PartnerFeedbackApplication interface {
	SubmitFeedback(context.Context, PartnerFeedbackInput) (contracts.FeedbackResult, error)
}

type feedbackCitationDTO struct {
	DocumentID string                  `json:"document_id"`
	RevisionID string                  `json:"revision_id"`
	BlockID    string                  `json:"block_id"`
	Source     feedbackSourceAnchorDTO `json:"source_anchor"`
}

type feedbackSourceAnchorDTO struct {
	Kind            string                `json:"kind"`
	BlobID          string                `json:"blob_id"`
	RevisionBlockID string                `json:"revision_block_id"`
	Locator         feedbackLocatorDTO    `json:"locator"`
	DerivedFrom     []feedbackOriginalDTO `json:"derived_from"`
}

type feedbackOriginalDTO struct {
	BlobID  string             `json:"blob_id"`
	Locator feedbackLocatorDTO `json:"locator"`
}

type feedbackLocatorDTO struct {
	Precision     string    `json:"precision"`
	Page          *int      `json:"page"`
	Slide         *int      `json:"slide"`
	StartMS       *int64    `json:"start_ms"`
	EndMS         *int64    `json:"end_ms"`
	Confidence    *float64  `json:"confidence"`
	Worksheet     string    `json:"worksheet"`
	CellRange     string    `json:"cell_range"`
	ParagraphPath []int     `json:"paragraph_path"`
	CharStart     *int      `json:"char_start"`
	CharEnd       *int      `json:"char_end"`
	BBox          []float64 `json:"bbox"`
}

func (c feedbackCitationDTO) contract() contracts.Citation {
	locator := c.Source.Locator.contract()
	derived := make([]contracts.OriginalAnchor, 0, len(c.Source.DerivedFrom))
	for _, item := range c.Source.DerivedFrom {
		derived = append(derived, contracts.OriginalAnchor{BlobID: contracts.ID(item.BlobID), Locator: item.Locator.contract()})
	}
	return contracts.Citation{DocumentID: contracts.ID(c.DocumentID), RevisionID: contracts.ID(c.RevisionID), BlockID: contracts.ID(c.BlockID), Source: contracts.SourceAnchor{Kind: c.Source.Kind, BlobID: contracts.ID(c.Source.BlobID), RevisionBlockID: contracts.ID(c.Source.RevisionBlockID), Locator: locator, DerivedFrom: derived}}
}

func (l feedbackLocatorDTO) contract() contracts.Locator {
	var characters *contracts.CharacterRange
	if l.CharStart != nil && l.CharEnd != nil {
		characters = &contracts.CharacterRange{Start: *l.CharStart, End: *l.CharEnd}
	}
	var box *contracts.BoundingBox
	if len(l.BBox) == 4 {
		box = &contracts.BoundingBox{Left: l.BBox[0], Top: l.BBox[1], Right: l.BBox[2], Bottom: l.BBox[3]}
	}
	return contracts.Locator{Precision: l.Precision, Page: l.Page, Slide: l.Slide, StartMS: l.StartMS, EndMS: l.EndMS, Confidence: l.Confidence, Worksheet: l.Worksheet, CellRange: l.CellRange, ParagraphPath: append([]int(nil), l.ParagraphPath...), Characters: characters, Box: box}
}

func (c feedbackCitationDTO) validShape() bool {
	l := c.Source.Locator
	return (l.CharStart == nil) == (l.CharEnd == nil) && (l.StartMS == nil) == (l.EndMS == nil) && (l.StartMS == nil || *l.StartMS >= 0 && *l.EndMS >= *l.StartMS) && (l.Confidence == nil || *l.Confidence >= 0 && *l.Confidence <= 100) && (len(l.BBox) == 0 || len(l.BBox) == 4)
}

type PartnerFeedbackHandler struct{ Service PartnerFeedbackApplication }

func (h PartnerFeedbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := requestNonce(w)
	fail := func(status int, code, message string) {
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": message, "retryable": status == 503 || status == 504, "request_id": requestID})
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(405, "invalid_argument", "POST required")
		return
	}
	if h.Service == nil {
		fail(503, "capability_missing", "Feedback not configured")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		fail(415, "invalid_argument", "JSON required")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || len(raw) == 0 {
		fail(400, "invalid_argument", "Invalid feedback request")
		return
	}
	if _, err = canonical.JSON(raw); err != nil {
		fail(400, "invalid_argument", "Invalid feedback request")
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		fail(400, "invalid_argument", "Invalid feedback request")
		return
	}
	for key := range fields {
		switch key {
		case "space_id", "citation", "target_request_id", "kind", "comment", "idempotency_key":
		default:
			fail(400, "invalid_argument", "Invalid feedback request")
			return
		}
	}
	var input struct {
		SpaceID         contracts.ID         `json:"space_id"`
		Citation        *feedbackCitationDTO `json:"citation"`
		TargetRequestID *contracts.ID        `json:"target_request_id"`
		Kind            string               `json:"kind"`
		Comment         string               `json:"comment"`
		IdempotencyKey  string               `json:"idempotency_key"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || !partnerID(input.SpaceID) || !validFeedbackKind(input.Kind) || !validFeedbackComment(input.Comment) || !partnerID(contracts.ID(input.IdempotencyKey)) {
		fail(400, "invalid_argument", "Invalid feedback request")
		return
	}
	var citation *contracts.Citation
	if input.Citation != nil {
		if !input.Citation.validShape() {
			fail(400, "invalid_argument", "Exactly one valid feedback target is required")
			return
		}
		value := input.Citation.contract()
		citation = &value
	}
	if (citation == nil) == (input.TargetRequestID == nil) || input.TargetRequestID != nil && !partnerID(*input.TargetRequestID) || citation != nil && !validPartnerCitation(*citation) {
		fail(400, "invalid_argument", "Exactly one valid feedback target is required")
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || len(headers[0]) > 16400 {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	scheme, token, ok := strings.Cut(headers[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || len(token) > 16384 || strings.ContainsAny(token, " \t\r\n,") {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	result, err := h.Service.SubmitFeedback(ctx, PartnerFeedbackInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, Deadline: deadline}, SpaceID: input.SpaceID, Citation: citation, TargetRequestID: input.TargetRequestID, Kind: input.Kind, Comment: input.Comment, IdempotencyKey: input.IdempotencyKey})
	if ctx.Err() != nil {
		fail(504, "deadline_exceeded", "Feedback deadline exceeded")
		return
	}
	if errors.Is(err, ErrPartnerFeedbackUnauthenticated) {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	if errors.Is(err, ErrPartnerFeedbackConflict) {
		fail(409, "conflict", "Feedback idempotency conflict")
		return
	}
	if err != nil || result.FeedbackID == "" || result.RequestID != "" && result.RequestID != requestID {
		fail(503, "dependency_unavailable", "Feedback unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	body := map[string]any{"request_id": requestID, "feedback_id": result.FeedbackID, "state": result.State}
	if result.DraftID != nil {
		body["draft_id"] = *result.DraftID
	}
	_ = json.NewEncoder(w).Encode(body)
}

var (
	ErrPartnerFeedbackUnauthenticated = errors.New("feedback authentication required")
	ErrPartnerFeedbackConflict        = errors.New("feedback idempotency conflict")
)

func requestNonce(w http.ResponseWriter) contracts.ID {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "request-unavailable"
	}
	id := contracts.ID(hex.EncodeToString(nonce[:]))
	w.Header().Set("X-Request-ID", string(id))
	return id
}

func validFeedbackKind(value string) bool {
	switch value {
	case "incorrect", "missing", "outdated", "helpful", "other":
		return true
	default:
		return false
	}
}

func validFeedbackComment(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && len(value) <= 10000
}

func validPartnerCitation(c contracts.Citation) bool {
	if !partnerID(c.DocumentID) || !partnerID(c.RevisionID) || !partnerID(c.BlockID) || (c.Source.Kind != "revision" && c.Source.Kind != "original") || c.Source.Locator.Precision == "" {
		return false
	}
	if c.Source.Kind == "revision" {
		return c.Source.RevisionBlockID == c.BlockID && c.Source.BlobID == ""
	}
	return c.Source.BlobID != "" && c.Source.RevisionBlockID == ""
}
