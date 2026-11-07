package localcli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

const maxTextBytes = 256 << 10

var errImport = errors.New("import rejected: require nonempty UTF-8 TXT/Markdown up to 256 KiB, supported Office/PDF, OCR image, or transcribed audio")

type importResult struct {
	DocumentID  contracts.ID     `json:"document_id"`
	DraftID     contracts.ID     `json:"draft_id"`
	ContentHash contracts.Digest `json:"content_hash"`
	Preview     string           `json:"preview"`
	Bytes       int              `json:"bytes"`
	Reused      bool             `json:"reused"`
}

func (s *state) textDraft(text string) (contracts.Draft, error) {
	return s.textDraftForDocument("", nil, text)
}

// textDraftForDocument creates a new editable draft for an existing document.
// Submitted drafts are immutable; editing therefore creates a new draft head
// that points at the currently published revision as its parent.
func (s *state) textDraftForDocument(documentID contracts.ID, baseRevisionID *contracts.ID, text string) (contracts.Draft, error) {
	if len(text) > maxTextBytes || !utf8.ValidString(text) || strings.TrimSpace(text) == "" || strings.ContainsRune(text, 0) {
		return contracts.Draft{}, errImport
	}
	seed := string(s.m.Installation) + "\x00" + string(documentID) + "\x00" + text
	if documentID == "" {
		seed = string(s.m.Installation) + "\x00" + text
		documentID = contracts.ID("doc-" + digest([]byte(seed)))
	}
	id := digest([]byte(seed))
	draft, block := contracts.ID("draft-"+id), contracts.ID("block-"+id)
	length := utf8.RuneCountInString(text)
	return contracts.Draft{ID: draft, DocumentID: documentID, BaseRevisionID: cloneID(baseRevisionID), State: "draft", RowVersion: 1, Blocks: []contracts.DraftBlock{{ID: block, DraftID: draft, RowVersion: 1, Content: contracts.Content{Kind: contracts.TextBlock, Text: text, Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: block, Locator: contracts.Locator{Precision: "exact", Characters: &contracts.CharacterRange{Start: 0, End: length}}}}}}}, nil
}

func cloneID(id *contracts.ID) *contracts.ID {
	if id == nil {
		return nil
	}
	copy := *id
	return &copy
}

func textDraftMatches(actual, expected contracts.Draft) bool {
	if reflect.DeepEqual(actual, expected) {
		return true
	}
	// Drafts created before exact character locators were introduced remain
	// valid. They are accepted only when every deterministic field matches and
	// the sole difference is the legacy coarse locator.
	legacy := expected
	legacy.Blocks = append([]contracts.DraftBlock(nil), expected.Blocks...)
	legacy.Blocks[0].Content.Source.Locator = contracts.Locator{Precision: "coarse"}
	return reflect.DeepEqual(actual, legacy)
}

func resultFor(d contracts.Draft, reused bool) (importResult, error) {
	f, err := document.BuildFrozenContent(d)
	if err != nil {
		return importResult{}, errImport
	}
	text := draftText(d)
	preview := []rune(text)
	if len(preview) > 240 {
		preview = append(preview[:240], []rune("…")...)
	}
	return importResult{DocumentID: d.DocumentID, DraftID: d.ID, ContentHash: f.ContentHash, Preview: string(preview), Bytes: len(text), Reused: reused}, nil
}

func draftText(d contracts.Draft) string {
	parts := make([]string, 0, len(d.Blocks))
	for _, block := range d.Blocks {
		parts = append(parts, block.Content.Text)
	}
	return strings.Join(parts, "\n\n")
}

func (s *state) importFile(ctx context.Context, db *sql.DB, a *authority, path string) (importResult, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if format := strings.TrimPrefix(ext, "."); supportedOriginalFormat(format) {
		return s.importOriginal(ctx, db, a, path, canonicalOriginalFormat(format))
	}
	if ext != ".txt" && ext != ".md" && ext != ".markdown" {
		return importResult{}, errImport
	}
	f, err := os.Open(path)
	if err != nil {
		return importResult{}, errImport
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxTextBytes+1))
	if err != nil {
		return importResult{}, errImport
	}
	return s.importText(ctx, db, a, string(raw))
}

func (s *state) loadDraft(ctx context.Context, q queryer, id contracts.ID) (contracts.Draft, error) {
	var raw []byte
	var rowVersion, state string
	if q.QueryRowContext(ctx, `SELECT payload,row_version::text,state FROM akos_withdraw_v1.draft_heads WHERE space_id=$1 AND kb_id=$2 AND id=$3`, s.m.Space, s.m.KB, id).Scan(&raw, &rowVersion, &state) != nil || len(raw) > 4<<20 {
		return contracts.Draft{}, errImport
	}
	var d contracts.Draft
	if json.Unmarshal(raw, &d) != nil || len(d.Blocks) != 1 || d.ID != id || !(state == "draft" && rowVersion == "1" && d.State == "draft" && d.RowVersion == 1 || state == "submitted" && rowVersion == "2" && d.State == "submitted" && d.RowVersion == 2) {
		return contracts.Draft{}, errImport
	}
	d.State = "draft"
	d.RowVersion = 1
	if len(d.Blobs) > 0 {
		if s.validateOriginalDraft(d) != nil {
			return contracts.Draft{}, errImport
		}
		return d, nil
	}
	// A first draft is seeded from installation+text. Drafts for an existing
	// document are seeded from installation+document+text. Use the same seed
	// that created the row before comparing the stored deterministic payload.
	var expected contracts.Draft
	var err error
	if d.BaseRevisionID == nil {
		expected, err = s.textDraft(d.Blocks[0].Content.Text)
		if err == nil && expected.DocumentID != d.DocumentID {
			expected, err = s.textDraftForDocument(d.DocumentID, nil, d.Blocks[0].Content.Text)
		}
	} else {
		expected, err = s.textDraftForDocument(d.DocumentID, d.BaseRevisionID, d.Blocks[0].Content.Text)
	}
	if err != nil || !textDraftMatches(d, expected) {
		return contracts.Draft{}, errImport
	}
	return d, nil
}

func (s *state) editDraft(ctx context.Context, db *sql.DB, a *authority, sourceID contracts.ID, text string) (importResult, error) {
	if sourceID == "" {
		return importResult{}, errImport
	}
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil {
		return importResult{}, errAuthority
	}
	source, err := s.loadDraft(ctx, db, sourceID)
	if err != nil || len(source.Blobs) != 0 || len(source.Blocks) != 1 || source.Blocks[0].Content.Kind != contracts.TextBlock {
		return importResult{}, errImport
	}
	var active sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(d.active_revision_id,(SELECT h.previous_revision_id FROM akos_withdraw_v1.publication_history h WHERE h.space_id=d.space_id AND h.kb_id=d.kb_id AND h.document_id=d.id AND h.previous_revision_id IS NOT NULL ORDER BY h.publication_epoch DESC LIMIT 1)) FROM akos_withdraw_v1.documents d WHERE d.space_id=$1 AND d.kb_id=$2 AND d.id=$3 AND NOT d.deleted`, s.m.Space, s.m.KB, source.DocumentID).Scan(&active); err != nil {
		return importResult{}, errImport
	}
	var base *contracts.ID
	if active.Valid && active.String != "" {
		id := contracts.ID(active.String)
		base = &id
	}
	draft, err := s.textDraftForDocument(source.DocumentID, base, text)
	if err != nil {
		return importResult{}, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return importResult{}, errImport
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", string(source.DocumentID)); err != nil {
		return importResult{}, errImport
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.draft_heads WHERE space_id=$1 AND kb_id=$2 AND id=$3)`, s.m.Space, s.m.KB, draft.ID).Scan(&exists); err != nil {
		return importResult{}, errImport
	}
	if exists {
		stored, err := s.loadDraft(ctx, tx, draft.ID)
		if err != nil || !reflect.DeepEqual(stored, draft) {
			return importResult{}, errImport
		}
	} else if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_heads VALUES($1,$2,$3,$4,1,'draft',$5::jsonb)`, s.m.Space, s.m.KB, draft.DocumentID, draft.ID, string(encode(draft))); err != nil {
		return importResult{}, errImport
	}
	if a.verify(ctx, scope) != nil {
		return importResult{}, errAuthority
	}
	if err = tx.Commit(); err != nil {
		return importResult{}, errors.New("草稿保存结果不确定，请重试相同内容。")
	}
	return resultFor(draft, false)
}

func (s *state) importText(ctx context.Context, db *sql.DB, a *authority, text string) (importResult, error) {
	d, err := s.textDraft(text)
	if err != nil {
		return importResult{}, err
	}
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil {
		return importResult{}, errAuthority
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return importResult{}, errImport
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", string(d.ID)); err != nil {
		return importResult{}, errImport
	}
	var exists, alive bool
	if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.documents WHERE id=$1),EXISTS(SELECT 1 FROM akos_withdraw_v1.documents WHERE id=$1 AND space_id=$2 AND kb_id=$3 AND NOT deleted)`, d.DocumentID, s.m.Space, s.m.KB).Scan(&exists, &alive) != nil {
		return importResult{}, errImport
	}
	if exists {
		if !alive {
			return importResult{}, errImport
		}
		previous, err := s.loadDraft(ctx, tx, d.ID)
		if err != nil || previous.DocumentID != d.DocumentID || len(previous.Blocks) != 1 || previous.Blocks[0].Content.Text != d.Blocks[0].Content.Text {
			return importResult{}, errImport
		}
		// Keep the persisted legacy representation stable while returning the
		// existing content as the idempotent import result.
		d = previous
	} else {
		if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.documents VALUES($1,$2,$3,0,1,NULL,NULL,false)`, s.m.Space, s.m.KB, d.DocumentID); err != nil {
			return importResult{}, errImport
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_heads VALUES($1,$2,$3,$4,1,'draft',$5::jsonb)`, s.m.Space, s.m.KB, d.DocumentID, d.ID, string(encode(d))); err != nil {
			return importResult{}, errImport
		}
	}
	if a.verify(ctx, scope) != nil {
		return importResult{}, errAuthority
	}
	if tx.Commit() != nil {
		return importResult{}, errors.New("import outcome uncertain; retry the same content to reconcile")
	}
	return resultFor(d, exists)
}
