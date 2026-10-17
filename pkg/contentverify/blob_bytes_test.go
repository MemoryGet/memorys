package contentverify_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/storage"
)

type originalStore struct {
	contracts.BlobStore
	stream io.ReadCloser
	err    error
	calls  int
}

func (s *originalStore) Open(_ context.Context, r contracts.BlobReadRequest) (io.ReadCloser, error) {
	s.calls++
	r.Scope.KnowledgeBaseIDs[0] = "provider-mutated"
	return s.stream, s.err
}

type originalStream struct {
	io.Reader
	closed   int
	closeErr error
	read     func([]byte) (int, error)
}

func (s *originalStream) Read(p []byte) (int, error) {
	if s.read != nil {
		return s.read(p)
	}
	return s.Reader.Read(p)
}
func (s *originalStream) Close() error { s.closed++; return s.closeErr }
func originalRequest(data []byte) contracts.BlobReadRequest {
	digest := sha256.Sum256(data)
	return contracts.BlobReadRequest{Request: contracts.RequestContext{RequestID: "verify-original", Deadline: time.Now().Add(time.Minute)}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "subject", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, Action: "blob.read", ExpiresAt: time.Now().Add(time.Minute)}, Blob: contracts.BlobRef{ID: "blob", SHA256: contracts.Digest(fmt.Sprintf("%x", digest)), Size: int64(len(data)), MediaType: "application/octet-stream"}}
}
func TestOriginalBytesWithActualLocalStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	data := []byte("\x00原件😀\r\n\xff")
	q := originalRequest(data)
	store := storage.LocalBlobStore{Root: t.TempDir(), Authorize: func(_ context.Context, s contracts.AccessScope, _ contracts.BlobRef) error {
		if s.SubjectID != "subject" {
			return errors.New("denied")
		}
		return nil
	}}
	ref, err := store.Put(ctx, contracts.BlobWriteRequest{Request: q.Request, Scope: q.Scope, ExpectedSHA256: q.Blob.SHA256, Size: q.Blob.Size, MediaType: q.Blob.MediaType, IdempotencyKey: "original"}, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	q.Blob = ref
	if err := contentverify.VerifyBlobBytes(ctx, store, q); err != nil {
		t.Fatal(err)
	}
	q.Scope.SubjectID = "other"
	if err := contentverify.VerifyBlobBytes(ctx, store, q); err == nil {
		t.Fatal("unauthorized original verified")
	}
}
func TestOriginalBytesRejectsShortLongCorruptAndFailedReads(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, kind := range []string{"valid", "empty", "short", "long", "corrupt", "read-error", "close-error", "open-error", "no-progress"} {
		t.Run(kind, func(t *testing.T) {
			data := []byte("approved")
			q := originalRequest(data)
			actual := data
			s := &originalStream{}
			switch kind {
			case "empty":
				actual = nil
				q = originalRequest(nil)
			case "short":
				actual = data[:3]
			case "long":
				actual = append(append([]byte(nil), data...), 1)
			case "corrupt":
				actual = []byte("changed!")
			case "close-error":
				s.closeErr = errors.New("private storage path")
			case "read-error":
				s.read = func(p []byte) (int, error) { return copy(p, data), io.ErrUnexpectedEOF }
			case "no-progress":
				s.read = func([]byte) (int, error) { return 0, nil }
			}
			s.Reader = bytes.NewReader(actual)
			store := &originalStore{stream: s}
			if kind == "open-error" {
				store.err = errors.New("private token")
			}
			err := contentverify.VerifyBlobBytes(ctx, store, q)
			if kind == "valid" || kind == "empty" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, contentverify.ErrContent) {
				t.Fatalf("unverified stream accepted: %v", err)
			}
			if s.closed != 1 || q.Scope.KnowledgeBaseIDs[0] != "kb" {
				t.Fatal("stream leaked/closed twice or scope mutated")
			}
		})
	}
}
func TestOriginalBytesCancellationAndAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	q := originalRequest([]byte("x"))
	store := &originalStore{}
	if contentverify.VerifyBlobBytes(context.Background(), store, q) == nil || store.calls != 0 {
		t.Fatal("unbounded read admitted")
	}
	bad := q
	bad.Blob.Size = 64<<20 + 1
	if contentverify.VerifyBlobBytes(ctx, store, bad) == nil || store.calls != 0 {
		t.Fatal("oversize read admitted")
	}
	child, stop := context.WithCancel(ctx)
	s := &originalStream{read: func(p []byte) (int, error) { stop(); p[0] = 'x'; return 1, io.EOF }}
	store.stream = s
	if err := contentverify.VerifyBlobBytes(child, store, q); !errors.Is(err, context.Canceled) || s.closed != 1 {
		t.Fatal("cancelled bytes accepted or stream leaked", err)
	}
}
