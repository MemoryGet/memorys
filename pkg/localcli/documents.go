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

var errImport = errors.New("import rejected: require nonempty UTF-8 TXT/Markdown up to 256 KiB, or matching existing content")

type importResult struct {
	DocumentID  contracts.ID     `json:"document_id"`
	DraftID     contracts.ID     `json:"draft_id"`
	ContentHash contracts.Digest `json:"content_hash"`
	Preview     string           `json:"preview"`
	Bytes       int              `json:"bytes"`
	Reused      bool             `json:"reused"`
}

func (s *state) textDraft(text string) (contracts.Draft, error) {
	if len(text) > maxTextBytes || !utf8.ValidString(text) || strings.TrimSpace(text) == "" || strings.ContainsRune(text, 0) {
		return contracts.Draft{}, errImport
	}
	id := digest([]byte(string(s.m.Installation) + "\x00" + text))
	doc, draft, block := contracts.ID("doc-"+id), contracts.ID("draft-"+id), contracts.ID("block-"+id)
	return contracts.Draft{ID: draft, DocumentID: doc, State: "draft", RowVersion: 1, Blocks: []contracts.DraftBlock{{ID: block, DraftID: draft, RowVersion: 1, Content: contracts.Content{Kind: contracts.TextBlock, Text: text, Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: block, Locator: contracts.Locator{Precision: "coarse"}}}}}}, nil
}

func resultFor(d contracts.Draft, reused bool) (importResult, error) {
	f, err := document.BuildFrozenContent(d)
	if err != nil {
		return importResult{}, errImport
	}
	text := d.Blocks[0].Content.Text
	preview := []rune(text)
	if len(preview) > 240 {
		preview = append(preview[:240], []rune("…")...)
	}
	return importResult{DocumentID: d.DocumentID, DraftID: d.ID, ContentHash: f.ContentHash, Preview: string(preview), Bytes: len(text), Reused: reused}, nil
}

func (s *state) importFile(ctx context.Context, db *sql.DB, a *authority, path string) (importResult, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if format := strings.TrimPrefix(ext, "."); mediaFormats[format] != "" {
		return s.importOriginal(ctx, db, a, path, format)
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
	expected, err := s.textDraft(d.Blocks[0].Content.Text)
	if err != nil || !reflect.DeepEqual(d, expected) {
		return contracts.Draft{}, errImport
	}
	return d, nil
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
		if err != nil || !reflect.DeepEqual(previous, d) {
			return importResult{}, errImport
		}
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
