package modulewire

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// ValidateBlobStream consumes a bounded stream, requiring EOF, exact size and SHA-256.
// It does not close the reader or interrupt a blocked Read. The transport must
// enforce its deadline/cancellation and close the body; never commit staging on error.
func ValidateBlobStream(ctx context.Context, source io.Reader, blob contracts.BlobRef, maxBytes int64) error {
	if ctx == nil || source == nil || !validBlob(blob) || maxBytes < 0 || blob.Size > maxBytes {
		return invalid()
	}
	digest := sha256.New()
	buffer := make([]byte, 64<<10)
	var count int64
	idle := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Read at most the remaining declared bytes plus one overflow sentinel.
		limit := len(buffer)
		if remaining := blob.Size - count; remaining < int64(limit) {
			limit = int(remaining) + 1
		}
		n, err := source.Read(buffer[:limit])
		if n < 0 || n > limit {
			return invalid()
		}
		if cancel := ctx.Err(); cancel != nil {
			return cancel
		}
		if int64(n) > blob.Size-count {
			return invalid()
		}
		if n > 0 {
			count += int64(n)
			_, _ = digest.Write(buffer[:n])
			idle = 0
		} else if err == nil {
			idle++
			if idle >= 100 {
				return invalid()
			}
		}
		if err == io.EOF {
			if count != blob.Size || contracts.Digest(hex.EncodeToString(digest.Sum(nil))) != blob.SHA256 {
				return invalid()
			}
			return nil
		}
		if err != nil {
			return invalid()
		} // Never leak a provider's raw cause.
	}
}

// ValidateBlobResponseHeaders checks correlation and framing metadata, not authorization.
// A missing Content-Length permits chunked transfer; actual body validation is still required.
func ValidateBlobResponseHeaders(headers http.Header, requestID, invocationID contracts.ID, blob contracts.BlobRef) error {
	if ensureIDs(requestID, invocationID) != nil || !validBlob(blob) {
		return invalid()
	}
	for name, expected := range map[string]string{"X-AKOS-Request-Id": string(requestID), "X-AKOS-Invocation-Id": string(invocationID)} {
		values := blobHeaderValues(headers, name)
		if len(values) != 1 || values[0] != expected {
			return invalid()
		}
	}
	values := blobHeaderValues(headers, "Content-Type")
	if len(values) != 1 {
		return invalid()
	}
	mediaType, _, err := mime.ParseMediaType(values[0])
	if err != nil || mediaType != "application/octet-stream" {
		return invalid()
	}
	if lengths := blobHeaderValues(headers, "Content-Length"); len(lengths) > 0 {
		if len(lengths) != 1 || lengths[0] == "" {
			return invalid()
		}
		for _, char := range lengths[0] {
			if char < '0' || char > '9' {
				return invalid()
			}
		}
		length, err := strconv.ParseInt(lengths[0], 10, 64)
		if err != nil || length != blob.Size {
			return invalid()
		}
	}
	return nil
}

func blobHeaderValues(headers http.Header, name string) []string {
	var values []string
	for key, entries := range headers {
		if strings.EqualFold(key, name) {
			values = append(values, entries...)
		}
	}
	return values
}
