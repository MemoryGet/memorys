package indexsnapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

const Format = "akos.index.snapshot/1"

var ErrSnapshot = errors.New("index snapshot invalid")

func id(v contracts.ID) bool {
	return v != "" && utf8.ValidString(string(v)) && utf8.RuneCountInString(string(v)) <= 200
}

// Seal owns, validates and sorts the data before producing its content digest.
func Seal(input contracts.IndexSnapshot) (contracts.IndexSnapshot, error) {
	if len(input.Generations) < 1 || len(input.Generations) > 1000 || len(input.Records) > 10000 || len(input.ChunkFences) > 10000 {
		return contracts.IndexSnapshot{}, ErrSnapshot
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 4<<20 {
		return contracts.IndexSnapshot{}, ErrSnapshot
	}
	var s contracts.IndexSnapshot
	if json.Unmarshal(raw, &s) != nil {
		return s, ErrSnapshot
	}
	if s.Format != Format || !id(s.SpaceID) || !id(s.KnowledgeBaseID) {
		return contracts.IndexSnapshot{}, ErrSnapshot
	}
	owners := map[contracts.ID]contracts.IndexDeletionState{}
	counts := map[contracts.ID]int{}
	keys := map[[2]contracts.ID]bool{}
	for _, g := range s.Generations {
		if !id(g.GenerationID) || !id(g.DocumentID) || !id(g.RevisionID) || !id(g.BackendEpochID) || g.SpaceID != s.SpaceID || g.KnowledgeBaseID != s.KnowledgeBaseID || g.RecordCount < 0 {
			return contracts.IndexSnapshot{}, ErrSnapshot
		}
		if _, ok := owners[g.GenerationID]; ok {
			return contracts.IndexSnapshot{}, ErrSnapshot
		}
		owners[g.GenerationID] = g
	}
	for _, r := range s.Records {
		g, ok := owners[r.GenerationID]
		key := [2]contracts.ID{r.GenerationID, r.Chunk.ID}
		if !ok || g.Deleted || keys[key] || !id(r.Chunk.ID) || r.SpaceID != s.SpaceID || r.KnowledgeBaseID != s.KnowledgeBaseID || r.DocumentID != g.DocumentID || r.Chunk.RevisionID != g.RevisionID || r.BackendEpochID != g.BackendEpochID || r.Citation.DocumentID != r.DocumentID || r.Citation.RevisionID != r.Chunk.RevisionID || r.Citation.BlockID == "" || len(r.Chunk.Spans) == 0 || r.Chunk.Spans[0].BlockID != r.Citation.BlockID || r.Citation.Source.Kind != "original" && r.Citation.Source.Kind != "revision" {
			return contracts.IndexSnapshot{}, ErrSnapshot
		}
		keys[key] = true
		counts[r.GenerationID]++
	}
	fences := map[[2]contracts.ID]bool{}
	for _, f := range s.ChunkFences {
		key := [2]contracts.ID{f.GenerationID, f.ChunkID}
		if _, ok := owners[f.GenerationID]; !ok || !id(f.ChunkID) || fences[key] || keys[key] {
			return contracts.IndexSnapshot{}, ErrSnapshot
		}
		fences[key] = true
	}
	for _, g := range s.Generations {
		if counts[g.GenerationID] != g.RecordCount {
			return contracts.IndexSnapshot{}, ErrSnapshot
		}
	}
	sort.Slice(s.Generations, func(i, j int) bool { return s.Generations[i].GenerationID < s.Generations[j].GenerationID })
	sort.Slice(s.Records, func(i, j int) bool {
		a, b := s.Records[i], s.Records[j]
		if a.GenerationID != b.GenerationID {
			return a.GenerationID < b.GenerationID
		}
		return a.Chunk.ID < b.Chunk.ID
	})
	sort.Slice(s.ChunkFences, func(i, j int) bool {
		a, b := s.ChunkFences[i], s.ChunkFences[j]
		if a.GenerationID != b.GenerationID {
			return a.GenerationID < b.GenerationID
		}
		return a.ChunkID < b.ChunkID
	})
	if s.Records == nil {
		s.Records = []contracts.IndexProjection{}
	}
	if s.ChunkFences == nil {
		s.ChunkFences = []contracts.IndexChunkFence{}
	}
	s.Checksum = ""
	raw, err = json.Marshal(s)
	if err != nil {
		return contracts.IndexSnapshot{}, ErrSnapshot
	}
	raw, err = canonical.JSON(raw)
	if err != nil {
		return contracts.IndexSnapshot{}, ErrSnapshot
	}
	h := sha256.New()
	h.Write([]byte(Format + "\n"))
	h.Write(raw)
	s.Checksum = contracts.Digest(hex.EncodeToString(h.Sum(nil)))
	return s, nil
}
func Verify(s contracts.IndexSnapshot) (contracts.IndexSnapshot, error) {
	owned, err := Seal(s)
	if err != nil || s.Checksum == "" || owned.Checksum != s.Checksum {
		return contracts.IndexSnapshot{}, ErrSnapshot
	}
	return owned, nil
}
