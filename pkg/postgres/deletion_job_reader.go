package postgres

import (
	"context"
	"database/sql"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type DeletionJobReader struct {
	DB         *sql.DB
	Authorizer contracts.Authorizer
}

func (p DeletionJobReader) ReadResourceJob(ctx context.Context, input contracts.JobReadRequest) (contracts.ResourceJob, error) {
	var empty contracts.ResourceJob
	q, e := copyQuery(input)
	if ctx == nil || e != nil || p.DB == nil || p.Authorizer == nil || q.Request.Deadline.IsZero() || !commandID(string(q.Request.RequestID)) || !commandID(string(q.JobID)) {
		return empty, ErrRepository
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrRepository
	}
	ctx, cancel := context.WithDeadline(ctx, q.Request.Deadline)
	defer cancel()
	load := func() (deletionJobView, error) { return loadDeletionJobView(ctx, p.DB, q.JobID, false) }
	authorize := func(o deletionJobView) (contracts.AccessScope, error) {
		who, e := copyQuery(q.Principal)
		if e != nil {
			return contracts.AccessScope{}, ErrRepository
		}
		scope, e := p.Authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "resource.view_job", SpaceID: o.record.Job.SpaceID, KnowledgeBaseIDs: []contracts.ID{o.kb}, ResourceIDs: []contracts.ID{o.record.ResourceID}})
		if e != nil || scope.Action != "resource.view_job" || scope.SubjectID != q.Principal.SubjectID || scope.SpaceID != o.record.Job.SpaceID || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] != o.kb || scope.PolicyVersion == 0 || scope.ScopeID == "" || !scope.ExpiresAt.After(time.Now()) {
			return contracts.AccessScope{}, contracts.ErrJobReadDenied
		}
		return scope, nil
	}
	before, e := load()
	if e != nil {
		return empty, e
	}
	scope, e := authorize(before)
	if e != nil {
		return empty, e
	}
	after, e := load()
	if e != nil || before.kb != after.kb || before.record.ResourceID != after.record.ResourceID || before.record.Job.SpaceID != after.record.Job.SpaceID || before.record.Job.OperationID != after.record.Job.OperationID || before.record.Job.ProfileDigest != after.record.Job.ProfileDigest || before.record.Job.Stage != after.record.Job.Stage || after.record.Job.RowVersion < before.record.Job.RowVersion || after.record.Job.RowVersion == before.record.Job.RowVersion && !reflect.DeepEqual(before, after) {
		return empty, ErrRepository
	}
	fresh, e := authorize(after)
	if e != nil || fresh.PolicyVersion != scope.PolicyVersion || ctx.Err() != nil {
		return empty, contracts.ErrJobReadDenied
	}
	return after.record, nil
}

var _ contracts.ResourceJobReader = DeletionJobReader{}
