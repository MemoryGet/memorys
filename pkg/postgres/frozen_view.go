package postgres

import (
	"context"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// frozenReadView serves one already-read revision only inside the same native
// read callback that established its current publication and actual bytes.
// The surrounding view rechecks current authority before releasing any output.
// It is never a cache across views, requests, or original projection calls.
type frozenReadView struct {
	ctx     context.Context
	request contracts.FrozenContentReadRequest
	record  contracts.FrozenContentRecord
}

func (v frozenReadView) ReadFrozenContent(ctx context.Context, q contracts.FrozenContentReadRequest) (contracts.FrozenContentRecord, error) {
	if ctx == nil || v.ctx == nil || ctx.Err() != nil || v.ctx.Err() != nil ||
		!q.Scope.ExpiresAt.After(time.Now()) || !reflect.DeepEqual(q, v.request) {
		return contracts.FrozenContentRecord{}, queryread.ErrRead
	}
	if _, ok := ctx.Deadline(); !ok {
		return contracts.FrozenContentRecord{}, queryread.ErrRead
	}
	return contracts.FrozenContentRecord{DocumentID: v.record.DocumentID, RevisionID: v.record.RevisionID,
		Payload: append([]byte(nil), v.record.Payload...)}, nil
}
