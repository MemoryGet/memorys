package postgres

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func (s *session) loadWithdrawalCurrent(ctx context.Context, scope contracts.AccessScope, doc contracts.ID) (snapshot contracts.WithdrawalSnapshot, err error) {
	if scope.Action != "publication.withdraw" {
		return snapshot, ErrRepository
	}
	if err = s.enter(ctx, scope); err != nil {
		return snapshot, err
	}
	defer s.mu.Unlock()
	var epoch, version string
	var revision, set sql.NullString
	err = s.tx.QueryRowContext(ctx, `SELECT space_id,kb_id,id,publication_epoch::text,row_version::text,active_revision_id,active_generation_set_id FROM akos_withdraw_v1.documents WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text AND NOT deleted FOR UPDATE`, string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), string(doc)).Scan(&snapshot.SpaceID, &snapshot.KnowledgeBaseID, &snapshot.DocumentID, &epoch, &version, &revision, &set)
	if err != nil || !revision.Valid || !set.Valid {
		return contracts.WithdrawalSnapshot{}, ErrRepository
	}
	snapshot.RevisionID, snapshot.GenerationSetID = contracts.ID(revision.String), contracts.ID(set.String)
	snapshot.PublicationEpoch, err = strconv.ParseUint(epoch, 10, 64)
	if err != nil || snapshot.PublicationEpoch == 0 {
		return contracts.WithdrawalSnapshot{}, ErrRepository
	}
	snapshot.RowVersion, err = strconv.ParseUint(version, 10, 64)
	if err != nil || snapshot.RowVersion == 0 {
		return contracts.WithdrawalSnapshot{}, ErrRepository
	}
	args := []any{string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), string(doc), revision.String, set.String, epoch}
	var expected int
	err = s.tx.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.index_generations WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND revision_id=$4::text AND generation_set_id=$5::text`, args[:5]...).Scan(&expected)
	if err != nil || expected < 1 || expected > 3 {
		return contracts.WithdrawalSnapshot{}, ErrRepository
	}
	rows, err := s.tx.QueryContext(ctx, `SELECT 'index' AS kind,b.id AS binding_reference,b.module_instance_id,b.profile_digest,g.id,g.backend_epoch_id,g.channel
 FROM akos_withdraw_v1.index_generations g LEFT JOIN akos_withdraw_v1.binding_definitions b
 ON b.space_id=g.space_id AND b.kb_id=g.kb_id AND b.kind='index' AND b.module_instance_id=g.module_instance_id AND b.profile_digest=g.profile_digest AND b.definition->>'generation_id'=g.id
 WHERE g.space_id=$1::text AND g.kb_id=$2::text AND g.document_id=$3::text AND g.revision_id=$4::text AND g.generation_set_id=$5::text
 UNION ALL SELECT d.kind,d.binding_reference,d.module_instance_id,d.profile_digest,NULL,NULL,NULL
 FROM akos_withdraw_v1.publication_cleanup_dependencies d
 WHERE d.space_id=$1::text AND d.kb_id=$2::text AND d.document_id=$3::text AND d.revision_id=$4::text AND d.generation_set_id=$5::text AND d.publication_epoch=$6::numeric
 ORDER BY kind,binding_reference LIMIT 1001`, args...)
	if err != nil {
		return contracts.WithdrawalSnapshot{}, ErrRepository
	}
	defer rows.Close()
	seen := map[contracts.ID]bool{}
	indexCount := 0
	for rows.Next() {
		var target contracts.CleanupTarget
		var generation, backend, channel sql.NullString
		if rows.Scan(&target.Kind, &target.BindingReference, &target.ModuleInstanceID, &target.ProfileDigest, &generation, &backend, &channel) != nil {
			return contracts.WithdrawalSnapshot{}, ErrRepository
		}
		target.ID = target.BindingReference
		if target.ID == "" || seen[target.ID] {
			return contracts.WithdrawalSnapshot{}, ErrRepository
		}
		seen[target.ID] = true
		target.GenerationID, target.BackendEpochID = idPointer(generation), idPointer(backend)
		if channel.Valid {
			c := contracts.Channel(channel.String)
			target.Channel = &c
		}
		if target.Kind == "index" {
			indexCount++
		}
		snapshot.Targets = append(snapshot.Targets, target)
		if len(snapshot.Targets) > 1000 {
			return contracts.WithdrawalSnapshot{}, ErrRepository
		}
	}
	if rows.Err() != nil || indexCount != expected {
		return contracts.WithdrawalSnapshot{}, ErrRepository
	}
	return snapshot, nil
}
