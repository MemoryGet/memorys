package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

// Reviews persists human-authorized review decisions. It does not implement
// publication, current-policy validity evaluation, or the full metadata UoW.
type Reviews struct {
	DB     *sql.DB
	Verify VerifyScope
	// Must verify current Owner/Admin (or personal owner), explicit human intent,
	// exact document/revision authority, and trusted delegation for service actors.
	AuthorizeDecision   func(context.Context, document.ReviewCommand) error
	ProfileDigest       contracts.Digest
	ReviewPolicyVersion uint64
}

var _ document.ReviewRepository = Reviews{}

func (p Reviews) DecideReview(ctx context.Context, command document.ReviewCommand) (contracts.Approval, error) {
	result, err := p.decide(ctx, command)
	if err == nil {
		return result, nil
	}
	if errors.Is(err, ErrCommitUnknown) {
		return contracts.Approval{}, errors.Join(ErrCommitUnknown, document.ErrReviewUnknown)
	}
	return contracts.Approval{}, errors.Join(document.ErrReviewRejected, ErrRepository)
}

func (p Reviews) decide(ctx context.Context, command document.ReviewCommand) (contracts.Approval, error) {
	var result contracts.Approval
	r := command.Request
	c := r.Command
	if ctx == nil || p.DB == nil || p.AuthorizeDecision == nil || p.ReviewPolicyVersion == 0 || !commandDigest(p.ProfileDigest) || c.Request.ProfileDigest != p.ProfileDigest || !validActorCommand(c, command.DocumentID, r.RevisionID) || c.Scope.Action != "revision.approve" || c.Principal.SubjectID != c.Scope.SubjectID || c.Request.Deadline.IsZero() || (c.Principal.SubjectType != "user" && c.Principal.SubjectType != "service") || (c.Principal.SubjectType == "service" && c.Principal.Delegation == nil) || !utf8.ValidString(r.Reason) || utf8.RuneCountInString(r.Reason) > 10000 || (r.Decision != "approve" && r.Decision != "reject" && r.Decision != "revoke") {
		return result, ErrRepository
	}
	if _, ok := ctx.Deadline(); !ok {
		return result, ErrRepository
	}
	raw, err := json.Marshal(command)
	if err != nil || len(raw) > 131072 {
		return result, ErrRepository
	}
	var owned document.ReviewCommand
	if json.Unmarshal(raw, &owned) != nil {
		return result, ErrRepository
	}
	command = owned
	r = command.Request
	c = r.Command
	ctx, cancel := context.WithDeadline(ctx, c.Request.Deadline)
	defer cancel()
	actor := c.Principal
	if actor.Delegation != nil {
		d := *actor.Delegation
		d.ProofReference = ""
		actor.Delegation = &d
	}
	semantic, _ := json.Marshal(struct {
		Space              contracts.ID
		KBs                []contracts.ID
		Actor              contracts.Principal
		Document, Revision contracts.ID
		Version, Policy    uint64
		Profile            contracts.Digest
		Decision, Reason   string
	}{c.Scope.SpaceID, c.Scope.KnowledgeBaseIDs, actor, command.DocumentID, r.RevisionID, r.ExpectedApprovalVersion, p.ReviewPolicyVersion, p.ProfileDigest, r.Decision, r.Reason})
	hash := sha256.Sum256(append([]byte("akos.pg-review/1\n"), semantic...))
	digest := hex.EncodeToString(hash[:])
	verify := func(cx context.Context, s contracts.AccessScope) error {
		if p.Verify == nil || p.Verify(cx, cloneScope(s)) != nil {
			return ErrRepository
		}
		var copy document.ReviewCommand
		if json.Unmarshal(raw, &copy) != nil || p.AuthorizeDecision(cx, copy) != nil || cx.Err() != nil {
			return ErrRepository
		}
		return nil
	}
	err = withinSession(ctx, p.DB, c.Scope, verify, func(cx context.Context, s *session) error {
		if err := s.enter(cx, c.Scope); err != nil {
			return err
		}
		defer s.mu.Unlock()
		args := []any{string(c.Scope.SpaceID), string(c.Scope.KnowledgeBaseIDs[0]), string(command.DocumentID), string(r.RevisionID)}
		var alive bool
		if s.tx.QueryRowContext(cx, `SELECT NOT deleted FROM akos_withdraw_v1.documents WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text FOR SHARE`, args[:3]...).Scan(&alive) != nil || !alive {
			return ErrRepository
		}
		var oldDigest string
		var receipt []byte
		err := s.tx.QueryRowContext(cx, `SELECT request_digest,receipt FROM akos_withdraw_v1.review_receipts WHERE space_id=$1::text AND kb_id=$2::text AND actor_id=$3::text AND command_key=$4::text`, args[0], args[1], string(c.Principal.SubjectID), c.IdempotencyKey).Scan(&oldDigest, &receipt)
		if err == nil {
			if len(receipt) > 131072 || oldDigest != digest || json.Unmarshal(receipt, &result) != nil || result.ID == "" || result.RevisionID != r.RevisionID || result.Decision != r.Decision || result.ActorID != c.Principal.SubjectID {
				return ErrRepository
			}
			return nil
		}
		if err != sql.ErrNoRows {
			return ErrRepository
		}
		var payload []byte
		var version, format, blocksHash, contentHash string
		var current sql.NullString
		if s.tx.QueryRowContext(cx, `SELECT q.payload,q.row_version::text,rc.hash_format,rc.blocks_hash,rc.content_hash,q.current_decision_id FROM akos_withdraw_v1.approval_requests q JOIN akos_withdraw_v1.revision_contents rc ON rc.space_id=q.space_id AND rc.kb_id=q.kb_id AND rc.document_id=q.document_id AND rc.revision_id=q.revision_id JOIN akos_withdraw_v1.revisions rev ON rev.space_id=rc.space_id AND rev.kb_id=rc.kb_id AND rev.document_id=rc.document_id AND rev.id=rc.revision_id AND rev.content_digest=rc.content_hash WHERE q.space_id=$1::text AND q.kb_id=$2::text AND q.document_id=$3::text AND q.revision_id=$4::text FOR UPDATE OF q`, args...).Scan(&payload, &version, &format, &blocksHash, &contentHash, &current) != nil {
			return ErrRepository
		}
		var request contracts.ApprovalRequest
		if json.Unmarshal(payload, &request) != nil || strconv.FormatUint(request.RowVersion, 10) != version || request.DocumentID != command.DocumentID || request.RevisionID != r.RevisionID {
			return ErrRepository
		}
		if request.State == "submitted" {
			if current.Valid {
				return ErrRepository
			}
		} else {
			var previous []byte
			var prior contracts.Approval
			if !current.Valid || s.tx.QueryRowContext(cx, `SELECT payload FROM akos_withdraw_v1.review_decisions WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND revision_id=$4::text AND id=$5::text`, append(args, current.String)...).Scan(&previous) != nil || json.Unmarshal(previous, &prior) != nil || prior.ID != contracts.ID(current.String) || prior.RowVersion != request.RowVersion || prior.RevisionID != request.RevisionID || prior.ContentHash != request.ContentHash || prior.PolicyVersion != request.PolicyVersion || prior.Decision != "approve" {
				return ErrRepository
			}
		}
		id, err := submissionID("decision-")
		if err != nil {
			return err
		}
		prepared, err := document.PrepareReview(request, contracts.FrozenRevisionFact{ID: r.RevisionID, DocumentID: command.DocumentID, HashFormat: format, BlocksHash: contracts.Digest(blocksHash), ContentHash: contracts.Digest(contentHash)}, r.ExpectedApprovalVersion, id, c.Principal.SubjectID, r.Decision, r.Reason, p.ReviewPolicyVersion, time.Now().UTC())
		if err != nil {
			return ErrRepository
		}
		result = prepared.Decision
		decisionBytes, _ := json.Marshal(result)
		requestBytes, _ := json.Marshal(prepared.Request)
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.review_decisions(space_id,kb_id,document_id,revision_id,approval_request_id,id,row_version,content_hash,payload) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::numeric,$8::text,$9::jsonb)`, append(args, string(request.ID), string(id), strconv.FormatUint(result.RowVersion, 10), string(result.ContentHash), string(decisionBytes))...); err != nil {
			return ErrRepository
		}
		updated, err := s.tx.ExecContext(cx, `UPDATE akos_withdraw_v1.approval_requests SET row_version=$6::numeric,state=$7::text,current_decision_id=$8::text,payload=$9::jsonb WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND revision_id=$4::text AND row_version=$5::numeric`, append(args, version, strconv.FormatUint(prepared.Request.RowVersion, 10), prepared.Request.State, string(id), string(requestBytes))...)
		if err != nil {
			return ErrRepository
		}
		if n, err := updated.RowsAffected(); err != nil || n != 1 {
			return ErrRepository
		}
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.review_receipts(space_id,kb_id,actor_id,command_key,decision_id,profile_digest,review_policy_version,request_digest,receipt) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::numeric,$8::text,$9::jsonb)`, args[0], args[1], string(c.Principal.SubjectID), c.IdempotencyKey, string(id), string(p.ProfileDigest), strconv.FormatUint(p.ReviewPolicyVersion, 10), digest, string(decisionBytes)); err != nil {
			return ErrRepository
		}
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.review_audit(space_id,kb_id,decision_id,actor_id,request_id) VALUES($1::text,$2::text,$3::text,$4::text,$5::text)`, args[0], args[1], string(id), string(c.Principal.SubjectID), string(c.Request.RequestID)); err != nil {
			return ErrRepository
		}
		event := map[string]string{"approve": "revision.approved", "reject": "revision.rejected", "revoke": "revision.approval_revoked"}[r.Decision]
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.review_outbox(space_id,kb_id,decision_id,event_type,payload) VALUES($1::text,$2::text,$3::text,$4::text,$5::jsonb)`, args[0], args[1], string(id), event, string(decisionBytes)); err != nil {
			return ErrRepository
		}
		return nil
	})
	if err != nil {
		return contracts.Approval{}, err
	}
	return result, nil
}
