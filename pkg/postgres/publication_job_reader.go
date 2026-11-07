package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrPublicationJob = errors.New("publication job unavailable")

// PublicationTaskResourceReader projects a preparation task as a management
// job only after resolving its document binding and reauthorizing publish
// access. It never treats a worker lease as user authorization.
type PublicationTaskResourceReader struct {
	DB         *sql.DB
	Authorizer contracts.Authorizer
}

func (r PublicationTaskResourceReader) ReadResourceJob(ctx context.Context, input contracts.JobReadRequest) (contracts.ResourceJob, error) {
	var empty contracts.ResourceJob
	if ctx == nil || r.DB == nil || r.Authorizer == nil || input.JobID == "" || input.Principal.SubjectID == "" {
		return empty, contracts.ErrJobReadDenied
	}
	var raw []byte
	var space, kb, document string
	if err := r.DB.QueryRowContext(ctx, `SELECT h.payload,h.space_id,h.kb_id,i.document_id FROM akos_withdraw_v1.publication_task_heads h JOIN akos_withdraw_v1.publication_intents i ON i.space_id=h.space_id AND i.kb_id=h.kb_id AND i.job_id=h.id WHERE h.id=$1`, string(input.JobID)).Scan(&raw, &space, &kb, &document); err != nil || len(raw) == 0 || len(raw) > 32768 || space == "" || kb == "" || document == "" {
		return empty, contracts.ErrJobReadDenied
	}
	var job contracts.Job
	if json.Unmarshal(raw, &job) != nil || job.ID != input.JobID || job.SpaceID != contracts.ID(space) || job.Stage != "publication.prepare" || job.RowVersion == 0 || job.ProfileDigest == "" {
		return empty, contracts.ErrJobReadDenied
	}
	scope, err := r.Authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: input.Principal, Action: "publication.publish", SpaceID: contracts.ID(space), KnowledgeBaseIDs: []contracts.ID{contracts.ID(kb)}, ResourceIDs: []contracts.ID{contracts.ID(document)}})
	if err != nil || scope.Action != "publication.publish" || scope.SubjectID != input.Principal.SubjectID || scope.SpaceID != contracts.ID(space) || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] != contracts.ID(kb) || !scope.ExpiresAt.After(time.Now()) {
		return empty, contracts.ErrJobReadDenied
	}
	return contracts.ResourceJob{ResourceID: contracts.ID(document), KnowledgeBaseID: contracts.ID(kb), Job: job}, nil
}
