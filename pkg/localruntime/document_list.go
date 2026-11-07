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

type documentCursor struct{ Space, KB, Subject, After string }

func encodeDocumentCursor(v documentCursor) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > 4096 || v.Space == "" || v.KB == "" || v.Subject == "" || v.After == "" {
		return "", errors.New("invalid document cursor")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func decodeDocumentCursor(raw string, expected documentCursor) (documentCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(raw) > 10000 || len(data) > 4096 {
		return documentCursor{}, errors.New("invalid document cursor")
	}
	var value documentCursor
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || expected.Space != "" && value.Space != expected.Space || expected.KB != "" && value.KB != expected.KB || expected.Subject != "" && value.Subject != expected.Subject || value.After == "" || len(value.After) > 200 {
		return documentCursor{}, errors.New("invalid document cursor")
	}
	return value, nil
}

func (s *Search) ListDocuments(ctx context.Context, input contracts.DocumentListInput) (contracts.DocumentPage, error) {
	var empty contracts.DocumentPage
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.KnowledgeBaseID == "" || input.PageSize < 1 || input.PageSize > 100 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 {
		return empty, errors.New("documents unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil {
		return empty, errors.New("documents unauthenticated")
	}
	_, digest, err := s.ProfileIdentity()
	if err != nil || digest != input.Request.ProfileDigest {
		return empty, errors.New("documents profile changed")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	spaceID, after := "", ""
	if input.Cursor != "" {
		cursor, err := decodeDocumentCursor(input.Cursor, documentCursor{KB: string(input.KnowledgeBaseID), Subject: string(who.SubjectID)})
		if err != nil {
			return empty, err
		}
		spaceID, after = cursor.Space, cursor.After
	}
	if spaceID == "" {
		rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT space_id FROM akos_withdraw_v1.documents WHERE kb_id=$1::text ORDER BY space_id LIMIT 100`, string(input.KnowledgeBaseID))
		if err != nil {
			return empty, errors.New("documents unavailable")
		}
		for rows.Next() {
			var candidate string
			if rows.Scan(&candidate) == nil {
				if _, e := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "knowledge.read", SpaceID: contracts.ID(candidate), KnowledgeBaseIDs: []contracts.ID{input.KnowledgeBaseID}}); e == nil {
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
	query := `SELECT id,publication_epoch::text,row_version::text,active_revision_id,active_generation_set_id,deleted FROM akos_withdraw_v1.documents WHERE space_id=$1::text AND kb_id=$2::text`
	args := []any{spaceID, string(input.KnowledgeBaseID)}
	if after != "" {
		query += ` AND id>$3::text`
		args = append(args, after)
	}
	query += ` ORDER BY id LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, input.PageSize+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return empty, errors.New("documents unavailable")
	}
	defer rows.Close()
	items := make([]contracts.Document, 0, input.PageSize+1)
	ids := make([]contracts.ID, 0, input.PageSize+1)
	for rows.Next() {
		var d contracts.Document
		var epoch, version string
		var revision, generation sql.NullString
		var deleted bool
		if err := rows.Scan(&d.ID, &epoch, &version, &revision, &generation, &deleted); err != nil {
			return empty, errors.New("documents unavailable")
		}
		d.SpaceID, d.KnowledgeBaseID = contracts.ID(spaceID), input.KnowledgeBaseID
		d.Title = documentTitle(ctx, s.db, d.SpaceID, d.KnowledgeBaseID, d.ID)
		if deleted {
			value := time.Unix(0, 0).UTC()
			d.DeletedAt = &value
		}
		d.PublicationEpoch, err = strconv.ParseUint(epoch, 10, 64)
		if err != nil {
			return empty, errors.New("documents unavailable")
		}
		d.RowVersion, err = strconv.ParseUint(version, 10, 64)
		if err != nil || d.RowVersion == 0 {
			return empty, errors.New("documents unavailable")
		}
		if revision.Valid {
			value := contracts.ID(revision.String)
			d.ActiveRevisionID = &value
		}
		if generation.Valid {
			value := contracts.ID(generation.String)
			d.ActiveGenerationSetID = &value
		}
		items = append(items, d)
		ids = append(ids, d.ID)
	}
	if rows.Err() != nil {
		return empty, errors.New("documents unavailable")
	}
	if _, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "knowledge.read", SpaceID: contracts.ID(spaceID), KnowledgeBaseIDs: []contracts.ID{input.KnowledgeBaseID}, ResourceIDs: ids}); err != nil {
		return empty, errors.New("documents denied")
	}
	next := ""
	if len(items) > input.PageSize {
		items = items[:input.PageSize]
		next, err = encodeDocumentCursor(documentCursor{Space: spaceID, KB: string(input.KnowledgeBaseID), Subject: string(who.SubjectID), After: string(items[len(items)-1].ID)})
		if err != nil {
			return empty, err
		}
	}
	return contracts.DocumentPage{Items: items, NextCursor: next}, nil
}

func documentTitle(ctx context.Context, db *sql.DB, space, kb, id contracts.ID) string {
	if db != nil {
		var title string
		if err := db.QueryRowContext(ctx, `SELECT title FROM akos_withdraw_v1.document_metadata WHERE space_id=$1 AND kb_id=$2 AND document_id=$3`, string(space), string(kb), string(id)).Scan(&title); err == nil && strings.TrimSpace(title) != "" {
			return title
		}
	}
	return string(id)
}

var _ contracts.DocumentListApplication = (*Search)(nil)
