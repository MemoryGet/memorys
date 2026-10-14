package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"strconv"
	"time"
)

type historyRepo struct{ s *session }
type actorWire struct {
	SubjectID   contracts.ID    `json:"subject_id"`
	SubjectType string          `json:"subject_type"`
	Issuer      string          `json:"issuer"`
	ClientID    contracts.ID    `json:"client_id,omitempty"`
	Delegation  *delegationWire `json:"delegation,omitempty"`
}
type delegationWire struct {
	UserID         contracts.ID `json:"user_id"`
	ClientID       contracts.ID `json:"client_id"`
	ProofReference string       `json:"proof_reference"`
}

func encodeActor(p contracts.Principal) ([]byte, error) {
	a := actorWire{p.SubjectID, p.SubjectType, p.Issuer, p.ClientID, nil}
	if p.Delegation != nil {
		a.Delegation = &delegationWire{p.Delegation.UserID, p.Delegation.ClientID, p.Delegation.ProofReference}
	}
	return json.Marshal(a)
}
func decodeActor(raw []byte) (contracts.Principal, error) {
	var a actorWire
	if json.Unmarshal(raw, &a) != nil {
		return contracts.Principal{}, ErrRepository
	}
	p := contracts.Principal{SubjectID: a.SubjectID, SubjectType: a.SubjectType, Issuer: a.Issuer, ClientID: a.ClientID}
	if a.Delegation != nil {
		p.Delegation = &contracts.Delegation{UserID: a.Delegation.UserID, ClientID: a.Delegation.ClientID, ProofReference: a.Delegation.ProofReference}
	}
	return p, nil
}

func (r historyRepo) Load(ctx context.Context, scope contracts.AccessScope, id contracts.ID) (contracts.PublicationHistory, error) {
	var h contracts.PublicationHistory
	if r.s.enter(ctx, scope) != nil {
		return h, ErrRepository
	}
	defer r.s.mu.Unlock()
	var previous, current, occurred string
	var pr, pg, rev, gen sql.NullString
	var actor []byte
	err := r.s.tx.QueryRowContext(ctx, `SELECT id,operation_id,space_id,kb_id,document_id,previous_epoch::text,publication_epoch::text,action,previous_revision_id,previous_generation_set_id,revision_id,generation_set_id,actor_id,actor,reason,occurred_at,command_digest FROM akos_withdraw_v1.publication_history WHERE id=$1::text AND space_id=$2::text AND kb_id=$3::text`, string(id), string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0])).Scan(&h.ID, &h.OperationID, &h.SpaceID, &h.KnowledgeBaseID, &h.DocumentID, &previous, &current, &h.Action, &pr, &pg, &rev, &gen, &h.ActorID, &actor, &h.Reason, &occurred, &h.CommandDigest)
	if err != nil {
		return contracts.PublicationHistory{}, ErrRepository
	}
	h.PreviousEpoch, err = strconv.ParseUint(previous, 10, 64)
	if err != nil {
		return contracts.PublicationHistory{}, ErrRepository
	}
	h.PublicationEpoch, err = strconv.ParseUint(current, 10, 64)
	if err != nil {
		return contracts.PublicationHistory{}, ErrRepository
	}
	h.OccurredAt, err = time.Parse(timeFormat, occurred)
	if err != nil {
		return contracts.PublicationHistory{}, ErrRepository
	}
	h.Actor, err = decodeActor(actor)
	if err != nil {
		return contracts.PublicationHistory{}, err
	}
	h.PreviousRevisionID, h.PreviousGenerationSetID, h.RevisionID, h.GenerationSetID = idPointer(pr), idPointer(pg), idPointer(rev), idPointer(gen)
	if contracts.ValidatePublicationHistory(h) != nil {
		return contracts.PublicationHistory{}, ErrRepository
	}
	return h, nil
}
func (r historyRepo) Append(ctx context.Context, scope contracts.AccessScope, h contracts.PublicationHistory) error {
	if r.s.enter(ctx, scope) != nil {
		return ErrRepository
	}
	defer r.s.mu.Unlock()
	if contracts.ValidatePublicationHistory(h) != nil || h.SpaceID != scope.SpaceID || h.KnowledgeBaseID != scope.KnowledgeBaseIDs[0] || h.ActorID != scope.SubjectID {
		return ErrRepository
	}
	expected := map[string]contracts.Action{"publish": "publication.publish", "withdraw": "publication.withdraw", "delete": "document.delete"}[h.Action]
	if scope.Action != expected {
		return ErrRepository
	}
	actor, err := encodeActor(h.Actor)
	if err != nil {
		return ErrRepository
	}
	_, err = r.s.tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.publication_history(id,operation_id,space_id,kb_id,document_id,previous_epoch,publication_epoch,action,previous_revision_id,previous_generation_set_id,revision_id,generation_set_id,actor_id,actor,reason,occurred_at,command_digest) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text::numeric,$7::text::numeric,$8::text,$9::text,$10::text,$11::text,$12::text,$13::text,$14::jsonb,$15::text,$16::text,$17::text)`, string(h.ID), string(h.OperationID), string(h.SpaceID), string(h.KnowledgeBaseID), string(h.DocumentID), strconv.FormatUint(h.PreviousEpoch, 10), strconv.FormatUint(h.PublicationEpoch, 10), h.Action, nullableID(h.PreviousRevisionID), nullableID(h.PreviousGenerationSetID), nullableID(h.RevisionID), nullableID(h.GenerationSetID), string(h.ActorID), string(actor), h.Reason, h.OccurredAt.UTC().Format(timeFormat), string(h.CommandDigest))
	if err != nil {
		return ErrRepository
	}
	return nil
}
