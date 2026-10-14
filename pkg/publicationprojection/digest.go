// Package publicationprojection contains draft publication projection helpers.
package publicationprojection

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

// DigestUnchecked computes the fixed-domain digest of canonical object bytes.
// The caller MUST validate the projection schema, relationships and trusted
// provenance separately. A digest alone is not a valid projection or grant.
func DigestUnchecked(raw []byte) (contracts.Digest, error) {
	encoded, err := canonical.JSON(raw)
	if err != nil || len(encoded) == 0 || encoded[0] != '{' {
		return "", errors.New("invalid publication projection digest input")
	}
	h := sha256.New()
	h.Write([]byte("akos.publication-projection/1\x00"))
	h.Write(encoded)
	return contracts.Digest(hex.EncodeToString(h.Sum(nil))), nil
}
