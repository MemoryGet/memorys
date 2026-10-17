package contentverify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// VerifyBlobBytes independently reads the complete original/media object and
// compares its length and SHA256. Request.Blob must come from hash-verified
// approved content. This proves byte identity, not excerpt/locator semantics.
// The store must authorize current ownership; this helper never changes scope
// actions or treats possession of a BlobRef as a read grant.
func VerifyBlobBytes(ctx context.Context, store contracts.BlobStore, request contracts.BlobReadRequest) error {
	if ctx == nil || store == nil || request.Request.RequestID == "" || request.Request.Deadline.IsZero() || request.Blob.ID == "" || !digestShape(request.Blob.SHA256) || request.Blob.Size < 0 || request.Blob.Size > 64<<20 || request.Scope.SubjectID == "" || request.Scope.ScopeID == "" || request.Scope.SpaceID == "" || request.Scope.Action == "" || request.Scope.PolicyVersion == 0 || len(request.Scope.KnowledgeBaseIDs) < 1 || len(request.Scope.KnowledgeBaseIDs) > 256 || !request.Scope.ExpiresAt.After(time.Now()) {
		return ErrContent
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrContent
	}
	if _, _, err := mime.ParseMediaType(request.Blob.MediaType); err != nil {
		return ErrContent
	}
	ctx, cancel := context.WithDeadline(ctx, request.Request.Deadline)
	defer cancel()
	ctx, expire := context.WithDeadline(ctx, request.Scope.ExpiresAt)
	defer expire()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	request.Scope.KnowledgeBaseIDs = append([]contracts.ID(nil), request.Scope.KnowledgeBaseIDs...)
	stream, err := store.Open(ctx, request)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return ErrContent
	}
	if stream == nil {
		return ErrContent
	}
	closed := false
	defer func() {
		if !closed {
			_ = stream.Close()
		}
	}()
	hash := sha256.New()
	buffer := make([]byte, 32<<10)
	var count int64
	empty := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		limit := min(int64(len(buffer)), request.Blob.Size-count+1)
		n, readErr := stream.Read(buffer[:int(limit)])
		if n < 0 || n > int(limit) {
			return ErrContent
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		count += int64(n)
		if count > request.Blob.Size {
			return ErrContent
		}
		if n > 0 {
			empty = 0
			_, _ = hash.Write(buffer[:n])
		} else {
			empty++
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil || empty >= 100 {
			return ErrContent
		}
	}
	if count != request.Blob.Size || hex.EncodeToString(hash.Sum(nil)) != string(request.Blob.SHA256) {
		return ErrContent
	}
	closed = true
	if stream.Close() != nil {
		return ErrContent
	}
	return ctx.Err()
}
