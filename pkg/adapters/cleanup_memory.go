package adapters

import (
	"context"
	"errors"

	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrMemoryCleanup = errors.New("local index cleanup rejected")

// MemoryCleanup connects the existing process-local index. Authorize must issue
// a fresh, verified index.delete request for this exact fixed operation; never
// relabel the task Scope's Action to fabricate an index grant.
type IndexCleanup struct {
	Index            contracts.IndexCleanupBackend
	ModuleInstanceID contracts.ID
	ProfileDigest    contracts.Digest
	Authorize        func(context.Context, contracts.CleanupOperation) (contracts.IndexDeleteRequest, error)
}

func (a IndexCleanup) request(ctx context.Context, op contracts.CleanupOperation) (contracts.IndexDeleteRequest, error) {
	if ctx == nil {
		return contracts.IndexDeleteRequest{}, ErrMemoryCleanup
	}
	if _, ok := ctx.Deadline(); !ok {
		return contracts.IndexDeleteRequest{}, ErrMemoryCleanup
	}
	if a.Index == nil || a.Authorize == nil || op.Target.Kind != "index" || op.Target.ModuleInstanceID != a.ModuleInstanceID || op.Target.ProfileDigest != a.ProfileDigest || op.Target.GenerationID == nil || op.Target.BackendEpochID == nil || op.Target.Channel == nil || *op.Target.Channel != contracts.KeywordChannel {
		return contracts.IndexDeleteRequest{}, ErrMemoryCleanup
	}
	expected, err := cleanup.Operation(contracts.CleanupLease{SpaceID: op.SpaceID, KnowledgeBaseID: op.KnowledgeBaseID, DocumentID: op.DocumentID, PlanID: op.PlanID, TargetID: op.TargetID, RevisionID: op.RevisionID, GenerationSetID: op.GenerationSetID, PublicationEpoch: op.PublicationEpoch, Target: op.Target})
	if err != nil || expected.ID != op.ID || expected.Digest != op.Digest {
		return contracts.IndexDeleteRequest{}, ErrMemoryCleanup
	}
	authorizedOp := op
	if op.Target.GenerationID != nil {
		v := *op.Target.GenerationID
		authorizedOp.Target.GenerationID = &v
	}
	if op.Target.BackendEpochID != nil {
		v := *op.Target.BackendEpochID
		authorizedOp.Target.BackendEpochID = &v
	}
	if op.Target.Channel != nil {
		v := *op.Target.Channel
		authorizedOp.Target.Channel = &v
	}
	req, err := a.Authorize(ctx, authorizedOp)
	if err != nil {
		return contracts.IndexDeleteRequest{}, err
	}
	if ctx.Err() != nil || req.Scope.Action != "index.delete" || req.Scope.SpaceID != op.SpaceID || len(req.Scope.KnowledgeBaseIDs) != 1 || req.Scope.KnowledgeBaseIDs[0] != op.KnowledgeBaseID || req.GenerationID != *op.Target.GenerationID || len(req.ChunkIDs) != 0 || req.Request.ProfileDigest != op.Target.ProfileDigest {
		return contracts.IndexDeleteRequest{}, ErrMemoryCleanup
	}
	req.Scope.KnowledgeBaseIDs = append([]contracts.ID(nil), req.Scope.KnowledgeBaseIDs...)
	return req, nil
}
func (a IndexCleanup) inspect(ctx context.Context, op contracts.CleanupOperation) (contracts.IndexDeletionState, error) {
	req, err := a.request(ctx, op)
	if err != nil {
		return contracts.IndexDeletionState{}, err
	}
	state, err := a.Index.InspectGeneration(ctx, req.Scope, req.GenerationID)
	if err != nil {
		return state, err
	}
	if state.SpaceID != op.SpaceID || state.KnowledgeBaseID != op.KnowledgeBaseID || state.DocumentID != op.DocumentID || state.RevisionID != op.RevisionID || state.BackendEpochID != *op.Target.BackendEpochID {
		return contracts.IndexDeletionState{}, ErrMemoryCleanup
	}
	return state, nil
}
func resultReference(op contracts.CleanupOperation) contracts.ID {
	return contracts.ID(string(op.ID) + "-deleted")
}
func (a IndexCleanup) Lookup(ctx context.Context, op contracts.CleanupOperation) (contracts.CleanupReceipt, error) {
	state, err := a.inspect(ctx, op)
	if err != nil {
		return contracts.CleanupReceipt{}, err
	}
	receipt := contracts.CleanupReceipt{OperationID: op.ID, RequestDigest: op.Digest, State: "absent"}
	if state.Deleted {
		if state.RecordCount != 0 {
			return contracts.CleanupReceipt{}, ErrMemoryCleanup
		}
		receipt.State = "succeeded"
		receipt.ResultReference = resultReference(op)
	}
	return receipt, nil
}
func (a IndexCleanup) Execute(ctx context.Context, op contracts.CleanupOperation) (contracts.CleanupReceipt, error) {
	if _, err := a.inspect(ctx, op); err != nil {
		return contracts.CleanupReceipt{}, err
	}
	req, err := a.request(ctx, op)
	if err != nil {
		return contracts.CleanupReceipt{}, err
	}
	if err := a.Index.Delete(ctx, req); err != nil {
		return contracts.CleanupReceipt{}, err
	}
	return a.Lookup(ctx, op)
}

// VerifyReceipt rereads owner+tombstone+records. Matching echo fields alone do
// not prove deletion; the absent state means this known generation is not yet deleted.
func (a IndexCleanup) VerifyReceipt(ctx context.Context, op contracts.CleanupOperation, r contracts.CleanupReceipt) error {
	actual, err := a.Lookup(ctx, op)
	if err != nil {
		return err
	}
	if r != actual {
		return ErrMemoryCleanup
	}
	return nil
}

// MemoryCleanup retains the original source-compatible name.
type MemoryCleanup = IndexCleanup
