package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrConflict = errors.New("upload idempotency conflict")
var ErrPending = errors.New("upload outcome requires recovery")

type uploadReceipt struct {
	Version       int
	RequestDigest string
	State         string
	Blob          contracts.BlobRef
}
type uploadJournal struct {
	file    *os.File
	path    string
	receipt uploadReceipt
	effect  bool
}

// Exclusive creation reserves the operation across store instances/processes.
// A pending or malformed log is never deleted/replayed as a fresh write.
func reserveUpload(dir string, req contracts.BlobWriteRequest) (*uploadJournal, *contracts.BlobRef, error) {
	keyJSON, _ := json.Marshal([]string{string(req.Scope.SubjectID), string(req.Scope.Action), req.IdempotencyKey})
	key := sha256.Sum256(append([]byte("AKOS-UPLOAD-KEY/1\n"), keyJSON...))
	semantic, _ := json.Marshal(struct {
		SHA   contracts.Digest
		Size  int64
		Media string
	}{req.ExpectedSHA256, req.Size, req.MediaType})
	digest := sha256.Sum256(append([]byte("AKOS-UPLOAD-REQUEST/1\n"), semantic...))
	requestDigest := hex.EncodeToString(digest[:])
	path := filepath.Join(dir, ".upload-"+hex.EncodeToString(key[:]))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, ErrPending
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil || stat.Size() > 8192 {
			return nil, nil, ErrPending
		}
		d := json.NewDecoder(io.LimitReader(f, 8193))
		d.DisallowUnknownFields()
		var pending, complete uploadReceipt
		if d.Decode(&pending) != nil || pending.Version != 1 || pending.State != "pending" {
			return nil, nil, ErrPending
		}
		if pending.RequestDigest != requestDigest {
			return nil, nil, ErrConflict
		}
		if d.Decode(&complete) != nil || complete.Version != 1 || complete.State != "complete" || complete.RequestDigest != pending.RequestDigest || complete.Blob != pending.Blob || d.Decode(new(any)) != io.EOF {
			return nil, nil, ErrPending
		}
		if !validHex(string(complete.Blob.ID), 32) || complete.Blob.SHA256 != req.ExpectedSHA256 || complete.Blob.Size != req.Size || complete.Blob.MediaType != req.MediaType {
			return nil, nil, ErrPending
		}
		return nil, &complete.Blob, nil
	}
	if err != nil {
		return nil, nil, ErrBlob
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		file.Close()
		os.Remove(path)
		return nil, nil, ErrBlob
	}
	j := &uploadJournal{file: file, path: path, receipt: uploadReceipt{Version: 1, RequestDigest: requestDigest, State: "pending", Blob: contracts.BlobRef{ID: contracts.ID(hex.EncodeToString(id[:])), SHA256: req.ExpectedSHA256, Size: req.Size, MediaType: req.MediaType}}}
	if json.NewEncoder(file).Encode(j.receipt) != nil || file.Sync() != nil {
		file.Close()
		os.Remove(path)
		return nil, nil, ErrBlob
	}
	return j, nil, nil
}
func (j *uploadJournal) finish() error {
	j.receipt.State = "complete"
	if json.NewEncoder(j.file).Encode(j.receipt) != nil || j.file.Sync() != nil {
		return ErrPending
	}
	return nil
}
func (j *uploadJournal) close() {
	j.file.Close()
	if !j.effect {
		os.Remove(j.path)
	}
}
