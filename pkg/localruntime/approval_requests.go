package localruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

type approvalRequestCursor struct {
	Space   string `json:"space"`
	KB      string `json:"kb"`
	Subject string `json:"subject"`
	After   string `json:"after"`
}

func encodeApprovalCursor(value approvalRequestCursor) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 4096 || value.Space == "" || value.KB == "" || value.Subject == "" || value.After == "" {
		return "", errors.New("invalid approval cursor")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeApprovalCursor(raw, space, kb, subject string) (string, string, error) {
	if raw == "" || len(raw) > 10000 {
		return "", "", errors.New("invalid approval cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > 4096 {
		return "", "", errors.New("invalid approval cursor")
	}
	var value approvalRequestCursor
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || space != "" && value.Space != space || value.KB != kb || value.Subject != subject || value.After == "" || len(value.After) > 200 {
		return "", "", errors.New("invalid approval cursor")
	}
	return value.Space, value.After, nil
}

func (s *Search) ListApprovalRequests(ctx context.Context, input contracts.ApprovalRequestListInput) (contracts.ApprovalRequestPage, error) {
	var empty contracts.ApprovalRequestPage
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.KnowledgeBaseID == "" || input.PageSize < 1 || input.PageSize > 100 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 {
		return empty, errors.New("approval requests unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("approval requests unauthenticated")
	}
	profileVersion, profileDigest, err := s.ProfileIdentity()
	if err != nil || profileVersion == 0 || profileDigest != input.Request.ProfileDigest {
		return empty, errors.New("approval profile changed")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	spaceID, after := "", ""
	if input.Cursor != "" {
		spaceID, after, err = decodeApprovalCursor(input.Cursor, "", string(input.KnowledgeBaseID), string(who.SubjectID))
		if err != nil {
			return empty, err
		}
	}
	if spaceID == "" {
		rows, queryErr := s.db.QueryContext(ctx, `SELECT DISTINCT space_id FROM akos_withdraw_v1.approval_requests WHERE kb_id=$1::text ORDER BY space_id LIMIT 100`, string(input.KnowledgeBaseID))
		if queryErr != nil {
			return empty, errors.New("approval requests unavailable")
		}
		for rows.Next() {
			var candidate string
			if rows.Scan(&candidate) == nil {
				if _, authErr := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "revision.approve", SpaceID: contracts.ID(candidate), KnowledgeBaseIDs: []contracts.ID{input.KnowledgeBaseID}}); authErr == nil {
					spaceID = candidate
					break
				}
			}
		}
		rows.Close()
	}
	if spaceID == "" {
		return empty, nil
	}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "revision.approve", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{input.KnowledgeBaseID}})
	if err != nil {
		return empty, errors.New("approval requests denied")
	}
	query := `SELECT id,revision_id,document_id,submitted_by,submitted_at,state,row_version::text,content_hash,policy_version::text FROM akos_withdraw_v1.approval_requests WHERE space_id=$1::text AND kb_id=$2::text`
	args := []any{string(scope.SpaceID), string(input.KnowledgeBaseID)}
	if after != "" {
		query += ` AND id>$3::text`
		args = append(args, after)
	}
	query += ` ORDER BY id LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, input.PageSize+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return empty, errors.New("approval requests unavailable")
	}
	defer rows.Close()
	items := make([]contracts.ApprovalRequest, 0, input.PageSize+1)
	for rows.Next() {
		var item contracts.ApprovalRequest
		var rowVersion, policyVersion string
		if err := rows.Scan(&item.ID, &item.RevisionID, &item.DocumentID, &item.SubmittedBy, &item.SubmittedAt, &item.State, &rowVersion, &item.ContentHash, &policyVersion); err != nil {
			return empty, errors.New("approval requests unavailable")
		}
		item.RowVersion, err = strconv.ParseUint(rowVersion, 10, 64)
		if err != nil {
			return empty, errors.New("approval requests unavailable")
		}
		item.PolicyVersion, err = strconv.ParseUint(policyVersion, 10, 64)
		if err != nil {
			return empty, errors.New("approval requests unavailable")
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return empty, errors.New("approval requests unavailable")
	}
	next := ""
	if len(items) > input.PageSize {
		items = items[:input.PageSize]
		next, err = encodeApprovalCursor(approvalRequestCursor{Space: string(scope.SpaceID), KB: string(input.KnowledgeBaseID), Subject: string(who.SubjectID), After: string(items[len(items)-1].ID)})
		if err != nil {
			return empty, errors.New("approval cursor unavailable")
		}
	}
	return contracts.ApprovalRequestPage{Items: items, NextCursor: next}, nil
}

var _ contracts.ApprovalRequestListApplication = (*Search)(nil)
