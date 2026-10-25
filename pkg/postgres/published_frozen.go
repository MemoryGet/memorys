package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// PublishedFrozenReader implements the independent standard content-read port.
// It returns only the currently approved/published revision. Source/original
// bytes and index effects require separate verifiers before output.
type PublishedFrozenReader struct{ Reads QueryReads }

var _ contracts.FrozenContentReader = PublishedFrozenReader{}

func (p PublishedFrozenReader) ReadFrozenContent(ctx context.Context, input contracts.FrozenContentReadRequest) (contracts.FrozenContentRecord, error) {
	var empty contracts.FrozenContentRecord
	q, err := copyQuery(input)
	if err != nil || ctx == nil || !commandID(string(q.Request.RequestID)) || !commandDigest(q.Request.ProfileDigest) || q.Request.Deadline.IsZero() || !commandID(string(q.DocumentID)) || !commandID(string(q.RevisionID)) {
		return empty, queryread.ErrRead
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, queryread.ErrRead
	}
	ctx, cancel := context.WithDeadline(ctx, q.Request.Deadline)
	defer cancel()
	var result contracts.FrozenContentRecord
	var expected queryread.CurrentExpectation
	policies := map[contracts.ID]uint64{}
	err = p.Reads.withinQuery(ctx, q.Scope, func(c context.Context, identity contracts.QueryReadIdentity, repo *queryRepository) error {
		state, err := repo.load(c, q.DocumentID)
		if err != nil || state.PublicationReview == nil {
			return queryread.ErrRead
		}
		fact, err := queryread.PublishedCurrentFact(q.Scope, identity, state)
		if err != nil || fact.DocumentID != q.DocumentID || fact.RevisionID != q.RevisionID {
			return queryread.ErrRead
		}
		_, payload, err := readFrozenRevision(c, repo.tx, q.Scope.SpaceID, fact.KnowledgeBaseID, state.PublicationReview.Revision)
		if err != nil {
			return err
		}
		expected = queryread.CurrentExpectation{StoreEpochID: identity.StoreEpochID, ScopeVersion: q.Scope.PolicyVersion, Published: []contracts.PublishedFact{fact}}
		policies[fact.KnowledgeBaseID] = state.PublicationReview.Validity.ReviewPolicyVersion
		result = contracts.FrozenContentRecord{DocumentID: q.DocumentID, RevisionID: q.RevisionID, Payload: append([]byte(nil), payload...)}
		return nil
	})
	if err != nil {
		return empty, queryread.ErrRead
	}
	current, err := queryread.ObserveCurrent(ctx, p.Reads, q.Scope, []contracts.ID{q.DocumentID}, queryread.CurrentLimits{MaxDocuments: 1, MaxBytes: 4 << 20})
	if err != nil || queryread.MatchCurrentReview(q.Scope, expected, current, policies) != nil || ctx.Err() != nil {
		return empty, queryread.ErrRead
	}
	return result, nil
}

// Both owned preparation and published reads compare the stored canonical
// payload with a fresh reconstruction from the persisted typed Revision.
func readFrozenRevision(ctx context.Context, tx *sql.Tx, space, kb contracts.ID, expected contracts.FrozenRevisionFact) (contracts.Revision, []byte, error) {
	var revisionBytes, payload []byte
	if tx.QueryRowContext(ctx, `SELECT payload,frozen_payload FROM akos_withdraw_v1.revision_contents WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND revision_id=$4::text`, string(space), string(kb), string(expected.DocumentID), string(expected.ID)).Scan(&revisionBytes, &payload) != nil {
		return contracts.Revision{}, nil, ErrRepository
	}
	return frozenRevisionBytes(revisionBytes, payload, expected)
}

func frozenRevisionBytes(revisionBytes, payload []byte, expected contracts.FrozenRevisionFact) (contracts.Revision, []byte, error) {
	var revision contracts.Revision
	if len(revisionBytes) > 8<<20 || len(payload) > 4<<20 || json.Unmarshal(revisionBytes, &revision) != nil || revision.ID != expected.ID || revision.DocumentID != expected.DocumentID || revision.HashFormat != expected.HashFormat || revision.ContentHash != expected.ContentHash || revision.BlocksHash != expected.BlocksHash {
		return contracts.Revision{}, nil, ErrRepository
	}
	draft := contracts.Draft{ID: "work-content-check", DocumentID: revision.DocumentID, RowVersion: 1, State: "draft", Blobs: revision.Blobs}
	for _, block := range revision.Blocks {
		if block.RevisionID != revision.ID {
			return contracts.Revision{}, nil, ErrRepository
		}
		draft.Blocks = append(draft.Blocks, contracts.DraftBlock{ID: block.ID, DraftID: draft.ID, RowVersion: 1, Order: block.Order, Content: block.Content})
	}
	frozen, err := document.BuildFrozenContent(draft)
	if err != nil || !bytes.Equal(frozen.Payload, payload) || frozen.ContentHash != revision.ContentHash || frozen.BlocksHash != revision.BlocksHash {
		return contracts.Revision{}, nil, ErrRepository
	}
	return revision, append([]byte(nil), payload...), nil
}
