package preparation

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

var ErrPublicationRejected = errors.New("final publication unavailable or stale")
var ErrPublicationUnknown = errors.New("final publication requires same-command reconciliation")

type PublicationRequest struct {
	Command        contracts.CommandContext
	OperationID    contracts.ID
	ExpectedEpoch  uint64
	ArtifactDigest contracts.Digest
}
type PublicationResult struct {
	Operation          contracts.PublicationOperation
	History            contracts.PublicationHistory
	DocumentRowVersion uint64
	CleanupPlanID      *contracts.ID
}
type PublicationStore interface {
	Finalize(context.Context, PublicationRequest) (PublicationResult, error)
}

// PublisherIdentity omits a transient delegation proof reference, retaining the
// issuer/client/user identity that must remain bound to the original operation.
func PublisherIdentity(p contracts.Principal) contracts.Principal {
	if p.Delegation != nil {
		d := *p.Delegation
		d.ProofReference = ""
		p.Delegation = &d
	}
	return p
}

func PublicationDigests(r PublicationRequest) (contracts.Digest, contracts.Digest, error) {
	if !validStrings(reflect.ValueOf(r)) || len(r.Command.Scope.KnowledgeBaseIDs) != 1 || !id(r.OperationID) || !id(r.Command.Scope.SpaceID) || !id(r.Command.Scope.KnowledgeBaseIDs[0]) {
		return "", "", ErrPublicationRejected
	}
	actor := PublisherIdentity(r.Command.Principal)
	raw, err := json.Marshal(actor)
	if err != nil {
		return "", "", ErrPublicationRejected
	}
	encoded, err := canonical.JSON(raw)
	if err != nil {
		return "", "", err
	}
	identity := hash(append([]byte("akos.publisher-identity/1\x00"), encoded...))
	raw, err = json.Marshal(map[string]any{"format": "akos.publication-finalize/1", "space_id": r.Command.Scope.SpaceID, "kb_id": r.Command.Scope.KnowledgeBaseIDs[0], "actor": actor, "operation_id": r.OperationID, "expected_epoch": r.ExpectedEpoch, "artifact_digest": r.ArtifactDigest, "profile_digest": r.Command.Request.ProfileDigest})
	if err != nil {
		return "", "", ErrPublicationRejected
	}
	encoded, err = canonical.JSON(raw)
	if err != nil {
		return "", "", err
	}
	return identity, hash(append([]byte("akos.publication-finalize/1\x00"), encoded...)), nil
}

// PublicationFacts calculates an immutable history from independently verified
// facts. Current authorization, actual index inspection and CAS remain adapter
// obligations; a completion receipt by itself is not current authority.
func PublicationFacts(r PublicationRequest, plan document.PublicationStartPlan, completed CompletionResult, doc contracts.Document, historyID contracts.ID, digest contracts.Digest, at time.Time) (PublicationResult, error) {
	a := completed.Artifact
	job := completed.Job
	if !id(historyID) || !id(r.OperationID) || r.OperationID != plan.Result.Operation.ID || r.ArtifactDigest != a.Digest || a.OperationID != r.OperationID || job.OperationID != r.OperationID || job.ID != plan.Result.Job.ID || job.ID != a.JobID || job.State != "completed" || job.GenerationID == nil || *job.GenerationID != a.GenerationSet.Channels[contracts.KeywordChannel].ID || job.FencingToken != a.FencingToken || doc.ID != plan.Result.Operation.DocumentID || doc.SpaceID != plan.SpaceID || doc.KnowledgeBaseID != plan.KnowledgeBaseID || doc.DeletedAt != nil || doc.PublicationEpoch != r.ExpectedEpoch || r.ExpectedEpoch != plan.Result.Operation.ExpectedEpoch || doc.RowVersion == 0 || doc.RowVersion == math.MaxUint64 || r.ExpectedEpoch == math.MaxUint64 || r.Command.Scope.Action != "publication.publish" || r.Command.Scope.SpaceID != doc.SpaceID || len(r.Command.Scope.KnowledgeBaseIDs) != 1 || r.Command.Scope.KnowledgeBaseIDs[0] != doc.KnowledgeBaseID || r.Command.Scope.SubjectID != r.Command.Principal.SubjectID || !reflect.DeepEqual(PublisherIdentity(r.Command.Principal), plan.OriginalPrincipal) || r.Command.Request.ProfileDigest != plan.ProfileDigest || a.Profile.Digest != plan.Backend.ProfileDigest || a.GenerationSet.RevisionID != plan.Result.Operation.RevisionID || a.GenerationSet.BackendEpochID != plan.Backend.EpochID || a.GenerationSet.State != "ready" || completed.CompletedAt.IsZero() || at.Before(completed.CompletedAt) {
		return PublicationResult{}, ErrPublicationRejected
	}
	if actual, err := ArtifactDigest(a); err != nil || actual != a.Digest {
		return PublicationResult{}, ErrPublicationRejected
	}
	set := a.GenerationSet.ID
	revision := a.GenerationSet.RevisionID
	operation := plan.Result.Operation
	operation.State = "committed"
	operation.GenerationSetID = &set
	h := contracts.PublicationHistory{ID: historyID, OperationID: r.OperationID, SpaceID: doc.SpaceID, KnowledgeBaseID: doc.KnowledgeBaseID, DocumentID: doc.ID, PreviousEpoch: doc.PublicationEpoch, PublicationEpoch: doc.PublicationEpoch + 1, Action: "publish", PreviousRevisionID: doc.ActiveRevisionID, PreviousGenerationSetID: doc.ActiveGenerationSetID, RevisionID: &revision, GenerationSetID: &set, ActorID: r.Command.Principal.SubjectID, Actor: r.Command.Principal, OccurredAt: at.UTC(), CommandDigest: digest}
	if contracts.ValidatePublicationHistory(h) != nil {
		return PublicationResult{}, ErrPublicationRejected
	}
	return own(PublicationResult{Operation: operation, History: h, DocumentRowVersion: doc.RowVersion + 1}, 2<<20)
}
