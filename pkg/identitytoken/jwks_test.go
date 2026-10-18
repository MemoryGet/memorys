package identitytoken_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MemoryGet/memorys/pkg/identitytoken"
	jose "github.com/go-jose/go-jose/v4"
)

func writeJWKS(t *testing.T, set jose.JSONWebKeySet) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "jwks.json")
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRS256PublicKeysCopiesDeploymentKeys(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	path := writeJWKS(t, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "key-1", Algorithm: string(jose.RS256), Use: "sig"}}})
	keys, err := identitytoken.LoadRS256PublicKeys(path)
	if err != nil || len(keys) != 1 || keys["key-1"] == nil || keys["key-1"].N.Cmp(key.N) != 0 || keys["key-1"] == &key.PublicKey {
		t.Fatalf("JWKS key was not safely copied: keys=%v err=%v", keys, err)
	}
}

func TestLoadRS256PublicKeysRejectsUnsafeSets(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, set := range []jose.JSONWebKeySet{
		{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "key-1", Algorithm: "RS384", Use: "sig"}}},
		{Keys: []jose.JSONWebKey{{Key: key, KeyID: "key-1", Algorithm: string(jose.RS256), Use: "sig"}}},
		{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "same", Algorithm: string(jose.RS256), Use: "sig"}, {Key: &key.PublicKey, KeyID: "same", Algorithm: string(jose.RS256), Use: "sig"}}},
	} {
		if _, err := identitytoken.LoadRS256PublicKeys(writeJWKS(t, set)); err == nil {
			t.Fatal("unsafe JWKS accepted")
		}
	}
	if _, err := identitytoken.LoadRS256PublicKeys(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing JWKS accepted")
	}
}

func TestParseRS256PublicKeysRejectsTrailingJSON(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "key-1", Algorithm: string(jose.RS256), Use: "sig"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = identitytoken.ParseRS256PublicKeys(append(raw, []byte(" {}")...)); err == nil {
		t.Fatal("trailing JWKS JSON accepted")
	}
}
