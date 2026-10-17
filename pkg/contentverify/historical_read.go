package contentverify

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// TextHistoricalReadVerifier applies the same text/original capability rules
// as current reads. Every frozen reread retains the independent history grant
// and target publication/review binding; it never impersonates a current read.
type TextHistoricalReadVerifier struct {
	Reader    contracts.HistoricalContentReader
	Originals contracts.BlobStore
	Control   OriginalReadControl
	Projector OriginalTextProjector
}

func (v TextHistoricalReadVerifier) VerifyHistoricalContent(ctx context.Context, input contracts.HistoricalContentReadRequest, record contracts.HistoricalContentRecord) error {
	if v.Reader == nil {
		return ErrContent
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return ErrContent
	}
	var owned contracts.HistoricalContentReadRequest
	if json.Unmarshal(raw, &owned) != nil {
		return ErrContent
	}
	expected := record
	expected.Content.Payload = nil
	raw, err = json.Marshal(expected)
	if err != nil {
		return ErrContent
	}
	var ownedExpected contracts.HistoricalContentRecord
	if json.Unmarshal(raw, &ownedExpected) != nil {
		return ErrContent
	}
	bound := boundHistoricalFrozen{reader: v.Reader, request: owned, expected: ownedExpected}
	return (TextReadVerifier{Reader: bound, Originals: v.Originals, Control: v.Control, Projector: v.Projector}).VerifyReadContent(ctx, owned.Base, record.Review.Revision, record.Content)
}

type boundHistoricalFrozen struct {
	reader   contracts.HistoricalContentReader
	request  contracts.HistoricalContentReadRequest
	expected contracts.HistoricalContentRecord
}

func (r boundHistoricalFrozen) ReadFrozenContent(ctx context.Context, q contracts.FrozenContentReadRequest) (contracts.FrozenContentRecord, error) {
	var empty contracts.FrozenContentRecord
	if !reflect.DeepEqual(q, r.request.Base) {
		return empty, ErrContent
	}
	raw, err := json.Marshal(r.request)
	if err != nil {
		return empty, ErrContent
	}
	var request contracts.HistoricalContentReadRequest
	if json.Unmarshal(raw, &request) != nil {
		return empty, ErrContent
	}
	actual, err := r.reader.ReadHistoricalContent(ctx, request)
	if err != nil || len(actual.Content.Payload) > 4<<20 {
		return empty, ErrContent
	}
	meta := actual
	meta.Content.Payload = nil
	raw, err = json.Marshal(meta)
	if err != nil {
		return empty, ErrContent
	}
	var normalized contracts.HistoricalContentRecord
	if json.Unmarshal(raw, &normalized) != nil {
		return empty, ErrContent
	}
	if !reflect.DeepEqual(normalized, r.expected) {
		return empty, ErrContent
	}
	actual.Content.Payload = append([]byte(nil), actual.Content.Payload...)
	return actual.Content, nil
}

var _ contracts.HistoricalContentVerifier = TextHistoricalReadVerifier{}
