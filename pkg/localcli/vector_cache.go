package localcli

import (
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// Optional numeric memo only: it never stores credentials, publication facts,
// text, citations, scopes or grants. The normal corpus/source and model-pin
// validations run independently on every invocation.
type vectorMemo struct {
	mu        sync.Mutex
	namespace string
	info      os.FileInfo
	values    map[string]contracts.Vector
}

func sameVectorFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && a.Mode().IsRegular() && b.Mode().IsRegular() &&
		a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && os.SameFile(a, b)
}

func cloneVectors(vectors map[string]contracts.Vector) map[string]contracts.Vector {
	if vectors == nil {
		return nil
	}
	owned := make(map[string]contracts.Vector, len(vectors))
	for key, vector := range vectors {
		owned[key] = append(contracts.Vector(nil), vector...)
	}
	return owned
}

func boundedVectors(vectors map[string]contracts.Vector) bool {
	if len(vectors) > 512 {
		return false
	}
	for key, vector := range vectors {
		if len(key) > 256 || len(vector) > 4096 {
			return false
		}
		for _, value := range vector {
			if math.IsInf(float64(value), 0) || math.IsNaN(float64(value)) {
				return false
			}
		}
	}
	return true
}

func equalVectors(a, b map[string]contracts.Vector) bool {
	if len(a) != len(b) {
		return false
	}
	for key, vector := range a {
		other, ok := b[key]
		if !ok || len(vector) != len(other) {
			return false
		}
		for i, value := range vector {
			if math.Float32bits(value) != math.Float32bits(other[i]) {
				return false
			}
		}
	}
	return true
}

// Caller holds vectors.mu. File identity, size, mtime and the full embedding
// namespace invalidate the snapshot. A corrupt/missing optional cache is a
// miss; semantic.Rank still validates dimensions and unit norms before use.
func (s *state) vectorSnapshot(namespace string) map[string]contracts.Vector {
	path := filepath.Join(s.root, "vectors.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		s.vectors.namespace, s.vectors.info, s.vectors.values = namespace, nil, nil
		return nil
	}
	if s.vectors.namespace == namespace && sameVectorFile(info, s.vectors.info) {
		return s.vectors.values
	}
	s.vectors.namespace, s.vectors.info, s.vectors.values = namespace, nil, nil
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(f, 32<<20+1))
	if err != nil || len(raw) > 32<<20 {
		return nil
	}
	finished, err := f.Stat()
	if err != nil || !sameVectorFile(info, finished) {
		return nil
	}
	var cache vectorCache
	if json.Unmarshal(raw, &cache) != nil || cache.Namespace != namespace || !boundedVectors(cache.Vectors) {
		return nil
	}
	s.vectors.info, s.vectors.values = info, cache.Vectors
	return cache.Vectors
}
