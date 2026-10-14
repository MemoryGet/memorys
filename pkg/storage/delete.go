package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type deleteMarker struct {
	Version int
	Blob    contracts.BlobRef
}

func readDeleteMarker(path string) (deleteMarker, error) {
	var marker deleteMarker
	f, err := os.Open(path)
	if err != nil {
		return marker, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 1025))
	if err != nil || len(raw) > 1024 {
		return marker, ErrPending
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&marker) != nil || marker.Version != 1 || d.Decode(new(any)) != io.EOF {
		return marker, ErrPending
	}
	return marker, nil
}

// Delete persists a logical tombstone before removing physical files. Repeated
// authorized deletion of the same BlobRef resumes cleanup instead of failing.
func (s LocalBlobStore) Delete(ctx context.Context, req contracts.BlobReadRequest) error {
	if ctx == nil || req.Request.RequestID == "" || req.Request.Deadline.IsZero() || !validHex(string(req.Blob.ID), 32) {
		return ErrBlob
	}
	deadline := req.Request.Deadline
	if req.Scope.ExpiresAt.Before(deadline) {
		deadline = req.Scope.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	dir, err := s.directory(ctx, req.Scope, req.Blob)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, string(req.Blob.ID))
	markerPath := path + ".deleted"
	marker, err := readDeleteMarker(markerPath)
	if err == nil {
		if marker.Blob != req.Blob {
			return ErrConflict
		}
	} else if errors.Is(err, os.ErrNotExist) {
		// Existing metadata must match; a caller cannot tombstone an unknown ID.
		needMarker := true
		if _, locateErr := s.locate(ctx, req); locateErr != nil {
			// A concurrent deleter may have installed the tombstone. Reauthorize
			// before retrying so a permission failure is never treated as a race.
			if _, err := s.directory(ctx, req.Scope, req.Blob); err != nil {
				return err
			}
			current, err := readDeleteMarker(markerPath)
			if err != nil {
				return locateErr
			}
			if current.Blob != req.Blob {
				return ErrConflict
			}
			needMarker = false
		}
		if needMarker {
			tmp, err := os.CreateTemp(dir, ".deletion-")
			if err != nil {
				return ErrBlob
			}
			temporary := tmp.Name()
			defer os.Remove(temporary)
			err = json.NewEncoder(tmp).Encode(deleteMarker{Version: 1, Blob: req.Blob})
			syncErr := tmp.Sync()
			closeErr := tmp.Close()
			if err != nil || syncErr != nil || closeErr != nil {
				return ErrPending
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if os.Link(temporary, markerPath) != nil {
				// Link installs the fully synced file without replacing a tombstone
				// another deleter is reading. Unsupported filesystems fail closed.
				if _, err := s.directory(ctx, req.Scope, req.Blob); err != nil {
					return err
				}
				current, err := readDeleteMarker(markerPath)
				if err != nil {
					return ErrPending
				}
				if current.Blob != req.Blob {
					return ErrConflict
				}
			}
		}
	} else {
		return ErrPending
	}
	for _, file := range []string{path, path + ".json"} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrPending
		}
	}
	return nil
}
