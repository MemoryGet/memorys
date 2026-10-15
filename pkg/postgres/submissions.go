package postgres

import (
	"context"
	"crypto/rand"
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

// Submissions is the synchronous submit transaction, not the entire metadata
// UoW or approval/publication service. Callbacks must verify current authority
// and original/media bytes against trusted resources, never client assertions.
type Submissions struct {
	DB     *sql.DB
	Verify VerifyScope
	// ReviewPolicyVersion is trusted review configuration, distinct from ACL.
	ReviewPolicyVersion uint64
	ProfileDigest       contracts.Digest // Trusted immutable pipeline/config binding.
	AuthorizeSubmission func(context.Context, document.SubmissionCommand) error
	VerifyContent       func(context.Context, contracts.AccessScope, contracts.Draft) error
}

var _ document.SubmissionRepository = Submissions{}

func (p Submissions) SubmitDraft(ctx context.Context, command document.SubmissionCommand) (contracts.SubmitResult, error) {
	result, err := p.submit(ctx, command)
	if err == nil {
		return result, nil
	}
	if errors.Is(err, ErrCommitUnknown) {
		return contracts.SubmitResult{}, errors.Join(ErrCommitUnknown, document.ErrSubmissionUnknown)
	}
	return contracts.SubmitResult{}, errors.Join(document.ErrSubmissionRejected, ErrRepository)
}

func (p Submissions) submit(ctx context.Context, command document.SubmissionCommand) (contracts.SubmitResult, error) {
	var result contracts.SubmitResult
	if ctx == nil || p.DB == nil || p.AuthorizeSubmission == nil || p.VerifyContent == nil || p.ReviewPolicyVersion == 0 || !commandDigest(p.ProfileDigest) || command.Request.Command.Request.ProfileDigest != p.ProfileDigest || !validSubmissionCommand(command) {
		return result, ErrRepository
	}
	if _, ok := ctx.Deadline(); !ok {
		return result, ErrRepository
	}
	raw, err := json.Marshal(command)
	if err != nil || len(raw) > 65536 {
		return result, ErrRepository
	}
	var owned document.SubmissionCommand
	if json.Unmarshal(raw, &owned) != nil {
		return result, ErrRepository
	}
	command = owned
	r := command.Request
	c := r.Command
	if c.Scope.Action != "document.submit" || c.Scope.SubjectID != c.Principal.SubjectID || c.Principal.Issuer == "" || (c.Principal.SubjectType != "user" && c.Principal.SubjectType != "service") || command.DocumentID == "" || r.DraftID == "" || r.ExpectedVersion == 0 || c.Request.RequestID == "" || c.Request.Deadline.IsZero() || !validSubmissionKey(c.IdempotencyKey) {
		return result, ErrRepository
	}
	ctx, cancel := context.WithDeadline(ctx, c.Request.Deadline)
	defer cancel()
	ctx, scopeCancel := context.WithDeadline(ctx, c.Scope.ExpiresAt)
	defer scopeCancel()
	// Stable actor semantics and every command parameter, excluding retry-local
	// token/scope IDs, request IDs and deadlines. Private adapter digest format.
	actor := c.Principal
	if actor.Delegation != nil {
		delegation := *actor.Delegation
		delegation.ProofReference = "" // Authentication proof rotates; delegated identity does not.
		actor.Delegation = &delegation
	}
	semantic, _ := json.Marshal(struct {
		Space                 contracts.ID
		KBs                   []contracts.ID
		Principal             contracts.Principal
		Document, Draft       contracts.ID
		Version, ReviewPolicy uint64
		Profile               contracts.Digest
	}{c.Scope.SpaceID, c.Scope.KnowledgeBaseIDs, actor, command.DocumentID, r.DraftID, r.ExpectedVersion, p.ReviewPolicyVersion, c.Request.ProfileDigest})
	digest := sha256.Sum256(append([]byte("akos.pg-submission/1\n"), semantic...))
	digestText := hex.EncodeToString(digest[:])
	verify := func(cx context.Context, scope contracts.AccessScope) error {
		if p.Verify == nil || p.Verify(cx, cloneScope(scope)) != nil {
			return ErrRepository
		}
		var copy document.SubmissionCommand
		if json.Unmarshal(raw, &copy) != nil || p.AuthorizeSubmission(cx, copy) != nil || cx.Err() != nil {
			return ErrRepository
		}
		return nil
	}
	if verify(ctx, c.Scope) != nil {
		return result, ErrRepository
	}
	// Byte verification can call a remote blob store. It must finish before the
	// write transaction, whose lock later pins this exact draft snapshot by CAS
	// and payload checksum. Immutable blobs are addressed by platform ID+SHA.
	var checkedPayload []byte
	var checkedVersion string
	checkedErr := p.DB.QueryRowContext(ctx, `SELECT d.payload,d.row_version::text FROM akos_withdraw_v1.draft_heads d JOIN akos_withdraw_v1.documents doc ON doc.space_id=d.space_id AND doc.kb_id=d.kb_id AND doc.id=d.document_id WHERE d.space_id=$1::text AND d.kb_id=$2::text AND d.document_id=$3::text AND d.id=$4::text AND d.state='draft' AND d.row_version=$5::numeric AND NOT doc.deleted`, string(c.Scope.SpaceID), string(c.Scope.KnowledgeBaseIDs[0]), string(command.DocumentID), string(r.DraftID), strconv.FormatUint(r.ExpectedVersion, 10)).Scan(&checkedPayload, &checkedVersion)
	verified := false
	if checkedErr == nil {
		var checked contracts.Draft
		if json.Unmarshal(checkedPayload, &checked) != nil || checked.ID != r.DraftID || checked.DocumentID != command.DocumentID || checked.State != "draft" || checked.RowVersion != r.ExpectedVersion || checkedVersion != strconv.FormatUint(r.ExpectedVersion, 10) || p.VerifyContent(ctx, cloneScope(c.Scope), checked) != nil || ctx.Err() != nil {
			return result, ErrRepository
		}
		verified = true
	} else if checkedErr != sql.ErrNoRows {
		return result, ErrRepository
	}
	checkedDigest := sha256.Sum256(checkedPayload)
	err = withinSession(ctx, p.DB, c.Scope, verify, func(cx context.Context, s *session) error {
		if err := s.enter(cx, c.Scope); err != nil {
			return err
		}
		defer s.mu.Unlock()
		args := []any{string(c.Scope.SpaceID), string(c.Scope.KnowledgeBaseIDs[0]), string(command.DocumentID)}
		var alive bool
		if s.tx.QueryRowContext(cx, `SELECT NOT deleted FROM akos_withdraw_v1.documents WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text FOR SHARE`, args...).Scan(&alive) != nil || !alive {
			return ErrRepository
		}
		var previousDigest string
		var receipt []byte
		err := s.tx.QueryRowContext(cx, `SELECT request_digest,receipt FROM akos_withdraw_v1.submission_receipts WHERE space_id=$1::text AND kb_id=$2::text AND actor_id=$3::text AND command_key=$4::text`, args[0], args[1], string(c.Principal.SubjectID), c.IdempotencyKey).Scan(&previousDigest, &receipt)
		if err == nil {
			if previousDigest != digestText || json.Unmarshal(receipt, &result) != nil || result.Revision.DocumentID != command.DocumentID || result.Revision.ID == "" || result.ApprovalRequestID == "" {
				return ErrRepository
			}
			return nil
		}
		if err != sql.ErrNoRows {
			return ErrRepository
		}
		var payload []byte
		var version string
		if s.tx.QueryRowContext(cx, `SELECT payload,row_version::text FROM akos_withdraw_v1.draft_heads WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND id=$4::text FOR UPDATE`, append(args, string(r.DraftID))...).Scan(&payload, &version) != nil {
			return ErrRepository
		}
		var draft contracts.Draft
		if !verified || sha256.Sum256(payload) != checkedDigest || json.Unmarshal(payload, &draft) != nil || draft.ID != r.DraftID || draft.DocumentID != command.DocumentID || strconv.FormatUint(draft.RowVersion, 10) != version || draft.RowVersion != r.ExpectedVersion {
			return ErrRepository
		}
		if draft.BaseRevisionID != nil {
			var parent string
			if s.tx.QueryRowContext(cx, `SELECT id FROM akos_withdraw_v1.revisions WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND id=$4::text`, append(args, string(*draft.BaseRevisionID))...).Scan(&parent) != nil {
				return ErrRepository
			}
		}
		revisionID, err := submissionID("revision-")
		if err != nil {
			return err
		}
		approvalID, err := submissionID("review-")
		if err != nil {
			return err
		}
		prepared, err := document.PrepareSubmission(draft, r.ExpectedVersion, revisionID, approvalID, c.Principal.SubjectID, p.ReviewPolicyVersion, time.Now().UTC())
		if err != nil {
			return err
		}
		revisionBytes, _ := json.Marshal(prepared.Revision)
		approvalBytes, _ := json.Marshal(prepared.Approval)
		draftBytes, _ := json.Marshal(prepared.Draft)
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.revisions VALUES($1::text,$2::text,$3::text,$4::text,$5::text)`, append(args, string(revisionID), string(prepared.Frozen.ContentHash))...); err != nil {
			return ErrRepository
		}
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.revision_contents(space_id,kb_id,document_id,revision_id,hash_format,blocks_hash,content_hash,frozen_payload,payload) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::text,$8::bytea,$9::jsonb)`, append(args, string(revisionID), prepared.Frozen.HashFormat, string(prepared.Frozen.BlocksHash), string(prepared.Frozen.ContentHash), prepared.Frozen.Payload, string(revisionBytes))...); err != nil {
			return ErrRepository
		}
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.approval_requests(space_id,kb_id,document_id,id,revision_id,row_version,state,payload) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,0,'submitted',$6::jsonb)`, append(args, string(approvalID), string(revisionID), string(approvalBytes))...); err != nil {
			return ErrRepository
		}
		updated, err := s.tx.ExecContext(cx, `UPDATE akos_withdraw_v1.draft_heads SET state='submitted',row_version=$6::numeric,payload=$7::jsonb WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND id=$4::text AND row_version=$5::numeric AND state='draft'`, append(args, string(r.DraftID), version, strconv.FormatUint(prepared.Draft.RowVersion, 10), string(draftBytes))...)
		if err != nil {
			return ErrRepository
		}
		if n, err := updated.RowsAffected(); err != nil || n != 1 {
			return ErrRepository
		}
		result = contracts.SubmitResult{Revision: prepared.Revision, ApprovalRequestID: approvalID}
		receipt, err = json.Marshal(result)
		if err != nil {
			return ErrRepository
		}
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.submission_receipts(space_id,kb_id,actor_id,command_key,document_id,draft_id,revision_id,approval_id,profile_digest,review_policy_version,request_digest,receipt) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::text,$8::text,$9::text,$10::numeric,$11::text,$12::jsonb)`, args[0], args[1], string(c.Principal.SubjectID), c.IdempotencyKey, args[2], string(r.DraftID), string(revisionID), string(approvalID), string(p.ProfileDigest), strconv.FormatUint(p.ReviewPolicyVersion, 10), digestText, string(receipt)); err != nil {
			return ErrRepository
		}
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.submission_audit(space_id,kb_id,revision_id,actor_id,request_id) VALUES($1::text,$2::text,$3::text,$4::text,$5::text)`, args[0], args[1], string(revisionID), string(c.Principal.SubjectID), string(c.Request.RequestID)); err != nil {
			return ErrRepository
		}
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.submission_outbox(space_id,kb_id,revision_id,payload) VALUES($1::text,$2::text,$3::text,$4::jsonb)`, args[0], args[1], string(revisionID), string(receipt)); err != nil {
			return ErrRepository
		}
		return nil
	})
	if err != nil {
		return contracts.SubmitResult{}, err
	}
	return result, nil
}

func validSubmissionKey(key string) bool {
	return utf8.ValidString(key) && len(key) > 0 && utf8.RuneCountInString(key) <= 200
}

func validSubmissionCommand(command document.SubmissionCommand) bool {
	return validActorCommand(command.Request.Command, command.DocumentID, command.Request.DraftID)
}
func validActorCommand(c contracts.CommandContext, resources ...contracts.ID) bool {
	values := []string{string(c.Principal.SubjectID), string(c.Scope.SpaceID), string(c.Scope.SubjectID), string(c.Scope.ScopeID), string(c.Request.RequestID), c.IdempotencyKey}
	for _, resource := range resources {
		values = append(values, string(resource))
	}
	for _, value := range values {
		if !validSubmissionKey(value) {
			return false
		}
	}
	if !utf8.ValidString(c.Principal.Issuer) || c.Principal.Issuer == "" || len(c.Principal.Issuer) > 4096 || !commandDigest(c.Request.ProfileDigest) || len(c.Scope.KnowledgeBaseIDs) != 1 || !validSubmissionKey(string(c.Scope.KnowledgeBaseIDs[0])) {
		return false
	}
	if c.Principal.ClientID != "" && !validSubmissionKey(string(c.Principal.ClientID)) {
		return false
	}
	if d := c.Principal.Delegation; d != nil {
		if !validSubmissionKey(string(d.UserID)) || !validSubmissionKey(string(d.ClientID)) || d.ClientID != c.Principal.ClientID || d.ProofReference == "" || !utf8.ValidString(d.ProofReference) || len(d.ProofReference) > 8192 {
			return false
		}
	}
	return true
}
func submissionID(prefix string) (contracts.ID, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", ErrRepository
	}
	return contracts.ID(prefix + hex.EncodeToString(raw[:])), nil
}
