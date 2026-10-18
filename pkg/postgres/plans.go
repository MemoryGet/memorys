package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"strconv"
	"time"
)

type planRepo struct{ s *session }

func (r planRepo) Load(ctx context.Context, scope contracts.AccessScope, id contracts.ID) (contracts.CleanupPlan, error) {
	var p contracts.CleanupPlan
	if r.s.enter(ctx, scope) != nil {
		return p, ErrRepository
	}
	var epoch, created string
	err := r.s.tx.QueryRowContext(ctx, `SELECT id,history_id,operation_id,space_id,kb_id,document_id,revision_id,generation_set_id,publication_epoch::text,profile_digest,created_at FROM akos_withdraw_v1.cleanup_plans WHERE id=$1::text AND space_id=$2::text AND kb_id=$3::text`, string(id), string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0])).Scan(&p.ID, &p.HistoryID, &p.OperationID, &p.SpaceID, &p.KnowledgeBaseID, &p.DocumentID, &p.RevisionID, &p.GenerationSetID, &epoch, &p.ProfileDigest, &created)
	if err != nil {
		r.s.mu.Unlock()
		return contracts.CleanupPlan{}, ErrRepository
	}
	p.PublicationEpoch, err = strconv.ParseUint(epoch, 10, 64)
	if err != nil {
		r.s.mu.Unlock()
		return contracts.CleanupPlan{}, ErrRepository
	}
	p.CreatedAt, err = time.Parse(timeFormat, created)
	if err != nil {
		r.s.mu.Unlock()
		return contracts.CleanupPlan{}, ErrRepository
	}
	rows, err := r.s.tx.QueryContext(ctx, `SELECT id,kind,module_instance_id,binding_reference,profile_digest,generation_id,backend_epoch_id,channel FROM akos_withdraw_v1.cleanup_targets WHERE plan_id=$1::text ORDER BY id LIMIT 1001`, string(p.ID))
	if err != nil {
		r.s.mu.Unlock()
		return contracts.CleanupPlan{}, ErrRepository
	}
	for rows.Next() {
		var t contracts.CleanupTarget
		var generation, backend, channel sql.NullString
		if rows.Scan(&t.ID, &t.Kind, &t.ModuleInstanceID, &t.BindingReference, &t.ProfileDigest, &generation, &backend, &channel) != nil {
			err = ErrRepository
			break
		}
		t.GenerationID, t.BackendEpochID = idPointer(generation), idPointer(backend)
		if channel.Valid {
			c := contracts.Channel(channel.String)
			t.Channel = &c
		}
		p.Targets = append(p.Targets, t)
		if len(p.Targets) > 1000 {
			err = ErrRepository
			break
		}
	}
	if rows.Err() != nil {
		err = ErrRepository
	}
	rows.Close()
	r.s.mu.Unlock()
	if err != nil {
		return contracts.CleanupPlan{}, ErrRepository
	}
	h, err := (historyRepo{r.s}).Load(ctx, scope, p.HistoryID)
	if err != nil || contracts.ValidatePublicationCleanup(p, h) != nil {
		return contracts.CleanupPlan{}, ErrRepository
	}
	return p, nil
}
func (r planRepo) Append(ctx context.Context, scope contracts.AccessScope, p contracts.CleanupPlan) error {
	if len(p.Targets) > 1000 {
		return ErrRepository
	}
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > 4<<20 {
		return ErrRepository
	}
	var owned contracts.CleanupPlan
	if json.Unmarshal(raw, &owned) != nil {
		return ErrRepository
	}
	p = owned
	h, err := (historyRepo{r.s}).Load(ctx, scope, p.HistoryID)
	if err != nil || contracts.ValidatePublicationCleanup(p, h) != nil {
		return ErrRepository
	}
	if r.s.enter(ctx, scope) != nil {
		return ErrRepository
	}
	defer r.s.mu.Unlock()
	if scope.Action != "publication.withdraw" && scope.Action != "publication.publish" && scope.Action != "document.delete" || scope.Action == "publication.withdraw" && h.Action != "withdraw" || scope.Action == "publication.publish" && h.Action != "publish" || scope.Action == "document.delete" && h.Action != "delete" {
		return ErrRepository
	}
	_, err = r.s.tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.cleanup_plans(id,history_id,operation_id,space_id,kb_id,document_id,revision_id,generation_set_id,publication_epoch,profile_digest,created_at,history_action) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::text,$8::text,$9::text::numeric,$10::text,$11::text,$12::text)`, string(p.ID), string(p.HistoryID), string(p.OperationID), string(p.SpaceID), string(p.KnowledgeBaseID), string(p.DocumentID), string(p.RevisionID), string(p.GenerationSetID), strconv.FormatUint(p.PublicationEpoch, 10), string(p.ProfileDigest), p.CreatedAt.UTC().Format(timeFormat), h.Action)
	if err != nil {
		return ErrRepository
	}
	for _, t := range p.Targets {
		var channel any
		if t.Channel != nil {
			channel = string(*t.Channel)
		}
		_, err = r.s.tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.cleanup_targets(space_id,kb_id,document_id,revision_id,generation_set_id,plan_id,id,kind,module_instance_id,binding_reference,profile_digest,generation_id,backend_epoch_id,channel) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::text,$8::text,$9::text,$10::text,$11::text,$12::text,$13::text,$14::text)`, string(p.SpaceID), string(p.KnowledgeBaseID), string(p.DocumentID), string(p.RevisionID), string(p.GenerationSetID), string(p.ID), string(t.ID), t.Kind, string(t.ModuleInstanceID), string(t.BindingReference), string(t.ProfileDigest), nullableID(t.GenerationID), nullableID(t.BackendEpochID), channel)
		if err != nil {
			return ErrRepository
		}
	}
	return nil
}
