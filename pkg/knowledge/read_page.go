package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrReadCursor = errors.New("read cursor invalid or snapshot changed")
var ErrReadPagination = errors.New("read pagination unavailable")

type ReadPagination struct {
	Codec    contracts.ReadCursorCodec
	PageSize int
	TTL      time.Duration
}
type ReadPageOptions struct {
	BlockIDs []contracts.ID
	Cursor   string
}
type ReadPageResult struct {
	Record     contracts.FrozenContentRecord
	BlockIDs   []contracts.ID
	NextCursor string
}

// ReadPage repeats the full current/source verification on each request. Cursor
// commitments are compared only with freshly authenticated authoritative facts.
// Selection must be resupplied unchanged on later pages; nil differs from [].
func (s *CurrentReadService) ReadPage(ctx context.Context, in CurrentReadInput, options ReadPageOptions) (ReadPageResult, error) {
	var empty ReadPageResult
	if s == nil || ctx == nil || ctx.Err() != nil || len(options.BlockIDs) > 1000 || len(options.Cursor) > 4096 {
		return empty, ErrRead
	}
	ctx, cancel := context.WithDeadline(ctx, in.Request.Deadline)
	defer cancel()
	ctx, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	ids := options.BlockIDs
	if ids != nil {
		ids = append([]contracts.ID{}, ids...)
	}
	seen := map[contracts.ID]bool{}
	for _, value := range ids {
		if !id(value) || seen[value] {
			return empty, ErrRead
		}
		seen[value] = true
	}
	sorted := ids
	if ids != nil {
		sorted = append([]contracts.ID{}, ids...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	}
	raw, _ := json.Marshal(sorted)
	hash := sha256.Sum256(append([]byte("akos.read-selection/1\n"), raw...))
	selection := contracts.Digest(hex.EncodeToString(hash[:]))
	var cursor contracts.ReadCursorState
	if options.Cursor != "" {
		if s.deps.Pagination == nil {
			return empty, ErrReadPagination
		}
		var err error
		cursor, err = s.deps.Pagination.Codec.OpenReadCursor(ctx, options.Cursor)
		if err != nil || cursor.Selection != selection || cursor.PageSize != s.deps.Pagination.PageSize || cursor.Position < 1 || cursor.Position > 1000 || !cursor.ExpiresAt.After(time.Now()) || cursor.ExpiresAt.After(time.Now().Add(s.deps.Pagination.TTL)) {
			return empty, ErrReadCursor
		}
	}
	var result ReadPageResult
	var tokenExpiry time.Time
	var err error
	snapshot, err := s.readSnapshot(ctx, in, func(snapshot currentReadSnapshot) error {
		if options.Cursor != "" && (cursor.Binding != snapshot.Binding || !cursor.ExpiresAt.After(time.Now())) {
			return ErrReadCursor
		}
		var content struct {
			Blocks []struct {
				ID contracts.ID `json:"id"`
			} `json:"blocks"`
		}
		if json.Unmarshal(snapshot.Record.Payload, &content) != nil || len(content.Blocks) > 1000 {
			return ErrRead
		}
		selected := make([]contracts.ID, 0, len(content.Blocks))
		found := map[contracts.ID]bool{}
		for _, block := range content.Blocks {
			if !id(block.ID) || found[block.ID] {
				return ErrRead
			}
			found[block.ID] = true
			if ids == nil || seen[block.ID] {
				selected = append(selected, block.ID)
			}
		}
		for _, value := range ids {
			if !found[value] {
				return ErrRead
			}
		}
		start, end := 0, len(selected)
		if s.deps.Pagination != nil {
			if options.Cursor != "" {
				start = cursor.Position
			}
			if start > len(selected) || options.Cursor != "" && start == len(selected) {
				return ErrReadCursor
			}
			if start+s.deps.Pagination.PageSize < end {
				end = start + s.deps.Pagination.PageSize
			}
		}
		result = ReadPageResult{Record: snapshot.Record, BlockIDs: append([]contracts.ID{}, selected[start:end]...)}
		if end < len(selected) {
			expires := cursor.ExpiresAt
			if options.Cursor == "" {
				expires = time.Now().Add(s.deps.Pagination.TTL)
			}
			if snapshot.ExpiresAt.Before(expires) {
				expires = snapshot.ExpiresAt
			}
			tokenExpiry = expires
			result.NextCursor, err = s.deps.Pagination.Codec.SealReadCursor(ctx, contracts.ReadCursorState{Binding: snapshot.Binding, Selection: selection, Position: end, PageSize: s.deps.Pagination.PageSize, ExpiresAt: expires})
			if err != nil || result.NextCursor == "" || len(result.NextCursor) > 4096 || ctx.Err() != nil {
				return ErrRead
			}
		}
		if ctx.Err() != nil || !snapshot.ExpiresAt.After(time.Now()) {
			return ErrRead
		}
		return nil
	})
	if err != nil {
		return empty, err
	}
	if ctx.Err() != nil || !snapshot.ExpiresAt.After(time.Now()) || !tokenExpiry.IsZero() && tokenExpiry.After(snapshot.ExpiresAt) {
		return empty, ErrRead
	}
	result.Record = snapshot.Record
	return result, nil
}
