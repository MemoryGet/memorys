package localruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/ingestion"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

func uploadDocumentID(kb, actor, key, digest string) contracts.ID {
	sum := sha256.Sum256([]byte("document-upload\x00" + string(kb) + "\x00" + string(actor) + "\x00" + key + "\x00" + digest))
	return contracts.ID("doc-" + hex.EncodeToString(sum[:]))
}

func uploadDraftID(document, actor, key string) contracts.ID {
	sum := sha256.Sum256([]byte("document-upload-draft\x00" + document + "\x00" + actor + "\x00" + key))
	return contracts.ID("draft-" + hex.EncodeToString(sum[:]))
}

func uploadJobID(draft, actor, key string) contracts.ID {
	sum := sha256.Sum256([]byte("document-upload-parse\x00" + draft + "\x00" + actor + "\x00" + key))
	return contracts.ID("parse-job-" + hex.EncodeToString(sum[:]))
}

// UploadDocument performs the management ingest transaction: authenticate,
// authorize, persist the document and source draft, store the original, then
// enqueue a durable parse task. The parser owns candidate generation; this
// method never treats the uploaded bytes as approved content.
func (s *Search) UploadDocument(ctx context.Context, input contracts.DocumentUploadInput, reader io.Reader) (contracts.DocumentUploadResult, error) {
	var empty contracts.DocumentUploadResult
	if s == nil || s.db == nil || s.auth == nil || s.blobs == nil || ctx == nil || reader == nil || input.KnowledgeBaseID == "" || input.Size < 0 || input.Size > 64<<20 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 200 || input.MediaType == "" || len(input.ExpectedSHA256) != 64 || strings.TrimSpace(input.Title) == "" && len(input.Title) > 0 {
		return empty, errors.New("document upload unavailable")
	}
	if _, _, err := mime.ParseMediaType(input.MediaType); err != nil {
		return empty, errors.New("document upload unavailable")
	}
	if decoded, err := hex.DecodeString(string(input.ExpectedSHA256)); err != nil || len(decoded) != 32 || strings.ToLower(string(input.ExpectedSHA256)) != string(input.ExpectedSHA256) {
		return empty, errors.New("document upload unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil || ctx.Err() != nil {
		return empty, errors.New("document upload unauthenticated")
	}
	version, profileDigest, err := s.ProfileIdentity()
	if err != nil || version == 0 || profileDigest != input.Request.ProfileDigest {
		return empty, errors.New("document upload profile changed")
	}
	var space string
	if err := s.db.QueryRowContext(ctx, `SELECT g.space_id FROM akos_withdraw_v1.access_policy_heads h JOIN akos_withdraw_v1.access_policy_grants g ON g.space_id=h.space_id AND g.version=h.version WHERE g.issuer=$1 AND g.subject_id=$2 AND g.client_id=$3 AND g.kb_id=$4 AND g.action='document.create' ORDER BY g.space_id LIMIT 1`, who.Issuer, string(who.SubjectID), string(who.ClientID), string(input.KnowledgeBaseID)).Scan(&space); err != nil || space == "" {
		return empty, errors.New("knowledge base unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	createScope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "document.create", SpaceID: contracts.ID(space), KnowledgeBaseIDs: []contracts.ID{input.KnowledgeBaseID}})
	if err != nil {
		return empty, errors.New("document upload denied")
	}
	parseScope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "document.parse", SpaceID: contracts.ID(space), KnowledgeBaseIDs: []contracts.ID{input.KnowledgeBaseID}})
	if err != nil {
		return empty, errors.New("document parse denied")
	}
	if parseScope.PolicyVersion != createScope.PolicyVersion {
		return empty, errors.New("document upload policy changed")
	}
	documentID := uploadDocumentID(string(input.KnowledgeBaseID), string(who.SubjectID), input.IdempotencyKey, string(input.ExpectedSHA256))
	draftID := uploadDraftID(string(documentID), string(who.SubjectID), input.IdempotencyKey)
	jobID := uploadJobID(string(draftID), string(who.SubjectID), input.IdempotencyKey)
	blob, err := s.blobs.Put(ctx, contracts.BlobWriteRequest{Request: input.Request, Scope: parseScope, ExpectedSHA256: input.ExpectedSHA256, Size: input.Size, MediaType: input.MediaType, IdempotencyKey: input.IdempotencyKey}, reader)
	if err != nil {
		return empty, errors.New("document upload failed")
	}
	cleanupBlob := true
	defer func() {
		if cleanupBlob {
			_ = s.blobs.Delete(context.Background(), contracts.BlobReadRequest{Request: input.Request, Scope: parseScope, Blob: blob})
		}
	}()
	draft := contracts.Draft{ID: draftID, DocumentID: documentID, Blobs: []contracts.BlobRef{blob}, RowVersion: 1, State: "draft"}
	draftBytes, err := json.Marshal(draft)
	if err != nil || len(draftBytes) > 8<<20 {
		return empty, errors.New("draft unavailable")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, errors.New("document upload unavailable")
	}
	defer tx.Rollback()
	var existingDoc string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM akos_withdraw_v1.documents WHERE id=$1::text`, string(documentID)).Scan(&existingDoc); err == nil {
		var existingDraft []byte
		if tx.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.draft_heads WHERE id=$1::text`, string(draftID)).Scan(&existingDraft) == nil && json.Unmarshal(existingDraft, &draft) == nil {
			if tx.Commit() != nil {
				return empty, errors.New("document upload outcome unknown")
			}
			cleanupBlob = false
			job := contracts.Job{ID: jobID, SpaceID: contracts.ID(space), Stage: "document.parse", ProfileDigest: profileDigest, State: "queued", Attempt: 1, RowVersion: 1, Progress: 0}
			command := ingestion.DraftParseCommand{Request: input.Request, Principal: who, Scope: parseScope, DocumentID: documentID, DraftID: draftID, CandidateID: parseJobID("candidate", draftID, input.IdempotencyKey, who.SubjectID), ExpectedVersion: draft.RowVersion, Input: blob, Format: parseFormat(blob.MediaType), Options: contracts.ParseOptions{EnableOCR: true}}
			if err = ensureUploadParseTask(ctx, s.db, contracts.ID(space), input.KnowledgeBaseID, job, command); err != nil {
				return empty, err
			}
			return contracts.DocumentUploadResult{Document: contracts.Document{ID: documentID, SpaceID: contracts.ID(space), KnowledgeBaseID: input.KnowledgeBaseID, Title: input.Title, RowVersion: 1}, Draft: draft, Job: job, Blob: blob}, nil
		}
		return empty, contracts.ErrDocumentUploadConflict
	}
	if err != sql.ErrNoRows {
		return empty, errors.New("document upload unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.documents(space_id,kb_id,id,publication_epoch,row_version,active_revision_id,active_generation_set_id,deleted) VALUES($1,$2,$3,0,1,NULL,NULL,false)`, space, string(input.KnowledgeBaseID), string(documentID)); err != nil {
		return empty, errors.New("document upload unavailable")
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = string(documentID)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.document_metadata(space_id,kb_id,document_id,title) VALUES($1,$2,$3,$4)`, space, string(input.KnowledgeBaseID), string(documentID), title); err != nil {
		return empty, errors.New("document metadata unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_heads(space_id,kb_id,document_id,id,row_version,state,payload) VALUES($1,$2,$3,$4,1,'draft',$5::jsonb)`, space, string(input.KnowledgeBaseID), string(documentID), string(draftID), string(draftBytes)); err != nil {
		return empty, errors.New("draft unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_create_receipts(space_id,kb_id,document_id,draft_id,actor_id,command_key,receipt) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`, space, string(input.KnowledgeBaseID), string(documentID), string(draftID), string(who.SubjectID), input.IdempotencyKey, string(draftBytes)); err != nil {
		return empty, errors.New("draft unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_create_audit(space_id,kb_id,document_id,draft_id,actor_id,request_id,action) VALUES($1,$2,$3,$4,$5,$6,'draft.created')`, space, string(input.KnowledgeBaseID), string(documentID), string(draftID), string(who.SubjectID), string(input.Request.RequestID)); err != nil {
		return empty, errors.New("draft unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_create_outbox(space_id,kb_id,draft_id,event_type,payload) VALUES($1,$2,$3,'draft.created',$4::jsonb)`, space, string(input.KnowledgeBaseID), string(draftID), string(draftBytes)); err != nil {
		return empty, errors.New("draft unavailable")
	}
	if err = tx.Commit(); err != nil {
		return empty, errors.New("document upload outcome unknown")
	}
	job := contracts.Job{ID: jobID, SpaceID: contracts.ID(space), Stage: "document.parse", ProfileDigest: profileDigest, State: "queued", Attempt: 1, RowVersion: 1, Progress: 0}
	command := ingestion.DraftParseCommand{Request: input.Request, Principal: who, Scope: parseScope, DocumentID: documentID, DraftID: draftID, CandidateID: parseJobID("candidate", draftID, input.IdempotencyKey, who.SubjectID), ExpectedVersion: 1, Input: blob, Format: parseFormat(blob.MediaType), Options: contracts.ParseOptions{EnableOCR: true}}
	if err = ensureUploadParseTask(ctx, s.db, contracts.ID(space), input.KnowledgeBaseID, job, command); err != nil {
		return empty, err
	}
	cleanupBlob = false
	document := contracts.Document{ID: documentID, SpaceID: contracts.ID(space), KnowledgeBaseID: input.KnowledgeBaseID, Title: input.Title, PublicationEpoch: 0, RowVersion: 1}
	return contracts.DocumentUploadResult{Document: document, Draft: draft, Job: job, Blob: blob}, nil
}

var _ contracts.DocumentUploadApplication = (*Search)(nil)

func ensureUploadParseTask(ctx context.Context, db *sql.DB, space, kb contracts.ID, job contracts.Job, command ingestion.DraftParseCommand) error {
	repo := postgres.ParseTaskRepository{DB: db}
	if err := repo.CreateParseTask(ctx, ingestion.ParseTaskCreate{Space: space, KnowledgeBase: kb, Job: job, Command: command}); err == nil {
		return nil
	}
	record, err := repo.Read(ctx, space, kb, job.ID)
	if err == nil && record.Job.ID == job.ID && record.Job.SpaceID == space && record.Job.Stage == "document.parse" {
		return nil
	}
	return errors.New("parse task unavailable")
}
