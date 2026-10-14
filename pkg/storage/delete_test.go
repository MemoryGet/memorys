package storage_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/storage"
)

func TestDeleteReplaysAcrossInstancesAndDoesNotRestoreAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	root := t.TempDir()
	deny := false
	authorize := func(context.Context, contracts.AccessScope, contracts.BlobRef) error {
		if deny {
			return errors.New("denied")
		}
		return nil
	}
	store := storage.LocalBlobStore{Root: root, Authorize: authorize}
	req := writeRequest([]byte("data"))
	ref, err := store.Put(ctx, req, bytes.NewReader([]byte("data")))
	if err != nil {
		t.Fatal(err)
	}
	read := contracts.BlobReadRequest{Request: req.Request, Scope: req.Scope, Blob: ref}
	var path string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if d.Name() == string(ref.ID) {
			path = p
		}
		return err
	})
	metadata, err := os.ReadFile(path + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, read); err != nil {
		t.Fatal(err)
	}
	second := storage.LocalBlobStore{Root: root, Authorize: authorize}
	if err := second.Delete(ctx, read); err != nil {
		t.Fatal("delete not idempotent", err)
	}
	// Simulate a data-only restore: the existing tombstone still hides the blob.
	if os.WriteFile(path, []byte("data"), 0600) != nil || os.WriteFile(path+".json", metadata, 0600) != nil {
		t.Fatal("restore fixture")
	}
	if _, err := second.Open(ctx, read); err == nil {
		t.Fatal("restored data bypassed tombstone")
	}
	if err := second.Delete(ctx, read); err != nil {
		t.Fatal("cleanup retry failed", err)
	}
	wrong := read
	wrong.Blob.SHA256 = contracts.Digest(strings.Repeat("0", 64))
	if second.Delete(ctx, wrong) != storage.ErrConflict {
		t.Fatal("changed blob ref deleted")
	}
	deny = true
	if second.Delete(ctx, read) == nil {
		t.Fatal("repeated delete bypassed current authorization")
	}
}

func TestConcurrentDeletesAndMalformedTombstoneFailClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	root := t.TempDir()
	store := storage.LocalBlobStore{Root: root, Authorize: func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return nil }}
	req := writeRequest([]byte("data"))
	ref, err := store.Put(ctx, req, bytes.NewReader([]byte("data")))
	if err != nil {
		t.Fatal(err)
	}
	read := contracts.BlobReadRequest{Request: req.Request, Scope: req.Scope, Blob: ref}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.Delete(ctx, read); err != nil {
				t.Error("concurrent delete", err)
			}
		}()
	}
	wg.Wait()
	var marker string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if strings.HasSuffix(p, ".deleted") {
			marker = p
		}
		return err
	})
	if marker == "" || os.WriteFile(marker, []byte("{"), 0600) != nil {
		t.Fatal("marker fixture")
	}
	if store.Delete(ctx, read) != storage.ErrPending {
		t.Fatal("malformed tombstone treated as success")
	}
	if _, err := store.Open(ctx, read); err == nil {
		t.Fatal("malformed tombstone allowed read")
	}
}
