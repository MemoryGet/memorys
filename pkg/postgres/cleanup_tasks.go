package postgres

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrCleanupLease = errors.New("cleanup lease unavailable or stale")

type CleanupTasks struct {
	DB     *sql.DB
	Verify VerifyScope
}

func (a CleanupTasks) Claim(ctx context.Context, scope contracts.AccessScope, planID, targetID, workerID contracts.ID, duration time.Duration) (contracts.CleanupLease, error) {
	var lease contracts.CleanupLease
	if scope.Action != "publication.cleanup.execute" || scope.SubjectID != workerID || duration < time.Millisecond || duration > 30*time.Second || duration%time.Millisecond != 0 {
		return lease, ErrCleanupLease
	}
	err := withinSession(ctx, a.DB, scope, a.Verify, func(c context.Context, s *session) error {
		plan, err := (planRepo{s}).Load(c, scope, planID)
		if err != nil {
			return err
		}
		found := false
		for _, target := range plan.Targets {
			if target.ID == targetID {
				lease.Target = target
				found = true
				break
			}
		}
		if !found {
			return ErrCleanupLease
		}
		if err := s.enter(c, scope); err != nil {
			return err
		}
		defer s.mu.Unlock()
		var token, attempt, version string
		err = s.tx.QueryRowContext(c, `UPDATE akos_withdraw_v1.cleanup_progress q SET state='running',worker_id=$3::text,lease_until=LEAST(clock_timestamp()+$4::bigint*interval '1 millisecond',$5::timestamptz),fencing_token=fencing_token+1,attempt=attempt+1,row_version=row_version+1
 WHERE q.plan_id=$1::text AND q.target_id=$2::text
 AND (q.state='pending' OR (q.state='running' AND q.lease_until<=clock_timestamp()))
 AND q.fencing_token<18446744073709551615 AND q.attempt<18446744073709551615 AND q.row_version<18446744073709551615 AND $5::timestamptz>clock_timestamp()
 AND EXISTS(SELECT 1 FROM akos_withdraw_v1.cleanup_plans p JOIN akos_withdraw_v1.command_operations o ON o.id=p.operation_id WHERE p.id=q.plan_id AND p.space_id=$6::text AND p.kb_id=$7::text AND o.state='committed')
 RETURNING q.fencing_token::text,q.attempt::text,q.row_version::text,q.lease_until`, string(planID), string(targetID), string(workerID), duration.Milliseconds(), scope.ExpiresAt, string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0])).Scan(&token, &attempt, &version, &lease.ExpiresAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCleanupLease
		}
		if err != nil {
			return ErrRepository
		}
		lease.FencingToken, err = strconv.ParseUint(token, 10, 64)
		if err != nil {
			return ErrRepository
		}
		lease.Attempt, err = strconv.ParseUint(attempt, 10, 64)
		if err != nil {
			return ErrRepository
		}
		lease.RowVersion, err = strconv.ParseUint(version, 10, 64)
		if err != nil {
			return ErrRepository
		}
		lease.SpaceID, lease.KnowledgeBaseID, lease.DocumentID = plan.SpaceID, plan.KnowledgeBaseID, plan.DocumentID
		lease.PlanID, lease.TargetID, lease.WorkerID = plan.ID, targetID, workerID
		lease.RevisionID, lease.GenerationSetID, lease.PublicationEpoch = plan.RevisionID, plan.GenerationSetID, plan.PublicationEpoch
		lease.ExpiresAt = lease.ExpiresAt.UTC()
		return nil
	})
	if err != nil {
		return contracts.CleanupLease{}, err
	}
	return lease, nil
}

func (a CleanupTasks) Finish(ctx context.Context, scope contracts.AccessScope, lease contracts.CleanupLease, outcome string, result contracts.ID) error {
	if scope.Action != "publication.cleanup.execute" || scope.SubjectID != lease.WorkerID || scope.SpaceID != lease.SpaceID || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] != lease.KnowledgeBaseID || lease.FencingToken == 0 || lease.RowVersion == 0 || lease.RowVersion == math.MaxUint64 || outcome != "succeeded" && outcome != "failed" && outcome != "unknown" || outcome == "succeeded" && !commandID(string(result)) || outcome != "succeeded" && result != "" {
		return ErrCleanupLease
	}
	return withinSession(ctx, a.DB, scope, a.Verify, func(c context.Context, s *session) error {
		if err := s.enter(c, scope); err != nil {
			return err
		}
		defer s.mu.Unlock()
		args := []any{string(lease.PlanID), string(lease.TargetID), string(lease.WorkerID), strconv.FormatUint(lease.FencingToken, 10), strconv.FormatUint(lease.RowVersion, 10), outcome, nil, string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), lease.ExpiresAt}
		if result != "" {
			args[6] = string(result)
		}
		updated, err := s.tx.ExecContext(c, `UPDATE akos_withdraw_v1.cleanup_progress q SET state=$6::text,result_reference=$7::text,lease_until=NULL,row_version=row_version+1
 WHERE q.plan_id=$1::text AND q.target_id=$2::text AND q.worker_id=$3::text AND q.fencing_token=$4::numeric AND q.row_version=$5::numeric AND q.state='running' AND q.lease_until=$10::timestamptz AND q.lease_until>clock_timestamp()
 AND EXISTS(SELECT 1 FROM akos_withdraw_v1.cleanup_plans p WHERE p.id=q.plan_id AND p.space_id=$8::text AND p.kb_id=$9::text)`, args...)
		if err != nil {
			return ErrRepository
		}
		n, err := updated.RowsAffected()
		if err != nil {
			return ErrRepository
		}
		if n == 1 {
			return nil
		}
		var same bool
		err = s.tx.QueryRowContext(c, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.cleanup_progress q JOIN akos_withdraw_v1.cleanup_plans p ON p.id=q.plan_id WHERE q.plan_id=$1::text AND q.target_id=$2::text AND q.worker_id=$3::text AND q.fencing_token=$4::numeric AND q.row_version=$5::numeric+1 AND q.state=$6::text AND q.result_reference IS NOT DISTINCT FROM $7::text AND p.space_id=$8::text AND p.kb_id=$9::text)`, args[:9]...).Scan(&same)
		if err != nil {
			return ErrRepository
		}
		if !same {
			return ErrCleanupLease
		}
		return nil
	})
}
