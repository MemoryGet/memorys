package document

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrSubmissionRejected = errors.New("submission rejected without committed effects")
var ErrSubmissionUnknown = errors.New("submission outcome requires reconciliation with the same key")

// SubmissionCommand is an internal command. Current identity, ownership and
// original/media integrity require trusted checks; these fields are not proof.
type SubmissionCommand struct {
	DocumentID contracts.ID
	Request    contracts.SubmitRequest
}

// SubmissionRepository atomically persists an immutable version, pending review,
// submitted draft, durable receipt, audit and outbox. It must reauthorize replay.
type SubmissionRepository interface {
	SubmitDraft(context.Context, SubmissionCommand) (contracts.SubmitResult, error)
}

type SubmissionPackage struct {
	Draft    contracts.Draft
	Revision contracts.Revision
	Approval contracts.ApprovalRequest
	Frozen   FrozenContent
}

// PrepareSubmission has no side effects. IDs/time/policy come from trusted host
// code; the repository must CAS the same draft and commit all effects together.
func PrepareSubmission(draft contracts.Draft, expected uint64, revisionID, approvalID, actor contracts.ID, policy uint64, at time.Time) (SubmissionPackage, error) {
	if expected == 0 || expected == math.MaxUint64 || draft.RowVersion != expected || !frozenID(draft.ID) || !frozenID(draft.DocumentID) || !frozenID(revisionID) || !frozenID(approvalID) || !frozenID(actor) || policy == 0 || at.IsZero() || at.UTC().Year() < 1 || at.UTC().Year() > 9999 {
		return SubmissionPackage{}, ErrSubmissionRejected
	}
	frozen, err := BuildFrozenContent(draft)
	if err != nil {
		return SubmissionPackage{}, ErrSubmissionRejected
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		return SubmissionPackage{}, ErrSubmissionRejected
	}
	var owned contracts.Draft
	if json.Unmarshal(raw, &owned) != nil {
		return SubmissionPackage{}, ErrSubmissionRejected
	}
	if owned.BaseRevisionID != nil && (!frozenID(*owned.BaseRevisionID) || *owned.BaseRevisionID == revisionID) {
		return SubmissionPackage{}, ErrSubmissionRejected
	}
	revision := contracts.Revision{ID: revisionID, DocumentID: owned.DocumentID, ParentRevisionID: owned.BaseRevisionID, Blobs: owned.Blobs, HashFormat: frozen.HashFormat, ContentHash: frozen.ContentHash, BlocksHash: frozen.BlocksHash, CreatedBy: actor, CreatedAt: at.UTC(), Blocks: make([]contracts.Block, 0, len(owned.Blocks))}
	for _, block := range owned.Blocks {
		revision.Blocks = append(revision.Blocks, contracts.Block{ID: block.ID, RevisionID: revisionID, Order: block.Order, Content: block.Content})
	}
	// Draft and Revision must not share mutable nested content even within the
	// returned package; downstream draft serialization may change independently.
	revisionBytes, err := json.Marshal(revision)
	if err != nil {
		return SubmissionPackage{}, ErrSubmissionRejected
	}
	var revisionCopy contracts.Revision
	if json.Unmarshal(revisionBytes, &revisionCopy) != nil {
		return SubmissionPackage{}, ErrSubmissionRejected
	}
	revision = revisionCopy
	sort.Slice(revision.Blobs, func(i, j int) bool { return revision.Blobs[i].ID < revision.Blobs[j].ID })
	for i := range revision.Blocks {
		content := &revision.Blocks[i].Content
		if content.Table != nil {
			sort.Slice(content.Table.Cells, func(i, j int) bool {
				a, b := content.Table.Cells[i], content.Table.Cells[j]
				return a.Row < b.Row || a.Row == b.Row && a.Column < b.Column
			})
		}
		if content.Audio != nil {
			sort.SliceStable(content.Audio.Segments, func(i, j int) bool {
				a, b := content.Audio.Segments[i], content.Audio.Segments[j]
				return a.StartMS < b.StartMS || a.StartMS == b.StartMS && a.EndMS < b.EndMS
			})
		}
	}
	owned.State = "submitted"
	owned.RowVersion++
	approval := contracts.ApprovalRequest{ID: approvalID, RevisionID: revisionID, DocumentID: owned.DocumentID, SubmittedBy: actor, SubmittedAt: at.UTC(), State: "submitted", RowVersion: 0, ContentHash: frozen.ContentHash, PolicyVersion: policy}
	return SubmissionPackage{Draft: owned, Revision: revision, Approval: approval, Frozen: frozen}, nil
}
