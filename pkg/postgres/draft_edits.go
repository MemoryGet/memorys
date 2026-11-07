package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

// DraftEdits persists a complete ordered block replacement. The table-level
// CAS and receipt/audit/outbox inserts are one serializable transaction.
type DraftEdits struct {
	DB     *sql.DB
	Verify VerifyScope
}

func (p DraftEdits) EditDraft(ctx context.Context, input contracts.DraftEditInput, scope contracts.AccessScope, principal contracts.Principal) (contracts.Draft, error) {
	var empty contracts.Draft
	if ctx == nil || p.DB == nil || p.Verify == nil || scope.Action != "document.create" || scope.SubjectID != principal.SubjectID || len(scope.KnowledgeBaseIDs) != 1 || scope.PolicyVersion == 0 || !scope.ExpiresAt.After(time.Now()) || input.DraftID == "" || input.ExpectedVersion == 0 || input.ExpectedVersion == math.MaxUint64 || len(input.Blocks) == 0 || len(input.Blocks) > 100000 || !validSubmissionKey(input.IdempotencyKey) || input.Request.RequestID == "" || input.Request.Deadline.IsZero() || !commandDigest(input.Request.ProfileDigest) {
		return empty, ErrRepository
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrRepository
	}
	actor := principal
	if actor.Delegation != nil {
		delegation := *actor.Delegation
		delegation.ProofReference = ""
		actor.Delegation = &delegation
	}
	blocksBytes, err := json.Marshal(input.Blocks)
	if err != nil || len(blocksBytes) > 8<<20 {
		return empty, ErrRepository
	}
	semantic, err := json.Marshal(struct {
		Space           contracts.ID
		KBs             []contracts.ID
		Principal       contracts.Principal
		Draft           contracts.ID
		ExpectedVersion uint64
		Blocks          json.RawMessage
		Profile         contracts.Digest
	}{scope.SpaceID, scope.KnowledgeBaseIDs, actor, input.DraftID, input.ExpectedVersion, blocksBytes, input.Request.ProfileDigest})
	if err != nil {
		return empty, ErrRepository
	}
	digest := sha256.Sum256(append([]byte("akos.pg-draft-edit/1\n"), semantic...))
	digestText := hex.EncodeToString(digest[:])
	verify := func(c context.Context) error {
		if p.Verify(c, cloneScope(scope)) != nil || c.Err() != nil {
			return ErrRepository
		}
		return nil
	}
	if err := verify(ctx); err != nil {
		return empty, err
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	ctx, scopeCancel := context.WithDeadline(ctx, scope.ExpiresAt)
	defer scopeCancel()
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, ErrRepository
	}
	defer tx.Rollback()
	var previousDigest string
	var previousReceipt []byte
	err = tx.QueryRowContext(ctx, `SELECT request_digest,receipt FROM akos_withdraw_v1.draft_edit_receipts WHERE space_id=$1::text AND kb_id=$2::text AND actor_id=$3::text AND command_key=$4::text`, string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), string(principal.SubjectID), input.IdempotencyKey).Scan(&previousDigest, &previousReceipt)
	if err == nil {
		if previousDigest != digestText || json.Unmarshal(previousReceipt, &empty) != nil || empty.ID != input.DraftID {
			return contracts.Draft{}, contracts.ErrDraftEditConflict
		}
		if err := verify(ctx); err != nil {
			return contracts.Draft{}, err
		}
		if tx.Commit() != nil {
			return contracts.Draft{}, ErrCommitUnknown
		}
		return empty, nil
	}
	if err != sql.ErrNoRows {
		return empty, ErrRepository
	}
	var payload []byte
	var version string
	var documentID string
	err = tx.QueryRowContext(ctx, `SELECT h.document_id,h.payload,h.row_version::text FROM akos_withdraw_v1.draft_heads h JOIN akos_withdraw_v1.documents d ON d.space_id=h.space_id AND d.kb_id=h.kb_id AND d.id=h.document_id WHERE h.space_id=$1::text AND h.kb_id=$2::text AND h.id=$3::text AND h.state='draft' AND NOT d.deleted FOR UPDATE OF h`, string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), string(input.DraftID)).Scan(&documentID, &payload, &version)
	if err != nil {
		return empty, ErrRepository
	}
	var current contracts.Draft
	if json.Unmarshal(payload, &current) != nil || current.ID != input.DraftID || current.DocumentID != contracts.ID(documentID) || current.State != "draft" || version != strconv.FormatUint(current.RowVersion, 10) || current.RowVersion != input.ExpectedVersion {
		return empty, contracts.ErrDraftEditConflict
	}
	updated, err := document.ApplyDraftEdit(current, input.Blocks)
	if errors.Is(err, document.ErrDraftEditConflict) {
		return empty, contracts.ErrDraftEditConflict
	}
	if err != nil {
		return empty, ErrRepository
	}
	updatedBytes, err := json.Marshal(updated)
	if err != nil || len(updatedBytes) > 8<<20 {
		return empty, ErrRepository
	}
	result, err := tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.draft_heads SET row_version=$5::numeric,payload=$6::jsonb WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND id=$4::text AND row_version=$7::numeric AND state='draft'`, string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), documentID, string(input.DraftID), strconv.FormatUint(updated.RowVersion, 10), string(updatedBytes), strconv.FormatUint(input.ExpectedVersion, 10))
	if err != nil {
		return empty, ErrRepository
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return empty, contracts.ErrDraftEditConflict
	}
	receipt, err := json.Marshal(updated)
	if err != nil {
		return empty, ErrRepository
	}
	actorBytes, err := json.Marshal(principal)
	if err != nil {
		return empty, ErrRepository
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_edit_receipts(space_id,kb_id,document_id,draft_id,actor_id,command_key,request_digest,receipt) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::text,$8::jsonb)`, string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), documentID, string(input.DraftID), string(principal.SubjectID), input.IdempotencyKey, digestText, string(receipt)); err != nil {
		return empty, ErrRepository
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_edit_audit(space_id,kb_id,draft_id,row_version,actor_id,request_id,action,actor) VALUES($1::text,$2::text,$3::text,$4::numeric,$5::text,$6::text,'document.edit',$7::jsonb)`, string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), string(input.DraftID), strconv.FormatUint(updated.RowVersion, 10), string(principal.SubjectID), string(input.Request.RequestID), string(actorBytes)); err != nil {
		return empty, ErrRepository
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_edit_outbox(space_id,kb_id,draft_id,row_version,event_type,payload) VALUES($1::text,$2::text,$3::text,$4::numeric,'draft.edited',$5::jsonb)`, string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), string(input.DraftID), strconv.FormatUint(updated.RowVersion, 10), string(receipt)); err != nil {
		return empty, ErrRepository
	}
	if err := verify(ctx); err != nil {
		return empty, err
	}
	if err := tx.Commit(); err != nil {
		return empty, ErrCommitUnknown
	}
	return updated, nil
}
