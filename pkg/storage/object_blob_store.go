package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"mime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// ObjectClient is the small provider-neutral boundary used by ObjectBlobStore.
// Adapters for S3, MinIO, Azure Blob, and other providers only need to map
// these three operations to their SDK; provider types must not enter contracts.
type ObjectClient interface {
	PutObject(context.Context, string, io.Reader, int64, string) error
	GetObject(context.Context, string) (io.ReadCloser, error)
	DeleteObject(context.Context, string) error
}

// ObjectBlobStore stores BlobRefs in an object service while retaining the
// same authorization and byte-integrity checks as LocalBlobStore.
type ObjectBlobStore struct {
	Client    ObjectClient
	Prefix    string
	MaxBytes  int64
	Authorize func(context.Context, contracts.AccessScope, contracts.BlobRef) error
}

var _ contracts.BlobStore = ObjectBlobStore{}

func (s ObjectBlobStore) limits() int64 {
	if s.MaxBytes <= 0 {
		return 64 << 20
	}
	return s.MaxBytes
}

func (s ObjectBlobStore) validateScope(ctx context.Context, scope contracts.AccessScope, blob contracts.BlobRef) error {
	if ctx == nil || s.Client == nil || s.Authorize == nil || scope.ScopeID == "" || scope.SubjectID == "" || scope.SpaceID == "" || scope.PolicyVersion == 0 || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] == "" || !scope.ExpiresAt.After(time.Now()) {
		return ErrBlob
	}
	if scope.Action == "" || !utf8.ValidString(string(scope.Action)) || !utf8.ValidString(string(scope.SubjectID)) || !utf8.ValidString(string(scope.SpaceID)) || !utf8.ValidString(string(scope.KnowledgeBaseIDs[0])) {
		return ErrBlob
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrBlob
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.Authorize(ctx, scope, blob); err != nil {
		return ErrBlob
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (s ObjectBlobStore) key(scope contracts.AccessScope, id contracts.ID) (string, error) {
	if !validHex(string(id), 32) {
		return "", ErrBlob
	}
	if strings.HasPrefix(s.Prefix, "/") || strings.Contains(s.Prefix, "..") || !utf8.ValidString(s.Prefix) {
		return "", ErrBlob
	}
	ns, _ := sha256FromStrings(string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]))
	base := hex.EncodeToString(ns[:])
	if s.Prefix == "" {
		return base + "/" + string(id), nil
	}
	return strings.TrimSuffix(s.Prefix, "/") + "/" + base + "/" + string(id), nil
}

func sha256FromStrings(values ...string) ([32]byte, error) {
	h := sha256.New()
	for _, value := range values {
		if _, err := io.WriteString(h, value); err != nil {
			return [32]byte{}, err
		}
		if _, err := h.Write([]byte{0}); err != nil {
			return [32]byte{}, err
		}
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

func (s ObjectBlobStore) Put(ctx context.Context, req contracts.BlobWriteRequest, r io.Reader) (contracts.BlobRef, error) {
	limit := s.limits()
	if limit <= 0 || limit > 64<<20 || ctx == nil || r == nil || req.Request.Deadline.IsZero() || req.Request.RequestID == "" || req.IdempotencyKey == "" || len(req.IdempotencyKey) > 200 || len(req.MediaType) > 200 || req.Size < 0 || req.Size > limit || req.MediaType == "" || !validHex(string(req.ExpectedSHA256), 64) || !utf8.ValidString(req.IdempotencyKey) || !utf8.ValidString(req.MediaType) {
		return contracts.BlobRef{}, ErrBlob
	}
	if _, _, err := mime.ParseMediaType(req.MediaType); err != nil {
		return contracts.BlobRef{}, ErrBlob
	}
	deadline := req.Request.Deadline
	if req.Scope.ExpiresAt.Before(deadline) {
		deadline = req.Scope.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if err := s.validateScope(ctx, req.Scope, contracts.BlobRef{}); err != nil {
		return contracts.BlobRef{}, err
	}
	// Deriving the id from the idempotency key makes retries address the same
	// object without requiring provider-specific metadata or a second database.
	idSum, err := sha256FromStrings("AKOS-OBJECT-ID/1", string(req.Scope.SpaceID), string(req.Scope.KnowledgeBaseIDs[0]), string(req.Scope.SubjectID), string(req.Scope.Action), req.IdempotencyKey, string(req.ExpectedSHA256), strconv.FormatInt(req.Size, 10), req.MediaType)
	if err != nil {
		return contracts.BlobRef{}, ErrBlob
	}
	id := contracts.ID(hex.EncodeToString(idSum[:16]))
	key, err := s.key(req.Scope, id)
	if err != nil {
		return contracts.BlobRef{}, err
	}
	h := sha256.New()
	counting := &countingReader{ctx: ctx, reader: io.LimitReader(io.TeeReader(r, h), req.Size+1)}
	if err := s.Client.PutObject(ctx, key, counting, req.Size, req.MediaType); err != nil {
		if ctx.Err() != nil {
			return contracts.BlobRef{}, ctx.Err()
		}
		return contracts.BlobRef{}, ErrBlob
	}
	if counting.count != req.Size || hex.EncodeToString(h.Sum(nil)) != string(req.ExpectedSHA256) || ctx.Err() != nil || !req.Scope.ExpiresAt.After(time.Now()) {
		_ = s.Client.DeleteObject(ctx, key)
		if ctx.Err() != nil {
			return contracts.BlobRef{}, ctx.Err()
		}
		return contracts.BlobRef{}, ErrBlob
	}
	return contracts.BlobRef{ID: id, SHA256: req.ExpectedSHA256, Size: req.Size, MediaType: req.MediaType}, nil
}

func (s ObjectBlobStore) Open(ctx context.Context, req contracts.BlobReadRequest) (io.ReadCloser, error) {
	if ctx == nil || req.Request.Deadline.IsZero() || req.Request.RequestID == "" || !validHex(string(req.Blob.ID), 32) || !validHex(string(req.Blob.SHA256), 64) || req.Blob.Size < 0 || req.Blob.MediaType == "" {
		return nil, ErrBlob
	}
	deadline := req.Request.Deadline
	if req.Scope.ExpiresAt.Before(deadline) {
		deadline = req.Scope.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	if err := s.validateScope(ctx, req.Scope, req.Blob); err != nil {
		cancel()
		return nil, err
	}
	key, err := s.key(req.Scope, req.Blob.ID)
	if err != nil {
		cancel()
		return nil, err
	}
	reader, err := s.Client.GetObject(ctx, key)
	if err != nil {
		cancel()
		return nil, ErrBlob
	}
	return &checkedObjectReader{reader: reader, ctx: ctx, cancel: cancel, digest: sha256.New(), expected: req.Blob}, nil
}

func (s ObjectBlobStore) Delete(ctx context.Context, req contracts.BlobReadRequest) error {
	if ctx == nil || req.Request.Deadline.IsZero() || req.Request.RequestID == "" || !validHex(string(req.Blob.ID), 32) {
		return ErrBlob
	}
	deadline := req.Request.Deadline
	if req.Scope.ExpiresAt.Before(deadline) {
		deadline = req.Scope.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if err := s.validateScope(ctx, req.Scope, req.Blob); err != nil {
		return err
	}
	key, err := s.key(req.Scope, req.Blob.ID)
	if err != nil {
		return err
	}
	if err := s.Client.DeleteObject(ctx, key); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrBlob
	}
	return nil
}

type countingReader struct {
	ctx    context.Context
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	r.count += int64(n)
	return n, err
}

type checkedObjectReader struct {
	reader   io.ReadCloser
	ctx      context.Context
	cancel   context.CancelFunc
	digest   hash.Hash
	expected contracts.BlobRef
	count    int64
}

func (r *checkedObjectReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if n > 0 {
		_, _ = r.digest.Write(p[:n])
		r.count += int64(n)
	}
	if r.count > r.expected.Size || (errors.Is(err, io.EOF) && (r.count != r.expected.Size || hex.EncodeToString(r.digest.Sum(nil)) != string(r.expected.SHA256))) {
		return n, ErrBlob
	}
	return n, err
}

func (r *checkedObjectReader) Close() error {
	r.cancel()
	return r.reader.Close()
}
