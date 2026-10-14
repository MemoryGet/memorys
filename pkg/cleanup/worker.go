// Package cleanup coordinates fixed-target cleanup through replaceable ports.
package cleanup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrRejected = errors.New("cleanup execution rejected")
var ErrUnknown = errors.New("cleanup result requires reconciliation")

func validID(id contracts.ID) bool {
	return id != "" && utf8.ValidString(string(id)) && utf8.RuneCountInString(string(id)) <= 200
}
func validDigest(d contracts.Digest) bool {
	if len(d) != 64 {
		return false
	}
	for _, c := range d {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

type Worker struct {
	Tasks contracts.CleanupTaskStore
	// VerifyLease reloads current ownership and the frozen source/target binding.
	VerifyLease func(context.Context, contracts.AccessScope, contracts.CleanupLease) error
	Resolve     func(context.Context, contracts.CleanupOperation) (contracts.CleanupExecutor, error)
	// Required trusted admission check for the adapter's stable idempotency and
	// current authority; an executor's unverified capability claim is insufficient.
	AuthorizeExecution func(context.Context, contracts.AccessScope, contracts.CleanupLease, contracts.CleanupOperation) error
	// Required independent check of actual backend facts, not matching echo fields.
	VerifyReceipt func(context.Context, contracts.CleanupOperation, contracts.CleanupReceipt) error
}

func copyLease(lease contracts.CleanupLease) (contracts.CleanupLease, error) {
	raw, err := json.Marshal(lease)
	if err != nil || len(raw) > 1<<20 {
		return contracts.CleanupLease{}, ErrRejected
	}
	var owned contracts.CleanupLease
	if json.Unmarshal(raw, &owned) != nil {
		return owned, ErrRejected
	}
	return owned, nil
}
func copyOperation(op contracts.CleanupOperation) contracts.CleanupOperation {
	raw, _ := json.Marshal(op)
	var owned contracts.CleanupOperation
	json.Unmarshal(raw, &owned)
	return owned
}

// Operation commits to fixed platform facts; attempts, leases and worker IDs
// never change the external idempotency key. This format is an in-process draft.
func Operation(lease contracts.CleanupLease) (contracts.CleanupOperation, error) {
	owned, err := copyLease(lease)
	if err != nil {
		return contracts.CleanupOperation{}, err
	}
	for _, id := range []contracts.ID{owned.SpaceID, owned.KnowledgeBaseID, owned.DocumentID, owned.PlanID, owned.TargetID, owned.RevisionID, owned.GenerationSetID, owned.Target.BindingReference, owned.Target.ModuleInstanceID} {
		if !validID(id) {
			return contracts.CleanupOperation{}, ErrRejected
		}
	}
	if owned.PublicationEpoch == 0 || owned.TargetID != owned.Target.ID {
		return contracts.CleanupOperation{}, ErrRejected
	}
	if !validDigest(owned.Target.ProfileDigest) {
		return contracts.CleanupOperation{}, ErrRejected
	}
	switch owned.Target.Kind {
	case "index":
		if owned.Target.GenerationID == nil || owned.Target.BackendEpochID == nil || owned.Target.Channel == nil || !validID(*owned.Target.GenerationID) || !validID(*owned.Target.BackendEpochID) {
			return contracts.CleanupOperation{}, ErrRejected
		}
		if *owned.Target.Channel != contracts.KeywordChannel && *owned.Target.Channel != contracts.VectorChannel && *owned.Target.Channel != contracts.GraphChannel {
			return contracts.CleanupOperation{}, ErrRejected
		}
	case "graph", "memory", "cache":
		if owned.Target.GenerationID != nil || owned.Target.BackendEpochID != nil || owned.Target.Channel != nil {
			return contracts.CleanupOperation{}, ErrRejected
		}
	default:
		return contracts.CleanupOperation{}, ErrRejected
	}
	body := map[string]any{"format": "akos.cleanup.effect/1", "space_id": owned.SpaceID, "knowledge_base_id": owned.KnowledgeBaseID, "document_id": owned.DocumentID, "plan_id": owned.PlanID, "target_id": owned.TargetID, "revision_id": owned.RevisionID, "generation_set_id": owned.GenerationSetID, "publication_epoch": strconv.FormatUint(owned.PublicationEpoch, 10), "kind": owned.Target.Kind, "module_instance_id": owned.Target.ModuleInstanceID, "binding_reference": owned.Target.BindingReference, "profile_digest": owned.Target.ProfileDigest, "generation_id": owned.Target.GenerationID, "backend_epoch_id": owned.Target.BackendEpochID, "channel": owned.Target.Channel}
	raw, err := json.Marshal(body)
	if err != nil {
		return contracts.CleanupOperation{}, ErrRejected
	}
	raw, err = canonical.JSON(raw)
	if err != nil {
		return contracts.CleanupOperation{}, ErrRejected
	}
	h := sha256.New()
	h.Write([]byte("akos.cleanup.effect/1\n"))
	h.Write(raw)
	digest := contracts.Digest(hex.EncodeToString(h.Sum(nil)))
	return contracts.CleanupOperation{ID: contracts.ID("cleanup-" + string(digest)), Digest: digest, SpaceID: owned.SpaceID, KnowledgeBaseID: owned.KnowledgeBaseID, DocumentID: owned.DocumentID, PlanID: owned.PlanID, TargetID: owned.TargetID, RevisionID: owned.RevisionID, GenerationSetID: owned.GenerationSetID, PublicationEpoch: owned.PublicationEpoch, Target: owned.Target}, nil
}

func (w Worker) RunOne(ctx context.Context, scope contracts.AccessScope, plan, target, worker contracts.ID, duration time.Duration) error {
	if ctx == nil || w.Tasks == nil || w.VerifyLease == nil || w.Resolve == nil || w.AuthorizeExecution == nil || w.VerifyReceipt == nil {
		return ErrRejected
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrRejected
	}
	if scope.Action != "publication.cleanup.execute" || scope.SubjectID != worker {
		return ErrRejected
	}
	scope.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	lease, err := w.Tasks.Claim(ctx, scope, plan, target, worker, duration)
	if err != nil {
		return err
	}
	lease, err = copyLease(lease)
	if err != nil {
		return err
	}
	if lease.PlanID != plan || lease.TargetID != target || lease.WorkerID != worker || lease.SpaceID != scope.SpaceID || len(scope.KnowledgeBaseIDs) != 1 || lease.KnowledgeBaseID != scope.KnowledgeBaseIDs[0] || !lease.ExpiresAt.After(time.Now()) {
		return ErrRejected
	}
	op, err := Operation(lease)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithDeadline(ctx, lease.ExpiresAt)
	defer cancel()
	verify := func() error {
		owned, _ := copyLease(lease)
		s := scope
		s.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := w.VerifyLease(ctx, s, owned); err != nil {
			return err
		}
		return ctx.Err()
	}
	unknown := func() error {
		// Finish rechecks ownership/authority. If the budget expired, leave running
		// for a new owner to reconcile; do not mark a possibly completed effect failed.
		if err := w.Tasks.Finish(ctx, scope, lease, "unknown", ""); err != nil {
			return errors.Join(ErrUnknown, err)
		}
		return ErrUnknown
	}
	if err := verify(); err != nil {
		return err
	}
	executor, err := w.Resolve(ctx, copyOperation(op))
	if err != nil || executor == nil {
		return unknown()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	receipt, err := executor.Lookup(ctx, copyOperation(op))
	if err != nil {
		return unknown()
	}
	check := func(r contracts.CleanupReceipt) error {
		if r.OperationID != op.ID || r.RequestDigest != op.Digest || r.State != "absent" && r.State != "succeeded" && r.State != "pending" && r.State != "unknown" || r.State == "succeeded" && r.ResultReference == "" || r.State != "succeeded" && r.ResultReference != "" {
			return ErrRejected
		}
		return w.VerifyReceipt(ctx, copyOperation(op), r)
	}
	if check(receipt) != nil {
		return unknown()
	}
	if receipt.State == "absent" {
		if err := verify(); err != nil {
			return err
		}
		owned, _ := copyLease(lease)
		s := scope
		s.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
		if w.AuthorizeExecution(ctx, s, owned, copyOperation(op)) != nil {
			return unknown()
		}
		// Admission may finish after revocation/cancellation; reread ownership.
		if err := verify(); err != nil {
			return err
		}
		receipt, err = executor.Execute(ctx, copyOperation(op))
		if err != nil || check(receipt) != nil {
			return unknown()
		}
	}
	if receipt.State != "succeeded" {
		return unknown()
	}
	if err := verify(); err != nil {
		return err
	}
	return w.Tasks.Finish(ctx, scope, lease, "succeeded", receipt.ResultReference)
}
