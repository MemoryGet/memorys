package localruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

func (s *Search) ReadDraft(ctx context.Context, input contracts.DraftReadInput) (contracts.Draft, error) {
	var empty contracts.Draft
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.DraftID == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" {
		return empty, errors.New("draft read unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("draft read unauthenticated")
	}
	_, digest, err := s.ProfileIdentity()
	if err != nil || digest != input.Request.ProfileDigest {
		return empty, errors.New("draft read profile changed")
	}
	var spaceID, knowledgeBaseID, payload, rowVersionText string
	err = s.db.QueryRowContext(ctx, `SELECT d.space_id,d.kb_id,dh.payload,dh.row_version::text
FROM akos_withdraw_v1.draft_heads dh
JOIN akos_withdraw_v1.documents d ON d.space_id=dh.space_id AND d.kb_id=dh.kb_id AND d.id=dh.document_id
WHERE dh.id=$1::text AND NOT d.deleted`, string(input.DraftID)).Scan(&spaceID, &knowledgeBaseID, &payload, &rowVersionText)
	if err != nil {
		return empty, errors.New("draft unavailable")
	}
	rowVersion, err := strconv.ParseUint(rowVersionText, 10, 64)
	if err != nil || rowVersion == 0 {
		return empty, errors.New("draft unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	// Management draft reads are an edit capability: Viewer grants must not
	// expose unpublished content. The persisted ACL action used by the current
	// runtime for Contributor edit access is document.create.
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "document.create", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{contracts.ID(knowledgeBaseID)}})
	if err != nil {
		return empty, errors.New("draft denied")
	}
	var draft contracts.Draft
	if json.Unmarshal([]byte(payload), &draft) != nil || draft.ID != input.DraftID || draft.DocumentID == "" || draft.RowVersion != rowVersion || (draft.State != "draft" && draft.State != "submitted") {
		return empty, errors.New("draft unavailable")
	}
	// Recheck the grant immediately before returning mutable content. This keeps
	// a policy change during the read from leaking a draft under a stale scope.
	fresh, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: scope.Action, SpaceID: scope.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), scope.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{draft.DocumentID}})
	if err != nil || fresh.SpaceID != scope.SpaceID || fresh.PolicyVersion != scope.PolicyVersion {
		return empty, errors.New("draft denied")
	}
	return draft, nil
}

var _ contracts.DraftReadApplication = (*Search)(nil)
