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

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

// PublicationStarts registers the asynchronous preparation intent. It never
// treats registration as index readiness or changes the publication head.
type PublicationStarts struct {
	DB             *sql.DB
	Verify         VerifyScope
	AuthorizeStart func(context.Context, contracts.PublishRequest) error
	ProfileDigest  contracts.Digest
}

var _ document.PublicationStarter = PublicationStarts{}

func (p PublicationStarts) StartPublication(ctx context.Context, r contracts.PublishRequest) (document.PublicationStartResult, error) {
	result, err := p.start(ctx, r)
	if err == nil {
		return result, nil
	}
	if errors.Is(err, ErrCommitUnknown) {
		return document.PublicationStartResult{}, errors.Join(ErrCommitUnknown, document.ErrPublicationStartUnknown)
	}
	return document.PublicationStartResult{}, errors.Join(ErrRepository, document.ErrPublicationStartRejected)
}
func (p PublicationStarts) start(ctx context.Context, r contracts.PublishRequest) (document.PublicationStartResult, error) {
	var result document.PublicationStartResult
	c := r.Command
	if ctx == nil || p.DB == nil || p.AuthorizeStart == nil || !commandDigest(p.ProfileDigest) || c.Request.ProfileDigest != p.ProfileDigest || !validActorCommand(c, r.DocumentID, r.RevisionID, r.ApprovalID) || c.Scope.Action != "publication.start" || c.Scope.SubjectID != c.Principal.SubjectID || (c.Principal.SubjectType != "user" && c.Principal.SubjectType != "service") || c.Request.Deadline.IsZero() {
		return result, ErrRepository
	}
	if _, ok := ctx.Deadline(); !ok {
		return result, ErrRepository
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > 65536 {
		return result, ErrRepository
	}
	var owned contracts.PublishRequest
	if json.Unmarshal(raw, &owned) != nil {
		return result, ErrRepository
	}
	r = owned
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
		Space                        contracts.ID
		KBs                          []contracts.ID
		Actor                        contracts.Principal
		Document, Revision, Approval contracts.ID
		Epoch                        uint64
		Profile                      contracts.Digest
	}{c.Scope.SpaceID, c.Scope.KnowledgeBaseIDs, actor, r.DocumentID, r.RevisionID, r.ApprovalID, r.ExpectedEpoch, p.ProfileDigest})
	hash := sha256.Sum256(append([]byte("akos.pg-publication-start/1\n"), semantic...))
	digest := hex.EncodeToString(hash[:])
	verify := func(cx context.Context, scope contracts.AccessScope) error {
		if p.Verify == nil || p.Verify(cx, cloneScope(scope)) != nil {
			return ErrRepository
		}
		var copy contracts.PublishRequest
		if json.Unmarshal(raw, &copy) != nil || p.AuthorizeStart(cx, copy) != nil || cx.Err() != nil {
			return ErrRepository
		}
		return nil
	}
	err = withinSession(ctx, p.DB, c.Scope, verify, func(cx context.Context, s *session) error {
		if err := s.enter(cx, c.Scope); err != nil {
			return err
		}
		defer s.mu.Unlock()
		args := []any{string(c.Scope.SpaceID), string(c.Scope.KnowledgeBaseIDs[0]), string(r.DocumentID)}
		var epoch, version string
		var revision, set sql.NullString
		var deleted bool
		if s.tx.QueryRowContext(cx, `SELECT publication_epoch::text,row_version::text,active_revision_id,active_generation_set_id,deleted FROM akos_withdraw_v1.documents WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text FOR SHARE`, args...).Scan(&epoch, &version, &revision, &set, &deleted) != nil || deleted {
			return ErrRepository
		}
		var oldDigest string
		var receipt []byte
		err := s.tx.QueryRowContext(cx, `SELECT request_digest,receipt FROM akos_withdraw_v1.publication_intents WHERE space_id=$1::text AND kb_id=$2::text AND actor_id=$3::text AND command_key=$4::text`, args[0], args[1], string(c.Principal.SubjectID), c.IdempotencyKey).Scan(&oldDigest, &receipt)
		if err == nil {
			if len(receipt) > 131072 || oldDigest != digest || json.Unmarshal(receipt, &result) != nil || result.Operation.DocumentID != r.DocumentID || result.Operation.RevisionID != r.RevisionID || result.Operation.ApprovalID != r.ApprovalID || result.Job.OperationID != result.Operation.ID || result.Operation.ID == "" || result.Job.ID == "" {
				return ErrRepository
			}
			return nil
		}
		if err != sql.ErrNoRows {
			return ErrRepository
		}
		publicationEpoch, err := strconv.ParseUint(epoch, 10, 64)
		if err != nil {
			return ErrRepository
		}
		rowVersion, err := strconv.ParseUint(version, 10, 64)
		if err != nil {
			return ErrRepository
		}
		doc := contracts.Document{ID: r.DocumentID, SpaceID: c.Scope.SpaceID, KnowledgeBaseID: c.Scope.KnowledgeBaseIDs[0], PublicationEpoch: publicationEpoch, RowVersion: rowVersion, ActiveRevisionID: idPointer(revision), ActiveGenerationSetID: idPointer(set)}
		var requestBytes, approvalBytes []byte
		var format, blocksHash, contentHash string
		var head sql.NullString
		if s.tx.QueryRowContext(cx, `SELECT q.payload,q.current_decision_id,rc.hash_format,rc.blocks_hash,rc.content_hash FROM akos_withdraw_v1.approval_requests q JOIN akos_withdraw_v1.revision_contents rc ON rc.space_id=q.space_id AND rc.kb_id=q.kb_id AND rc.document_id=q.document_id AND rc.revision_id=q.revision_id WHERE q.space_id=$1::text AND q.kb_id=$2::text AND q.document_id=$3::text AND q.revision_id=$4::text FOR SHARE OF q`, append(args, string(r.RevisionID))...).Scan(&requestBytes, &head, &format, &blocksHash, &contentHash) != nil || !head.Valid || head.String != string(r.ApprovalID) {
			return ErrRepository
		}
		var request contracts.ApprovalRequest
		var approval contracts.Approval
		if json.Unmarshal(requestBytes, &request) != nil || s.tx.QueryRowContext(cx, `SELECT payload FROM akos_withdraw_v1.review_decisions WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND revision_id=$4::text AND id=$5::text`, append(args, string(r.RevisionID), string(r.ApprovalID))...).Scan(&approvalBytes) != nil || json.Unmarshal(approvalBytes, &approval) != nil {
			return ErrRepository
		}
		var policyBytes []byte
		var policyVersion, policyHeadVersion, policyDigest string
		if s.tx.QueryRowContext(cx, `SELECT h.policy_version::text,h.row_version::text,p.digest,p.payload FROM akos_withdraw_v1.review_policy_heads h JOIN akos_withdraw_v1.review_policy_snapshots p ON p.space_id=h.space_id AND p.kb_id=h.kb_id AND p.version=h.policy_version WHERE h.space_id=$1::text AND h.kb_id=$2::text FOR SHARE OF h`, args[:2]...).Scan(&policyVersion, &policyHeadVersion, &policyDigest, &policyBytes) != nil {
			return ErrRepository
		}
		var policy document.ReviewPolicySnapshot
		if json.Unmarshal(policyBytes, &policy) != nil || strconv.FormatUint(policy.Version, 10) != policyVersion || string(policy.Digest) != policyDigest {
			return ErrRepository
		}
		policyHead, err := strconv.ParseUint(policyHeadVersion, 10, 64)
		if err != nil {
			return ErrRepository
		}
		view, err := document.CurrentApproval(contracts.FrozenRevisionFact{ID: r.RevisionID, DocumentID: r.DocumentID, HashFormat: format, BlocksHash: contracts.Digest(blocksHash), ContentHash: contracts.Digest(contentHash)}, request, contracts.ID(head.String), approval, r.ApprovalID, policy)
		if err != nil {
			return ErrRepository
		}
		var backendBytes []byte
		var backendHeadVersion, backendID string
		if s.tx.QueryRowContext(cx, `SELECT h.row_version::text,h.backend_epoch_id,e.payload FROM akos_withdraw_v1.publication_backend_heads h JOIN akos_withdraw_v1.publication_backend_epochs e ON e.space_id=h.space_id AND e.kb_id=h.kb_id AND e.id=h.backend_epoch_id WHERE h.space_id=$1::text AND h.kb_id=$2::text FOR SHARE OF h`, args[:2]...).Scan(&backendHeadVersion, &backendID, &backendBytes) != nil {
			return ErrRepository
		}
		var backend document.PublicationBackendSnapshot
		if json.Unmarshal(backendBytes, &backend) != nil || string(backend.EpochID) != backendID {
			return ErrRepository
		}
		backendHead, err := strconv.ParseUint(backendHeadVersion, 10, 64)
		if err != nil {
			return ErrRepository
		}
		operationID, err := submissionID("publication-")
		if err != nil {
			return err
		}
		jobID, err := submissionID("publish-job-")
		if err != nil {
			return err
		}
		plan, err := document.PreparePublicationStart(r, doc, view, policy, policyHead, backend, backendHead, operationID, jobID, time.Now().UTC())
		if err != nil {
			return ErrRepository
		}
		result = plan.Result
		planBytes, _ := json.Marshal(plan)
		receipt, _ = json.Marshal(result)
		jobBytes, _ := json.Marshal(result.Job)
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.publication_intents(id,space_id,kb_id,document_id,revision_id,approval_id,expected_epoch,actor_id,command_key,request_digest,review_policy_version,backend_epoch_id,job_id,payload,receipt) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::numeric,$8::text,$9::text,$10::text,$11::numeric,$12::text,$13::text,$14::jsonb,$15::jsonb)`, string(operationID), args[0], args[1], args[2], string(r.RevisionID), string(r.ApprovalID), epoch, string(c.Principal.SubjectID), c.IdempotencyKey, digest, strconv.FormatUint(policy.Version, 10), string(backend.EpochID), string(jobID), string(planBytes), string(receipt)); err != nil {
			return ErrRepository
		}
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.publication_queued_jobs(space_id,kb_id,id,operation_id,payload) VALUES($1::text,$2::text,$3::text,$4::text,$5::jsonb)`, args[0], args[1], string(jobID), string(operationID), string(jobBytes)); err != nil {
			return ErrRepository
		}
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.publication_task_heads(space_id,kb_id,id,row_version,state,attempt,fencing_token,payload) VALUES($1::text,$2::text,$3::text,1,'queued',0,0,$4::jsonb)`, args[0], args[1], string(jobID), string(jobBytes)); err != nil {
			return ErrRepository
		}
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.publication_start_audit(space_id,kb_id,operation_id,actor_id,request_id) VALUES($1::text,$2::text,$3::text,$4::text,$5::text)`, args[0], args[1], string(operationID), string(c.Principal.SubjectID), string(c.Request.RequestID)); err != nil {
			return ErrRepository
		}
		if _, err = s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.publication_prepare_outbox(space_id,kb_id,operation_id,job_id,event_type) VALUES($1::text,$2::text,$3::text,$4::text,'publication.prepare.requested')`, args[0], args[1], string(operationID), string(jobID)); err != nil {
			return ErrRepository
		}
		return nil
	})
	if err != nil {
		return document.PublicationStartResult{}, err
	}
	return result, nil
}
