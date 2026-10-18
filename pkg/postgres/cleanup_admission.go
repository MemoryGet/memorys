package postgres

import (
	"context"
	"reflect"

	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

// VerifyDeletionOperation admits only a currently leased frozen deletion member.
// Module deployment registration and remote invocation grants remain separate.
func (a CleanupTasks) VerifyDeletionOperation(ctx context.Context, scope contracts.AccessScope, op contracts.CleanupOperation) error {
	if scope.Action != "publication.cleanup.execute" || op.SpaceID != scope.SpaceID || len(scope.KnowledgeBaseIDs) != 1 || op.KnowledgeBaseID != scope.KnowledgeBaseIDs[0] {
		return ErrCleanupLease
	}
	return withinSession(ctx, a.DB, scope, a.Verify, func(c context.Context, s *session) error {
		plan, e := (planRepo{s}).Load(c, scope, op.PlanID)
		if e != nil {
			return e
		}
		found := false
		for _, target := range plan.Targets {
			if target.ID != op.TargetID {
				continue
			}
			expected, e := cleanup.Operation(contracts.CleanupLease{SpaceID: plan.SpaceID, KnowledgeBaseID: plan.KnowledgeBaseID, DocumentID: plan.DocumentID, PlanID: plan.ID, TargetID: target.ID, RevisionID: plan.RevisionID, GenerationSetID: plan.GenerationSetID, PublicationEpoch: plan.PublicationEpoch, Target: target})
			if e != nil || !reflect.DeepEqual(expected, op) {
				return ErrCleanupLease
			}
			found = true
		}
		if !found {
			return ErrCleanupLease
		}
		var valid bool
		e = s.tx.QueryRowContext(c, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.cleanup_progress x
   JOIN akos_withdraw_v1.document_delete_job_targets m ON m.plan_id=x.plan_id AND m.target_id=x.target_id
   JOIN akos_withdraw_v1.document_delete_jobs j ON j.id=m.job_id
   JOIN akos_withdraw_v1.document_delete_job_progress p ON p.job_id=j.id AND p.state='running'
   JOIN akos_withdraw_v1.command_operations o ON o.id=j.operation_id AND o.state='committed' AND o.action='delete'
   JOIN akos_withdraw_v1.documents d ON d.id=j.document_id AND d.deleted
   WHERE x.plan_id=$1::text AND x.target_id=$2::text AND x.worker_id=$3::text AND x.state='running' AND x.lease_until>clock_timestamp()
   AND j.space_id=$4::text AND j.kb_id=$5::text AND j.document_id=$6::text)`, op.PlanID, op.TargetID, scope.SubjectID, op.SpaceID, op.KnowledgeBaseID, op.DocumentID).Scan(&valid)
		if e != nil {
			return ErrRepository
		}
		if !valid {
			return ErrCleanupLease
		}
		return nil
	})
}
