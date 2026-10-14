package contracts

import (
	"context"
	"math"
	"time"
	"unicode/utf8"
)

// PublicationHistory is immutable authoritative state. Public management DTOs
// expose only their reviewed fields; private operation/target binding stays here.
type PublicationHistory struct {
	ID, OperationID, SpaceID, KnowledgeBaseID, DocumentID ID
	PreviousEpoch, PublicationEpoch                       uint64
	Action                                                string
	PreviousRevisionID, PreviousGenerationSetID           *ID
	RevisionID, GenerationSetID                           *ID
	ActorID                                               ID
	Actor                                                 Principal
	Reason                                                string
	OccurredAt                                            time.Time
	CommandDigest                                         Digest
}

type PublicationHistoryRepository interface {
	Load(context.Context, AccessScope, ID) (PublicationHistory, error)
	Append(context.Context, AccessScope, PublicationHistory) error
}

// CleanupPlan freezes the old publication's targets. Consumer state belongs in
// separately versioned task/progress records; it must not mutate this definition.
type CleanupPlan struct {
	ID, HistoryID, OperationID, SpaceID, KnowledgeBaseID, DocumentID ID
	RevisionID, GenerationSetID                                      ID
	PublicationEpoch                                                 uint64 // Old epoch whose projections must be invalidated.
	ProfileDigest                                                    Digest
	Targets                                                          []CleanupTarget
	CreatedAt                                                        time.Time
}

type CleanupTarget struct {
	ID                           ID
	Kind                         string // index, graph, memory, cache; withdrawal does not delete originals.
	ModuleInstanceID             ID
	BindingReference             ID // Immutable platform target/config definition.
	ProfileDigest                Digest
	GenerationID, BackendEpochID *ID
	Channel                      *Channel
}

type CleanupPlanRepository interface {
	Load(context.Context, AccessScope, ID) (CleanupPlan, error)
	Append(context.Context, AccessScope, CleanupPlan) error
}

func historyID(id ID) bool {
	return id != "" && utf8.ValidString(string(id)) && utf8.RuneCountInString(string(id)) <= 200
}
func historyPair(revision, generation *ID) bool {
	return revision == nil && generation == nil || revision != nil && generation != nil && historyID(*revision) && historyID(*generation)
}
func historyTime(t time.Time) bool {
	_, offset := t.Zone()
	return !t.IsZero() && offset == 0 && t.Year() >= 1 && t.Year() <= 9999
}
func invalidHistory() error {
	return PublicError{Code: "validation_failed", SafeMessage: "Invalid publication history or cleanup plan"}
}

// Shape checks do not authenticate Actor, current scope, persisted CAS or data
// provenance. Repository implementations must enforce the transaction boundary.
func ValidatePublicationHistory(h PublicationHistory) error {
	for _, id := range []ID{h.ID, h.OperationID, h.SpaceID, h.KnowledgeBaseID, h.DocumentID, h.ActorID} {
		if !historyID(id) {
			return invalidHistory()
		}
	}
	if h.PreviousEpoch == math.MaxUint64 || h.PublicationEpoch != h.PreviousEpoch+1 || !historyPair(h.PreviousRevisionID, h.PreviousGenerationSetID) || !historyPair(h.RevisionID, h.GenerationSetID) || h.Actor.SubjectID != h.ActorID || h.Actor.Issuer == "" || !utf8.ValidString(h.Actor.Issuer) || h.Actor.SubjectType != "user" && h.Actor.SubjectType != "service" || !validDigest(h.CommandDigest) || !historyTime(h.OccurredAt) || !utf8.ValidString(h.Reason) || utf8.RuneCountInString(h.Reason) > 10000 {
		return invalidHistory()
	}
	if d := h.Actor.Delegation; d != nil && (!historyID(d.UserID) || !historyID(d.ClientID) || d.ProofReference == "" || !utf8.ValidString(d.ProofReference)) {
		return invalidHistory()
	}
	if h.PreviousEpoch == 0 && h.PreviousRevisionID != nil || h.Actor.ClientID != "" && !historyID(h.Actor.ClientID) {
		return invalidHistory()
	}
	switch h.Action {
	case "publish":
		if h.RevisionID == nil {
			return invalidHistory()
		}
	case "withdraw":
		if h.PreviousEpoch == 0 || h.PreviousRevisionID == nil || h.RevisionID != nil || h.Reason == "" {
			return invalidHistory()
		}
	case "delete":
		if h.RevisionID != nil {
			return invalidHistory()
		}
	default:
		return invalidHistory()
	}
	return nil
}

// The expected history must be independently loaded from the authoritative
// transaction. A supplier returning a matching plan/history pair is not proof.
func ValidateWithdrawalCleanup(plan CleanupPlan, history PublicationHistory) error {
	if ValidatePublicationHistory(history) != nil || history.Action != "withdraw" || plan.HistoryID != history.ID || plan.OperationID != history.OperationID || plan.SpaceID != history.SpaceID || plan.KnowledgeBaseID != history.KnowledgeBaseID || plan.DocumentID != history.DocumentID || plan.PublicationEpoch != history.PreviousEpoch || plan.RevisionID != *history.PreviousRevisionID || plan.GenerationSetID != *history.PreviousGenerationSetID || !historyID(plan.ID) || !validDigest(plan.ProfileDigest) || !historyTime(plan.CreatedAt) || plan.CreatedAt.Before(history.OccurredAt) || len(plan.Targets) == 0 || len(plan.Targets) > 1000 {
		return invalidHistory()
	}
	ids := map[ID]bool{}
	generations := map[ID]bool{}
	for _, t := range plan.Targets {
		if !historyID(t.ID) || ids[t.ID] || !historyID(t.ModuleInstanceID) || !historyID(t.BindingReference) || !validDigest(t.ProfileDigest) {
			return invalidHistory()
		}
		ids[t.ID] = true
		switch t.Kind {
		case "index":
			if t.GenerationID == nil || t.BackendEpochID == nil || t.Channel == nil || !historyID(*t.GenerationID) || !historyID(*t.BackendEpochID) || !knownChannel(*t.Channel) || generations[*t.GenerationID] {
				return invalidHistory()
			}
			generations[*t.GenerationID] = true
		case "graph", "memory", "cache":
			if t.GenerationID != nil || t.BackendEpochID != nil || t.Channel != nil {
				return invalidHistory()
			}
		default:
			return invalidHistory()
		}
	}
	return nil
}

// ValidateCleanupGenerations checks complete index coverage against a separately
// loaded immutable set. Call ValidateWithdrawalCleanup first. Live backend
// binding/definition reads remain mandatory.
func ValidateCleanupGenerations(plan CleanupPlan, set GenerationSet) error {
	if set.ID != plan.GenerationSetID || set.RevisionID != plan.RevisionID || set.BackendEpochID == "" || len(set.Channels) == 0 || len(set.Channels) > 3 {
		return invalidHistory()
	}
	count := 0
	seen := map[Channel]bool{}
	for _, target := range plan.Targets {
		if target.Kind != "index" {
			continue
		}
		if target.Channel == nil || target.GenerationID == nil || target.BackendEpochID == nil || seen[*target.Channel] {
			return invalidHistory()
		}
		seen[*target.Channel] = true
		generation, exists := set.Channels[*target.Channel]
		if !exists || generation.Channel != *target.Channel || generation.ID != *target.GenerationID || generation.RevisionID != plan.RevisionID || generation.KnowledgeBaseID != plan.KnowledgeBaseID || generation.BackendEpochID != *target.BackendEpochID || generation.BackendEpochID != set.BackendEpochID || generation.ProfileDigest != target.ProfileDigest || generation.BackendBinding == nil || generation.BackendBinding.ModuleInstanceID != target.ModuleInstanceID {
			return invalidHistory()
		}
		count++
	}
	if count != len(set.Channels) {
		return invalidHistory()
	}
	return nil
}
