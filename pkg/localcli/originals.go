package localcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/localruntime"
	"github.com/MemoryGet/memorys/pkg/parser"
	"github.com/MemoryGet/memorys/pkg/storage"
)

var mediaFormats = map[string]string{"pdf": "application/pdf", "docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation", "png": "image/png", "jpg": "image/jpeg", "wav": "audio/wav", "mp3": "audio/mpeg", "m4a": "audio/mp4"}

func canonicalOriginalFormat(format string) string {
	if format == "jpeg" {
		return "jpg"
	}
	return format
}

func supportedOriginalFormat(format string) bool {
	_, ok := mediaFormats[canonicalOriginalFormat(format)]
	return ok
}

func isOCRFormat(format string) bool {
	return format == "png" || format == "jpg" || format == "jpeg"
}

func isAudioFormat(format string) bool {
	return format == "wav" || format == "mp3" || format == "m4a"
}

func originalHeaderValid(format string, raw []byte) bool {
	if format == "pdf" {
		return bytes.HasPrefix(raw, []byte("%PDF-"))
	}
	if isOCRFormat(format) {
		return bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")) || (len(raw) >= 3 && bytes.Equal(raw[:3], []byte{0xff, 0xd8, 0xff}))
	}
	if isAudioFormat(format) {
		return format == "wav" && bytes.HasPrefix(raw, []byte("RIFF")) && len(raw) > 12 && string(raw[8:12]) == "WAVE" || format == "mp3" && (bytes.HasPrefix(raw, []byte("ID3")) || len(raw) >= 2 && raw[0] == 0xff && raw[1]&0xe0 == 0xe0) || format == "m4a" && len(raw) > 12 && string(raw[4:8]) == "ftyp"
	}
	return bytes.HasPrefix(raw, []byte("PK"))
}

func parsedText(blocks []contracts.ParsedBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		parts = append(parts, block.Content.Text)
	}
	return strings.Join(parts, "\n\n")
}

func (s *state) blobStore(a *authority, action contracts.Action, ref contracts.BlobRef) storage.LocalBlobStore {
	return storage.LocalBlobStore{Root: filepath.Join(s.root, "blobs"), MaxBytes: 8 << 20, Authorize: func(c context.Context, before contracts.AccessScope, b contracts.BlobRef) error {
		if before.Action != action || a.verify(c, before) != nil {
			return errAuthority
		}
		if action == "original.read" && b != ref {
			return errAuthority
		}
		return nil
	}}
}

func (s *state) importOriginal(ctx context.Context, db *sql.DB, a *authority, path, format string) (importResult, error) {
	format = canonicalOriginalFormat(format)
	if s.m.Features != 1 {
		return importResult{}, errors.New("Office/PDF、OCR 和音频导入需要先运行 enable-ai")
	}
	if _, err := a.scope(ctx, "document.parse", ""); err != nil {
		return importResult{}, errAuthority
	}
	f, err := os.Open(path)
	if err != nil {
		return importResult{}, errImport
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 8<<20+1))
	if err != nil || len(raw) == 0 || len(raw) > 8<<20 {
		return importResult{}, errImport
	}
	if !originalHeaderValid(format, raw) {
		return importResult{}, errImport
	}
	scope, err := a.scope(ctx, "original.write", "")
	if err != nil {
		return importResult{}, errAuthority
	}
	deadline, _ := ctx.Deadline()
	if scope.ExpiresAt.Before(deadline) {
		deadline = scope.ExpiresAt
	}
	request := contracts.RequestContext{RequestID: contracts.ID(nonce()), ProfileDigest: s.m.ProfileDigest, Deadline: deadline}
	store := s.blobStore(a, "original.write", contracts.BlobRef{})
	blob, err := store.Put(ctx, contracts.BlobWriteRequest{Request: request, Scope: scope, ExpectedSHA256: contracts.Digest(digest(raw)), Size: int64(len(raw)), MediaType: mediaFormats[format], IdempotencyKey: "source-" + digest([]byte(format+"/"+digest(raw)))}, bytes.NewReader(raw))
	if err != nil {
		return importResult{}, errors.New("original upload incomplete; retry the same file")
	}
	var conv parser.OfficeConverter
	var parserVersion string
	if isOCRFormat(format) {
		o, ocrErr := ocrConverter()
		if ocrErr != nil || o.Health(ctx) != nil {
			return importResult{}, errors.New("OCR converter unavailable; install tesseract and configure MEMORYS_OCR_LANG")
		}
		conv, parserVersion = o, "ocr/tesseract-5"
	} else if isAudioFormat(format) {
		t, transcribeErr := transcriptionConverter()
		if transcribeErr != nil || t.Health(ctx) != nil {
			return importResult{}, errors.New("音频转写工具不可用；请安装 whisper 并配置 MEMORYS_TRANSCRIPTION_LANG")
		}
		conv, parserVersion = t, "transcription/whisper-1"
	} else {
		conv, parserVersion, err = originalConverter(ctx, format, true)
		if err != nil {
			return importResult{}, err
		}
	}
	id := digest([]byte(string(s.m.Installation) + "/original/" + format + "/" + string(blob.SHA256) + "/" + parserVersion))
	draftID := contracts.ID("draft-original-" + id)
	docID := contracts.ID("doc-original-" + id)
	// Re-importing a submitted original reuses the frozen candidate, while still
	// rechecking its real bytes and converter. Parser output cannot overwrite it.
	readScope, err := a.scope(ctx, "original.read", "")
	if err != nil {
		return importResult{}, errAuthority
	}
	parseScope, err := a.scope(ctx, "document.parse", "")
	if err != nil {
		return importResult{}, errAuthority
	}
	readStore := s.blobStore(a, "original.read", blob)
	var parsed contracts.ParseResult
	if isOCRFormat(format) {
		parsed, err = (parser.OCRParser{Store: readStore, Converter: conv, MaxBytes: 8 << 20}).Parse(ctx, contracts.ParseRequest{Request: request, Scope: readScope, DraftID: draftID, Input: blob, Format: format, Options: contracts.ParseOptions{EnableOCR: true}})
	} else if isAudioFormat(format) {
		parsed, err = (parser.AudioParser{Store: readStore, Converter: conv, MaxBytes: 8 << 20}).Parse(ctx, contracts.ParseRequest{Request: request, Scope: readScope, DraftID: draftID, Input: blob, Format: format, Options: contracts.ParseOptions{EnableOCR: true}})
	} else {
		parsed, err = (parser.OfficeParser{Store: readStore, Converter: conv, MaxBytes: 8 << 20}).Parse(ctx, contracts.ParseRequest{Request: request, Scope: readScope, DraftID: draftID, Input: blob, Format: format})
	}
	if (err != nil || len(parsed.Blocks) == 0) && format == "pdf" {
		if o, ocrErr := ocrConverter(); ocrErr == nil && o.Health(ctx) == nil {
			conv, parserVersion = o, "ocr/tesseract-5"
			parsed, err = (parser.OCRParser{Store: readStore, Converter: conv, MaxBytes: 8 << 20}).Parse(ctx, contracts.ParseRequest{Request: request, Scope: readScope, DraftID: draftID, Input: blob, Format: format, Options: contracts.ParseOptions{EnableOCR: true}})
		}
	}
	if err != nil || parsed.ParserVersion != parserVersion || len(parsed.Blocks) == 0 {
		return importResult{}, errors.New("document conversion failed; require a supported text PDF/Office document or an available OCR tool")
	}
	projectedText := strings.TrimSpace(parsedText(parsed.Blocks))
	if len(projectedText) > maxTextBytes || projectedText == "" {
		return importResult{}, errors.New("converted text must be nonempty and at most 256 KiB")
	}
	blocks := make([]contracts.DraftBlock, 0, len(parsed.Blocks))
	for _, block := range parsed.Blocks {
		blocks = append(blocks, contracts.DraftBlock{ID: block.ID, DraftID: draftID, RowVersion: 1, Order: block.Order, Content: block.Content})
	}
	draft := contracts.Draft{ID: draftID, DocumentID: docID, State: "draft", RowVersion: 1, Blobs: []contracts.BlobRef{blob}, Blocks: blocks}
	if _, err = document.BuildFrozenContent(draft); err != nil {
		return importResult{}, errImport
	}
	create, err := a.scope(ctx, "document.create", "")
	if err != nil {
		return importResult{}, errAuthority
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return importResult{}, errImport
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", string(draftID)); err != nil {
		return importResult{}, errImport
	}
	var exists bool
	if tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.documents WHERE id=$1)", docID).Scan(&exists) != nil {
		return importResult{}, errImport
	}
	if exists {
		previous, err := s.loadDraft(ctx, tx, draftID)
		if err != nil || !reflect.DeepEqual(previous, draft) {
			return importResult{}, errImport
		}
	} else {
		if _, err = tx.ExecContext(ctx, "INSERT INTO akos_withdraw_v1.documents VALUES($1,$2,$3,0,1,NULL,NULL,false)", s.m.Space, s.m.KB, docID); err != nil {
			return importResult{}, errImport
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO akos_withdraw_v1.draft_heads VALUES($1,$2,$3,$4,1,'draft',$5::jsonb)", s.m.Space, s.m.KB, docID, draftID, string(encode(draft))); err != nil {
			return importResult{}, errImport
		}
	}
	if a.verify(ctx, create) != nil || a.verify(ctx, parseScope) != nil || a.verify(ctx, readScope) != nil {
		return importResult{}, errAuthority
	}
	if tx.Commit() != nil {
		return importResult{}, errors.New("original import outcome uncertain; retry the same file")
	}
	return resultFor(draft, exists)
}

func (s *state) validateOriginalDraft(d contracts.Draft) error {
	if s.m.Features != 1 || len(d.Blobs) != 1 || len(d.Blocks) == 0 || d.BaseRevisionID != nil {
		return errImport
	}
	b := d.Blobs[0]
	format := ""
	for f, media := range mediaFormats {
		if b.MediaType == media {
			format = f
		}
	}
	if format == "" || b.Size <= 0 || b.Size > 8<<20 || len(b.SHA256) != 64 || len(draftText(d)) > maxTextBytes || strings.TrimSpace(draftText(d)) == "" {
		return errImport
	}
	parserVersion := "markitdown/0.1.3"
	if isOCRFormat(format) {
		parserVersion = "ocr/tesseract-5"
	} else if isAudioFormat(format) {
		parserVersion = "transcription/whisper-1"
	}
	exact, ocr, transcript := false, false, false
	for index, block := range d.Blocks {
		if block.ID == "" || block.DraftID != d.ID || block.Content.Kind != contracts.TextBlock || block.Content.Source.Kind != "original" || block.Content.Source.BlobID != b.ID || block.Content.Source.RevisionBlockID != "" || block.Content.Text == "" || block.Order != index {
			return errImport
		}
		locator := block.Content.Source.Locator
		if locator.Precision == "exact" || locator.Precision == "ocr" || locator.Precision == "transcript" {
			if locator.Precision == "ocr" {
				if transcript || exact && !ocr {
					return errImport
				}
				ocr = true
			} else if locator.Precision == "transcript" {
				if ocr || exact && !transcript {
					return errImport
				}
				transcript = true
			} else if ocr || transcript {
				return errImport
			}
			exact = true
			if locator.Characters == nil || locator.Characters.Start < 0 || locator.Characters.End < locator.Characters.Start || locator.Precision != "transcript" && (locator.Characters.Start != 0 || locator.Characters.End != utf8.RuneCountInString(block.Content.Text)) {
				return errImport
			}
			switch format {
			case "pdf":
				if locator.Page == nil || *locator.Page < 1 || index > 0 && (d.Blocks[index-1].Content.Source.Locator.Page == nil || *locator.Page <= *d.Blocks[index-1].Content.Source.Locator.Page) {
					return errImport
				}
				if locator.Confidence != nil && (*locator.Confidence < 0 || *locator.Confidence > 100) {
					return errImport
				}
			case "png", "jpg", "jpeg":
				if locator.Page == nil || *locator.Page != 1 {
					return errImport
				}
				if locator.Confidence != nil && (*locator.Confidence < 0 || *locator.Confidence > 100) {
					return errImport
				}
			case "wav", "mp3", "m4a":
				if locator.Characters == nil {
					return errImport
				}
				if (locator.StartMS == nil) != (locator.EndMS == nil) || locator.StartMS != nil && (*locator.StartMS < 0 || *locator.EndMS < *locator.StartMS) {
					return errImport
				}
			case "docx":
				if len(locator.ParagraphPath) == 0 || locator.Worksheet != "" || locator.CellRange != "" || locator.Slide != nil {
					return errImport
				}
			case "xlsx":
				if locator.Worksheet == "" || locator.CellRange == "" || len(locator.ParagraphPath) != 0 || locator.Slide != nil {
					return errImport
				}
			case "pptx":
				if locator.Slide == nil || *locator.Slide < 1 || len(locator.ParagraphPath) != 0 || locator.Worksheet != "" || locator.CellRange != "" {
					return errImport
				}
			default:
				return errImport
			}
		} else if locator.Precision != "coarse" || locator.Page != nil || locator.Characters != nil || locator.StartMS != nil || locator.EndMS != nil || locator.Confidence != nil {
			return errImport
		}
	}
	if exact {
		if ocr {
			parserVersion = "ocr/tesseract-5"
			if !isOCRFormat(format) && format != "pdf" {
				return errImport
			}
		} else if transcript {
			parserVersion = "transcription/whisper-1"
			if !isAudioFormat(format) {
				return errImport
			}
		} else if format == "pdf" {
			parserVersion = "pypdf/6.19.0"
		} else if isAudioFormat(format) {
			parserVersion = "transcription/whisper-1"
		} else if isOCRFormat(format) {
			parserVersion = "ocr/tesseract-5"
		} else if format != "docx" && format != "xlsx" && format != "pptx" {
			return errImport
		}
		for index, block := range d.Blocks {
			var blockID contracts.ID
			if format == "pdf" {
				blockID = parser.PageBlockID(d.ID, string(b.SHA256), *block.Content.Source.Locator.Page, block.Content.Text, string(s.m.ProfileDigest), parserVersion)
			} else if isAudioFormat(format) {
				hash := sha256.Sum256([]byte(string(d.ID) + "\x00" + string(b.SHA256) + "\x00transcript\x00" + format + "\x00" + string(s.m.ProfileDigest) + "\x00" + parserVersion + "\x00" + block.Content.Text))
				blockID = contracts.ID(hex.EncodeToString(hash[:]))
			} else if isOCRFormat(format) {
				hash := sha256.Sum256([]byte(string(d.ID) + "\x00" + string(b.SHA256) + "\x00ocr\x00" + format + "\x00" + string(s.m.ProfileDigest) + "\x00" + parserVersion + "\x00" + block.Content.Text))
				blockID = contracts.ID(hex.EncodeToString(hash[:]))
			} else {
				blockID = parser.ConvertedBlockID(d.ID, string(b.SHA256), format, index, block.Content.Text, string(s.m.ProfileDigest), parserVersion)
			}
			if block.ID != blockID {
				return errImport
			}
		}
	} else if len(d.Blocks) != 1 {
		return errImport
	} else {
		blockID := digest([]byte(string(d.ID) + "\x00" + string(b.SHA256) + "\x00" + format + "\x00" + string(s.m.ProfileDigest) + "\x00" + parserVersion))
		if string(d.Blocks[0].ID) != blockID {
			return errImport
		}
	}
	id := digest([]byte(string(s.m.Installation) + "/original/" + format + "/" + string(b.SHA256) + "/" + parserVersion))
	if d.ID != contracts.ID("draft-original-"+id) || d.DocumentID != contracts.ID("doc-original-"+id) || d.State != "draft" || d.RowVersion != 1 {
		return errImport
	}
	_, err := document.BuildFrozenContent(d)
	return err
}

func (s *state) verifyOriginalDraft(ctx context.Context, a *authority, d contracts.Draft) error {
	if s.validateOriginalDraft(d) != nil {
		return errImport
	}
	scope, err := a.scope(ctx, "original.read", d.DocumentID)
	if err != nil {
		return errAuthority
	}
	deadline, _ := ctx.Deadline()
	if scope.ExpiresAt.Before(deadline) {
		deadline = scope.ExpiresAt
	}
	store := s.blobStore(a, "original.read", d.Blobs[0])
	request := contracts.BlobReadRequest{Request: contracts.RequestContext{RequestID: contracts.ID(nonce()), ProfileDigest: s.m.ProfileDigest, Deadline: deadline}, Scope: scope, Blob: d.Blobs[0]}
	if contentverify.VerifyBlobBytes(ctx, store, request) != nil {
		return errImport
	}
	format := ""
	for name, media := range mediaFormats {
		if d.Blobs[0].MediaType == media {
			format = name
		}
	}
	if format == "" {
		return errImport
	}
	stream, err := store.Open(ctx, request)
	if err != nil {
		return errImport
	}
	defer stream.Close()
	raw, err := io.ReadAll(io.LimitReader(stream, 8<<20+1))
	if err != nil || int64(len(raw)) != d.Blobs[0].Size || digest(raw) != string(d.Blobs[0].SHA256) {
		return errImport
	}
	exact := d.Blocks[0].Content.Source.Locator.Precision == "exact"
	transcript := d.Blocks[0].Content.Source.Locator.Precision == "transcript"
	var conv parser.OfficeConverter
	var parserVersion string
	if isOCRFormat(format) || d.Blocks[0].Content.Source.Locator.Precision == "ocr" {
		o, ocrErr := ocrConverter()
		if ocrErr != nil {
			return errImport
		}
		conv, parserVersion = o, "ocr/tesseract-5"
	} else if isAudioFormat(format) || transcript {
		t, transcribeErr := transcriptionConverter()
		if transcribeErr != nil {
			return errImport
		}
		conv, parserVersion = t, "transcription/whisper-1"
	} else {
		conv, parserVersion, err = originalConverter(ctx, format, exact)
		if err != nil {
			return err
		}
	}
	var parsed contracts.ParseResult
	if isOCRFormat(format) || d.Blocks[0].Content.Source.Locator.Precision == "ocr" {
		parsed, err = (parser.OCRParser{Store: store, Converter: conv, MaxBytes: 8 << 20}).Parse(ctx, contracts.ParseRequest{Request: request.Request, Scope: scope, DraftID: d.ID, Input: d.Blobs[0], Format: format, Options: contracts.ParseOptions{EnableOCR: true}})
	} else if isAudioFormat(format) || transcript {
		parsed, err = (parser.AudioParser{Store: store, Converter: conv, MaxBytes: 8 << 20}).Parse(ctx, contracts.ParseRequest{Request: request.Request, Scope: scope, DraftID: d.ID, Input: d.Blobs[0], Format: format, Options: contracts.ParseOptions{EnableOCR: true}})
	} else {
		parsed, err = (parser.OfficeParser{Store: store, Converter: conv, MaxBytes: 8 << 20}).Parse(ctx, contracts.ParseRequest{Request: request.Request, Scope: scope, DraftID: d.ID, Input: d.Blobs[0], Format: format})
	}
	if err != nil || parsed.ParserVersion != parserVersion || len(parsed.Blocks) != len(d.Blocks) {
		return errors.New("original or converted content changed; publication refused")
	}
	for index, block := range parsed.Blocks {
		stored := d.Blocks[index]
		if block.ID != stored.ID || block.Order != stored.Order || !reflect.DeepEqual(block.Content, stored.Content) {
			return errors.New("original or converted content changed; publication refused")
		}
	}
	if strings.TrimSpace(parsedText(parsed.Blocks)) != strings.TrimSpace(draftText(d)) {
		return errors.New("original or converted content changed; publication refused")
	}
	return a.verify(ctx, scope)
}

type originalAccess struct {
	a        *authority
	bindings map[contracts.ID]contracts.FrozenContentReadRequest
}

func (o *originalAccess) current(ctx context.Context, q contracts.FrozenContentReadRequest, b contracts.BlobRef) error {
	if o.a.verify(ctx, q.Scope) != nil {
		return errAuthority
	}
	var raw []byte
	err := o.a.db.QueryRowContext(ctx, `SELECT rc.payload FROM akos_withdraw_v1.revision_contents rc JOIN akos_withdraw_v1.documents d ON d.id=rc.document_id AND d.active_revision_id=rc.revision_id JOIN akos_withdraw_v1.approval_requests a ON a.document_id=d.id AND a.revision_id=rc.revision_id AND a.state='approved' JOIN akos_withdraw_v1.review_decisions r ON r.id=a.current_decision_id AND r.revision_id=rc.revision_id AND r.content_hash=rc.content_hash WHERE rc.space_id=$1 AND rc.kb_id=$2 AND rc.document_id=$3 AND rc.revision_id=$4 AND NOT d.deleted`, q.Scope.SpaceID, q.Scope.KnowledgeBaseIDs[0], q.DocumentID, q.RevisionID).Scan(&raw)
	var revision contracts.Revision
	if err != nil || len(raw) > 4<<20 || json.Unmarshal(raw, &revision) != nil || revision.ID != q.RevisionID || revision.DocumentID != q.DocumentID {
		return errAuthority
	}
	for _, ref := range revision.Blobs {
		if ref == b {
			return nil
		}
	}
	return errAuthority
}
func (o *originalAccess) ResolveOriginalRead(ctx context.Context, q contracts.FrozenContentReadRequest, b contracts.BlobRef) (contracts.BlobReadRequest, error) {
	if !contracts.IsCurrentContentReadAction(q.Scope.Action) || len(q.Scope.KnowledgeBaseIDs) != 1 || o.current(ctx, q, b) != nil {
		return contracts.BlobReadRequest{}, errAuthority
	}
	scope, err := o.a.scope(ctx, "original.read", q.DocumentID)
	if err != nil {
		return contracts.BlobReadRequest{}, errAuthority
	}
	request := q.Request
	deadline, _ := ctx.Deadline()
	for _, limit := range []time.Time{scope.ExpiresAt, q.Scope.ExpiresAt, deadline} {
		if limit.Before(request.Deadline) {
			request.Deadline = limit
		}
	}
	o.bindings[b.ID] = q
	return contracts.BlobReadRequest{Request: request, Scope: scope, Blob: b}, nil
}
func (s *state) originalFactory(db *sql.DB) localruntime.OriginalFactory {
	return func(auth contracts.Authenticator, secret contracts.Credential, who contracts.Principal) (contracts.BlobStore, contentverify.OriginalReadControl, contentverify.OriginalTextProjector, error) {
		if s.m.Features != 1 {
			return nil, nil, nil, errState
		}
		a := &authority{s: s, db: db, auth: auth, credential: secret, principal: who}
		control := &originalAccess{a: a, bindings: map[contracts.ID]contracts.FrozenContentReadRequest{}}
		store := storage.LocalBlobStore{Root: filepath.Join(s.root, "blobs"), MaxBytes: 8 << 20, Authorize: func(c context.Context, before contracts.AccessScope, b contracts.BlobRef) error {
			q, ok := control.bindings[b.ID]
			if !ok || before.Action != "original.read" || a.verify(c, before) != nil || control.current(c, q, b) != nil {
				return errAuthority
			}
			return nil
		}}
		conv, err := converter()
		if err != nil {
			return nil, nil, nil, err
		}
		projector, err := adapters.NewOfficeOriginalTextProjector(conv)
		if err != nil {
			return nil, nil, nil, err
		}
		return store, control, &memoProjector{inner: projector, cache: map[string]contentverify.OriginalTextProjection{}}, nil
	}
}

// Cache only an invocation's deterministic conversion, keyed by the bytes that
// the verifier has just hash-checked. Each use still rechecks bytes and grants.
type memoProjector struct {
	inner contentverify.OriginalTextProjector
	mu    sync.Mutex
	cache map[string]contentverify.OriginalTextProjection
}

func (p *memoProjector) ProjectOriginalText(c context.Context, media string, raw []byte) (contentverify.OriginalTextProjection, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := media + "/" + digest(raw)
	if result, ok := p.cache[key]; ok && c.Err() == nil {
		return result, nil
	}
	result, err := p.inner.ProjectOriginalText(c, media, raw)
	if err == nil {
		p.cache[key] = result
	}
	return result, err
}
