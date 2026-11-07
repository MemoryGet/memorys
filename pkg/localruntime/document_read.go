package localruntime

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

func (s *Search) ReadDocument(ctx context.Context, input contracts.DocumentReadInput) (contracts.Document, error) {
	var empty contracts.Document
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.DocumentID == "" || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 {
		return empty, errors.New("document unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil || ctx.Err() != nil {
		return empty, errors.New("document unauthenticated")
	}
	_, digest, err := s.ProfileIdentity()
	if err != nil || digest != input.Request.ProfileDigest {
		return empty, errors.New("document profile changed")
	}
	var d contracts.Document
	var space, kb, epoch, version string
	var revision, generation sql.NullString
	var deleted bool
	if err := s.db.QueryRowContext(ctx, `SELECT space_id,kb_id,publication_epoch::text,row_version::text,active_revision_id,active_generation_set_id,deleted FROM akos_withdraw_v1.documents WHERE id=$1`, string(input.DocumentID)).Scan(&space, &kb, &epoch, &version, &revision, &generation, &deleted); err != nil {
		return empty, errors.New("document unavailable")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "knowledge.read", SpaceID: contracts.ID(space), KnowledgeBaseIDs: []contracts.ID{contracts.ID(kb)}, ResourceIDs: []contracts.ID{input.DocumentID}})
	if err != nil {
		return empty, errors.New("document denied")
	}
	d.ID, d.SpaceID, d.KnowledgeBaseID = input.DocumentID, contracts.ID(space), contracts.ID(kb)
	d.Title = documentTitle(ctx, s.db, d.SpaceID, d.KnowledgeBaseID, d.ID)
	d.PublicationEpoch, err = strconv.ParseUint(epoch, 10, 64)
	if err != nil {
		return empty, errors.New("document unavailable")
	}
	d.RowVersion, err = strconv.ParseUint(version, 10, 64)
	if err != nil || d.RowVersion == 0 {
		return empty, errors.New("document unavailable")
	}
	if revision.Valid {
		value := contracts.ID(revision.String)
		d.ActiveRevisionID = &value
	}
	if generation.Valid {
		value := contracts.ID(generation.String)
		d.ActiveGenerationSetID = &value
	}
	if deleted {
		at := time.Unix(0, 0).UTC()
		d.DeletedAt = &at
	}
	fresh, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "knowledge.read", SpaceID: d.SpaceID, KnowledgeBaseIDs: []contracts.ID{d.KnowledgeBaseID}, ResourceIDs: []contracts.ID{d.ID}})
	if err != nil || fresh.PolicyVersion != scope.PolicyVersion || ctx.Err() != nil {
		return empty, errors.New("document authorization changed")
	}
	return d, nil
}

var _ contracts.DocumentReadApplication = (*Search)(nil)
