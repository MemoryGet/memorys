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
)

func (s *Search) CompareDraft(ctx context.Context, input contracts.DraftCompareInput) (contracts.DraftCompareResult, error) {
	var empty contracts.DraftCompareResult
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.DraftID == "" || input.FromVersion == 0 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 {
		return empty, errors.New("draft comparison unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("draft comparison unauthenticated")
	}
	_, digest, err := s.ProfileIdentity()
	if err != nil || digest != input.Request.ProfileDigest {
		return empty, errors.New("draft comparison profile changed")
	}
	var spaceID, kbID, documentID string
	var currentPayload, previousPayload []byte
	var currentVersion string
	if err := s.db.QueryRowContext(ctx, `SELECT h.space_id,h.kb_id,h.document_id,h.row_version::text,h.payload FROM akos_withdraw_v1.draft_heads h JOIN akos_withdraw_v1.documents d ON d.space_id=h.space_id AND d.kb_id=h.kb_id AND d.id=h.document_id WHERE h.id=$1::text AND h.state='draft' AND NOT d.deleted`, string(input.DraftID)).Scan(&spaceID, &kbID, &documentID, &currentVersion, &currentPayload); err != nil {
		return empty, errors.New("draft unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	if _, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "document.create", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{contracts.ID(kbID)}, ResourceIDs: []contracts.ID{contracts.ID(documentID)}}); err != nil {
		return empty, errors.New("draft comparison denied")
	}
	if err := s.db.QueryRowContext(ctx, `SELECT receipt FROM akos_withdraw_v1.draft_edit_receipts WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND draft_id=$4::text AND (receipt->>'RowVersion')=$5`, spaceID, kbID, documentID, string(input.DraftID), strconv.FormatUint(input.FromVersion, 10)).Scan(&previousPayload); err != nil {
		return empty, errors.New("draft version unavailable")
	}
	var before, after contracts.Draft
	if json.Unmarshal(previousPayload, &before) != nil || json.Unmarshal(currentPayload, &after) != nil {
		return empty, errors.New("draft version unavailable")
	}
	diff, err := document.CompareDrafts(before, after)
	if err != nil {
		return empty, err
	}
	result := contracts.DraftCompareResult{DraftID: input.DraftID, FromVersion: diff.FromVersion, ToVersion: diff.ToVersion, Changes: make([]contracts.DraftCompareChange, 0, len(diff.Changes))}
	for _, change := range diff.Changes {
		var beforeVersion, afterVersion uint64
		if change.Before != nil {
			beforeVersion = change.Before.RowVersion
		}
		if change.After != nil {
			afterVersion = change.After.RowVersion
		}
		result.Changes = append(result.Changes, contracts.DraftCompareChange{BlockID: change.BlockID, Kind: change.Kind, BeforeVersion: beforeVersion, AfterVersion: afterVersion})
	}
	return result, nil
}

var _ contracts.DraftCompareApplication = (*Search)(nil)
