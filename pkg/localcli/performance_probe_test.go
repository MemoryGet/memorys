package localcli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type performanceSQL struct {
	SQL          string
	Calls        int
	Milliseconds float64
}
type performanceTrace struct {
	mu      sync.Mutex
	queries map[string]performanceSQL
}
type performanceTraceKey struct{}
type performanceQuery struct {
	started time.Time
	sql     string
}

func (p *performanceTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, q pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, performanceTraceKey{}, performanceQuery{time.Now(), strings.Join(strings.Fields(q.SQL), " ")})
}
func (p *performanceTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	q, ok := ctx.Value(performanceTraceKey{}).(performanceQuery)
	if !ok {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.queries == nil {
		p.queries = map[string]performanceSQL{}
	}
	r := p.queries[q.sql]
	r.SQL = q.sql
	r.Calls++
	r.Milliseconds += float64(time.Since(q.started)) / float64(time.Millisecond)
	p.queries[q.sql] = r
}
func (p *performanceTrace) take() []performanceSQL {
	p.mu.Lock()
	defer p.mu.Unlock()
	rows := []performanceSQL{}
	for _, r := range p.queries {
		rows = append(rows, r)
	}
	p.queries = nil
	sort.Slice(rows, func(i, j int) bool { return rows[i].Milliseconds > rows[j].Milliseconds })
	return rows
}

// Opt-in only: the diagnostic runner creates this disposable DB and state.
// Never use the ordinary user's MEMORYS_DATABASE_URL as integration consent.
func performanceDB(t *testing.T, trace *performanceTrace) (*sql.DB, string) {
	t.Helper()
	u, root := os.Getenv("MEMORYS_LOCAL_PERF_DATABASE_URL"), os.Getenv("MEMORYS_LOCAL_PERF_STATE")
	if u == "" || root == "" {
		t.Skip("owned performance DB and state required")
	}
	cfg, err := pgx.ParseConfig(u)
	if err != nil {
		t.Fatal("performance DB configuration")
	}
	cfg.Tracer = trace
	name := stdlib.RegisterConnConfig(cfg)
	t.Cleanup(func() { stdlib.UnregisterConnConfig(name) })
	db, err := sql.Open("pgx", name)
	if err != nil {
		t.Fatal("performance DB open")
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	t.Cleanup(func() { db.Close() })
	return db, root
}

func TestPostgresPerformanceSeed(t *testing.T) {
	db, root := performanceDB(t, &performanceTrace{})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	s, err := createState(root)
	if err != nil || s.initialize(ctx, db) != nil {
		t.Fatal("performance initialize", err)
	}
	var spec contracts.EmbeddingSpec
	if json.Unmarshal([]byte(os.Getenv("MEMORYS_LOCAL_PERF_EMBEDDING_SPEC")), &spec) != nil || spec.ModelRevision == "" {
		t.Fatal("explicit pinned embedding spec required for fixture setup")
	}
	if err = s.upgradeAI(ctx, db, spec); err != nil {
		t.Fatal("performance AI policy", err)
	}
	a, err := s.identity(ctx, db, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	for n := 0; n < 500; n++ {
		text := fmt.Sprintf("perfdoc%04d perfshared 恢复方案编号%04d\n星帆项目第%04d号恢复手册。紧急联络暗号是紫色海豚。", n, n, n) +
			"数据库故障时先停止写入，恢复数据库和匹配的本地状态目录，运行诊断后验证检索。\n" + strings.Repeat(" recovery backup verified source", 18)
		r, err := s.importText(ctx, db, a, text)
		if err != nil {
			t.Fatal("performance import", n, err)
		}
		if _, err = s.publish(ctx, db, a, r.DraftID, r.ContentHash, nil); err != nil {
			t.Fatal("performance publish", n, err)
		}
	}
	if _, err = db.ExecContext(ctx, "ANALYZE"); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresPerformanceProbe(t *testing.T) {
	trace := &performanceTrace{}
	db, root := performanceDB(t, trace)
	s, err := loadState(root, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	a, err := s.identity(ctx, db, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	service, err := s.searchService(db)
	if err != nil {
		t.Fatal(err)
	}
	deadline, _ := ctx.Deadline()
	input := knowledge.SearchInput{Credential: a.credential, RequestID: contracts.ID(nonce()), Deadline: deadline,
		SpaceID: s.m.Space, KnowledgeBaseIDs: []contracts.ID{s.m.KB}, Text: "perfdoc0000", Limit: 10}
	if path := os.Getenv("MEMORYS_LOCAL_PERF_CPU_PROFILE"); path != "" {
		f, err := os.Create(filepath.Clean(path))
		if err != nil {
			t.Fatal(err)
		}
		if err = pprof.StartCPUProfile(f); err != nil {
			f.Close()
			t.Fatal(err)
		}
		defer func() { pprof.StopCPUProfile(); f.Close() }()
	}
	measure := func(name string, run func() error) {
		trace.take()
		started := time.Now()
		err := run()
		elapsed := time.Since(started)
		queries := trace.take()
		count, sqlMS := 0, float64(0)
		for _, q := range queries {
			count += q.Calls
			sqlMS += q.Milliseconds
		}
		raw, _ := json.Marshal(map[string]any{"stage": name, "elapsed_ms": float64(elapsed) / float64(time.Millisecond),
			"sql_calls": count, "sql_ms": sqlMS, "queries": queries, "success": err == nil})
		t.Log("PERF_PROBE " + string(raw))
		if err != nil {
			t.Fatal(name, err)
		}
		if elapsed > 2*time.Second {
			t.Errorf("%s exceeds 2-second native-stage performance gate: %v", name, elapsed)
		}
	}
	measure("keyword_application", func() error {
		out, err := service.Search(ctx, input)
		if err == nil && (len(out) != 1 || !strings.Contains(out[0].Text, "perfdoc0000")) {
			t.Fatal("unexpected keyword evidence")
		}
		return err
	})
	var validate func() error
	measure("semantic_native_open", func() error {
		corpus, err := service.OpenCorpus(ctx, input)
		if err != nil {
			return err
		}
		t.Cleanup(corpus.Close)
		if len(corpus.Candidates) != 500 {
			t.Fatal("unexpected corpus size", len(corpus.Candidates))
		}
		validate = func() error { return corpus.Validate(ctx, corpus.Candidates[:10]) }
		return nil
	})
	measure("semantic_native_final_validate", validate)
}
