package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

const FrozenFormat = "akos.freeze/1"

type FrozenHashes struct {
	Format      string `json:"format"`
	BlocksHash  string `json:"blocks_hash"`
	ContentHash string `json:"content_hash"`
}

func hash(prefix string, value []byte) string {
	digest := sha256.Sum256(append([]byte(prefix), value...))
	return hex.EncodeToString(digest[:])
}

// Frozen computes byte identities after schema and domain validation by the
// caller. It does not validate source ownership, media or approval eligibility.
func Frozen(data []byte) (FrozenHashes, error) {
	canonical, err := JSON(data)
	if err != nil {
		return FrozenHashes{}, err
	}
	var envelope struct {
		Format string          `json:"format"`
		Blocks json.RawMessage `json:"blocks"`
		Blobs  json.RawMessage `json:"blobs"`
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || envelope.Format != FrozenFormat {
		return FrozenHashes{}, errors.New("invalid frozen content envelope")
	}
	var blocks, blobs []json.RawMessage
	if json.Unmarshal(envelope.Blocks, &blocks) != nil || len(blocks) == 0 ||
		json.Unmarshal(envelope.Blobs, &blobs) != nil || blobs == nil {
		return FrozenHashes{}, errors.New("invalid frozen content arrays")
	}
	blocksHash := hash("AKOS-BLOCKS/1\n", envelope.Blocks)
	payload := []byte(`{"format":"akos.freeze/1","blocks_hash":"` + blocksHash + `","blobs":` + string(envelope.Blobs) + `}`)
	payload, err = JSON(payload)
	if err != nil {
		return FrozenHashes{}, err
	}
	return FrozenHashes{Format: FrozenFormat, BlocksHash: blocksHash, ContentHash: hash("AKOS-CONTENT/1\n", payload)}, nil
}
