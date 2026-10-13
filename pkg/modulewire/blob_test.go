package modulewire_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func blobFixture(data []byte) contracts.BlobRef {
	digest := sha256.Sum256(data)
	return contracts.BlobRef{ID: "blob-1", SHA256: contracts.Digest(hex.EncodeToString(digest[:])), Size: int64(len(data)), MediaType: "application/octet-stream"}
}
func TestBlobMetadataRoundTripsAndPutResultChecks(t *testing.T) {
	base := parseRequest()
	call := modulewire.Call{InvocationID: "i", ModuleInstanceID: "blob-a"}
	blob := blobFixture([]byte("中文😀"))
	write := contracts.BlobWriteRequest{Request: base.Request, Scope: base.Scope, ExpectedSHA256: blob.SHA256, Size: blob.Size, MediaType: blob.MediaType, IdempotencyKey: "key-1"}
	metadata, err := modulewire.EncodeBlobPutMetadata(call, write)
	if err != nil {
		t.Fatal(err)
	}
	gotCall, got, err := modulewire.DecodeBlobPutMetadata(metadata)
	if err != nil || gotCall != call || !reflect.DeepEqual(got, write) {
		t.Fatal("lost put metadata", err)
	}
	read := contracts.BlobReadRequest{Request: base.Request, Scope: base.Scope, Blob: blob}
	data, err := modulewire.EncodeBlobReadRequest(call, read)
	if err != nil {
		t.Fatal(err)
	}
	gotCall, gotRead, err := modulewire.DecodeBlobReadRequest(data)
	if err != nil || gotCall != call || !reflect.DeepEqual(gotRead, read) {
		t.Fatal("lost open/delete metadata", err)
	}
	result, err := modulewire.EncodeBlobPutResult(base.Request.RequestID, call.InvocationID, blob)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := modulewire.DecodeBlobPutResult(result, base.Request.RequestID, call.InvocationID, write)
	if err != nil || actual != blob {
		t.Fatal("lost blob result", err)
	}
	write.Size++
	if _, err = modulewire.DecodeBlobPutResult(result, base.Request.RequestID, call.InvocationID, write); err == nil {
		t.Fatal("changed blob accepted")
	}
	if _, err = modulewire.DecodeBlobPutResult(result, "wrong", call.InvocationID, write); err == nil {
		t.Fatal("wrong response ID accepted")
	}
	ack, err := modulewire.EncodeBlobDeleteAck(base.Request.RequestID, call.InvocationID)
	if err != nil {
		t.Fatal(err)
	}
	if modulewire.DecodeBlobDeleteAck(ack, base.Request.RequestID, call.InvocationID) != nil {
		t.Fatal("ack rejected")
	}
	if modulewire.DecodeBlobDeleteAck([]byte(strings.Replace(string(ack), "true", "false", 1)), base.Request.RequestID, call.InvocationID) == nil {
		t.Fatal("incomplete deletion accepted")
	}
}

type blobReaderFunc func([]byte) (int, error)

func (read blobReaderFunc) Read(p []byte) (int, error) { return read(p) }

func TestBlobCancellationDuringReadAndCaseSplitHeaders(t *testing.T) {
	data := []byte("abc")
	blob := blobFixture(data)
	ctx, cancel := context.WithCancel(context.Background())
	reader := blobReaderFunc(func(p []byte) (int, error) { n := copy(p, data); cancel(); return n, io.EOF })
	if !errors.Is(modulewire.ValidateBlobStream(ctx, reader, blob, 3), context.Canceled) {
		t.Fatal("cancelled read reported complete")
	}
	headers := http.Header{"X-AKOS-Request-Id": {"r"}, "x-akos-request-id": {"r"}, "X-AKOS-Invocation-Id": {"i"}, "Content-Type": {"application/octet-stream"}}
	if modulewire.ValidateBlobResponseHeaders(headers, "r", "i", blob) == nil {
		t.Fatal("case-split duplicate header accepted")
	}
	delete(headers, "X-AKOS-Request-Id")
	if modulewire.ValidateBlobResponseHeaders(headers, "r", "i", blob) != nil {
		t.Fatal("lowercase single header rejected")
	}
}

type terminalReader struct {
	data []byte
	err  error
}

func (r *terminalReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, r.err
}

type idleReader struct{}

func (idleReader) Read([]byte) (int, error) { return 0, nil }
func TestBlobStreamRequiresEOFSizeHashAndBoundedProgress(t *testing.T) {
	data := []byte("中文😀")
	blob := blobFixture(data)
	for name, reader := range map[string]io.Reader{"ordinary": bytes.NewReader(data), "data-with-EOF": &terminalReader{data: data, err: io.EOF}} {
		if err := modulewire.ValidateBlobStream(context.Background(), reader, blob, blob.Size); err != nil {
			t.Fatal(name, err)
		}
	}
	for name, reader := range map[string]io.Reader{"truncated": bytes.NewReader(data[:len(data)-1]), "overrun": bytes.NewReader(append(append([]byte{}, data...), 1)), "wrong-hash": bytes.NewReader(bytes.Repeat([]byte{'x'}, len(data))), "provider-error": &terminalReader{data: data, err: errors.New("secret provider detail")}, "no-progress": idleReader{}} {
		if err := modulewire.ValidateBlobStream(context.Background(), reader, blob, blob.Size); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal(name, "invalid stream accepted or unsafe cause")
		}
	}
	if modulewire.ValidateBlobStream(context.Background(), bytes.NewReader(data), blob, blob.Size-1) == nil {
		t.Fatal("budget ignored")
	}
	empty := blobFixture(nil)
	if modulewire.ValidateBlobStream(context.Background(), bytes.NewReader(nil), empty, 0) != nil {
		t.Fatal("empty blob rejected")
	}
}
func TestBlobStreamCancellationAndHeaders(t *testing.T) {
	blob := blobFixture([]byte("abc"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(modulewire.ValidateBlobStream(ctx, bytes.NewReader([]byte("abc")), blob, 3), context.Canceled) {
		t.Fatal("cancel ignored")
	}
	headers := http.Header{}
	headers.Set("X-AKOS-Request-Id", "r")
	headers.Set("X-AKOS-Invocation-Id", "i")
	headers.Set("Content-Type", "application/octet-stream")
	if modulewire.ValidateBlobResponseHeaders(headers, "r", "i", blob) != nil {
		t.Fatal("chunked headers rejected")
	}
	headers.Set("Content-Length", "3")
	if modulewire.ValidateBlobResponseHeaders(headers, "r", "i", blob) != nil {
		t.Fatal("length rejected")
	}
	for name, values := range map[string][]string{"X-AKOS-Request-Id": {"wrong"}, "X-AKOS-Invocation-Id": {"i", "i"}, "Content-Type": {"application/json"}, "Content-Length": {"2"}} {
		bad := headers.Clone()
		bad[http.CanonicalHeaderKey(name)] = values
		if modulewire.ValidateBlobResponseHeaders(bad, "r", "i", blob) == nil {
			t.Fatal("bad header accepted", name)
		}
	}
}
