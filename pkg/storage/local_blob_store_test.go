package storage_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/storage"
)

func writeRequest(data []byte) contracts.BlobWriteRequest {
	h := sha256.Sum256(data)
	return contracts.BlobWriteRequest{Request: contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Minute)}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "user", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "blob.write", PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}, ExpectedSHA256: contracts.Digest(fmt.Sprintf("%x", h)), Size: int64(len(data)), MediaType: "text/markdown", IdempotencyKey: "key"}
}
func TestStoreRoundTripNamespaceAndAuthorization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store := storage.LocalBlobStore{Root: t.TempDir(), Authorize: func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return nil }}
	data := []byte("中文😀\n")
	req := writeRequest(data)
	ref, err := store.Put(ctx, req, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if string(ref.ID) == string(ref.SHA256) {
		t.Fatal("blob ID leaks content digest")
	}
	readReq := contracts.BlobReadRequest{Request: req.Request, Scope: req.Scope, Blob: ref}
	stream, err := store.Open(ctx, readReq)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(stream)
	stream.Close()
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("round trip", err)
	}
	readReq.Scope.SpaceID = "other"
	if _, err := store.Open(ctx, readReq); err == nil {
		t.Fatal("cross namespace read")
	}
	store.Authorize = nil
	if _, err := store.Put(ctx, req, bytes.NewReader(data)); err == nil {
		t.Fatal("missing authorization allowed write")
	}
}
func TestStoreRejectsPartialOversizeChecksumAndCorruption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	root := t.TempDir()
	store := storage.LocalBlobStore{Root: root, MaxBytes: 8, Authorize: func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return nil }}
	for _, kind := range []string{"size", "checksum", "limit"} {
		req := writeRequest([]byte("data"))
		input := []byte("data")
		switch kind {
		case "size":
			input = []byte("extra-data")
		case "checksum":
			input = []byte("evil")
		case "limit":
			req.Size = 9
		}
		if _, err := store.Put(ctx, req, bytes.NewReader(input)); err == nil {
			t.Fatal(kind)
		}
	}
	files := 0
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files++
		}
		return nil
	})
	if files != 0 {
		t.Fatal("failed uploads left files")
	}
	req := writeRequest([]byte("data"))
	ref, err := store.Put(ctx, req, bytes.NewReader([]byte("data")))
	if err != nil {
		t.Fatal(err)
	}
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if filepath.Base(p) == string(ref.ID) {
			return os.WriteFile(p, []byte("evil"), 0600)
		}
		return err
	})
	stream, err := store.Open(ctx, contracts.BlobReadRequest{Request: req.Request, Scope: req.Scope, Blob: ref})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := io.ReadAll(stream); err == nil {
		t.Fatal("corruption accepted")
	}
}
