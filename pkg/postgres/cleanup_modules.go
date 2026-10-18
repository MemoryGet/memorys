package postgres

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type CleanupModules struct {
	DB              *sql.DB
	Verify          VerifyScope
	VerifyOperation func(context.Context, contracts.AccessScope, contracts.CleanupOperation) error
}

func (p CleanupModules) ResolveCleanupModule(ctx context.Context, scope contracts.AccessScope, op contracts.CleanupOperation) (contracts.CleanupModuleBinding, error) {
	var out contracts.CleanupModuleBinding
	if ctx == nil || p.DB == nil || p.Verify == nil || p.VerifyOperation == nil || scope.Action != "publication.cleanup.execute" || scope.SpaceID != op.SpaceID || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] != op.KnowledgeBaseID || op.Target.Kind != "index" {
		return out, ErrRepository
	}
	q, e := copyQuery(op)
	if e != nil {
		return out, e
	}
	scope = cloneScope(scope)
	admission, e := copyQuery(q)
	if e != nil {
		return out, e
	}
	if p.VerifyOperation(ctx, cloneScope(scope), admission) != nil {
		return out, ErrCleanupLease
	}
	e = withinSession(ctx, p.DB, scope, p.Verify, func(c context.Context, s *session) error {
		var version string
		err := s.tx.QueryRowContext(c, `SELECT h.version::text,b.space_id,b.kb_id,b.binding_reference,b.module_instance_id,b.profile_digest,b.runtime_config_digest,b.provider,b.implementation_version,b.contract_version,b.generation_id,b.backend_epoch_id,b.channel
   FROM akos_withdraw_v1.cleanup_module_heads h JOIN akos_withdraw_v1.cleanup_module_snapshots v ON v.space_id=h.space_id AND v.kb_id=h.kb_id AND v.version=h.version AND v.sealed
   JOIN akos_withdraw_v1.cleanup_module_bindings b ON b.space_id=h.space_id AND b.kb_id=h.kb_id AND b.version=h.version
   WHERE h.space_id=$1::text AND h.kb_id=$2::text AND b.binding_reference=$3::text`, q.SpaceID, q.KnowledgeBaseID, q.Target.BindingReference).Scan(&version, &out.SpaceID, &out.KnowledgeBaseID, &out.BindingReference, &out.ModuleInstanceID, &out.ProfileDigest, &out.RuntimeConfigDigest, &out.Provider, &out.ImplementationVersion, &out.ContractVersion, &out.GenerationID, &out.BackendEpochID, &out.Channel)
		if err != nil {
			return ErrRepository
		}
		out.Version, err = strconv.ParseUint(version, 10, 64)
		if err != nil || out.Version == 0 {
			return ErrRepository
		}
		return nil
	})
	if e != nil {
		return contracts.CleanupModuleBinding{}, e
	}
	// A new primary read prevents a repeatable transaction snapshot from masking
	// an activation that occurred while this binding was being collected.
	var current string
	if p.DB.QueryRowContext(ctx, `SELECT version::text FROM akos_withdraw_v1.cleanup_module_heads WHERE space_id=$1::text AND kb_id=$2::text`, q.SpaceID, q.KnowledgeBaseID).Scan(&current) != nil || current != strconv.FormatUint(out.Version, 10) || p.Verify(ctx, cloneScope(scope)) != nil || ctx.Err() != nil {
		return contracts.CleanupModuleBinding{}, ErrRepository
	}
	return out, nil
}

var _ contracts.CleanupModuleSource = CleanupModules{}
