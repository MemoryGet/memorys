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

type historyCursor struct{ Space, KB, Document, Subject, After string }

func encodeHistoryCursor(v historyCursor) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > 4096 || v.Space == "" || v.KB == "" || v.Document == "" || v.Subject == "" || v.After == "" {
		return "", errors.New("invalid history cursor")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeHistoryCursor(raw string, expected historyCursor) (historyCursor, error) {
	if raw == "" || len(raw) > 10000 {
		return historyCursor{}, errors.New("invalid history cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > 4096 {
		return historyCursor{}, errors.New("invalid history cursor")
	}
	var value historyCursor
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || value.Space != expected.Space || value.KB != expected.KB || value.Document != expected.Document || value.Subject != expected.Subject || value.After == "" || len(value.After) > 200 {
		return historyCursor{}, errors.New("invalid history cursor")
	}
	return value, nil
}

func (s *Search) ListPublicationHistory(ctx context.Context, input contracts.PublicationHistoryListInput) (contracts.PublicationHistoryPage, error) {
	var empty contracts.PublicationHistoryPage
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.DocumentID == "" || input.PageSize < 1 || input.PageSize > 100 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 {
		return empty, errors.New("history unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("history unauthenticated")
	}
	_, digest, err := s.ProfileIdentity()
	if err != nil || digest != input.Request.ProfileDigest {
		return empty, errors.New("history profile changed")
	}
	var spaceID, kbID string
	if err := s.db.QueryRowContext(ctx, `SELECT space_id,kb_id FROM akos_withdraw_v1.documents WHERE id=$1::text`, string(input.DocumentID)).Scan(&spaceID, &kbID); err != nil {
		return empty, errors.New("history unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	_, err = authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "publication.history.read", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{contracts.ID(kbID)}, ResourceIDs: []contracts.ID{input.DocumentID}})
	if err != nil {
		return empty, errors.New("history denied")
	}
	expected := historyCursor{Space: spaceID, KB: kbID, Document: string(input.DocumentID), Subject: string(who.SubjectID)}
	after := ""
	if input.Cursor != "" {
		cursor, err := decodeHistoryCursor(input.Cursor, expected)
		if err != nil {
			return empty, err
		}
		after = cursor.After
	}
	query := `SELECT id,operation_id,previous_epoch::text,publication_epoch::text,action,revision_id,actor_id,reason,occurred_at FROM akos_withdraw_v1.publication_history WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text`
	args := []any{spaceID, kbID, string(input.DocumentID)}
	if after != "" {
		query += ` AND id>$4::text`
		args = append(args, after)
	}
	query += ` ORDER BY id LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, input.PageSize+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return empty, errors.New("history unavailable")
	}
	defer rows.Close()
	items := make([]contracts.PublicationHistory, 0, input.PageSize+1)
	for rows.Next() {
		var h contracts.PublicationHistory
		var previous, epoch string
		var revision sql.NullString
		if err := rows.Scan(&h.ID, &h.OperationID, &previous, &epoch, &h.Action, &revision, &h.ActorID, &h.Reason, &h.OccurredAt); err != nil {
			return empty, errors.New("history unavailable")
		}
		h.SpaceID, h.KnowledgeBaseID, h.DocumentID = contracts.ID(spaceID), contracts.ID(kbID), input.DocumentID
		if revision.Valid {
			value := contracts.ID(revision.String)
			h.RevisionID = &value
		}
		h.PreviousEpoch, err = strconv.ParseUint(previous, 10, 64)
		if err != nil {
			return empty, errors.New("history unavailable")
		}
		h.PublicationEpoch, err = strconv.ParseUint(epoch, 10, 64)
		if err != nil {
			return empty, errors.New("history unavailable")
		}
		items = append(items, h)
	}
	if rows.Err() != nil {
		return empty, errors.New("history unavailable")
	}
	next := ""
	if len(items) > input.PageSize {
		items = items[:input.PageSize]
		next, err = encodeHistoryCursor(historyCursor{Space: spaceID, KB: kbID, Document: string(input.DocumentID), Subject: string(who.SubjectID), After: string(items[len(items)-1].ID)})
		if err != nil {
			return empty, err
		}
	}
	return contracts.PublicationHistoryPage{Items: items, NextCursor: next}, nil
}

var _ contracts.PublicationHistoryListApplication = (*Search)(nil)
