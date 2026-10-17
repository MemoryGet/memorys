package queryread_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func TestReadCursorAuthenticatesClosedStateAndOwnsKey(t *testing.T) {
	key := []byte(strings.Repeat("s", 32))
	codec, err := queryread.NewHMACReadCursor("local-1", key)
	if err != nil {
		t.Fatal(err)
	}
	state := contracts.ReadCursorState{Binding: contracts.Digest(strings.Repeat("a", 64)), Selection: contracts.Digest(strings.Repeat("b", 64)), Position: 1, PageSize: 1, ExpiresAt: time.Now().Add(time.Minute).UTC()}
	if _, err := new(queryread.HMACReadCursor).SealReadCursor(context.Background(), state); err == nil {
		t.Fatal("zero-value signer admitted")
	}
	token, err := codec.SealReadCursor(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	key[0] = '!'
	actual, err := codec.OpenReadCursor(context.Background(), token)
	if err != nil || actual != state {
		t.Fatal("key ownership or state round trip", err)
	}
	for _, bad := range []string{token + "!", strings.Replace(token, "local-1", "other", 1), strings.Replace(token, "v1.", "v2.", 1), strings.Repeat("x", 4097)} {
		if _, err := codec.OpenReadCursor(context.Background(), bad); err == nil {
			t.Fatal("invalid token admitted")
		}
	}
	other, _ := queryread.NewHMACReadCursor("local-1", []byte(strings.Repeat("z", 32)))
	if _, err := other.OpenReadCursor(context.Background(), token); err == nil {
		t.Fatal("replacement key accepted old cursor")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := codec.OpenReadCursor(ctx, token); err == nil {
		t.Fatal("cancelled operation accepted")
	}
}

func TestReadCursorRejectsSignedExpiredAndUnknownState(t *testing.T) {
	key := []byte(strings.Repeat("s", 32))
	codec, _ := queryread.NewHMACReadCursor("key", key)
	for _, mode := range []string{"expired", "unknown", "duplicate", "position"} {
		state := contracts.ReadCursorState{Binding: contracts.Digest(strings.Repeat("a", 64)), Selection: contracts.Digest(strings.Repeat("b", 64)), Position: 1, PageSize: 1, ExpiresAt: time.Now().Add(time.Minute).UTC()}
		if mode == "expired" {
			state.ExpiresAt = time.Now().Add(-time.Second)
		}
		if mode == "position" {
			state.Position = 1001
		}
		raw, _ := json.Marshal(state)
		if mode == "unknown" {
			raw = append(raw[:len(raw)-1], []byte(`,"Admin":true}`)...)
		}
		if mode == "duplicate" {
			raw = append(raw[:len(raw)-1], []byte(`,"Position":1}`)...)
		}
		prefix := "v1.key." + base64.RawURLEncoding.EncodeToString(raw)
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte("akos.read-cursor/1\n" + prefix))
		token := prefix + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		if _, err := codec.OpenReadCursor(context.Background(), token); err == nil {
			t.Fatal("signed invalid state admitted", mode)
		}
	}
}
