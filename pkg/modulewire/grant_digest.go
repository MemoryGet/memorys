package modulewire

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

// GrantBindingDigest hashes exact received JSON under a fixed domain. It does
// not validate the request schema, token, service identity or current access.
// Callers must validate the wire operation before using the resulting binding.
func GrantBindingDigest(raw []byte, kind string) (contracts.Digest, error) {
	if kind != "request" && kind != "scope" {
		return "", errors.New("invalid grant digest domain")
	}
	encoded, err := canonical.JSON(raw)
	if err != nil || len(encoded) == 0 || encoded[0] != '{' {
		return "", errors.New("invalid grant binding JSON")
	}
	hash := sha256.New()
	hash.Write([]byte("akos.grant-" + kind + "/1\x00"))
	hash.Write(encoded)
	return contracts.Digest(hex.EncodeToString(hash.Sum(nil))), nil
}
