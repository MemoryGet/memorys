package moduletransport_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/moduletransport"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

type diskStage struct {
	file            *os.File
	path            string
	maxWrite        int
	closed, aborted atomic.Int32
}

func newDiskStage(t *testing.T) *diskStage {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "isolated-*")
	if err != nil {
		t.Fatal(err)
	}
	return &diskStage{file: file, path: file.Name()}
}
func (stage *diskStage) Write(p []byte) (int, error) {
	if len(p) > stage.maxWrite {
		stage.maxWrite = len(p)
	}
	return stage.file.Write(p)
}
func (stage *diskStage) Close() error { stage.closed.Add(1); return stage.file.Close() }
func (stage *diskStage) Abort(ctx context.Context) error {
	stage.aborted.Add(1)
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Remove(stage.path)
}

type uploadPartSpec struct {
	name, media, encoding string
	data                  []byte
}

func uploadFixture(t *testing.T, data []byte) ([]byte, contracts.BlobWriteRequest) {
	t.Helper()
	digest := sha256.Sum256(data)
	deadline := time.Now().UTC().Add(10 * time.Second)
	value := contracts.BlobWriteRequest{Request: contracts.RequestContext{RequestID: "r", ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), Deadline: deadline}, Scope: contracts.AccessScope{ScopeID: "scope-1", SubjectID: "subject-1", Action: "blob.put", SpaceID: "space-1", KnowledgeBaseIDs: []contracts.ID{"kb-1"}, PolicyVersion: 1, ExpiresAt: deadline}, ExpectedSHA256: contracts.Digest(hex.EncodeToString(digest[:])), Size: int64(len(data)), MediaType: "application/octet-stream", IdempotencyKey: "key-1"}
	metadata, err := modulewire.EncodeBlobPutMetadata(modulewire.Call{InvocationID: "i", ModuleInstanceID: "blob-a"}, value)
	if err != nil {
		t.Fatal(err)
	}
	return metadata, value
}
func multipartBytes(t *testing.T, parts []uploadPartSpec) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, spec := range parts {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="`+spec.name+`"; filename="../../untrusted.bin"`)
		header.Set("Content-Type", spec.media)
		if spec.encoding != "" {
			header.Set("Content-Transfer-Encoding", spec.encoding)
		}
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(spec.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}
func uploadLimits() moduletransport.BlobUploadLimits {
	return moduletransport.BlobUploadLimits{MaxMetadataBytes: 4 << 20, MaxFileBytes: 8 << 20, MaxWireBytes: 10 << 20, CleanupTimeout: time.Second}
}
func uploadRequest(t *testing.T, parts []uploadPartSpec) *http.Request {
	t.Helper()
	data, contentType := multipartBytes(t, parts)
	request := httptest.NewRequest("POST", "/modules/v1/blob_store/put", bytes.NewReader(data))
	request.Header.Set("Content-Type", contentType)
	return request
}
func allowUpload(context.Context, []byte, modulewire.Call, contracts.BlobWriteRequest) error {
	return nil
} // Test stub, not Grant verification.

func TestMultipartStreamsLargeFileIntoIsolatedDiskWithoutFilenameUse(t *testing.T) {
	for _, data := range [][]byte{bytes.Repeat([]byte("x"), 5<<20), {}} {
		metadata, value := uploadFixture(t, data)
		request := uploadRequest(t, []uploadPartSpec{{"metadata", "application/json", "", metadata}, {"file", "application/octet-stream", "", data}})
		stage := newDiskStage(t)
		checks := 0
		receipt, err := moduletransport.ReceiveBlobMultipart(bounded(t), request, uploadLimits(), func(context.Context, []byte, modulewire.Call, contracts.BlobWriteRequest) error { checks++; return nil }, func(context.Context, modulewire.Call, contracts.BlobWriteRequest) (moduletransport.UploadStaging, error) {
			return stage, nil
		})
		if err != nil || receipt.ByteCount != value.Size || checks != 2 || stage.aborted.Load() != 0 || stage.closed.Load() != 1 || stage.maxWrite > 64<<10 {
			t.Fatal("invalid streamed receipt", err, receipt.ByteCount, checks)
		}
		stored, err := os.ReadFile(stage.path)
		if err != nil || !bytes.Equal(stored, data) {
			t.Fatal("isolated bytes differ", err)
		}
		if receipt.Request.Scope.SpaceID != value.Scope.SpaceID || strings.Contains(stage.path, "untrusted.bin") {
			t.Fatal("filename became storage mapping")
		}
	}
}
func TestMultipartFailuresAbortStagingAndReturnNoReceipt(t *testing.T) {
	data := []byte("abcdef")
	metadata, _ := uploadFixture(t, data)
	for _, parts := range [][]uploadPartSpec{
		{{"metadata", "application/json", "", metadata}, {"file", "application/octet-stream", "", []byte("xxxxxx")}},
		{{"metadata", "application/json", "", metadata}, {"file", "application/octet-stream", "", data[:5]}},
		{{"metadata", "application/json", "", metadata}, {"file", "application/octet-stream", "", append(append([]byte{}, data...), 1)}},
		{{"metadata", "application/json", "", metadata}, {"file", "application/octet-stream", "", data}, {"extra", "application/octet-stream", "", []byte("hidden")}},
	} {
		stage := newDiskStage(t)
		receipt, err := moduletransport.ReceiveBlobMultipart(bounded(t), uploadRequest(t, parts), uploadLimits(), allowUpload, func(context.Context, modulewire.Call, contracts.BlobWriteRequest) (moduletransport.UploadStaging, error) {
			return stage, nil
		})
		if err == nil || receipt.Request.Request.RequestID != "" || stage.aborted.Load() != 1 {
			t.Fatal("failed upload became receipt", err)
		}
		if _, err := os.Stat(stage.path); !os.IsNotExist(err) {
			t.Fatal("failed disk candidate not removed")
		}
	}
}
func TestMultipartMetadataOrderEncodingAndBudgetsRejectBeforeStorage(t *testing.T) {
	data := []byte("abcdef")
	metadata, _ := uploadFixture(t, data)
	opened := 0
	for _, parts := range [][]uploadPartSpec{
		{{"file", "application/octet-stream", "", data}, {"metadata", "application/json", "", metadata}},
		{{"metadata", "text/plain", "", metadata}, {"file", "application/octet-stream", "", data}},
		{{"metadata", "application/json", "", metadata}, {"file", "application/octet-stream", "quoted-printable", data}},
		{{"metadata", "application/json", "", []byte(`{"bad":true}`)}, {"file", "application/octet-stream", "", data}},
	} {
		receipt, err := moduletransport.ReceiveBlobMultipart(bounded(t), uploadRequest(t, parts), uploadLimits(), allowUpload, func(context.Context, modulewire.Call, contracts.BlobWriteRequest) (moduletransport.UploadStaging, error) {
			opened++
			return nil, errors.New("SECRET stage")
		})
		if err == nil || receipt.Call.InvocationID != "" || opened != 0 {
			t.Fatal("invalid multipart opened storage", err)
		}
	}
	for _, change := range []func(*moduletransport.BlobUploadLimits){func(v *moduletransport.BlobUploadLimits) { v.MaxMetadataBytes = 8 }, func(v *moduletransport.BlobUploadLimits) { v.MaxFileBytes = 1 }, func(v *moduletransport.BlobUploadLimits) { v.MaxWireBytes = 8 }} {
		limits := uploadLimits()
		change(&limits)
		request := uploadRequest(t, []uploadPartSpec{{"metadata", "application/json", "", metadata}, {"file", "application/octet-stream", "", data}})
		if _, err := moduletransport.ReceiveBlobMultipart(bounded(t), request, limits, allowUpload, func(context.Context, modulewire.Call, contracts.BlobWriteRequest) (moduletransport.UploadStaging, error) {
			opened++
			return nil, errors.New("SECRET")
		}); err == nil || opened != 0 {
			t.Fatal("budget ignored")
		}
	}
}
func TestMultipartCurrentAuthorizationBeforeStorageAndAfterBytes(t *testing.T) {
	data := []byte("abcdef")
	metadata, _ := uploadFixture(t, data)
	for _, denyAt := range []int{1, 2} {
		checks := 0
		opened := 0
		var stage *diskStage
		receipt, err := moduletransport.ReceiveBlobMultipart(bounded(t), uploadRequest(t, []uploadPartSpec{{"metadata", "application/json", "", metadata}, {"file", "application/octet-stream", "", data}}), uploadLimits(), func(context.Context, []byte, modulewire.Call, contracts.BlobWriteRequest) error {
			checks++
			if checks == denyAt {
				return errors.New("SECRET authorization")
			}
			return nil
		}, func(context.Context, modulewire.Call, contracts.BlobWriteRequest) (moduletransport.UploadStaging, error) {
			opened++
			stage = newDiskStage(t)
			return stage, nil
		})
		if !errors.Is(err, moduletransport.ErrAccess) || receipt.Call.InvocationID != "" || strings.Contains(err.Error(), "SECRET") {
			t.Fatal("access denial bypassed", err)
		}
		if denyAt == 1 && opened != 0 {
			t.Fatal("storage opened before authorization")
		}
		if denyAt == 2 && (opened != 1 || stage.aborted.Load() != 1) {
			t.Fatal("revoked upload not aborted")
		}
	}
}
func TestActualHTTPMultipartReceiptAndInterruptedUploadAbort(t *testing.T) {
	data := []byte("abcdef")
	metadata, _ := uploadFixture(t, data)
	full, contentType := multipartBytes(t, []uploadPartSpec{{"metadata", "application/json", "", metadata}, {"file", "application/octet-stream", "", data}})
	for _, interrupt := range []bool{false, true} {
		stage := newDiskStage(t)
		opened := make(chan struct{})
		result := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			_, err := moduletransport.ReceiveHTTPBlobUpload(ctx, w, r, uploadLimits(), allowUpload, func(context.Context, modulewire.Call, contracts.BlobWriteRequest) (moduletransport.UploadStaging, error) {
				close(opened)
				return stage, nil
			})
			result <- err
			if err != nil {
				w.WriteHeader(400)
			} else {
				w.WriteHeader(200)
			}
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var body io.Reader = bytes.NewReader(full)
		if interrupt {
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			body = reader
			prefix := full[:bytes.Index(full, data)+2]
			go func() { _, _ = writer.Write(prefix); <-ctx.Done(); _ = writer.CloseWithError(ctx.Err()) }()
		}
		request, err := http.NewRequestWithContext(ctx, "POST", server.URL, body)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", contentType)
		clientDone := make(chan error, 1)
		go func() {
			response, err := server.Client().Do(request)
			if response != nil {
				_ = response.Body.Close()
			}
			clientDone <- err
		}()
		if interrupt {
			select {
			case <-opened:
				cancel()
			case <-ctx.Done():
				t.Fatal("upload did not reach staging")
			}
		}
		select {
		case err := <-result:
			if interrupt {
				if err == nil || stage.aborted.Load() != 1 {
					t.Fatal("interrupted HTTP upload not aborted", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("handler did not exit")
		}
		select {
		case <-clientDone:
		case <-time.After(time.Second):
			t.Fatal("client did not exit")
		}
		cancel()
		server.Close()
	}
}
