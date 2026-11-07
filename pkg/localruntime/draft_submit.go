package localruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func (s *Search) SubmitDraft(ctx context.Context, input contracts.DraftSubmitInput) (contracts.SubmitResult, error) {
	var empty contracts.SubmitResult
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.DraftID == "" || input.ExpectedVersion == 0 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.IdempotencyKey == "" {
		return empty, errors.New("submission unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("submission unauthenticated")
	}
	_, digest, err := s.ProfileIdentity()
	if err != nil || digest != input.Request.ProfileDigest {
		return empty, errors.New("submission profile changed")
	}
	var documentID, spaceID, knowledgeBaseID, rowVersion string
	var payload []byte
	if err := s.db.QueryRowContext(ctx, `SELECT h.document_id,h.space_id,h.kb_id,h.row_version::text,h.payload
FROM akos_withdraw_v1.draft_heads h JOIN akos_withdraw_v1.documents d ON d.space_id=h.space_id AND d.kb_id=h.kb_id AND d.id=h.document_id
WHERE h.id=$1::text AND NOT d.deleted`, string(input.DraftID)).Scan(&documentID, &spaceID, &knowledgeBaseID, &rowVersion, &payload); err != nil {
		return empty, errors.New("draft unavailable")
	}
	actual, err := strconv.ParseUint(rowVersion, 10, 64)
	if err != nil || actual != input.ExpectedVersion {
		return empty, contracts.ErrDraftEditConflict
	}
	var current contracts.Draft
	if json.Unmarshal(payload, &current) != nil || current.ID != input.DraftID || current.DocumentID != contracts.ID(documentID) || current.State != "draft" || current.RowVersion != actual {
		return empty, contracts.ErrDraftEditConflict
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "document.submit", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{contracts.ID(knowledgeBaseID)}, ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
	if err != nil {
		return empty, errors.New("submission denied")
	}
	verify := func(c context.Context, before contracts.AccessScope) error {
		credential := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
		defer clear(credential.Secret)
		currentPrincipal, authErr := s.auth.Authenticate(c, credential)
		if authErr != nil {
			return errors.New("submission denied")
		}
		fresh, authErr := authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: currentPrincipal, Action: before.Action, SpaceID: before.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), before.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
		if authErr != nil || queryread.MatchRefreshedScope(c, who, before, currentPrincipal, fresh, before.KnowledgeBaseIDs) != nil {
			return errors.New("submission denied")
		}
		return nil
	}
	var policyText string
	if err := s.db.QueryRowContext(ctx, `SELECT policy_version::text FROM akos_withdraw_v1.review_policy_heads WHERE space_id=$1::text AND kb_id=$2::text`, spaceID, knowledgeBaseID).Scan(&policyText); err != nil {
		return empty, errors.New("review policy unavailable")
	}
	policy, err := strconv.ParseUint(policyText, 10, 64)
	if err != nil || policy == 0 {
		return empty, errors.New("review policy unavailable")
	}
	command := document.SubmissionCommand{DocumentID: contracts.ID(documentID), Request: contracts.SubmitRequest{Command: contracts.CommandContext{Request: input.Request, Principal: who, Scope: scope, IdempotencyKey: input.IdempotencyKey}, DraftID: input.DraftID, ExpectedVersion: input.ExpectedVersion}}
	service := postgres.Submissions{DB: s.db, ProfileDigest: input.Request.ProfileDigest, ReviewPolicyVersion: policy, Verify: verify, AuthorizeSubmission: func(c context.Context, received document.SubmissionCommand) error {
		if received.DocumentID != command.DocumentID || received.Request.DraftID != command.Request.DraftID || received.Request.ExpectedVersion != command.Request.ExpectedVersion || received.Request.Command.IdempotencyKey != command.Request.Command.IdempotencyKey {
			return errors.New("submission command changed")
		}
		return verify(c, received.Request.Command.Scope)
	}, VerifyContent: func(_ context.Context, _ contracts.AccessScope, draft contracts.Draft) error {
		_, e := document.BuildFrozenContent(draft)
		return e
	}}
	return service.SubmitDraft(ctx, command)
}

var _ contracts.DraftSubmitApplication = (*Search)(nil)
