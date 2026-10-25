package localcli

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func vectorFixture(t testing.TB, count int) (*state, map[string]contracts.Vector) {
	t.Helper()
	s := &state{root: t.TempDir(), m: manifest{Installation: "vector-fixture", Store: "store-fixture",
		Embedding: &contracts.EmbeddingSpec{ProviderModel: "fixture", ModelRevision: strings.Repeat("a", 64),
			Dimension: 768, Distance: "cosine", Normalization: "l2", PreprocessVersion: "utf8-exact/1"}}}
	vectors := make(map[string]contracts.Vector, count)
	for n := 0; n < count; n++ {
		v, norm := make(contracts.Vector, 768), float64(0)
		for j := range v {
			v[j] = float32(math.Sin(float64(n*768 + j)))
			norm += float64(v[j]) * float64(v[j])
		}
		for j := range v {
			v[j] = float32(float64(v[j]) / math.Sqrt(norm))
		}
		vectors[fmt.Sprintf("%064x", n)] = v
	}
	if err := writeAtomic(s.root, "vectors.json", encode(vectorCache{Namespace: s.cacheNamespace(), Vectors: vectors})); err != nil {
		t.Fatal(err)
	}
	return s, vectors
}

func TestVectorCacheHotReadBudget(t *testing.T) {
	s, _ := vectorFixture(t, 500)
	if len(s.readVectors()) != 500 {
		t.Fatal("fixture missing")
	}
	allocs := testing.AllocsPerRun(5, func() {
		if len(s.readVectors()) != 500 {
			t.Fatal("hot cache missing")
		}
	})
	t.Logf("hot 500x768 vector-cache allocations: %.0f", allocs)
	if allocs > 1200 {
		t.Fatalf("hot cache decodes/rebuilds unchanged disk data: %.0f allocations", allocs)
	}
}

func TestVectorCacheReadOwnershipAndInvalidation(t *testing.T) {
	s, _ := vectorFixture(t, 2)
	one := s.readVectors()
	key := fmt.Sprintf("%064x", 0)
	want := one[key][0]
	one[key][0] = 999
	delete(one, fmt.Sprintf("%064x", 1))
	if two := s.readVectors(); len(two) != 2 || two[key][0] != want {
		t.Fatal("caller mutated cached map/vector")
	}
	// Atomic replacement can keep the same size and timestamp. File identity
	// must invalidate an in-memory snapshot, including corrupt replacements.
	path := filepath.Join(s.root, "vectors.json")
	prior, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeAtomic(s.root, "vectors.json", []byte(strings.Repeat("x", int(prior.Size())))); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(path, prior.ModTime(), prior.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got := s.readVectors(); len(got) != 0 {
		t.Fatal("atomic cache replacement ignored")
	}
	s, _ = vectorFixture(t, 2)
	s.readVectors()
	s.m.Embedding.ModelRevision = strings.Repeat("b", 64)
	if got := s.readVectors(); len(got) != 0 {
		t.Fatal("model revision namespace reused")
	}
}

func TestVectorCacheUnchangedWrite(t *testing.T) {
	s, vectors := vectorFixture(t, 2)
	path := filepath.Join(s.root, "vectors.json")
	stamp := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	s.readVectors()
	if err := s.writeVectors(vectors); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(stamp) {
		t.Fatal("unchanged vectors were serialized and rewritten", err)
	}
}

func TestVectorCacheConcurrentOwnership(t *testing.T) {
	s, vectors := vectorFixture(t, 2)
	base := append(contracts.Vector(nil), vectors[fmt.Sprintf("%064x", 0)]...)
	if err := s.writeVectors(map[string]contracts.Vector{"base": base}); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	errors := make(chan string, 8)
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(n int) {
			defer workers.Done()
			owned := map[string]contracts.Vector{fmt.Sprintf("writer-%d", n): append(contracts.Vector(nil), base...)}
			for i := 0; i < 8; i++ {
				if err := s.writeVectors(owned); err != nil {
					errors <- "concurrent optional cache write failed"
					return
				}
				got := s.readVectors()
				if len(got) != 1 {
					errors <- "concurrent cache lost its committed snapshot"
					return
				}
				for key, vector := range got {
					if len(vector) != len(base) || vector[0] != base[0] {
						errors <- "caller mutation escaped into cached vectors"
						return
					}
					vector[0] = 999
					delete(got, key)
				}
			}
		}(worker)
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestVectorCacheBounds(t *testing.T) {
	s, _ := vectorFixture(t, 2)
	for _, bad := range []map[string]contracts.Vector{
		{"too-wide": make(contracts.Vector, 4097)}, {"non-finite": {float32(math.NaN())}},
	} {
		if s.writeVectors(bad) == nil {
			t.Fatal("unbounded or non-finite cache accepted")
		}
	}
	tooMany := make(map[string]contracts.Vector, 513)
	for i := 0; i < 513; i++ {
		tooMany[fmt.Sprint(i)] = contracts.Vector{1}
	}
	if s.writeVectors(tooMany) == nil || len(s.readVectors()) != 2 {
		t.Fatal("cache bound failure changed the existing cache")
	}
}

func BenchmarkVectorCacheDiskCycle(b *testing.B) {
	s, vectors := vectorFixture(b, 500)
	b.ReportAllocs()
	b.SetBytes(int64(len(encode(vectorCache{Namespace: s.cacheNamespace(), Vectors: vectors}))))
	for b.Loop() {
		got := s.readVectors()
		if len(got) != 500 {
			b.Fatal("cache missing")
		}
		if err := s.writeVectors(got); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVectorCacheRead(b *testing.B) {
	s, _ := vectorFixture(b, 500)
	s.readVectors()
	b.ReportAllocs()
	for b.Loop() {
		if len(s.readVectors()) != 500 {
			b.Fatal("cache missing")
		}
	}
}
