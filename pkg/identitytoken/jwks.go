package identitytoken

import (
	"bytes"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"math/big"
	"os"

	jose "github.com/go-jose/go-jose/v4"
)

var ErrJWKS = errors.New("JWKS configuration rejected")

// LoadRS256PublicKeys reads a deployment-owned JWKS snapshot. It never fetches
// x5u/jku URLs and returns detached public-key values suitable for Config.Keys.
// Rotation is achieved by replacing the immutable file and rebuilding the
// authenticator; callers must keep the old snapshot alive until in-flight
// requests drain.
func LoadRS256PublicKeys(path string) (map[string]*rsa.PublicKey, error) {
	if path == "" {
		return nil, ErrJWKS
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return nil, ErrJWKS
	}
	var set jose.JSONWebKeySet
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&set) != nil || len(set.Keys) == 0 || len(set.Keys) > 100 {
		return nil, ErrJWKS
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for i := range set.Keys {
		key := set.Keys[i]
		if key.KeyID == "" || key.Algorithm != string(jose.RS256) || key.Use != "" && key.Use != "sig" || !key.IsPublic() {
			return nil, ErrJWKS
		}
		rsaKey, ok := key.Key.(*rsa.PublicKey)
		if !ok || rsaKey == nil || rsaKey.N == nil || rsaKey.N.Sign() <= 0 || rsaKey.E != 65537 || rsaKey.N.BitLen() < 2048 || rsaKey.N.BitLen() > 8192 || keys[key.KeyID] != nil {
			return nil, ErrJWKS
		}
		keys[key.KeyID] = &rsa.PublicKey{N: new(big.Int).Set(rsaKey.N), E: rsaKey.E}
	}
	return keys, nil
}
