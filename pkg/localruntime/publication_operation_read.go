package localruntime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func (s *Search) ReadPublicationOperation(ctx context.Context, input contracts.PublicationOperationReadInput) (contracts.PublicationOperation, error) {
	var empty contracts.PublicationOperation
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.OperationID == "" || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 {
		return empty, errors.New("publication operation unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("publication operation unauthenticated")
	}
	_, digest, err := s.ProfileIdentity()
	if err != nil || digest != input.Request.ProfileDigest {
		return empty, errors.New("publication operation profile changed")
	}
	var spaceID, kbID, documentID string
	var receipt []byte
	if err := s.db.QueryRowContext(ctx, `SELECT space_id,kb_id,document_id,receipt FROM akos_withdraw_v1.publication_intents WHERE id=$1::text`, string(input.OperationID)).Scan(&spaceID, &kbID, &documentID, &receipt); err != nil {
		return empty, errors.New("publication operation unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "publication.publish", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{contracts.ID(kbID)}, ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
	if err != nil {
		return empty, errors.New("publication operation denied")
	}
	var result document.PublicationStartResult
	if json.Unmarshal(receipt, &result) != nil || result.Operation.ID != input.OperationID || result.Operation.DocumentID != contracts.ID(documentID) {
		return empty, errors.New("publication operation unavailable")
	}
	credential := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
	defer clear(credential.Secret)
	current, err := s.auth.Authenticate(ctx, credential)
	if err != nil {
		return empty, errors.New("publication operation denied")
	}
	fresh, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: current, Action: scope.Action, SpaceID: scope.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), scope.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
	if err != nil || queryread.MatchRefreshedScope(ctx, who, scope, current, fresh, scope.KnowledgeBaseIDs) != nil {
		return empty, errors.New("publication operation denied")
	}
	return result.Operation, nil
}

var _ contracts.PublicationOperationReadApplication = (*Search)(nil)
