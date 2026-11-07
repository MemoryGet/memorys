package localruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"strconv"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/ingestion"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func parseJobID(kind string, draft contracts.ID, key string, subject contracts.ID) contracts.ID {
	sum := sha256.Sum256([]byte(kind + "\x00" + string(draft) + "\x00" + key + "\x00" + string(subject)))
	return contracts.ID(kind + "-" + hex.EncodeToString(sum[:]))
}

func parseFormat(mediaType string) string {
	kind, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		return "bin"
	}
	switch strings.ToLower(kind) {
	case "text/plain", "text/markdown", "text/x-markdown":
		return "markdown"
	case "application/pdf":
		return "pdf"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "docx"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "xlsx"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return "pptx"
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "audio/wav", "audio/x-wav":
		return "wav"
	case "audio/mpeg":
		return "mp3"
	case "audio/mp4", "audio/x-m4a":
		return "m4a"
	}
	if i := strings.LastIndex(kind, "/"); i >= 0 && i+1 < len(kind) {
		return strings.ToLower(kind[i+1:])
	}
	return strings.ToLower(kind)
}

func (s *Search) ParseDraft(ctx context.Context, input contracts.DraftParseInput) (contracts.DraftParseResult, error) {
	var empty contracts.DraftParseResult
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.DraftID == "" || input.ExpectedVersion == 0 || input.PipelineProfileID == "" || input.PipelineProfileVersion == 0 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.IdempotencyKey == "" {
		return empty, errors.New("parse unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("parse unauthenticated")
	}
	profileVersion, digest, err := s.ProfileIdentity()
	if err != nil || profileVersion != input.PipelineProfileVersion || digest != input.Request.ProfileDigest {
		return empty, errors.New("parse profile changed")
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
	var draft contracts.Draft
	if json.Unmarshal(payload, &draft) != nil || draft.ID != input.DraftID || draft.DocumentID != contracts.ID(documentID) || len(draft.Blobs) != 1 {
		return empty, errors.New("draft unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "document.parse", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{contracts.ID(kbID)}, ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
	if err != nil {
		return empty, errors.New("parse denied")
	}
	verify := func(c context.Context, before contracts.AccessScope) error {
		credential := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
		defer clear(credential.Secret)
		current, e := s.auth.Authenticate(c, credential)
		if e != nil {
			return errors.New("parse denied")
		}
		fresh, e := authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: current, Action: before.Action, SpaceID: before.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), before.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
		if e != nil || queryread.MatchRefreshedScope(c, who, before, current, fresh, before.KnowledgeBaseIDs) != nil {
			return errors.New("parse denied")
		}
		return nil
	}
	if err := verify(ctx, scope); err != nil {
		return empty, err
	}
	jobID := parseJobID("parse-job", input.DraftID, input.IdempotencyKey, who.SubjectID)
	candidateID := parseJobID("candidate", input.DraftID, input.IdempotencyKey, who.SubjectID)
	job := contracts.Job{ID: jobID, SpaceID: contracts.ID(spaceID), Stage: "document.parse", ProfileDigest: digest, State: "queued", Attempt: 1, RowVersion: 1, Progress: 0}
	command := ingestion.DraftParseCommand{Request: input.Request, Principal: who, Scope: scope, DocumentID: contracts.ID(documentID), DraftID: input.DraftID, CandidateID: candidateID, ExpectedVersion: input.ExpectedVersion, Input: draft.Blobs[0], Format: parseFormat(draft.Blobs[0].MediaType), Options: contracts.ParseOptions{EnableOCR: true}}
	if err := (postgres.ParseTaskRepository{DB: s.db}).CreateParseTask(ctx, ingestion.ParseTaskCreate{Space: contracts.ID(spaceID), KnowledgeBase: contracts.ID(kbID), Job: job, Command: command}); err != nil {
		// The deterministic job ID makes retries idempotent. A prior task with
		// the same key is returned as-is after the insert conflict.
		var envelope struct {
			Job contracts.Job `json:"job"`
		}
		var existing []byte
		if queryErr := s.db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.parse_task_heads WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text`, spaceID, kbID, string(jobID)).Scan(&existing); queryErr == nil && json.Unmarshal(existing, &envelope) == nil && envelope.Job.ID == jobID {
			return contracts.DraftParseResult{Job: envelope.Job, DocumentID: contracts.ID(documentID)}, nil
		}
		return empty, err
	}
	return contracts.DraftParseResult{Job: job, DocumentID: contracts.ID(documentID)}, nil
}

var _ contracts.DraftParseApplication = (*Search)(nil)
