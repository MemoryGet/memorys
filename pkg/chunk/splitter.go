package chunk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"unicode/utf8"
)

var ErrSplit = errors.New("chunk splitting rejected")

type SimpleSplitter struct{}

func (SimpleSplitter) Split(ctx context.Context, req contracts.SplitRequest) ([]contracts.Chunk, error) {
	if ctx == nil || req.RevisionID == "" || req.Policy.MaxCodepoints < 1 || req.Policy.MaxCodepoints > 1000000 || len(req.Blocks) > 10000 || req.Policy.OverlapCodepoints < 0 || req.Policy.OverlapCodepoints >= req.Policy.MaxCodepoints || req.Policy.Strategy != "characters" {
		return nil, ErrSplit
	}
	chunks := make([]contracts.Chunk, 0)
	seen := map[contracts.ID]bool{}
	bytes := 0
	for _, b := range req.Blocks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		bytes += len(b.Content.Text)
		if bytes > 16<<20 || b.ID == "" || b.RevisionID != req.RevisionID || seen[b.ID] || b.Content.Kind != contracts.TextBlock || !utf8.ValidString(b.Content.Text) {
			return nil, ErrSplit
		}
		seen[b.ID] = true
		runes := []rune(b.Content.Text)
		for start := 0; start < len(runes); {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if len(chunks) >= 10000 {
				return nil, ErrSplit
			}
			end := start + req.Policy.MaxCodepoints
			if end > len(runes) {
				end = len(runes)
			}
			text := string(runes[start:end])
			h := sha256.Sum256([]byte(text))
			identity, _ := json.Marshal(struct {
				Revision, Block contracts.ID
				Start, End      int
				Policy          contracts.ChunkPolicy
				Profile         contracts.Digest
			}{req.RevisionID, b.ID, start, end, req.Policy, req.Request.ProfileDigest})
			id := sha256.Sum256(identity)
			chunks = append(chunks, contracts.Chunk{ID: contracts.ID(hex.EncodeToString(id[:])), RevisionID: req.RevisionID, Text: text, ContentHash: contracts.Digest(hex.EncodeToString(h[:])), ProfileDigest: req.Request.ProfileDigest, Spans: []contracts.BlockSpan{{BlockID: b.ID, Characters: &contracts.CharacterRange{Start: start, End: end}}}})
			if end == len(runes) {
				break
			}
			start = end - req.Policy.OverlapCodepoints
		}
	}
	return chunks, nil
}
