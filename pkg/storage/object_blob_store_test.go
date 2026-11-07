package storage_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/storage"
)

type objectClient struct {
	mu      sync.Mutex
	objects map[string][]byte
	keys    []string
}

func (c *objectClient) PutObject(_ context.Context, key string, r io.Reader, size int64, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if int64(len(b)) != size {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.objects == nil {
		c.objects = map[string][]byte{}
	}
	c.objects[key] = append([]byte(nil), b...)
	c.keys = append(c.keys, key)
	return nil
}

func (c *objectClient) GetObject(_ context.Context, key string) (io.ReadCloser, error) {
	c.mu.Lock()
	b, ok := c.objects[key]
	c.mu.Unlock()
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), b...))), nil
}

func (c *objectClient) DeleteObject(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.objects, key)
	return nil
}

func objectRequest(data []byte) contracts.BlobWriteRequest {
	h := sha256.Sum256(data)
	return contracts.BlobWriteRequest{
		Request:        contracts.RequestContext{RequestID: "req", Deadline: time.Now().Add(time.Minute)},
		Scope:          contracts.AccessScope{ScopeID: "scope", SubjectID: "user", Action: "blob.write", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)},
		ExpectedSHA256: contracts.Digest(hexDigest(h[:])), Size: int64(len(data)), MediaType: "text/plain", IdempotencyKey: "idem",
	}
}

func hexDigest(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2], out[i*2+1] = hex[v>>4], hex[v&15]
	}
	return string(out)
}

func TestObjectBlobStorePutOpenDelete(t *testing.T) {
	client := &objectClient{}
	store := storage.ObjectBlobStore{Client: client, Prefix: "memorys", Authorize: func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return nil }}
	data := []byte("object payload")
	req := objectRequest(data)
	ref, err := store.Put(context.Background(), req, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	ref2, err := store.Put(context.Background(), req, bytes.NewReader(data))
	if err != nil || ref2 != ref {
		t.Fatalf("retry ref = %#v, %v; want %#v", ref2, err, ref)
	}
	stream, err := store.Open(context.Background(), contracts.BlobReadRequest{Request: req.Request, Scope: req.Scope, Blob: ref})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, err := io.ReadAll(stream)
	_ = stream.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read = %q, %v", got, err)
	}
	if err := store.Delete(context.Background(), contracts.BlobReadRequest{Request: req.Request, Scope: req.Scope, Blob: ref}); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestObjectBlobStoreRejectsDigestAndPathAbuse(t *testing.T) {
	client := &objectClient{}
	store := storage.ObjectBlobStore{Client: client, Prefix: "safe", Authorize: func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return nil }}
	data := []byte("payload")
	req := objectRequest(data)
	req.ExpectedSHA256 = contracts.Digest("0000000000000000000000000000000000000000000000000000000000000000")
	if _, err := store.Put(context.Background(), req, bytes.NewReader(data)); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	bad := req
	bad.ExpectedSHA256 = contracts.Digest("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	bad.Scope.ExpiresAt = time.Now().Add(time.Minute)
	if _, err := (storage.ObjectBlobStore{Client: client, Prefix: "../unsafe", Authorize: func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return nil }}).Put(context.Background(), bad, bytes.NewReader(data)); err == nil {
		t.Fatal("unsafe prefix accepted")
	}
}

func TestObjectBlobStoreAuthorizationFailure(t *testing.T) {
	client := &objectClient{}
	store := storage.ObjectBlobStore{Client: client, Authorize: func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return io.ErrUnexpectedEOF }}
	req := objectRequest([]byte("payload"))
	if _, err := store.Put(context.Background(), req, bytes.NewReader([]byte("payload"))); err == nil {
		t.Fatal("unauthorized put accepted")
	}
}
