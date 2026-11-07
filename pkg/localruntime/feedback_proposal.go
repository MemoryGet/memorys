package localruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func proposalID(feedbackID, actor, key contracts.ID) contracts.ID {
	sum := sha256.Sum256([]byte("feedback-proposal\x00" + string(feedbackID) + "\x00" + string(actor) + "\x00" + string(key)))
	return contracts.ID("proposal-" + hex.EncodeToString(sum[:]))
}

func proposalDraftID(proposal, document contracts.ID) contracts.ID {
	sum := sha256.Sum256([]byte("feedback-draft\x00" + string(proposal) + "\x00" + string(document)))
	return contracts.ID("draft-" + hex.EncodeToString(sum[:]))
}

// ProposeFeedback copies the referenced frozen revision into a new editable
// draft. It records the suggestion atomically with the draft and its audit /
// outbox effects. Approval and publication remain separate commands.
func (s *Search) ProposeFeedback(ctx context.Context, input contracts.FeedbackProposalInput) (contracts.FeedbackProposal, error) {
	var empty contracts.FeedbackProposal
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.FeedbackID == "" || strings.TrimSpace(input.Comment) == "" || len(input.Comment) > 10000 || strings.ContainsRune(input.Comment, 0) || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 200 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 {
		return empty, errors.New("feedback proposal unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil || ctx.Err() != nil {
		return empty, errors.New("feedback proposal unauthenticated")
	}
	version, digest, err := s.ProfileIdentity()
	if err != nil || version == 0 || digest != input.Request.ProfileDigest {
		return empty, errors.New("feedback proposal profile changed")
	}
	var space, kb, document, revision, state string
	if err := s.db.QueryRowContext(ctx, `SELECT space_id,kb_id,document_id,revision_id,state FROM akos_withdraw_v1.feedback_records WHERE id=$1::text`, string(input.FeedbackID)).Scan(&space, &kb, &document, &revision, &state); err != nil || state != "received" || space == "" || kb == "" || document == "" || revision == "" {
		return empty, errors.New("feedback unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "kb.manage_feedback", SpaceID: contracts.ID(space), KnowledgeBaseIDs: []contracts.ID{contracts.ID(kb)}})
	if err != nil {
		return empty, errors.New("feedback proposal denied")
	}
	draftScope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "document.create", SpaceID: contracts.ID(space), KnowledgeBaseIDs: []contracts.ID{contracts.ID(kb)}, ResourceIDs: []contracts.ID{contracts.ID(document)}})
	if err != nil {
		return empty, errors.New("feedback proposal denied")
	}
	if scope.PolicyVersion == 0 || draftScope.PolicyVersion != scope.PolicyVersion {
		return empty, errors.New("feedback proposal policy changed")
	}
	proposal := proposalID(input.FeedbackID, who.SubjectID, contracts.ID(input.IdempotencyKey))
	draftID := proposalDraftID(proposal, contracts.ID(document))
	var raw []byte
	if err := s.db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.revision_contents WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND revision_id=$4::text`, space, kb, document, revision).Scan(&raw); err != nil || len(raw) == 0 || len(raw) > 8<<20 {
		return empty, errors.New("revision unavailable")
	}
	var source contracts.Revision
	if json.Unmarshal(raw, &source) != nil || source.ID != contracts.ID(revision) || source.DocumentID != contracts.ID(document) {
		return empty, errors.New("revision unavailable")
	}
	draft := contracts.Draft{ID: draftID, DocumentID: contracts.ID(document), BaseRevisionID: cloneProposalID(contracts.ID(revision)), Blobs: append([]contracts.BlobRef(nil), source.Blobs...), RowVersion: 1, State: "draft"}
	for _, block := range source.Blocks {
		draft.Blocks = append(draft.Blocks, contracts.DraftBlock{ID: block.ID, DraftID: draft.ID, RowVersion: 1, Order: block.Order, Content: block.Content})
	}
	draftBytes, err := json.Marshal(draft)
	if err != nil || len(draftBytes) > 8<<20 {
		return empty, errors.New("draft unavailable")
	}
	requestDigest := proposalRequestDigest(input.FeedbackID, input.Comment, input.IdempotencyKey)
	proposalBytes, _ := json.Marshal(map[string]any{"ID": proposal, "FeedbackID": input.FeedbackID, "DraftID": draftID, "Comment": strings.TrimSpace(input.Comment), "State": "proposed"})
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, errors.New("feedback proposal unavailable")
	}
	defer tx.Rollback()
	var existingDigest, existingDraft, existingState string
	var existingCreated time.Time
	err = tx.QueryRowContext(ctx, `SELECT request_digest,draft_id,state,created_at FROM akos_withdraw_v1.feedback_proposals WHERE space_id=$1 AND actor_id=$2 AND idempotency_key=$3 FOR UPDATE`, space, string(who.SubjectID), input.IdempotencyKey).Scan(&existingDigest, &existingDraft, &existingState, &existingCreated)
	if err == nil {
		if existingDigest != requestDigest {
			return empty, contracts.ErrFeedbackProposalConflict
		}
		if existingState != "proposed" || existingDraft == "" || existingCreated.IsZero() {
			return empty, errors.New("feedback proposal unavailable")
		}
		if err = tx.Commit(); err != nil {
			return empty, errors.New("feedback proposal outcome unknown")
		}
		return contracts.FeedbackProposal{ID: proposal, FeedbackID: input.FeedbackID, DraftID: contracts.ID(existingDraft), State: existingState, CreatedAt: existingCreated.UTC()}, nil
	}
	if err != sql.ErrNoRows {
		return empty, errors.New("feedback proposal unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_heads(space_id,kb_id,document_id,id,row_version,state,payload) VALUES($1,$2,$3,$4,1,'draft',$5::jsonb)`, space, kb, document, string(draftID), string(draftBytes)); err != nil {
		return empty, errors.New("draft unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_create_receipts(space_id,kb_id,document_id,draft_id,actor_id,command_key,receipt) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`, space, kb, document, string(draftID), string(who.SubjectID), "feedback-proposal:"+input.IdempotencyKey, string(draftBytes)); err != nil {
		return empty, errors.New("draft unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_create_audit(space_id,kb_id,document_id,draft_id,actor_id,request_id,action) VALUES($1,$2,$3,$4,$5,$6,'draft.created')`, space, kb, document, string(draftID), string(who.SubjectID), string(input.Request.RequestID)); err != nil {
		return empty, errors.New("draft unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_create_outbox(space_id,kb_id,draft_id,event_type,payload) VALUES($1,$2,$3,'draft.created',$4::jsonb)`, space, kb, string(draftID), string(draftBytes)); err != nil {
		return empty, errors.New("draft unavailable")
	}
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.feedback_proposals(id,space_id,kb_id,feedback_id,document_id,revision_id,draft_id,actor_id,request_id,idempotency_key,request_digest,comment,state,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'proposed',clock_timestamp())`, string(proposal), space, kb, string(input.FeedbackID), document, revision, string(draftID), string(who.SubjectID), string(input.Request.RequestID), input.IdempotencyKey, requestDigest, strings.TrimSpace(input.Comment)); err != nil {
		return empty, errors.New("feedback proposal unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.feedback_proposal_audit(id,proposal_id,feedback_id,space_id,kb_id,actor_id,request_id,action,payload) VALUES($1,$2,$3,$4,$5,$6,$7,'feedback.proposed',$8::jsonb)`, string(proposal), string(proposal), string(input.FeedbackID), space, kb, string(who.SubjectID), string(input.Request.RequestID), string(proposalBytes)); err != nil {
		return empty, errors.New("feedback proposal unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.feedback_proposal_outbox(id,proposal_id,feedback_id,space_id,kb_id,event_type,payload) VALUES($1,$2,$3,$4,$5,'feedback.proposed',$6::jsonb)`, string(proposal), string(proposal), string(input.FeedbackID), space, kb, string(proposalBytes)); err != nil {
		return empty, errors.New("feedback proposal unavailable")
	}
	if err = tx.Commit(); err != nil {
		return empty, errors.New("feedback proposal outcome unknown")
	}
	credential := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
	defer clear(credential.Secret)
	current, authErr := s.auth.Authenticate(ctx, credential)
	fresh, policyErr := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: current, Action: "kb.manage_feedback", SpaceID: contracts.ID(space), KnowledgeBaseIDs: []contracts.ID{contracts.ID(kb)}})
	if authErr != nil || policyErr != nil || queryread.MatchRefreshedScope(ctx, who, scope, current, fresh, []contracts.ID{contracts.ID(kb)}) != nil {
		return empty, errors.New("feedback proposal authorization changed")
	}
	return contracts.FeedbackProposal{ID: proposal, FeedbackID: input.FeedbackID, DraftID: draftID, State: "proposed", CreatedAt: now}, nil
}

func cloneProposalID(value contracts.ID) *contracts.ID { copy := value; return &copy }

func proposalRequestDigest(feedbackID contracts.ID, comment, key string) string {
	sum := sha256.Sum256([]byte(string(feedbackID) + "\x00" + comment + "\x00" + key))
	return hex.EncodeToString(sum[:])
}

var _ contracts.FeedbackProposalApplication = (*Search)(nil)
