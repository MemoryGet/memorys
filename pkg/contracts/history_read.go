package contracts

import "context"

const HistoricalReadAction Action = "knowledge.history.read"

// HistoricalContentReadRequest requires two independently issued, current
// resource grants. Base.Scope remains knowledge.read; HistoryScope must use
// HistoricalReadAction. Neither scope value alone is proof of authorization.
type HistoricalContentReadRequest struct {
	Base         FrozenContentReadRequest
	Principal    Principal // Authenticated by the application; never a wire argument.
	HistoryScope AccessScope
}

// HistoricalContentRecord proves relationships only when supplied by an
// admitted authoritative reader. Current is the document's live publication;
// Review belongs to the historical target, not its replacement. An explicitly
// withdrawn/deleted target or currently withdrawn/deleted document is denied.
type HistoricalContentRecord struct {
	Content          FrozenContentRecord
	Review           CurrentPublicationReview
	PublicationID    ID
	PublicationEpoch uint64
	StoreEpochID     ID
	Current          PublishedFact
}

type HistoricalContentReader interface {
	ReadHistoricalContent(context.Context, HistoricalContentReadRequest) (HistoricalContentRecord, error)
}

type HistoricalContentVerifier interface {
	VerifyHistoricalContent(context.Context, HistoricalContentReadRequest, HistoricalContentRecord) error
}
