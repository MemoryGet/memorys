package moduletransport_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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
			t.Fatal("silent upload was not bounded and aborted", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server relied on client disconnect")
	}
}
