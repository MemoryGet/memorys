package contentverify

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

// OriginalReadControl authenticates the parent operation and resolves a current
// read grant for the approved object. Implementations must use trusted identity
// and ownership sources; arguments alone are not authority.
type OriginalReadControl interface {
	ResolveOriginalRead(context.Context, contracts.FrozenContentReadRequest, contracts.BlobRef) (contracts.BlobReadRequest, error)
}

// VerifyApprovedOriginal derives object metadata from freshly hash-verified
// approved content, resolves a separate current read grant, and verifies actual
// bytes. It does not prove excerpt/locator semantics or authorize an output.
func VerifyApprovedOriginal(ctx context.Context, reader contracts.FrozenContentReader, store contracts.BlobStore, control OriginalReadControl, request contracts.FrozenContentReadRequest, expected contracts.FrozenRevisionFact, blobID contracts.ID) error {
	if ctx == nil || store == nil || control == nil || blobID == "" || !utf8.ValidString(string(blobID)) || utf8.RuneCountInString(string(blobID)) > 200 {
		return ErrContent
	}
	request.Scope.KnowledgeBaseIDs = append([]contracts.ID(nil), request.Scope.KnowledgeBaseIDs...)
	payload, err := readVerifiedFrozen(ctx, reader, request, expected)
	if err != nil {
		return err
	}
	var envelope struct {
		Blobs []struct {
			ID        contracts.ID     `json:"id"`
			SHA256    contracts.Digest `json:"sha256"`
			Size      json.RawMessage  `json:"size"`
			MediaType string           `json:"media_type"`
		} `json:"blobs"`
	}
	if json.Unmarshal(payload, &envelope) != nil || len(envelope.Blobs) > 10000 {
		return ErrContent
	}
	var approved contracts.BlobRef
	seen := map[contracts.ID]bool{}
	for _, blob := range envelope.Blobs {
		if blob.ID == "" || seen[blob.ID] {
			return ErrContent
		}
		seen[blob.ID] = true
		if blob.ID == blobID {
			size, valid := approvedBlobSize(blob.Size)
			if !valid {
				return ErrContent
			}
			approved = contracts.BlobRef{ID: blob.ID, SHA256: blob.SHA256, Size: size, MediaType: blob.MediaType}
		}
	}
	if approved.ID == "" {
		return ErrContent
	}
	ctx, cancel := context.WithDeadline(ctx, request.Request.Deadline)
	defer cancel()
	ctx, expire := context.WithDeadline(ctx, request.Scope.ExpiresAt)
	defer expire()
	resolve := func() (contracts.BlobReadRequest, error) {
		argument := request
		argument.Scope.KnowledgeBaseIDs = append([]contracts.ID(nil), request.Scope.KnowledgeBaseIDs...)
		q, err := control.ResolveOriginalRead(ctx, argument, approved)
		if err != nil || ctx.Err() != nil || q.Blob != approved || q.Request.RequestID != request.Request.RequestID || q.Request.ProfileDigest != request.Request.ProfileDigest || !q.Request.Deadline.After(time.Now()) || q.Request.Deadline.After(request.Request.Deadline) || q.Scope.SubjectID != request.Scope.SubjectID || q.Scope.SpaceID != request.Scope.SpaceID || len(q.Scope.KnowledgeBaseIDs) != 1 {
			return contracts.BlobReadRequest{}, ErrContent
		}
		allowed := false
		for _, kb := range request.Scope.KnowledgeBaseIDs {
			if kb == q.Scope.KnowledgeBaseIDs[0] {
				allowed = true
			}
		}
		if !allowed || q.Scope.ScopeID == "" || q.Scope.Action == "" || q.Scope.PolicyVersion == 0 || q.Scope.ExpiresAt.Before(q.Request.Deadline) {
			return contracts.BlobReadRequest{}, ErrContent
		}
		q.Scope.KnowledgeBaseIDs = append([]contracts.ID(nil), q.Scope.KnowledgeBaseIDs...)
		return q, nil
	}
	readRequest, err := resolve()
	if err != nil {
		return err
	}
	if err := VerifyBlobBytes(ctx, store, readRequest); err != nil {
		return err
	}
	currentRead, err := resolve()
	if err != nil {
		return err
	}
	finalCtx, finish := context.WithDeadline(ctx, currentRead.Request.Deadline)
	defer finish()
	return VerifyFrozen(finalCtx, reader, request, expected)
}

// Sizes are bounded to exactly representable integers. Comparing canonical
// decimal bytes rejects fractional/rounded values rather than accepting a
// float conversion as proof of equality (AKOS-CJSON uses e notation).
func approvedBlobSize(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || len(raw) > 128 {
		return 0, false
	}
	f, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || !(f >= 0 && f <= 64<<20) {
		return 0, false
	}
	n := int64(f)
	before, err := canonical.JSON(raw)
	after, other := canonical.JSON(strconv.AppendInt(nil, n, 10))
	return n, err == nil && other == nil && bytes.Equal(before, after)
}
