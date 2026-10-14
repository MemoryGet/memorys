package storage_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/storage"
)

type forbiddenReader struct{ t *testing.T }

func (r forbiddenReader) Read([]byte) (int, error) {
	r.t.Fatal("replay consumed upload again")
	return 0, io.EOF
}

type gateReader struct {
	r       io.Reader
	entered chan struct{}
	release chan struct{}
	first   bool
}

func (r *gateReader) Read(p []byte) (int, error) {
	if !r.first {
		r.first = true
		close(r.entered)
		<-r.release
	}
	return r.r.Read(p)
}
func TestConcurrentUploadReservationBlocksSecondWriter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store := storage.LocalBlobStore{Root: t.TempDir(), Authorize: func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return nil }}
	data := []byte("data")
	req := writeRequest(data)
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	done := make(chan error, 1)
	go func() {
		_, err := store.Put(ctx, req, &gateReader{r: bytes.NewReader(data), entered: entered, release: release})
		done <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("first writer did not start")
	}
	if _, err := store.Put(ctx, req, forbiddenReader{t}); err != storage.ErrPending {
		t.Fatal("second writer bypassed reservation", err)
	}
	// Release without closing twice, then wait for the original writer.
	release <- struct{}{}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("first writer stalled")
	}
}
func TestUploadReplayAcrossInstancesAndSemanticConflict(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	root := t.TempDir()
	authorize := func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return nil }
	first := storage.LocalBlobStore{Root: root, Authorize: authorize}
	data := []byte("data")
	req := writeRequest(data)
	ref, err := first.Put(ctx, req, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	second := storage.LocalBlobStore{Root: root, Authorize: authorize}
	req.Scope.ScopeID = "refreshed-scope"
	req.Request.RequestID = "retry-request"
	replayed, err := second.Put(ctx, req, forbiddenReader{t})
	if err != nil || replayed != ref {
		t.Fatal("durable replay failed", err)
	}
	changed := writeRequest([]byte("evil"))
	if _, err := second.Put(ctx, changed, bytes.NewReader([]byte("evil"))); err != storage.ErrConflict {
		t.Fatal("same key changed content accepted", err)
	}
	req.Scope.SubjectID = "other-user"
	another, err := second.Put(ctx, req, bytes.NewReader(data))
	if err != nil || another.ID == ref.ID {
		t.Fatal("subject operation collision", err)
	}
}

func TestPendingUploadNeverCreatesDuplicateOrResurrectsDeletedBlob(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	root := t.TempDir()
	store := storage.LocalBlobStore{Root: root, Authorize: func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return nil }}
	data := []byte("data")
	req := writeRequest(data)
	ref, err := store.Put(ctx, req, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var journal string
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".upload-") {
			journal = p
		}
		return nil
	})
	if err != nil || journal == "" {
		t.Fatal("journal missing")
	}
	raw, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	end := bytes.IndexByte(raw, '\n') + 1
	if end < 1 {
		t.Fatal("pending receipt missing")
	}
	if err := os.WriteFile(journal, raw[:end], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(ctx, req, forbiddenReader{t}); err != storage.ErrPending {
		t.Fatal("unknown operation was retried", err)
	}
	if err := os.WriteFile(journal, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, contracts.BlobReadRequest{Request: req.Request, Scope: req.Scope, Blob: ref}); err != nil {
		t.Fatal(err)
	}
	replayed, err := store.Put(ctx, req, forbiddenReader{t})
	if err != nil || replayed != ref {
		t.Fatal("deleted operation receipt changed", err)
	}
	if _, err := store.Open(ctx, contracts.BlobReadRequest{Request: req.Request, Scope: req.Scope, Blob: ref}); err == nil {
		t.Fatal("replay resurrected deleted blob")
	}
}
