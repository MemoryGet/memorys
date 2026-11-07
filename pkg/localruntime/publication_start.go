package localruntime

import (
	"context"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func (s *Search) StartPublication(ctx context.Context, input contracts.PublicationStartInput) (contracts.PublicationStartResult, error) {
	var empty contracts.PublicationStartResult
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.DocumentID == "" || input.RevisionID == "" || input.ApprovalID == "" || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.IdempotencyKey == "" {
		return empty, errors.New("publication unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("publication unauthenticated")
	}
	_, digest, err := s.ProfileIdentity()
	if err != nil || digest != input.Request.ProfileDigest {
		return empty, errors.New("publication profile changed")
	}
	var spaceID, knowledgeBaseID string
	if err := s.db.QueryRowContext(ctx, `SELECT space_id,kb_id FROM akos_withdraw_v1.documents WHERE id=$1::text AND NOT deleted`, string(input.DocumentID)).Scan(&spaceID, &knowledgeBaseID); err != nil {
		return empty, errors.New("document unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "publication.start", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{contracts.ID(knowledgeBaseID)}, ResourceIDs: []contracts.ID{input.DocumentID}})
	if err != nil {
		return empty, errors.New("publication denied")
	}
	verify := func(c context.Context, before contracts.AccessScope) error {
		credential := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
		defer clear(credential.Secret)
		current, authErr := s.auth.Authenticate(c, credential)
		if authErr != nil {
			return errors.New("publication denied")
		}
		fresh, authErr := authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: current, Action: before.Action, SpaceID: before.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), before.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{input.DocumentID}})
		if authErr != nil || queryread.MatchRefreshedScope(c, who, before, current, fresh, before.KnowledgeBaseIDs) != nil {
			return errors.New("publication denied")
		}
		return nil
	}
	start := postgres.PublicationStarts{DB: s.db, Verify: func(c context.Context, before contracts.AccessScope) error { return verify(c, before) }, ProfileDigest: input.Request.ProfileDigest, AuthorizeStart: func(c context.Context, request contracts.PublishRequest) error {
		if request.DocumentID != input.DocumentID || request.RevisionID != input.RevisionID || request.ApprovalID != input.ApprovalID || request.ExpectedEpoch != input.ExpectedEpoch || request.Command.IdempotencyKey != input.IdempotencyKey {
			return errors.New("publication command changed")
		}
		return verify(c, request.Command.Scope)
	}}
	result, err := start.StartPublication(ctx, contracts.PublishRequest{Command: contracts.CommandContext{Request: input.Request, Principal: who, Scope: scope, IdempotencyKey: input.IdempotencyKey}, DocumentID: input.DocumentID, RevisionID: input.RevisionID, ApprovalID: input.ApprovalID, ExpectedEpoch: input.ExpectedEpoch})
	if err != nil {
		return empty, err
	}
	return contracts.PublicationStartResult{Operation: result.Operation, Job: result.Job}, nil
}

var _ contracts.PublicationStartApplication = (*Search)(nil)
