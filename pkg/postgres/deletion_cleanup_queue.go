package postgres

import (
	"context"
	"database/sql"

	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type DeletionCleanupQueue struct {
	DB           *sql.DB
	Verify       VerifyScope
	AuthorizeJob func(context.Context, contracts.AccessScope, contracts.ResourceJob) error
}

const actionableDeleteJob = `(p.state='queued' OR (p.state='running' AND (
 EXISTS(SELECT 1 FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id WHERE m.job_id=j.id AND (x.state IN('pending','failed','unknown') OR (x.state='running' AND x.lease_until<=clock_timestamp())))
 OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id WHERE m.job_id=j.id AND x.state IN('pending','running')))))`

func (p DeletionCleanupQueue) DiscoverDeletionCleanup(ctx context.Context, input contracts.DeletionQueueRequest) (contracts.DeletionQueuePage, error) {
	var out contracts.DeletionQueuePage
	q, e := copyQuery(input)
	if ctx == nil || e != nil || p.AuthorizeJob == nil || q.Request.Deadline.IsZero() || !commandID(string(q.Request.RequestID)) {
		return out, ErrRepository
	}
	binding, e := cleanup.QueueBinding(q.Scope, q.WorkerID, q.Limit)
	if e != nil {
		return out, ErrRepository
	}
	after, upper := contracts.ID(""), contracts.ID("")
	if q.Cursor != nil {
		if q.Cursor.Binding != binding || !commandID(string(q.Cursor.AfterJobID)) || !commandID(string(q.Cursor.UpperJobID)) || q.Cursor.AfterJobID > q.Cursor.UpperJobID {
			return out, ErrRepository
		}
		after, upper = q.Cursor.AfterJobID, q.Cursor.UpperJobID
	}
	ctx, cancel := context.WithDeadline(ctx, q.Request.Deadline)
	defer cancel()
	e = withinSession(ctx, p.DB, q.Scope, p.Verify, func(c context.Context, s *session) error {
		if upper == "" {
			var maximum sql.NullString
			query := `SELECT max(j.id COLLATE "C") FROM akos_withdraw_v1.document_delete_jobs j JOIN akos_withdraw_v1.document_delete_job_progress p ON p.job_id=j.id WHERE j.space_id=$1 AND j.kb_id=$2 AND ` + actionableDeleteJob
			if e := s.tx.QueryRowContext(c, query, q.Scope.SpaceID, q.Scope.KnowledgeBaseIDs[0]).Scan(&maximum); e != nil {
				return ErrRepository
			}
			if !maximum.Valid {
				out.Items = []contracts.DeletionCleanupTicket{}
				return nil
			}
			upper = contracts.ID(maximum.String)
		}
		out.UpperJobID = upper
		rows, e := s.tx.QueryContext(c, `SELECT j.id FROM akos_withdraw_v1.document_delete_jobs j JOIN akos_withdraw_v1.document_delete_job_progress p ON p.job_id=j.id WHERE j.space_id=$1 AND j.kb_id=$2 AND j.id COLLATE "C">$3 COLLATE "C" AND j.id COLLATE "C"<=$4 COLLATE "C" AND `+actionableDeleteJob+` ORDER BY j.id COLLATE "C" LIMIT $5`, q.Scope.SpaceID, q.Scope.KnowledgeBaseIDs[0], after, upper, q.Limit+1)
		if e != nil {
			return ErrRepository
		}
		var ids []contracts.ID
		for rows.Next() {
			var id contracts.ID
			if rows.Scan(&id) != nil {
				rows.Close()
				return ErrRepository
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return ErrRepository
		}
		more := len(ids) > q.Limit
		if more {
			ids = ids[:q.Limit]
		}
		out.Items = make([]contracts.DeletionCleanupTicket, 0, len(ids))
		for _, id := range ids {
			view, e := loadDeletionJobView(c, s.tx, id, false)
			if e != nil {
				return e
			}
			if view.record.Job.SpaceID != q.Scope.SpaceID || view.kb != q.Scope.KnowledgeBaseIDs[0] {
				return ErrRepository
			}
			record, e := copyQuery(view.record)
			if e != nil {
				return e
			}
			if e = p.AuthorizeJob(c, cloneScope(q.Scope), record); e != nil {
				return e
			}
			out.Items = append(out.Items, contracts.DeletionCleanupTicket{JobID: id, ResourceID: view.record.ResourceID, SpaceID: view.record.Job.SpaceID, KnowledgeBaseID: view.kb, ProfileDigest: view.record.Job.ProfileDigest})
		}
		if more {
			out.Next = &contracts.DeletionQueueCursor{AfterJobID: ids[len(ids)-1], UpperJobID: upper, Binding: binding}
		}
		return nil
	})
	if e != nil {
		return contracts.DeletionQueuePage{}, e
	}
	return out, nil
}

var _ contracts.DeletionCleanupQueue = DeletionCleanupQueue{}
