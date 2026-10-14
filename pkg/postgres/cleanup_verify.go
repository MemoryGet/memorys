package postgres

import (
	"context"
	"reflect"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// VerifyLease proves only current database ownership and frozen source facts.
// Actual module identity/config and backend effects need independent checks.
func (a CleanupTasks) VerifyLease(ctx context.Context, scope contracts.AccessScope, lease contracts.CleanupLease) error {
	if scope.Action != "publication.cleanup.execute" || scope.SubjectID != lease.WorkerID {
		return ErrCleanupLease
	}
	return withinSession(ctx, a.DB, scope, a.Verify, func(c context.Context, s *session) error {
		plan, err := (planRepo{s}).Load(c, scope, lease.PlanID)
		if err != nil {
			return err
		}
		if plan.SpaceID != lease.SpaceID || plan.KnowledgeBaseID != lease.KnowledgeBaseID || plan.DocumentID != lease.DocumentID || plan.RevisionID != lease.RevisionID || plan.GenerationSetID != lease.GenerationSetID || plan.PublicationEpoch != lease.PublicationEpoch {
			return ErrCleanupLease
		}
		matched := false
		for _, target := range plan.Targets {
			if target.ID == lease.TargetID && reflect.DeepEqual(target, lease.Target) {
				matched = true
				break
			}
		}
		if !matched {
			return ErrCleanupLease
		}
		if err := s.enter(c, scope); err != nil {
			return err
		}
		defer s.mu.Unlock()
		var valid bool
		err = s.tx.QueryRowContext(c, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.cleanup_progress WHERE plan_id=$1::text AND target_id=$2::text AND worker_id=$3::text AND fencing_token=$4::numeric AND row_version=$5::numeric AND attempt=$6::numeric AND state='running' AND lease_until=$7::timestamptz AND lease_until>clock_timestamp())`, string(lease.PlanID), string(lease.TargetID), string(lease.WorkerID), strconv.FormatUint(lease.FencingToken, 10), strconv.FormatUint(lease.RowVersion, 10), strconv.FormatUint(lease.Attempt, 10), lease.ExpiresAt).Scan(&valid)
		if err != nil {
			return ErrRepository
		}
		if !valid {
			return ErrCleanupLease
		}
		return nil
	})
}
