package queryread

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// HMACReadCursor is a local implementation of the replaceable cursor port.
// Keys must come from trusted deployment secrets. Replacing the configured key
// intentionally invalidates old cursors; key IDs never select caller keys.
type HMACReadCursor struct {
	keyID string
	key   []byte
}

func (*HMACReadCursor) String() string   { return "HMACReadCursor[redacted]" }
func (*HMACReadCursor) GoString() string { return "HMACReadCursor[redacted]" }

func NewHMACReadCursor(keyID string, key []byte) (*HMACReadCursor, error) {
	if len(key) < 32 || len(key) > 128 || len(keyID) < 1 || len(keyID) > 64 {
		return nil, ErrInvalid
	}
	for _, c := range keyID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return nil, ErrInvalid
		}
	}
	return &HMACReadCursor{keyID: keyID, key: append([]byte(nil), key...)}, nil
}

func validCursorState(s contracts.ReadCursorState) bool {
	return cursorDigest(s.Binding) && cursorDigest(s.Selection) && s.Position > 0 && s.Position <= 1000 && s.PageSize > 0 && s.PageSize <= 1000 && s.ExpiresAt.After(time.Now()) && !s.ExpiresAt.After(time.Now().Add(5*time.Minute))
}
func cursorDigest(d contracts.Digest) bool {
	if len(d) != 64 {
		return false
	}
	for _, c := range d {
		if !(c >= 'a' && c <= 'f' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func (c *HMACReadCursor) SealReadCursor(ctx context.Context, s contracts.ReadCursorState) (string, error) {
	if c == nil || len(c.key) < 32 || c.keyID == "" || ctx == nil || ctx.Err() != nil || !validCursorState(s) {
		return "", ErrInvalid
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return "", ErrInvalid
	}
	prefix := "v1." + c.keyID + "." + base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte("akos.read-cursor/1\n" + prefix))
	return prefix + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (c *HMACReadCursor) OpenReadCursor(ctx context.Context, token string) (contracts.ReadCursorState, error) {
	var empty contracts.ReadCursorState
	if c == nil || len(c.key) < 32 || c.keyID == "" || ctx == nil || ctx.Err() != nil || len(token) > 4096 {
		return empty, ErrInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "v1" || parts[1] != c.keyID {
		return empty, ErrInvalid
	}
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte("akos.read-cursor/1\n" + strings.Join(parts[:3], ".")))
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[3])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return empty, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil {
		return empty, ErrInvalid
	}
	var state contracts.ReadCursorState
	if json.Unmarshal(raw, &state) != nil || !validCursorState(state) {
		return empty, ErrInvalid
	}
	// Only our exact closed encoding is admitted, including absent-field rules.
	encoded, err := json.Marshal(state)
	if err != nil || !bytes.Equal(raw, encoded) {
		return empty, ErrInvalid
	}
	return state, nil
}

var _ contracts.ReadCursorCodec = (*HMACReadCursor)(nil)
