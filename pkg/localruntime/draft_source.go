package localruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/ingestion"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func (s *Search) ReplaceDraftSource(ctx context.Context, input contracts.DraftSourceInput, reader io.Reader) (contracts.DraftParseResult, error) {
	var empty contracts.DraftParseResult
	if s == nil || s.db == nil || s.auth == nil || s.blobs == nil || ctx == nil || reader == nil || input.DraftID == "" || input.ExpectedVersion == 0 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.Size < 0 || input.MediaType == "" || input.IdempotencyKey == "" {
		return empty, errors.New("draft source unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("draft source unauthenticated")
	}
	_, profileDigest, err := s.ProfileIdentity()
	if err != nil || profileDigest != input.Request.ProfileDigest {
		return empty, errors.New("draft source profile changed")
	}
	var documentID, spaceID, kbID, versionText string
	var payload []byte
	if err := s.db.QueryRowContext(ctx, `SELECT h.document_id,h.space_id,h.kb_id,h.row_version::text,h.payload FROM akos_withdraw_v1.draft_heads h JOIN akos_withdraw_v1.documents d ON d.space_id=h.space_id AND d.kb_id=h.kb_id AND d.id=h.document_id WHERE h.id=$1::text AND h.state='draft' AND NOT d.deleted`, string(input.DraftID)).Scan(&documentID, &spaceID, &kbID, &versionText, &payload); err != nil {
		return empty, errors.New("draft unavailable")
	}
	version, err := strconv.ParseUint(versionText, 10, 64)
	if err != nil || version != input.ExpectedVersion {
		return empty, errors.New("draft changed")
	}
	var current contracts.Draft
	if json.Unmarshal(payload, &current) != nil || current.ID != input.DraftID || current.DocumentID != contracts.ID(documentID) {
		return empty, errors.New("draft unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "document.parse", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{contracts.ID(kbID)}, ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
	if err != nil {
		return empty, errors.New("draft source denied")
	}
	verify := func(c context.Context, before contracts.AccessScope) error {
		credential := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
		defer clear(credential.Secret)
		currentPrincipal, e := s.auth.Authenticate(c, credential)
		if e != nil {
			return errors.New("draft source denied")
		}
		fresh, e := authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: currentPrincipal, Action: before.Action, SpaceID: before.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), before.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
		if e != nil || queryread.MatchRefreshedScope(c, who, before, currentPrincipal, fresh, before.KnowledgeBaseIDs) != nil {
			return errors.New("draft source denied")
		}
		return nil
	}
	if err := verify(ctx, scope); err != nil {
		return empty, err
	}
	blob, err := s.blobs.Put(ctx, contracts.BlobWriteRequest{Request: input.Request, Scope: scope, ExpectedSHA256: input.ExpectedSHA256, Size: input.Size, MediaType: input.MediaType, IdempotencyKey: input.IdempotencyKey}, reader)
	if err != nil {
		return empty, err
	}
	updated := current
	updated.RowVersion++
	updated.Blobs = []contracts.BlobRef{blob}
	updated.Blocks = nil
	updatedBytes, err := json.Marshal(updated)
	if err != nil {
		_ = s.blobs.Delete(ctx, contracts.BlobReadRequest{Request: input.Request, Scope: scope, Blob: blob})
		return empty, errors.New("draft source unavailable")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		_ = s.blobs.Delete(ctx, contracts.BlobReadRequest{Request: input.Request, Scope: scope, Blob: blob})
		return empty, errors.New("draft source unavailable")
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.draft_heads SET row_version=$5::numeric,payload=$6::jsonb WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND id=$4::text AND row_version=$7::numeric AND state='draft'`, spaceID, kbID, documentID, string(input.DraftID), strconv.FormatUint(updated.RowVersion, 10), string(updatedBytes), strconv.FormatUint(input.ExpectedVersion, 10))
	if err != nil {
		_ = s.blobs.Delete(ctx, contracts.BlobReadRequest{Request: input.Request, Scope: scope, Blob: blob})
		return empty, errors.New("draft source unavailable")
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		_ = s.blobs.Delete(ctx, contracts.BlobReadRequest{Request: input.Request, Scope: scope, Blob: blob})
		return empty, errors.New("draft changed")
	}
	if err := tx.Commit(); err != nil {
		return empty, errors.New("draft source outcome unknown")
	}
	job := contracts.Job{ID: parseJobID("parse-job", input.DraftID, input.IdempotencyKey, who.SubjectID), SpaceID: contracts.ID(spaceID), Stage: "document.parse", ProfileDigest: profileDigest, State: "queued", Attempt: 1, RowVersion: 1}
	candidate := parseJobID("candidate", input.DraftID, input.IdempotencyKey, who.SubjectID)
	command := ingestion.DraftParseCommand{Request: input.Request, Principal: who, Scope: scope, DocumentID: contracts.ID(documentID), DraftID: input.DraftID, CandidateID: candidate, ExpectedVersion: updated.RowVersion, Input: blob, Format: parseFormat(blob.MediaType), Options: contracts.ParseOptions{EnableOCR: true}}
	if err := (postgres.ParseTaskRepository{DB: s.db}).CreateParseTask(ctx, ingestion.ParseTaskCreate{Space: contracts.ID(spaceID), KnowledgeBase: contracts.ID(kbID), Job: job, Command: command}); err != nil {
		return empty, err
	}
	return contracts.DraftParseResult{Job: job, DocumentID: contracts.ID(documentID)}, nil
}

var _ contracts.DraftSourceApplication = (*Search)(nil)
