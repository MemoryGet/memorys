package localcli

import (
	"context"
	"encoding/json"
	"os"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/modelclient"
	"github.com/MemoryGet/memorys/pkg/semantic"
)

// Explicit owned PG fixture only. The cold pass builds real pinned vectors;
// the following passes expose the same stage order used by aiWithProviders.
func TestPostgresSemanticPerformanceProbe(t *testing.T) {
	db, root := performanceDB(t, &performanceTrace{})
	s, err := loadState(root, true)
	if err != nil || s.m.Embedding == nil {
		t.Fatal("pinned AI fixture required", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	a, err := s.identity(ctx, db, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	embedder, err := modelclient.NewOllama(ollamaURL(), *s.m.Embedding)
	if err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 4; iteration++ {
		if iteration == 1 && os.Getenv("MEMORYS_LOCAL_PERF_SEMANTIC_PROFILE") != "" {
			f, err := os.Create(os.Getenv("MEMORYS_LOCAL_PERF_SEMANTIC_PROFILE"))
			if err != nil {
				t.Fatal(err)
			}
			if err = pprof.StartCPUProfile(f); err != nil {
				f.Close()
				t.Fatal(err)
			}
			defer func() { pprof.StopCPUProfile(); f.Close() }()
		}
		timings := map[string]float64{}
		last := time.Now()
		mark := func(stage string) {
			now := time.Now()
			timings[stage] = float64(now.Sub(last)) / float64(time.Millisecond)
			last = now
		}
		search, err := s.searchService(db)
		if err != nil {
			t.Fatal(err)
		}
		deadline, _ := ctx.Deadline()
		question := "星帆项目发生数据库故障时如何恢复？"
		corpus, err := search.OpenCorpus(ctx, knowledge.SearchInput{Credential: a.credential, RequestID: contracts.ID(nonce()),
			Deadline: deadline, SpaceID: s.m.Space, KnowledgeBaseIDs: []contracts.ID{s.m.KB}, Text: question, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		mark("native_open")
		if err := embedder.Health(ctx); err != nil {
			corpus.Close()
			t.Fatal(err)
		}
		mark("model_health")
		cached := s.readVectors()
		mark("cache_read")
		items, vectors, err := semantic.Rank(ctx, corpus.Request, question, corpus.Candidates, *s.m.Embedding, embedder, cached, 10)
		if err != nil || len(items) != 10 || len(vectors) != 500 {
			corpus.Close()
			t.Fatal("real ranking failed", err)
		}
		mark("model_and_rank")
		if corpus.Validate(ctx, items) != nil {
			corpus.Close()
			t.Fatal("fresh source validation")
		}
		mark("native_validate")
		if err := s.writeVectors(vectors); err != nil {
			corpus.Close()
			t.Fatal(err)
		}
		mark("cache_write")
		corpus.Close()
		raw, _ := json.Marshal(map[string]any{"iteration": iteration, "timings_ms": timings})
		t.Log("SEMANTIC_PROBE " + string(raw))
	}
}
