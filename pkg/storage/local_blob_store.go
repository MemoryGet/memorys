package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"mime"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrBlob = errors.New("blob operation rejected")

// Authorize must verify current identity, scope and blob ownership against a
// trusted source. A caller-supplied AccessScope alone is not authorization.
type LocalBlobStore struct {
	Root      string
	MaxBytes  int64
	Authorize func(context.Context, contracts.AccessScope, contracts.BlobRef) error
}

var _ contracts.BlobStore = LocalBlobStore{}

func (s LocalBlobStore) directory(ctx context.Context, scope contracts.AccessScope, blob contracts.BlobRef) (string, error) {
	if ctx == nil || s.Root == "" || s.Authorize == nil || scope.ScopeID == "" || scope.SubjectID == "" || scope.SpaceID == "" || scope.PolicyVersion == 0 || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] == "" || !scope.ExpiresAt.After(time.Now()) {
		return "", ErrBlob
	}
	if scope.Action == "" || !utf8.ValidString(string(scope.Action)) || !utf8.ValidString(string(scope.SubjectID)) || !utf8.ValidString(string(scope.SpaceID)) || !utf8.ValidString(string(scope.KnowledgeBaseIDs[0])) {
		return "", ErrBlob
	}
	if _, ok := ctx.Deadline(); !ok {
		return "", ErrBlob
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	scope.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	if s.Authorize(ctx, scope, blob) != nil {
		return "", ErrBlob
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	namespaceJSON, _ := json.Marshal([]contracts.ID{scope.SpaceID, scope.KnowledgeBaseIDs[0]})
	namespace := sha256.Sum256(namespaceJSON)
	return filepath.Join(s.Root, hex.EncodeToString(namespace[:])), nil
}
func validHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
func (s LocalBlobStore) Put(ctx context.Context, req contracts.BlobWriteRequest, r io.Reader) (contracts.BlobRef, error) {
	limit := s.MaxBytes
	if limit <= 0 {
		limit = 64 << 20
	}
	if limit > 64<<20 {
		return contracts.BlobRef{}, ErrBlob
	}
	if ctx == nil || r == nil || req.Request.Deadline.IsZero() || req.Request.RequestID == "" || req.IdempotencyKey == "" || len(req.IdempotencyKey) > 200 || len(req.MediaType) > 200 || req.Size < 0 || req.Size > limit || req.MediaType == "" || !validHex(string(req.ExpectedSHA256), 64) {
		return contracts.BlobRef{}, ErrBlob
	}
	if !utf8.ValidString(req.IdempotencyKey) || !utf8.ValidString(req.MediaType) {
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
	dir, err := s.directory(ctx, req.Scope, contracts.BlobRef{})
	if err != nil {
		return contracts.BlobRef{}, err
	}
	if os.MkdirAll(dir, 0700) != nil {
		return contracts.BlobRef{}, ErrBlob
	}
	journal, replayed, err := reserveUpload(dir, req)
	if err != nil {
		return contracts.BlobRef{}, err
	}
	if replayed != nil {
		if ctx.Err() != nil {
			return contracts.BlobRef{}, ctx.Err()
		}
		if !req.Scope.ExpiresAt.After(time.Now()) {
			return contracts.BlobRef{}, ErrBlob
		}
		return *replayed, nil
	}
	defer journal.close()
	tmp, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return contracts.BlobRef{}, ErrBlob
	}
	path := tmp.Name()
	defer os.Remove(path)
	defer tmp.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(contextReader{ctx, r}, req.Size+1))
	if err != nil || n != req.Size || hex.EncodeToString(h.Sum(nil)) != string(req.ExpectedSHA256) || ctx.Err() != nil || !req.Scope.ExpiresAt.After(time.Now()) {
		return contracts.BlobRef{}, ErrBlob
	}
	if tmp.Sync() != nil || tmp.Close() != nil {
		return contracts.BlobRef{}, ErrBlob
	}
	ref := journal.receipt.Blob
	destination := filepath.Join(dir, string(ref.ID))
	if os.Rename(path, destination) != nil {
		return contracts.BlobRef{}, ErrBlob
	}
	journal.effect = true
	raw, _ := json.Marshal(ref)
	metadata, err := os.OpenFile(destination+".json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return contracts.BlobRef{}, ErrPending
	}
	_, err = metadata.Write(raw)
	syncErr := metadata.Sync()
	closeErr := metadata.Close()
	if err != nil || syncErr != nil || closeErr != nil {
		return contracts.BlobRef{}, ErrPending
	}
	if err := journal.finish(); err != nil {
		return contracts.BlobRef{}, err
	}
	return ref, nil
}
func (s LocalBlobStore) locate(ctx context.Context, req contracts.BlobReadRequest) (string, error) {
	if !validHex(string(req.Blob.ID), 32) || req.Request.Deadline.IsZero() || req.Request.RequestID == "" {
		return "", ErrBlob
	}
	dir, err := s.directory(ctx, req.Scope, req.Blob)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, string(req.Blob.ID))
	if _, err := os.Stat(path + ".deleted"); !os.IsNotExist(err) {
		return "", ErrBlob
	}
	raw, err := os.ReadFile(path + ".json")
	if err != nil {
		return "", ErrBlob
	}
	var stored contracts.BlobRef
	if json.Unmarshal(raw, &stored) != nil || stored != req.Blob {
		return "", ErrBlob
	}
	return path, nil
}
func (s LocalBlobStore) Open(ctx context.Context, req contracts.BlobReadRequest) (io.ReadCloser, error) {
	if ctx == nil {
		return nil, ErrBlob
	}
	deadline := req.Request.Deadline
	if req.Scope.ExpiresAt.Before(deadline) {
		deadline = req.Scope.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	path, err := s.locate(ctx, req)
	if err != nil {
		cancel()
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		cancel()
		return nil, ErrBlob
	}
	return &checkedReader{file: file, ctx: ctx, cancel: cancel, digest: sha256.New(), expected: req.Blob}, nil
}

type checkedReader struct {
	file     *os.File
	ctx      context.Context
	cancel   context.CancelFunc
	digest   hash.Hash
	expected contracts.BlobRef
	count    int64
}

func (r *checkedReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.file.Read(p)
	r.digest.Write(p[:n])
	r.count += int64(n)
	if r.count > r.expected.Size || err == io.EOF && (r.count != r.expected.Size || hex.EncodeToString(r.digest.Sum(nil)) != string(r.expected.SHA256)) {
		return n, ErrBlob
	}
	return n, err
}
func (r *checkedReader) Close() error { r.cancel(); return r.file.Close() }
