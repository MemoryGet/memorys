package localruntime

import (
	"context"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
	"github.com/MemoryGet/memorys/pkg/withdrawal"
)

func (s *Search) WithdrawPublication(ctx context.Context, input contracts.PublicationWithdrawInput) (contracts.WithdrawResult, error) {
	var empty contracts.WithdrawResult
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.DocumentID == "" || input.ExpectedEpoch == 0 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.IdempotencyKey == "" || input.Reason == "" {
		return empty, errors.New("withdrawal unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("withdrawal unauthenticated")
	}
	_, digest, err := s.ProfileIdentity()
	if err != nil || digest != input.Request.ProfileDigest {
		return empty, errors.New("withdrawal profile changed")
	}
	var spaceID, knowledgeBaseID string
	if err := s.db.QueryRowContext(ctx, `SELECT space_id,kb_id FROM akos_withdraw_v1.documents WHERE id=$1::text AND NOT deleted`, string(input.DocumentID)).Scan(&spaceID, &knowledgeBaseID); err != nil {
		return empty, errors.New("document unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "publication.withdraw", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{contracts.ID(knowledgeBaseID)}, ResourceIDs: []contracts.ID{input.DocumentID}})
	if err != nil {
		return empty, errors.New("withdrawal denied")
	}
	verify := func(c context.Context, before contracts.AccessScope) error {
		credential := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
		defer clear(credential.Secret)
		current, authErr := s.auth.Authenticate(c, credential)
		if authErr != nil {
			return errors.New("withdrawal denied")
		}
		fresh, authErr := authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: current, Action: before.Action, SpaceID: before.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), before.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{input.DocumentID}})
		if authErr != nil || queryread.MatchRefreshedScope(c, who, before, current, fresh, before.KnowledgeBaseIDs) != nil {
			return errors.New("withdrawal denied")
		}
		return nil
	}
	request := contracts.WithdrawRequest{Command: contracts.CommandContext{Request: input.Request, Principal: who, Scope: scope, IdempotencyKey: input.IdempotencyKey}, DocumentID: input.DocumentID, ExpectedEpoch: input.ExpectedEpoch, Reason: input.Reason}
	service := withdrawal.Service{Transactions: postgres.WithdrawalAdapter{DB: s.db, Verify: func(c context.Context, before contracts.AccessScope) error { return verify(c, before) }}, Prepare: (&withdrawal.Preparer{Verify: func(c context.Context, received contracts.WithdrawRequest) error {
		if received.DocumentID != request.DocumentID || received.ExpectedEpoch != request.ExpectedEpoch || received.Reason != request.Reason || received.Command.IdempotencyKey != request.Command.IdempotencyKey {
			return errors.New("withdrawal command changed")
		}
		return verify(c, received.Command.Scope)
	}}).Prepare}
	return service.Withdraw(ctx, request)
}

var _ contracts.PublicationWithdrawApplication = (*Search)(nil)
