// Package contentverify checks authoritative frozen content bytes against
// approval-bound metadata; it does not authorize a send or verify an index.
package contentverify

import (
	"context"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrContent = errors.New("frozen content verification failed")

// VerifyFrozen reads by platform document/revision IDs and checks both hashes.
// The caller must independently authenticate scope and the supplied revision
// fact. Frozen schema/domain validity is established when content is approved;
// this function proves canonical byte agreement, not that upstream validation.
func VerifyFrozen(ctx context.Context, reader contracts.FrozenContentReader, request contracts.FrozenContentReadRequest, expected contracts.FrozenRevisionFact) error {
	_, err := readVerifiedFrozen(ctx, reader, request, expected)
	return err
}

func readVerifiedFrozen(ctx context.Context, reader contracts.FrozenContentReader, request contracts.FrozenContentReadRequest, expected contracts.FrozenRevisionFact) ([]byte, error) {
	if ctx == nil || reader == nil || request.Request.RequestID == "" || request.Request.Deadline.IsZero() || request.Scope.SubjectID == "" || request.Scope.ScopeID == "" || request.Scope.SpaceID == "" || !contracts.IsCurrentContentReadAction(request.Scope.Action) || request.Scope.PolicyVersion == 0 || len(request.Scope.KnowledgeBaseIDs) == 0 || len(request.Scope.KnowledgeBaseIDs) > 256 || expected.ID == "" || expected.DocumentID == "" || request.DocumentID != expected.DocumentID || request.RevisionID != expected.ID || expected.HashFormat != canonical.FrozenFormat || !digestShape(expected.ContentHash) || !digestShape(expected.BlocksHash) {
		return nil, ErrContent
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrContent
	}
	ctx, cancel := context.WithDeadline(ctx, request.Request.Deadline)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !request.Scope.ExpiresAt.After(time.Now()) {
		return nil, ErrContent
	}
	// Prevent a reader from mutating the caller's scope slice.
	request.Scope.KnowledgeBaseIDs = append([]contracts.ID(nil), request.Scope.KnowledgeBaseIDs...)
	record, err := reader.ReadFrozenContent(ctx, request)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || record.DocumentID != expected.DocumentID || record.RevisionID != expected.ID || len(record.Payload) == 0 || len(record.Payload) > 4<<20 {
		return nil, ErrContent
	}
	hashes, err := canonical.Frozen(record.Payload)
	if err != nil || hashes.Format != expected.HashFormat || hashes.ContentHash != string(expected.ContentHash) || hashes.BlocksHash != string(expected.BlocksHash) {
		return nil, ErrContent
	}
	if !request.Scope.ExpiresAt.After(time.Now()) {
		return nil, ErrContent
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return record.Payload, nil
}

func digestShape(d contracts.Digest) bool {
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
