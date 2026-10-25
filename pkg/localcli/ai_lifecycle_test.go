package localcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

type testEmbedder func(context.Context, contracts.EmbedRequest) (contracts.EmbedResult, error)

func (f testEmbedder) Embed(c context.Context, q contracts.EmbedRequest) (contracts.EmbedResult, error) {
	return f(c, q)
}

type testGenerator func(context.Context, contracts.GenerateRequest, contracts.AnswerSink) error

func (f testGenerator) Generate(c context.Context, q contracts.GenerateRequest, s contracts.AnswerSink) error {
	return f(c, q, s)
}

// Controlled providers exercise native authorization/data boundaries. They are
// not semantic-quality or real-model evidence; the --ai CLI runner supplies that.
func TestPostgresAIGuards(t *testing.T) {
	u := os.Getenv("MEMORYS_LOCAL_AI_TEST_DATABASE_URL")
	if u == "" {
		t.Skip("isolated AI fixture required")
	}
	t.Setenv("MEMORYS_DATABASE_URL", u)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	db, err := openDB(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := createState(filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	if s.initialize(ctx, db) != nil {
		t.Fatal("initialize failed")
	}
	legacy := s.m
	spec := contracts.EmbeddingSpec{ProviderModel: "ollama/guard-fixture:latest", ModelRevision: strings.Repeat("a", 64), Dimension: 16, Distance: "cosine", Normalization: "l2", PreprocessVersion: "utf8-exact/1"}
	if err = s.upgradeAI(ctx, db, spec); err != nil {
		t.Fatal("upgrade", err)
	}
	// Simulate a crash after the policy transaction but before manifest ready.
	s.m = legacy
	if writeAtomic(s.root, "manifest.json", encode(s.m)) != nil {
		t.Fatal("pending manifest")
	}
	if err = s.upgradeAI(ctx, db, spec); err != nil {
		t.Fatal("upgrade reconciliation", err)
	}
	if err = s.upgradeAI(ctx, db, spec); err != nil {
		t.Fatal("upgrade replay", err)
	}
	fresh := func() *authority {
		t.Helper()
		a, err := s.identity(ctx, db, false)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(a.close)
		return a
	}
	owner := fresh()
	calls := 0
	providers := func() aiProviders {
		return aiProviders{model: "controlled-fixture", health: func(context.Context) error { return nil }, embedding: testEmbedder(func(_ context.Context, q contracts.EmbedRequest) (contracts.EmbedResult, error) {
			vectors := make([]contracts.Vector, len(q.Texts))
			for i := range vectors {
				vectors[i] = make(contracts.Vector, 16)
				vectors[i][0] = 1
			}
			return contracts.EmbedResult{Spec: q.Spec, Vectors: vectors}, nil
		}), generator: func() (contracts.Generator, error) { calls++; return nil, errors.New("unexpected generation") }}
	}
	if result, err := s.aiWithProviders(ctx, db, owner, "口令是什么？", true, providers()); err != nil || result["status"] != "insufficient_evidence" || calls != 0 {
		t.Fatal("empty corpus called generator", err)
	}
	r, err := s.importText(ctx, db, owner, "guardsemanticprobe 项目口令是蓝鲸。")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.publish(ctx, db, owner, r.DraftID, r.ContentHash, nil); err != nil {
		t.Fatal(err)
	}
	service, err := s.searchService(db)
	if err != nil {
		t.Fatal(err)
	}
	deadline, _ := ctx.Deadline()
	input := knowledge.SearchInput{Credential: owner.credential, RequestID: contracts.ID(nonce()), Deadline: deadline, SpaceID: s.m.Space, KnowledgeBaseIDs: []contracts.ID{s.m.KB}, Text: "口令", Limit: 10}
	corpus, err := service.OpenCorpus(ctx, input)
	if err != nil {
		t.Fatal("corpus", err)
	}
	if len(corpus.Candidates) != 1 {
		t.Fatal("published corpus missing")
	}
	corpus.Candidates[0].Text = "fabricated source"
	if corpus.Validate(ctx, corpus.Candidates) == nil {
		t.Fatal("mutated corpus accepted")
	}
	corpus.Close()
	if _, err = s.aiWithProviders(ctx, db, owner, "口令", false, providers()); err != nil {
		t.Fatal("controlled native semantic data flow", err)
	}
	revoke := func() {
		t.Helper()
		if _, err := db.ExecContext(ctx, "UPDATE akos_withdraw_v1.identity_tokens SET revoked=true WHERE issuer=$1 AND subject_id=$2", s.m.Issuer, s.m.Owner); err != nil {
			t.Fatal(err)
		}
	}
	p := providers()
	base := p.embedding
	p.embedding = testEmbedder(func(c context.Context, q contracts.EmbedRequest) (contracts.EmbedResult, error) {
		revoke()
		return base.Embed(c, q)
	})
	if result, err := s.aiWithProviders(ctx, db, owner, "口令", true, p); err == nil || result != nil || calls != 0 {
		t.Fatal("revoked during embedding sent data to generator")
	}
	owner = fresh()
	p = providers()
	p.generator = func() (contracts.Generator, error) {
		return testGenerator(func(_ context.Context, q contracts.GenerateRequest, sink contracts.AnswerSink) error {
			revoke()
			if err := sink(contracts.AnswerEvent{Kind: "meta"}); err != nil {
				return err
			}
			return sink(contracts.AnswerEvent{Kind: "done", Result: &contracts.AnswerResult{RequestID: q.Request.RequestID, ProfileVersion: 1, ProfileDigest: q.Request.ProfileDigest, Status: "answered", Answer: "蓝鲸 [1]", Citations: []contracts.Citation{q.Evidence[0].Citation}, TimingsMS: map[string]float64{}, Degraded: []contracts.Degradation{}}})
		}), nil
	}
	if result, err := s.aiWithProviders(ctx, db, owner, "口令", true, p); err == nil || result != nil {
		t.Fatal("revoked during generation released answer")
	}
	owner = fresh()
	p = providers()
	p.generator = func() (contracts.Generator, error) {
		return testGenerator(func(_ context.Context, q contracts.GenerateRequest, sink contracts.AnswerSink) error {
			if err := sink(contracts.AnswerEvent{Kind: "meta"}); err != nil {
				return err
			}
			bad := q.Evidence[0].Citation
			bad.DocumentID = "invented-document"
			return sink(contracts.AnswerEvent{Kind: "done", Result: &contracts.AnswerResult{RequestID: q.Request.RequestID, ProfileVersion: 1, ProfileDigest: q.Request.ProfileDigest, Status: "answered", Answer: "伪造 [1]", Citations: []contracts.Citation{bad}, TimingsMS: map[string]float64{}, Degraded: []contracts.Degradation{}}})
		}), nil
	}
	if result, err := s.aiWithProviders(ctx, db, owner, "口令", true, p); err == nil || result != nil {
		t.Fatal("invented model citation released")
	}
	// Optional real office source supplied by the isolated --ai harness.
	if fixtures := os.Getenv("MEMORYS_AI_FIXTURE_ROOT"); fixtures != "" {
		pdf, err := s.importFile(ctx, db, owner, filepath.Join(fixtures, "sample.pdf"))
		if err != nil {
			t.Fatal("PDF import", err)
		}
		if _, err = s.publish(ctx, db, owner, pdf.DraftID, pdf.ContentHash, nil); err != nil {
			t.Fatal("PDF publication", err)
		}
		input.Credential = owner.credential
		input.RequestID = contracts.ID(nonce())
		input.Text = "localpdfprobe"
		items, err := service.Search(ctx, input)
		if err != nil || len(items) != 1 || items[0].Citation.Source.Kind != "original" {
			t.Fatal("real original evidence", err)
		}
		// Revoke only original.read, retaining search. The running service must
		// not infer the independent source grant from keyword search authority.
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for _, query := range []string{`INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES($1,3,false)`, `INSERT INTO akos_withdraw_v1.access_policy_grants SELECT space_id,3,issuer,subject_id,client_id,kb_id,action FROM akos_withdraw_v1.access_policy_grants WHERE space_id=$1 AND version=2 AND action<>'original.read'`, `UPDATE akos_withdraw_v1.access_policy_snapshots SET sealed=true WHERE space_id=$1 AND version=3`, `UPDATE akos_withdraw_v1.access_policy_heads SET version=3 WHERE space_id=$1`} {
			if _, err = tx.ExecContext(ctx, query, s.m.Space); err != nil {
				t.Fatal(err)
			}
		}
		if tx.Commit() != nil {
			t.Fatal("source grant revocation")
		}
		if _, err = owner.scope(ctx, "knowledge.search", ""); err != nil {
			t.Fatal("search grant unexpectedly revoked")
		}
		if items, err := service.Search(ctx, input); err == nil || len(items) != 0 {
			t.Fatal("original read without independent grant")
		}
	}
}
