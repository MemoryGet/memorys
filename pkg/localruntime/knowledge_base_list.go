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

type knowledgeBaseCursor struct{ Space, Subject, After string }

func encodeKnowledgeBaseCursor(v knowledgeBaseCursor) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > 4096 || v.Space == "" || v.Subject == "" || v.After == "" {
		return "", errors.New("invalid knowledge base cursor")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func decodeKnowledgeBaseCursor(raw string, space, subject string) (knowledgeBaseCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(raw) > 10000 || len(data) > 4096 {
		return knowledgeBaseCursor{}, errors.New("invalid knowledge base cursor")
	}
	var v knowledgeBaseCursor
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil || v.Space != space || v.Subject != subject || v.After == "" {
		return knowledgeBaseCursor{}, errors.New("invalid knowledge base cursor")
	}
	return v, nil
}

func (s *Search) ListKnowledgeBases(ctx context.Context, input contracts.KnowledgeBaseListInput) (contracts.KnowledgeBasePage, error) {
	var empty contracts.KnowledgeBasePage
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.SpaceID == "" || input.PageSize < 1 || input.PageSize > 100 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 {
		return empty, errors.New("knowledge bases unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("knowledge bases unauthenticated")
	}
	_, digest, err := s.ProfileIdentity()
	if err != nil || digest != input.Request.ProfileDigest {
		return empty, errors.New("knowledge bases profile changed")
	}
	ids, err := (postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}).ListKnowledgeBases(ctx, who, input.SpaceID)
	if err != nil {
		return empty, errors.New("knowledge bases denied")
	}
	after := ""
	if input.Cursor != "" {
		cursor, err := decodeKnowledgeBaseCursor(input.Cursor, string(input.SpaceID), string(who.SubjectID))
		if err != nil {
			return empty, err
		}
		after = cursor.After
	}
	items := make([]contracts.KnowledgeBase, 0, input.PageSize+1)
	for _, id := range ids {
		if after != "" && string(id) <= after {
			continue
		}
		var policyText, backendID, backendVersion string
		if err := s.db.QueryRowContext(ctx, `SELECT policy_version::text FROM akos_withdraw_v1.review_policy_heads WHERE space_id=$1::text AND kb_id=$2::text`, string(input.SpaceID), string(id)).Scan(&policyText); err != nil {
			return empty, errors.New("knowledge base policy unavailable")
		}
		policy, err := strconv.ParseUint(policyText, 10, 64)
		if err != nil || policy == 0 {
			return empty, errors.New("knowledge base policy unavailable")
		}
		_ = s.db.QueryRowContext(ctx, `SELECT backend_epoch_id,row_version::text FROM akos_withdraw_v1.publication_backend_heads WHERE space_id=$1::text AND kb_id=$2::text`, string(input.SpaceID), string(id)).Scan(&backendID, &backendVersion)
		rowVersion := uint64(1)
		if n, e := strconv.ParseUint(backendVersion, 10, 64); e == nil && n > 0 {
			rowVersion = n
		}
		var backend *contracts.ID
		if backendID != "" {
			value := contracts.ID(backendID)
			backend = &value
		}
		items = append(items, contracts.KnowledgeBase{ID: id, SpaceID: input.SpaceID, Name: string(id), ActiveBackendEpochID: backend, RowVersion: rowVersion, PolicyVersion: policy})
		if len(items) > input.PageSize {
			break
		}
	}
	next := ""
	if len(items) > input.PageSize {
		items = items[:input.PageSize]
		next, err = encodeKnowledgeBaseCursor(knowledgeBaseCursor{Space: string(input.SpaceID), Subject: string(who.SubjectID), After: string(items[len(items)-1].ID)})
		if err != nil {
			return empty, err
		}
	}
	return contracts.KnowledgeBasePage{Items: items, NextCursor: next}, nil
}

var _ contracts.KnowledgeBaseListApplication = (*Search)(nil)
