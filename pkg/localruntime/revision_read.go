package localruntime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func (s *Search) ReadRevision(ctx context.Context, input contracts.RevisionReadInput) (contracts.Revision, error) {
	var empty contracts.Revision
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.RevisionID == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" {
		return empty, errors.New("revision read unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("revision read unauthenticated")
	}
	_, digest, err := s.ProfileIdentity()
	if err != nil || digest != input.Request.ProfileDigest {
		return empty, errors.New("revision read profile changed")
	}
	var spaceID, knowledgeBaseID, documentID, payload string
	if err := s.db.QueryRowContext(ctx, `SELECT r.space_id,r.kb_id,r.document_id,rc.payload
FROM akos_withdraw_v1.revisions r
JOIN akos_withdraw_v1.revision_contents rc ON rc.space_id=r.space_id AND rc.kb_id=r.kb_id AND rc.document_id=r.document_id AND rc.revision_id=r.id
JOIN akos_withdraw_v1.documents d ON d.space_id=r.space_id AND d.kb_id=r.kb_id AND d.id=r.document_id
WHERE r.id=$1::text AND NOT d.deleted`, string(input.RevisionID)).Scan(&spaceID, &knowledgeBaseID, &documentID, &payload); err != nil {
		return empty, errors.New("revision unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "revision.approve", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{contracts.ID(knowledgeBaseID)}, ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
	if err != nil {
		return empty, errors.New("revision denied")
	}
	var revision contracts.Revision
	if json.Unmarshal([]byte(payload), &revision) != nil || revision.ID != input.RevisionID || revision.DocumentID != contracts.ID(documentID) || revision.ID == "" {
		return empty, errors.New("revision unavailable")
	}
	current, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("revision denied")
	}
	fresh, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: current, Action: scope.Action, SpaceID: scope.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), scope.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{revision.DocumentID}})
	if err != nil || queryread.MatchRefreshedScope(ctx, who, scope, current, fresh, scope.KnowledgeBaseIDs) != nil {
		return empty, errors.New("revision denied")
	}
	return revision, nil
}

var _ contracts.RevisionReadApplication = (*Search)(nil)
