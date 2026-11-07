package localruntime

import (
	"context"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func (s *Search) EditDraft(ctx context.Context, input contracts.DraftEditInput) (contracts.Draft, error) {
	var empty contracts.Draft
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.DraftID == "" || input.ExpectedVersion == 0 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.IdempotencyKey == "" {
		return empty, errors.New("draft edit unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("draft edit unauthenticated")
	}
	_, digest, err := s.ProfileIdentity()
	if err != nil || digest != input.Request.ProfileDigest {
		return empty, errors.New("draft edit profile changed")
	}
	var documentID, spaceID, knowledgeBaseID string
	if err := s.db.QueryRowContext(ctx, `SELECT h.document_id,h.space_id,h.kb_id FROM akos_withdraw_v1.draft_heads h JOIN akos_withdraw_v1.documents d ON d.space_id=h.space_id AND d.kb_id=h.kb_id AND d.id=h.document_id WHERE h.id=$1::text AND h.state='draft' AND NOT d.deleted`, string(input.DraftID)).Scan(&documentID, &spaceID, &knowledgeBaseID); err != nil {
		return empty, errors.New("draft unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "document.create", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{contracts.ID(knowledgeBaseID)}, ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
	if err != nil {
		return empty, errors.New("draft edit denied")
	}
	verify := func(c context.Context, before contracts.AccessScope) error {
		credential := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
		defer clear(credential.Secret)
		current, authErr := s.auth.Authenticate(c, credential)
		if authErr != nil {
			return errors.New("draft edit denied")
		}
		fresh, authErr := authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: current, Action: before.Action, SpaceID: before.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), before.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
		if authErr != nil || queryread.MatchRefreshedScope(c, who, before, current, fresh, before.KnowledgeBaseIDs) != nil {
			return errors.New("draft edit denied")
		}
		return nil
	}
	return (postgres.DraftEdits{DB: s.db, Verify: verify}).EditDraft(ctx, input, scope, who)
}

var _ contracts.DraftEditApplication = (*Search)(nil)
