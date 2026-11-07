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

type grantListCursor struct {
	Space   string `json:"space"`
	KB      string `json:"kb"`
	Subject string `json:"subject"`
	After   string `json:"after"`
}

func encodeGrantListCursor(value grantListCursor) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 4096 || value.Space == "" || value.KB == "" || value.Subject == "" || value.After == "" {
		return "", errors.New("invalid grant cursor")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeGrantListCursor(raw, kb, subject string) (string, string, error) {
	if raw == "" || len(raw) > 10000 {
		return "", "", errors.New("invalid grant cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > 4096 {
		return "", "", errors.New("invalid grant cursor")
	}
	var value grantListCursor
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || value.KB != kb || value.Subject != subject || value.Space == "" || value.After == "" || len(value.After) > 200 {
		return "", "", errors.New("invalid grant cursor")
	}
	return value.Space, value.After, nil
}

func (s *Search) ListGrants(ctx context.Context, input contracts.GrantListInput) (contracts.GrantPage, error) {
	var empty contracts.GrantPage
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.KnowledgeBaseID == "" || input.PageSize < 1 || input.PageSize > 100 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 {
		return empty, contracts.ErrGrantUnavailable
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil || ctx.Err() != nil {
		return empty, contracts.ErrGrantUnauthenticated
	}
	version, digest, err := s.ProfileIdentity()
	if err != nil || version == 0 || digest != input.Request.ProfileDigest {
		return empty, contracts.ErrGrantUnavailable
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	spaceID, after := "", ""
	if input.Cursor != "" {
		spaceID, after, err = decodeGrantListCursor(input.Cursor, string(input.KnowledgeBaseID), string(who.SubjectID))
		if err != nil {
			return empty, err
		}
	}
	if spaceID == "" {
		rows, queryErr := s.db.QueryContext(ctx, `SELECT DISTINCT g.space_id FROM akos_withdraw_v1.access_policy_grants g JOIN akos_withdraw_v1.identity_subjects target ON target.issuer=g.issuer AND target.subject_id=g.subject_id AND target.enabled AND target.subject_type='user' WHERE g.kb_id=$1::text ORDER BY g.space_id LIMIT 100`, string(input.KnowledgeBaseID))
		if queryErr != nil {
			return empty, contracts.ErrGrantUnavailable
		}
		for rows.Next() {
			var candidate string
			if rows.Scan(&candidate) == nil {
				if _, authErr := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "kb.manage_access", SpaceID: contracts.ID(candidate), KnowledgeBaseIDs: []contracts.ID{input.KnowledgeBaseID}}); authErr == nil {
					spaceID = candidate
					break
				}
			}
		}
		_ = rows.Close()
	}
	if spaceID == "" {
		return empty, nil
	}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "kb.manage_access", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{input.KnowledgeBaseID}})
	if err != nil {
		return empty, contracts.ErrGrantDenied
	}
	query := `SELECT DISTINCT g.subject_id FROM akos_withdraw_v1.access_policy_grants g JOIN akos_withdraw_v1.identity_subjects target ON target.issuer=g.issuer AND target.subject_id=g.subject_id AND target.enabled AND target.subject_type='user' WHERE g.space_id=$1::text AND g.version=$2::numeric AND g.kb_id=$3::text AND g.issuer=$4 AND g.client_id=$5`
	args := []any{string(scope.SpaceID), scope.PolicyVersion, string(input.KnowledgeBaseID), who.Issuer, string(who.ClientID)}
	if after != "" {
		query += ` AND g.subject_id>$6::text`
		args = append(args, after)
	}
	query += ` ORDER BY g.subject_id LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, input.PageSize+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return empty, contracts.ErrGrantUnavailable
	}
	defer rows.Close()
	ids := make([]string, 0, input.PageSize+1)
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil || id == "" {
			return empty, contracts.ErrGrantUnavailable
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return empty, contracts.ErrGrantUnavailable
	}
	items := make([]contracts.GrantItem, 0, len(ids))
	for _, id := range ids {
		actionsRows, queryErr := s.db.QueryContext(ctx, `SELECT action FROM akos_withdraw_v1.access_policy_grants WHERE space_id=$1::text AND version=$2::numeric AND issuer=$3 AND subject_id=$4 AND client_id=$5 AND kb_id=$6::text`, string(scope.SpaceID), scope.PolicyVersion, who.Issuer, id, string(who.ClientID), string(input.KnowledgeBaseID))
		if queryErr != nil {
			return empty, contracts.ErrGrantUnavailable
		}
		actions := map[string]bool{}
		for actionsRows.Next() {
			var action string
			if actionsRows.Scan(&action) != nil {
				_ = actionsRows.Close()
				return empty, contracts.ErrGrantUnavailable
			}
			actions[action] = true
		}
		closeErr := actionsRows.Close()
		if closeErr != nil || actionsRows.Err() != nil {
			return empty, contracts.ErrGrantUnavailable
		}
		role := ""
		switch {
		case actions["kb.manage_access"]:
			role = "Admin"
		case actions["document.create"] && actions["document.submit"]:
			role = "Contributor"
		case actions["knowledge.search"] || actions["knowledge.read"]:
			role = "Viewer"
		}
		if role == "" {
			return empty, contracts.ErrGrantUnavailable
		}
		items = append(items, contracts.GrantItem{KnowledgeBaseID: input.KnowledgeBaseID, SubjectID: contracts.ID(id), Role: role, PolicyVersion: scope.PolicyVersion})
	}
	fresh, authErr := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "kb.manage_access", SpaceID: scope.SpaceID, KnowledgeBaseIDs: []contracts.ID{input.KnowledgeBaseID}})
	if authErr != nil || fresh.PolicyVersion != scope.PolicyVersion || ctx.Err() != nil {
		return empty, contracts.ErrGrantDenied
	}
	next := ""
	if len(items) > input.PageSize {
		items = items[:input.PageSize]
		next, err = encodeGrantListCursor(grantListCursor{Space: string(scope.SpaceID), KB: string(input.KnowledgeBaseID), Subject: string(who.SubjectID), After: string(items[len(items)-1].SubjectID)})
		if err != nil {
			return empty, contracts.ErrGrantUnavailable
		}
	}
	return contracts.GrantPage{Items: items, NextCursor: next}, nil
}

var _ contracts.GrantListApplication = (*Search)(nil)
