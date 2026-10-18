package moduletransport_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/moduletransport"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func TestHTTPUploadDeadlineInterruptsClientThatDoesNotDisconnect(t *testing.T) {
	data := []byte("abcdef")
	metadata, _ := uploadFixture(t, data)
	full, contentType := multipartBytes(t, []uploadPartSpec{{"metadata", "application/json", "", metadata}, {"file", "application/octet-stream", "", data}})
	stage := newDiskStage(t)
	result := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 150*time.Millisecond)
		defer cancel()
		_, err := moduletransport.ReceiveHTTPBlobUpload(ctx, w, r, uploadLimits(), allowUpload, func(context.Context, modulewire.Call, contracts.BlobWriteRequest) (moduletransport.UploadStaging, error) {
			return stage, nil
		})
		result <- err
		w.WriteHeader(400)
	}))
	defer server.Close()
	connection, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	prefix := full[:bytes.Index(full, data)+2]
	_, err = fmt.Fprintf(connection, "POST /modules/v1/blob_store/put HTTP/1.1\r\nHost: %s\r\nContent-Type: %s\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n", server.Listener.Addr().String(), contentType, len(prefix), prefix)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the TCP connection open without a next chunk or final boundary.
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) || stage.aborted.Load() != 1 {
			t.Fatalf("silent upload was not bounded and aborted: result=%v aborted=%d", err, stage.aborted.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("server relied on client disconnect")
	}
}

// Pin the scheduling order: the socket read deadline fires before an application
// deadline callback. net/http then cancels the request parent while unwinding
// the timed-out Body.Read. No independent context timer races this observation.
type uploadReadDeadlineContext struct {
	context.Context
	at time.Time
}

func (c uploadReadDeadlineContext) Deadline() (time.Time, bool) { return c.at, true }

type observedUploadBody struct {
	io.ReadCloser
	terminal error
}

func (b *observedUploadBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	if e != nil && e != io.EOF {
		b.terminal = e
	}
	return n, e
}

func TestHTTPUploadReadDeadlineBeforeContextTimer(t *testing.T) {
	testHTTPUploadReadClassification(t, "file", false, false)
}
func TestHTTPUploadCancellationBeforeDeadline(t *testing.T) {
	testHTTPUploadReadClassification(t, "file", true, false)
}
func TestHTTPUploadDeadlineCleanupFailure(t *testing.T) {
	testHTTPUploadReadClassification(t, "file", false, true)
}
func TestHTTPUploadMetadataReadDeadline(t *testing.T) {
	testHTTPUploadReadClassification(t, "metadata", false, false)
}
func TestHTTPUploadEpilogueReadDeadline(t *testing.T) {
	testHTTPUploadReadClassification(t, "epilogue", false, false)
}

type uploadDeadlineAbortFailure struct{ moduletransport.UploadStaging }

func (s uploadDeadlineAbortFailure) Abort(ctx context.Context) error {
	_ = s.UploadStaging.Abort(ctx)
	return errors.New("SECRET abort fixture")
}

func testHTTPUploadReadClassification(t *testing.T, segment string, earlyCancel, failCleanup bool) {
	t.Helper()
	data := []byte("abcdef")
	metadata, _ := uploadFixture(t, data)
	full, contentType := multipartBytes(t, []uploadPartSpec{{"metadata", "application/json", "", metadata}, {"file", "application/octet-stream", "", data}})
	stage := newDiskStage(t)
	// The metadata-only case never transfers this fixture handle to staging.
	t.Cleanup(func() { _ = stage.file.Close() })
	type observation struct {
		err, parent    error
		read           error
		deadlinePassed bool
		receipt        moduletransport.ValidatedBlobUpload
	}
	result := make(chan observation, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base, cancel := context.WithCancel(r.Context())
		defer cancel()
		budget := 250 * time.Millisecond
		if earlyCancel {
			budget = 2 * time.Second
		}
		ctx := uploadReadDeadlineContext{Context: base, at: time.Now().Add(budget)}
		var timer *time.Timer
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
		body := &observedUploadBody{ReadCloser: r.Body}
		r.Body = body
		receipt, err := moduletransport.ReceiveHTTPBlobUpload(ctx, w, r, uploadLimits(), allowUpload, func(context.Context, modulewire.Call, contracts.BlobWriteRequest) (moduletransport.UploadStaging, error) {
			if earlyCancel {
				timer = time.AfterFunc(10*time.Millisecond, cancel)
			}
			if failCleanup {
				return uploadDeadlineAbortFailure{stage}, nil
			}
			return stage, nil
		})
		result <- observation{err: err, parent: r.Context().Err(), read: body.terminal, deadlinePassed: !time.Now().Before(ctx.at), receipt: receipt}
		w.WriteHeader(400)
	}))
	defer server.Close()
	connection, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err = connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	prefix := full[:bytes.Index(full, data)+2]
	if segment == "metadata" {
		prefix = full[:bytes.Index(full, metadata)+len(metadata)/2]
	}
	if segment == "epilogue" {
		prefix = full
	}
	if _, err = fmt.Fprintf(connection, "POST /modules/v1/blob_store/put HTTP/1.1\r\nHost: %s\r\nContent-Type: %s\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n", server.Listener.Addr().String(), contentType, len(prefix), prefix); err != nil {
		t.Fatal(err)
	}
	select {
	case seen := <-result:
		expected, opposite := context.DeadlineExceeded, context.Canceled
		if earlyCancel {
			expected, opposite = context.Canceled, context.DeadlineExceeded
		}
		aborted := int32(1)
		if segment == "metadata" {
			aborted = 0
		}
		if !errors.Is(seen.err, expected) || errors.Is(seen.err, opposite) || stage.aborted.Load() != aborted || !reflect.DeepEqual(seen.receipt, moduletransport.ValidatedBlobUpload{}) || errors.Is(seen.err, moduletransport.ErrCleanup) != failCleanup || strings.Contains(seen.err.Error(), "SECRET") || seen.deadlinePassed == earlyCancel {
			t.Fatalf("read deadline lost classification: result=%v parent=%v read=%v deadline_passed=%t aborted=%d", seen.err, seen.parent, seen.read, seen.deadlinePassed, stage.aborted.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("socket deadline did not interrupt silent upload")
	}
}
