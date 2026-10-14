package ingestion

import (
	"context"
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"io"
)

type UploadService struct{ Store contracts.BlobStore }

// Upload stores an original only; Document/Draft creation belongs to the
// authoritative ingest transaction. Never invent a document ID from a hash.
func (s UploadService) Upload(ctx context.Context, req contracts.BlobWriteRequest, r io.Reader) (contracts.BlobRef, error) {
	if s.Store == nil {
		return contracts.BlobRef{}, errors.New("upload unavailable")
	}
	return s.Store.Put(ctx, req, r)
}
