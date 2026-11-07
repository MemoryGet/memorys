package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrFeedbackRepository = errors.New("feedback repository unavailable")

// FeedbackTargets resolves a citation to the platform-owned document and KB.
// Transport-supplied KB IDs are never trusted.
type FeedbackTargets struct{ DB *sql.DB }

var _ contracts.FeedbackTargetResolver = FeedbackTargets{}

func (p FeedbackTargets) ResolveCitation(ctx context.Context, space contracts.ID, citation contracts.Citation) (contracts.FeedbackTarget, error) {
	var target contracts.FeedbackTarget
	if ctx == nil || p.DB == nil || space == "" || citation.DocumentID == "" || citation.RevisionID == "" || citation.BlockID == "" {
		return target, ErrFeedbackRepository
	}
	if err := p.DB.QueryRowContext(ctx, `SELECT d.space_id,d.kb_id,d.id,r.id
FROM akos_withdraw_v1.documents d
JOIN akos_withdraw_v1.revision_contents r ON r.space_id=d.space_id AND r.kb_id=d.kb_id AND r.document_id=d.id
WHERE d.space_id=$1 AND d.id=$2 AND r.revision_id=$3 AND NOT d.deleted`, string(space), string(citation.DocumentID), string(citation.RevisionID)).Scan(&target.SpaceID, &target.KnowledgeBaseID, &target.DocumentID, &target.RevisionID); err != nil {
		return contracts.FeedbackTarget{}, ErrFeedbackRepository
	}
	var present bool
	if err := p.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.revision_contents r, jsonb_array_elements(r.payload->'Blocks') b WHERE r.space_id=$1 AND r.kb_id=$2 AND r.document_id=$3 AND r.revision_id=$4 AND b->>'ID'=$5)`, string(target.SpaceID), string(target.KnowledgeBaseID), string(target.DocumentID), string(target.RevisionID), string(citation.BlockID)).Scan(&present); err != nil || !present {
		return contracts.FeedbackTarget{}, ErrFeedbackRepository
	}
	target.BlockID = citation.BlockID
	return target, nil
}

func (p FeedbackTargets) ResolveRequest(context.Context, contracts.ID, contracts.ID) (contracts.FeedbackTarget, error) {
	// Request-level feedback needs a durable request/evidence ledger. Until the
	// query ledger is enabled, fail closed rather than guessing a KB.
	return contracts.FeedbackTarget{}, ErrFeedbackRepository
}

// FeedbackStore persists one feedback record with an idempotent actor/key
// boundary and atomic audit/outbox effects.
type FeedbackStore struct{ DB *sql.DB }

var _ contracts.FeedbackStore = FeedbackStore{}

func (p FeedbackStore) Submit(ctx context.Context, record contracts.FeedbackRecord) (contracts.FeedbackResult, error) {
	var empty contracts.FeedbackResult
	if ctx == nil || p.DB == nil || record.RequestID == "" || record.Principal.SubjectID == "" || record.Target.SpaceID == "" || record.Target.KnowledgeBaseID == "" || record.IdempotencyKey == "" {
		return empty, ErrFeedbackRepository
	}
	raw, err := json.Marshal(struct {
		Space, KnowledgeBase, Document, Revision, Block, Actor, Kind, Comment, Key string
		TargetRequestID       *contracts.ID
		Citation              *contracts.Citation
	}{string(record.Target.SpaceID), string(record.Target.KnowledgeBaseID), string(record.Target.DocumentID), string(record.Target.RevisionID), string(record.Target.BlockID), string(record.Principal.SubjectID), record.Request.Kind, record.Request.Comment, record.IdempotencyKey, record.TargetRequestID, record.Request.Citation})
	if err != nil || len(raw) > 1<<20 {
		return empty, ErrFeedbackRepository
	}
	sum := sha256.Sum256(append([]byte("akos.feedback/1\n"), raw...))
	digest := hex.EncodeToString(sum[:])
	idSum := sha256.Sum256([]byte(string(record.Target.SpaceID) + "\x00" + string(record.Principal.SubjectID) + "\x00" + record.IdempotencyKey))
	feedbackID := contracts.ID("feedback-" + hex.EncodeToString(idSum[:]))
	citationBytes, err := nullableCitation(record.Request.Citation)
	if err != nil {
		return empty, ErrFeedbackRepository
	}
	targetRequestID := contracts.ID("")
	if record.TargetRequestID != nil {
		targetRequestID = *record.TargetRequestID
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, ErrFeedbackRepository
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.feedback_records(id,space_id,kb_id,document_id,revision_id,block_id,target_request_id,actor_id,request_id,idempotency_key,request_digest,citation,kind,comment,state,created_at)
	VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13,$14,'received',clock_timestamp()) ON CONFLICT(space_id,actor_id,idempotency_key) DO NOTHING`, string(feedbackID), string(record.Target.SpaceID), string(record.Target.KnowledgeBaseID), nullableFeedbackID(record.Target.DocumentID), nullableFeedbackID(record.Target.RevisionID), nullableFeedbackID(record.Target.BlockID), nullableFeedbackID(targetRequestID), string(record.Principal.SubjectID), string(record.RequestID), record.IdempotencyKey, digest, citationBytes, record.Request.Kind, record.Request.Comment)
	if err != nil {
		return empty, ErrFeedbackRepository
	}
	inserted, _ := result.RowsAffected()
	var oldID, oldDigest, state string
	if err = tx.QueryRowContext(ctx, `SELECT id,request_digest,state FROM akos_withdraw_v1.feedback_records WHERE space_id=$1 AND actor_id=$2 AND idempotency_key=$3 FOR UPDATE`, string(record.Target.SpaceID), string(record.Principal.SubjectID), record.IdempotencyKey).Scan(&oldID, &oldDigest, &state); err != nil {
		return empty, ErrFeedbackRepository
	}
	if oldDigest != digest {
		return empty, contracts.ErrFeedbackConflict
	}
	if inserted > 0 {
		payload, marshalErr := json.Marshal(map[string]any{"feedback_id": oldID, "request_id": record.RequestID, "space_id": record.Target.SpaceID, "kb_id": record.Target.KnowledgeBaseID, "kind": record.Request.Kind})
		if marshalErr != nil || len(payload) > 65536 {
			return empty, ErrFeedbackRepository
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.feedback_audit(id,feedback_id,space_id,kb_id,actor_id,request_id,action,payload) VALUES($1,$2,$3,$4,$5,$6,'feedback.submit',$7::jsonb)`, oldID, oldID, string(record.Target.SpaceID), string(record.Target.KnowledgeBaseID), string(record.Principal.SubjectID), string(record.RequestID), string(payload)); err != nil {
			return empty, ErrFeedbackRepository
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.feedback_outbox(id,feedback_id,space_id,kb_id,event_type,payload) VALUES($1,$2,$3,$4,'feedback.received',$5::jsonb)`, oldID, oldID, string(record.Target.SpaceID), string(record.Target.KnowledgeBaseID), string(payload)); err != nil {
			return empty, ErrFeedbackRepository
		}
	}
	if oldID == string(feedbackID) {
		if tx.Commit() != nil {
			return empty, ErrFeedbackRepository
		}
		return contracts.FeedbackResult{RequestID: record.RequestID, FeedbackID: contracts.ID(oldID), State: state}, nil
	}
	return empty, ErrFeedbackRepository
}

func nullableFeedbackID(id contracts.ID) any {
	if id == "" {
		return nil
	}
	return string(id)
}

func nullableCitation(citation *contracts.Citation) (string, error) {
	if citation == nil {
		return "null", nil
	}
	raw, err := json.Marshal(citation)
	if err != nil || len(raw) > 1<<20 {
		return "", ErrFeedbackRepository
	}
	return string(raw), nil
}
