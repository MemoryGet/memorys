package localcli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/localruntime"
	"github.com/MemoryGet/memorys/pkg/parser"
	"github.com/MemoryGet/memorys/pkg/storage"
)

var mediaFormats = map[string]string{"pdf": "application/pdf", "docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation"}

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
	if s.m.Features != 1 {
		return importResult{}, errors.New("office/PDF import requires enable-ai first")
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
	if format == "pdf" && !bytes.HasPrefix(raw, []byte("%PDF-")) || format != "pdf" && !bytes.HasPrefix(raw, []byte("PK")) {
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
	id := digest([]byte(string(s.m.Installation) + "/original/" + format + "/" + string(blob.SHA256) + "/markitdown/0.1.3"))
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
	conv, err := converter()
	if err != nil {
		return importResult{}, err
	}
	p := parser.OfficeParser{Store: readStore, Converter: conv, MaxBytes: 8 << 20}
	parsed, err := p.Parse(ctx, contracts.ParseRequest{Request: request, Scope: readScope, DraftID: draftID, Input: blob, Format: format})
	if err != nil || parsed.ParserVersion != "markitdown/0.1.3" || len(parsed.Blocks) != 1 {
		return importResult{}, errors.New("document conversion failed; require a supported text PDF or Office document")
	}
	if len(parsed.Blocks[0].Content.Text) > maxTextBytes || strings.TrimSpace(parsed.Blocks[0].Content.Text) == "" {
		return importResult{}, errors.New("converted text must be nonempty and at most 256 KiB")
	}
	draft := contracts.Draft{ID: draftID, DocumentID: docID, State: "draft", RowVersion: 1, Blobs: []contracts.BlobRef{blob}, Blocks: []contracts.DraftBlock{{ID: parsed.Blocks[0].ID, DraftID: draftID, RowVersion: 1, Content: parsed.Blocks[0].Content}}}
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
	if s.m.Features != 1 || len(d.Blobs) != 1 || len(d.Blocks) != 1 || d.BaseRevisionID != nil {
		return errImport
	}
	b := d.Blobs[0]
	format := ""
	for f, media := range mediaFormats {
		if b.MediaType == media {
			format = f
		}
	}
	if format == "" || b.Size <= 0 || b.Size > 8<<20 || len(b.SHA256) != 64 || len(d.Blocks[0].Content.Text) > maxTextBytes || strings.TrimSpace(d.Blocks[0].Content.Text) == "" {
		return errImport
	}
	id := digest([]byte(string(s.m.Installation) + "/original/" + format + "/" + string(b.SHA256) + "/markitdown/0.1.3"))
	blockID := contracts.ID(digest([]byte(string(d.ID) + "\x00" + string(b.SHA256) + "\x00" + format + "\x00" + string(s.m.ProfileDigest) + "\x00markitdown/0.1.3")))
	expected := contracts.Draft{ID: contracts.ID("draft-original-" + id), DocumentID: contracts.ID("doc-original-" + id), State: "draft", RowVersion: 1, Blobs: []contracts.BlobRef{b}, Blocks: []contracts.DraftBlock{{ID: blockID, DraftID: d.ID, RowVersion: 1, Content: contracts.Content{Kind: contracts.TextBlock, Text: d.Blocks[0].Content.Text, Source: contracts.SourceAnchor{Kind: "original", BlobID: b.ID, Locator: contracts.Locator{Precision: "coarse"}}}}}}
	if !reflect.DeepEqual(d, expected) {
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
	stream, err := store.Open(ctx, request)
	if err != nil {
		return errImport
	}
	defer stream.Close()
	raw, err := io.ReadAll(io.LimitReader(stream, 8<<20+1))
	if err != nil || int64(len(raw)) != d.Blobs[0].Size || digest(raw) != string(d.Blobs[0].SHA256) {
		return errImport
	}
	conv, err := converter()
	if err != nil {
		return err
	}
	projector, err := adapters.NewOfficeOriginalTextProjector(conv)
	if err != nil {
		return err
	}
	projected, err := projector.ProjectOriginalText(ctx, d.Blobs[0].MediaType, raw)
	if err != nil || projected.Text != d.Blocks[0].Content.Text || projected.Version != "markitdown/0.1.3" {
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
