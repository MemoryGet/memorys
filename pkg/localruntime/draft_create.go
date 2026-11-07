package localruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func createDraftID(documentID contracts.ID, key string, subject contracts.ID) contracts.ID {
	sum := sha256.Sum256([]byte("draft\x00" + string(documentID) + "\x00" + key + "\x00" + string(subject)))
	return contracts.ID("draft-" + hex.EncodeToString(sum[:]))
}

func (s *Search) CreateDraft(ctx context.Context, input contracts.DraftCreateInput) (contracts.Draft, error) {
	var empty contracts.Draft
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.DocumentID == "" || input.ExpectedDocumentVersion == 0 || input.ExpectedDocumentVersion == ^uint64(0) || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.IdempotencyKey == "" {
		return empty, errors.New("draft creation unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("draft creation unauthenticated")
	}
	_, digest, err := s.ProfileIdentity()
	if err != nil || digest != input.Request.ProfileDigest {
		return empty, errors.New("draft creation profile changed")
	}
	var spaceID, kbID string
	if err := s.db.QueryRowContext(ctx, `SELECT space_id,kb_id FROM akos_withdraw_v1.documents WHERE id=$1::text`, string(input.DocumentID)).Scan(&spaceID, &kbID); err != nil {
		return empty, errors.New("document unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "document.create", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{contracts.ID(kbID)}, ResourceIDs: []contracts.ID{input.DocumentID}})
	if err != nil {
		return empty, errors.New("draft creation denied")
	}
	verify := func(c context.Context, before contracts.AccessScope) error {
		credential := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
		defer clear(credential.Secret)
		current, e := s.auth.Authenticate(c, credential)
		if e != nil {
			return errors.New("draft creation denied")
		}
		fresh, e := authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: current, Action: before.Action, SpaceID: before.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), before.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{input.DocumentID}})
		if e != nil || queryread.MatchRefreshedScope(c, who, before, current, fresh, before.KnowledgeBaseIDs) != nil {
			return errors.New("draft creation denied")
		}
		return nil
	}
	if err := verify(ctx, scope); err != nil {
		return empty, err
	}
	draftID := createDraftID(input.DocumentID, input.IdempotencyKey, who.SubjectID)
	draft := contracts.Draft{ID: draftID, DocumentID: input.DocumentID, BaseRevisionID: input.BaseRevisionID, RowVersion: 1, State: "draft"}
	if input.BaseRevisionID != nil {
		var raw []byte
		if err := s.db.QueryRowContext(ctx, `SELECT rc.payload FROM akos_withdraw_v1.revision_contents rc JOIN akos_withdraw_v1.revisions r ON r.space_id=rc.space_id AND r.kb_id=rc.kb_id AND r.document_id=rc.document_id AND r.id=rc.revision_id WHERE rc.space_id=$1::text AND rc.kb_id=$2::text AND rc.document_id=$3::text AND rc.revision_id=$4::text`, spaceID, kbID, string(input.DocumentID), string(*input.BaseRevisionID)).Scan(&raw); err != nil {
			return empty, errors.New("base revision unavailable")
		}
		var revision contracts.Revision
		if json.Unmarshal(raw, &revision) != nil || revision.ID != *input.BaseRevisionID {
			return empty, errors.New("base revision unavailable")
		}
		draft.Blobs = append([]contracts.BlobRef(nil), revision.Blobs...)
		for _, block := range revision.Blocks {
			draft.Blocks = append(draft.Blocks, contracts.DraftBlock{ID: block.ID, DraftID: draft.ID, RowVersion: 1, Order: block.Order, Content: block.Content})
		}
	}
	bytes, err := json.Marshal(draft)
	if err != nil || len(bytes) > 8<<20 {
		return empty, errors.New("draft unavailable")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, errors.New("draft unavailable")
	}
	defer tx.Rollback()
	var rowVersion string
	var deleted bool
	if err := tx.QueryRowContext(ctx, `SELECT row_version::text,deleted FROM akos_withdraw_v1.documents WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text FOR UPDATE`, spaceID, kbID, string(input.DocumentID)).Scan(&rowVersion, &deleted); err != nil || deleted {
		return empty, errors.New("document unavailable")
	}
	currentVersion, err := strconv.ParseUint(rowVersion, 10, 64)
	if err != nil || currentVersion != input.ExpectedDocumentVersion {
		return empty, errors.New("document changed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_heads(space_id,kb_id,document_id,id,row_version,state,payload) VALUES($1::text,$2::text,$3::text,$4::text,1,'draft',$5::jsonb)`, spaceID, kbID, string(input.DocumentID), string(draft.ID), string(bytes)); err != nil {
		if tx.Rollback() != nil {
		}
		var existing []byte
		if e := s.db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.draft_heads WHERE id=$1::text`, string(draft.ID)).Scan(&existing); e == nil && json.Unmarshal(existing, &draft) == nil {
			return draft, nil
		}
		return empty, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_create_receipts(space_id,kb_id,document_id,draft_id,actor_id,command_key,receipt) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::jsonb)`, spaceID, kbID, string(input.DocumentID), string(draft.ID), string(who.SubjectID), input.IdempotencyKey, string(bytes)); err != nil {
		return empty, errors.New("draft creation unavailable")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_create_audit(space_id,kb_id,document_id,draft_id,actor_id,request_id,action) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,'draft.created')`, spaceID, kbID, string(input.DocumentID), string(draft.ID), string(who.SubjectID), string(input.Request.RequestID)); err != nil {
		return empty, errors.New("draft creation unavailable")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_create_outbox(space_id,kb_id,draft_id,event_type,payload) VALUES($1::text,$2::text,$3::text,'draft.created',$4::jsonb)`, spaceID, kbID, string(draft.ID), string(bytes)); err != nil {
		return empty, errors.New("draft creation unavailable")
	}
	if err := tx.Commit(); err != nil {
		return empty, errors.New("draft creation outcome unknown")
	}
	return draft, nil
}

var _ contracts.DraftCreateApplication = (*Search)(nil)
