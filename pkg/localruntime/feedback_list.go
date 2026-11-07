package localruntime

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

type feedbackCursor struct {
	Space   string `json:"space"`
	KB      string `json:"kb"`
	Subject string `json:"subject"`
	After   string `json:"after"`
}

func encodeFeedbackCursor(value feedbackCursor) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 4096 || value.Space == "" || value.KB == "" || value.Subject == "" || value.After == "" {
		return "", errors.New("invalid feedback cursor")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeFeedbackCursor(raw, kb, subject string) (string, string, error) {
	if raw == "" || len(raw) > 10000 {
		return "", "", errors.New("invalid feedback cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > 4096 {
		return "", "", errors.New("invalid feedback cursor")
	}
	var value feedbackCursor
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || value.KB != kb || value.Subject != subject || value.Space == "" || value.After == "" || len(value.After) > 200 {
		return "", "", errors.New("invalid feedback cursor")
	}
	return value.Space, value.After, nil
}

func (s *Search) ListFeedback(ctx context.Context, input contracts.FeedbackListInput) (contracts.FeedbackPage, error) {
	var empty contracts.FeedbackPage
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.KnowledgeBaseID == "" || input.PageSize < 1 || input.PageSize > 100 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 {
		return empty, errors.New("feedback unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil || ctx.Err() != nil {
		return empty, errors.New("feedback unauthenticated")
	}
	version, digest, err := s.ProfileIdentity()
	if err != nil || version == 0 || digest != input.Request.ProfileDigest {
		return empty, errors.New("feedback profile changed")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	spaceID, after := "", ""
	if input.Cursor != "" {
		spaceID, after, err = decodeFeedbackCursor(input.Cursor, string(input.KnowledgeBaseID), string(who.SubjectID))
		if err != nil {
			return empty, err
		}
	}
	if spaceID == "" {
		rows, queryErr := s.db.QueryContext(ctx, `SELECT DISTINCT space_id FROM akos_withdraw_v1.feedback_records WHERE kb_id=$1::text ORDER BY space_id LIMIT 100`, string(input.KnowledgeBaseID))
		if queryErr != nil {
			return empty, errors.New("feedback unavailable")
		}
		for rows.Next() {
			var candidate string
			if rows.Scan(&candidate) == nil {
				if _, authErr := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "kb.manage_feedback", SpaceID: contracts.ID(candidate), KnowledgeBaseIDs: []contracts.ID{input.KnowledgeBaseID}}); authErr == nil {
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
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "kb.manage_feedback", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{input.KnowledgeBaseID}})
	if err != nil {
		return empty, errors.New("feedback denied")
	}
	query := `SELECT f.id,f.space_id,f.kb_id,f.document_id,f.revision_id,f.block_id,f.target_request_id,f.kind,f.comment,COALESCE(p.state,f.state),f.created_at FROM akos_withdraw_v1.feedback_records f LEFT JOIN LATERAL (SELECT state FROM akos_withdraw_v1.feedback_proposals p WHERE p.feedback_id=f.id ORDER BY p.created_at DESC LIMIT 1) p ON true WHERE f.space_id=$1::text AND f.kb_id=$2::text`
	args := []any{string(scope.SpaceID), string(input.KnowledgeBaseID)}
	if after != "" {
		query += ` AND id>$3::text`
		args = append(args, after)
	}
	query += ` ORDER BY id LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, input.PageSize+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "feedback_proposals") {
		// The proposal migration is optional for existing feedback-only
		// deployments. Preserve the original list capability until it is applied.
		query = `SELECT id,space_id,kb_id,document_id,revision_id,block_id,target_request_id,kind,comment,state,created_at FROM akos_withdraw_v1.feedback_records WHERE space_id=$1::text AND kb_id=$2::text`
		args = []any{string(scope.SpaceID), string(input.KnowledgeBaseID)}
		if after != "" {
			query += ` AND id>$3::text`
			args = append(args, after)
		}
		query += ` ORDER BY id LIMIT $` + strconv.Itoa(len(args)+1)
		args = append(args, input.PageSize+1)
		rows, err = s.db.QueryContext(ctx, query, args...)
	}
	if err != nil {
		return empty, errors.New("feedback unavailable")
	}
	defer rows.Close()
	items := make([]contracts.FeedbackItem, 0, input.PageSize+1)
	for rows.Next() {
		var item contracts.FeedbackItem
		var documentID, revisionID, blockID, targetRequestID sql.NullString
		if err := rows.Scan(&item.ID, &item.SpaceID, &item.KnowledgeBaseID, &documentID, &revisionID, &blockID, &targetRequestID, &item.Kind, &item.Comment, &item.State, &item.CreatedAt); err != nil {
			return empty, errors.New("feedback unavailable")
		}
		if documentID.Valid {
			item.DocumentID = contracts.ID(documentID.String)
		}
		if revisionID.Valid {
			item.RevisionID = contracts.ID(revisionID.String)
		}
		if blockID.Valid {
			item.BlockID = contracts.ID(blockID.String)
		}
		if targetRequestID.Valid {
			item.TargetRequestID = contracts.ID(targetRequestID.String)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil || ctx.Err() != nil {
		return empty, errors.New("feedback unavailable")
	}
	fresh, authErr := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "kb.manage_feedback", SpaceID: scope.SpaceID, KnowledgeBaseIDs: []contracts.ID{input.KnowledgeBaseID}})
	if authErr != nil || fresh.PolicyVersion != scope.PolicyVersion {
		return empty, errors.New("feedback authorization changed")
	}
	next := ""
	if len(items) > input.PageSize {
		items = items[:input.PageSize]
		next, err = encodeFeedbackCursor(feedbackCursor{Space: string(scope.SpaceID), KB: string(input.KnowledgeBaseID), Subject: string(who.SubjectID), After: string(items[len(items)-1].ID)})
		if err != nil {
			return empty, errors.New("feedback cursor unavailable")
		}
	}
	return contracts.FeedbackPage{Items: items, NextCursor: next}, nil
}

var _ contracts.FeedbackListApplication = (*Search)(nil)
