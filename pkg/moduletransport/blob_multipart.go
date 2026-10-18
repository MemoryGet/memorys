package moduletransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

var ErrStaging = errors.New("upload staging failed")
var ErrCleanup = errors.New("upload cleanup requires recovery")

// UploadStaging is isolated storage. Close flushes/closes but MUST NOT publish or
// activate a BlobRef. Abort is idempotent and must honor its cleanup context.
type UploadStaging interface {
	io.WriteCloser
	Abort(context.Context) error
}
type BlobUploadLimits struct {
	MaxMetadataBytes int64
	MaxFileBytes     int64
	MaxWireBytes     int64
	CleanupTimeout   time.Duration
	interruptRead    func()
}
type ValidatedBlobUpload struct {
	Call      modulewire.Call
	Request   contracts.BlobWriteRequest
	ByteCount int64
}

// ReceiveBlobMultipart accepts exactly metadata then file and never commits a
// BlobRef. authorize must verify trusted service/Grant/current access, including
// method/path and canonical metadata; a parsed Scope is not authorization.
func ReceiveBlobMultipart(ctx context.Context, request *http.Request, limits BlobUploadLimits, authorize func(context.Context, []byte, modulewire.Call, contracts.BlobWriteRequest) error, stage func(context.Context, modulewire.Call, contracts.BlobWriteRequest) (UploadStaging, error)) (receipt ValidatedBlobUpload, failure error) {
	if request == nil || request.Body == nil {
		return receipt, ErrProtocol
	}
	var once sync.Once
	closeBody := func() { once.Do(func() { _ = request.Body.Close() }) }
	defer closeBody()
	if ctx == nil || authorize == nil || stage == nil || limits.MaxMetadataBytes < 1 || limits.MaxMetadataBytes > 4<<20 || limits.MaxFileBytes < 0 || limits.MaxWireBytes < 1 || limits.MaxWireBytes == math.MaxInt64 || limits.CleanupTimeout <= 0 {
		return receipt, ErrProtocol
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return receipt, ErrProtocol
	}
	if err := ctx.Err(); err != nil {
		return receipt, err
	}
	interruptAndClose := func() {
		if limits.interruptRead != nil {
			limits.interruptRead()
		}
		closeBody()
	}
	stop := context.AfterFunc(ctx, interruptAndClose)
	defer stop()
	types := headerValues(request.Header, "Content-Type")
	if len(types) != 1 {
		return receipt, ErrProtocol
	}
	media, params, err := mime.ParseMediaType(types[0])
	if err != nil || media != "multipart/form-data" || params["boundary"] == "" {
		return receipt, ErrProtocol
	}
	raw := &uploadBody{reader: io.LimitReader(request.Body, limits.MaxWireBytes+1), limit: limits.MaxWireBytes}
	parts := multipart.NewReader(raw, params["boundary"])
	metadata, err := parts.NextRawPart()
	if err != nil || !uploadPart(metadata, "metadata", "application/json") {
		return receipt, uploadReadFailure(ctx, err, ErrProtocol)
	}
	bytes, err := io.ReadAll(io.LimitReader(metadata, limits.MaxMetadataBytes+1))
	if err != nil || int64(len(bytes)) > limits.MaxMetadataBytes {
		return receipt, uploadReadFailure(ctx, err, ErrProtocol)
	}
	call, value, err := modulewire.DecodeBlobPutMetadata(bytes)
	if err != nil || value.Size > limits.MaxFileBytes {
		return receipt, ErrProtocol
	}
	work, cancel := context.WithDeadline(ctx, value.Request.Deadline)
	defer cancel()
	stopWork := context.AfterFunc(work, interruptAndClose)
	defer stopWork()
	if err := work.Err(); err != nil {
		return receipt, err
	}
	if err := authorize(work, append([]byte(nil), bytes...), call, value); err != nil {
		return receipt, uploadFailure(work, ErrAccess)
	}
	// Re-decode the frozen bytes so an authorization callback cannot mutate the
	// slice-backed request values subsequently supplied to storage.
	call, value, err = modulewire.DecodeBlobPutMetadata(bytes)
	if err != nil {
		return receipt, ErrProtocol
	}
	file, err := parts.NextRawPart()
	if err != nil || !uploadPart(file, "file", "application/octet-stream") {
		return receipt, uploadReadFailure(work, err, ErrProtocol)
	}
	if err := work.Err(); err != nil {
		return receipt, err
	}
	writer, err := stage(work, call, value)
	if err != nil || writer == nil {
		return receipt, uploadFailure(work, ErrStaging)
	}
	var closeOnce sync.Once
	var closeErr error
	closeStage := func() { closeOnce.Do(func() { closeErr = writer.Close() }) }
	stopStage := context.AfterFunc(work, closeStage)
	defer func() {
		stopStage()
		closeStage()
		if closeErr != nil && failure == nil {
			failure = ErrStaging
			receipt = ValidatedBlobUpload{}
		}
		if failure != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(work), limits.CleanupTimeout)
			defer cancel()
			if writer.Abort(cleanup) != nil {
				failure = errors.Join(failure, ErrCleanup)
			}
		}
	}()
	count, err := copyUpload(work, writer, file, value.Size, value.ExpectedSHA256)
	if err != nil {
		return receipt, err
	}
	if extra, err := parts.NextRawPart(); err != io.EOF || extra != nil {
		return receipt, uploadReadFailure(work, err, ErrProtocol)
	}
	// multipart EOF denotes the final boundary, not necessarily HTTP-body EOF.
	// Drain bounded epilogue and inspect any earlier read error hidden by buffering.
	if _, err := io.Copy(io.Discard, raw); err != nil || raw.terminalError != nil {
		if err == nil {
			err = raw.terminalError
		}
		return receipt, uploadReadFailure(work, err, ErrProtocol)
	}
	if err := work.Err(); err != nil {
		return receipt, err
	}
	if err := authorize(work, append([]byte(nil), bytes...), call, value); err != nil {
		return receipt, uploadFailure(work, ErrAccess)
	}
	closeStage()
	if closeErr != nil {
		return receipt, ErrStaging
	}
	if err := work.Err(); err != nil {
		return receipt, err
	}
	call, value, err = modulewire.DecodeBlobPutMetadata(bytes)
	if err != nil {
		return receipt, ErrProtocol
	}
	return ValidatedBlobUpload{Call: call, Request: value, ByteCount: count}, nil
}

// ReceiveHTTPBlobUpload installs a server read deadline before parsing. Closing
// a net/http request Body alone may not interrupt a blocked server Read.
// ResponseWriter wrappers must expose Unwrap or deadline support; no fallback
// silently accepts an unbounded upload when the controller is unsupported.
func ReceiveHTTPBlobUpload(ctx context.Context, writer http.ResponseWriter, request *http.Request, limits BlobUploadLimits, authorize func(context.Context, []byte, modulewire.Call, contracts.BlobWriteRequest) error, stage func(context.Context, modulewire.Call, contracts.BlobWriteRequest) (UploadStaging, error)) (ValidatedBlobUpload, error) {
	if ctx == nil || writer == nil {
		return ValidatedBlobUpload{}, ErrProtocol
	}
	deadline, bounded := ctx.Deadline()
	if !bounded {
		return ValidatedBlobUpload{}, ErrProtocol
	}
	controller := http.NewResponseController(writer)
	if controller.SetReadDeadline(deadline) != nil {
		return ValidatedBlobUpload{}, ErrProtocol
	}
	defer controller.SetReadDeadline(time.Time{})
	limits.interruptRead = func() { _ = controller.SetReadDeadline(time.Now()) }
	return ReceiveBlobMultipart(ctx, request, limits, authorize, stage)
}

type uploadBody struct {
	reader        io.Reader
	count, limit  int64
	terminalError error
}

func (body *uploadBody) Read(p []byte) (int, error) {
	n, err := body.reader.Read(p)
	body.count += int64(n)
	if err != nil && err != io.EOF {
		body.terminalError = err
	}
	if body.count > body.limit {
		body.terminalError = ErrProtocol
		return 0, ErrProtocol
	}
	return n, err
}
func uploadPart(part *multipart.Part, name, contentType string) bool {
	if part == nil || part.FormName() != name || len(part.Header.Values("Content-Transfer-Encoding")) > 0 {
		return false
	}
	if len(part.Header.Values("Content-Disposition")) != 1 {
		return false
	}
	disposition, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	if err != nil || disposition != "form-data" || params["name"] != name {
		return false
	}
	values := part.Header.Values("Content-Type")
	if len(values) != 1 {
		return false
	}
	media, params, err := mime.ParseMediaType(values[0])
	if err != nil || media != contentType {
		return false
	}
	if charset, present := params["charset"]; present && !strings.EqualFold(charset, "utf-8") {
		return false
	}
	return true
}
func uploadFailure(ctx context.Context, fallback error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return fallback
}

// net/http cancels the request parent when Body.Read reports a socket timeout.
// That cancellation can reach ctx before its own deadline timer callback. Keep
// the actual read cause when the effective upload deadline has already passed;
// cancellation observed while that deadline is live remains context.Canceled.
func uploadReadFailure(ctx context.Context, cause, fallback error) error {
	var timeout net.Error
	if errors.As(cause, &timeout) && timeout.Timeout() {
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
	}
	return uploadFailure(ctx, fallback)
}
func copyUpload(ctx context.Context, destination io.Writer, source io.Reader, size int64, expected contracts.Digest) (int64, error) {
	buffer := make([]byte, 64<<10)
	digest := sha256.New()
	var count int64
	idle := 0
	for {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		limit := len(buffer)
		if remaining := size - count; remaining < int64(limit) {
			limit = int(remaining) + 1
		}
		n, err := source.Read(buffer[:limit])
		if n < 0 || n > limit || int64(n) > size-count {
			return count, ErrProtocol
		}
		if cancel := ctx.Err(); cancel != nil {
			return count, uploadReadFailure(ctx, err, cancel)
		}
		if n > 0 {
			written, writeErr := destination.Write(buffer[:n])
			if writeErr != nil || written != n {
				return count, uploadFailure(ctx, ErrStaging)
			}
			count += int64(n)
			_, _ = digest.Write(buffer[:n])
			idle = 0
		} else if err == nil {
			idle++
			if idle >= 100 {
				return count, ErrProtocol
			}
		}
		if err == io.EOF {
			if count != size || contracts.Digest(hex.EncodeToString(digest.Sum(nil))) != expected {
				return count, ErrProtocol
			}
			return count, nil
		}
		if err != nil {
			return count, uploadReadFailure(ctx, err, ErrProtocol)
		}
	}
}
