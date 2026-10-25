package localcli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/localruntime"
	"github.com/MemoryGet/memorys/pkg/modelclient"
	"github.com/MemoryGet/memorys/pkg/modulewire"
	"github.com/MemoryGet/memorys/pkg/semantic"
)

type vectorCache struct {
	Namespace string
	Vectors   map[string]contracts.Vector
}

type aiProviders struct {
	embedding contracts.Embedder
	health    func(context.Context) error
	generator func() (contracts.Generator, error)
	model     string
}

func (s *state) cacheNamespace() string {
	return digest(encode([]any{"memorys.semantic-cache/1", s.m.Installation, s.m.Store, s.m.Embedding}))
}
func (s *state) readVectors() map[string]contracts.Vector {
	namespace := s.cacheNamespace()
	s.vectors.mu.Lock()
	snapshot := s.vectorSnapshot(namespace)
	s.vectors.mu.Unlock()
	// Published snapshots are immutable. Copy outside the lock so concurrent
	// readers own their maps/vectors without blocking each other's ranking.
	return cloneVectors(snapshot)
}

func (s *state) writeVectors(vectors map[string]contracts.Vector) error {
	if !boundedVectors(vectors) {
		return errState
	}
	namespace := s.cacheNamespace()
	s.vectors.mu.Lock()
	defer s.vectors.mu.Unlock()
	info, err := os.Lstat(filepath.Join(s.root, "vectors.json"))
	if err == nil && s.vectors.namespace == namespace && sameVectorFile(info, s.vectors.info) && equalVectors(s.vectors.values, vectors) {
		return nil
	}
	owned := cloneVectors(vectors)
	raw, err := json.Marshal(vectorCache{Namespace: namespace, Vectors: owned})
	if err != nil || len(raw) > 32<<20 {
		return errState
	}
	if err := writeAtomic(s.root, "vectors.json", raw); err != nil {
		return err
	}
	// Another CLI process may replace the file immediately after our commit.
	// The next reader must bind decoded bytes to its own opened file identity;
	// never associate our input vectors with a later path-based Stat result.
	s.vectors.namespace, s.vectors.info, s.vectors.values = namespace, nil, nil
	return nil
}

func (s *state) ai(ctx context.Context, db *sql.DB, a *authority, question string, answer bool) (map[string]any, error) {
	if s.m.Features != 1 || s.m.Embedding == nil {
		return nil, errors.New("run enable-ai before semantic-search or ask")
	}
	embedding, err := modelclient.NewOllama(ollamaURL(), *s.m.Embedding)
	if err != nil {
		return nil, err
	}
	model := os.Getenv("MEMORYS_CHAT_MODEL")
	if model == "" {
		model = "deepseek-flash"
	}
	return s.aiWithProviders(ctx, db, a, question, answer, aiProviders{embedding: embedding, health: embedding.Health, model: model, generator: func() (contracts.Generator, error) {
		return modelclient.NewDeepSeek(os.Getenv("DEEPSEEK_API_KEY"), model, 1)
	}})
}

func (s *state) aiWithProviders(ctx context.Context, db *sql.DB, a *authority, question string, answer bool, p aiProviders) (map[string]any, error) {
	if s.m.Features != 1 || s.m.Embedding == nil || p.embedding == nil || p.health == nil {
		return nil, errState
	}
	var err error
	// Answer permission is independent of search and is checked before any work.
	if answer {
		if _, err = a.scope(ctx, "knowledge.answer", ""); err != nil {
			return nil, errAuthority
		}
	}
	search, err := s.searchService(db)
	if err != nil {
		return nil, err
	}
	deadline, _ := ctx.Deadline()
	corpus, err := search.OpenCorpus(ctx, knowledge.SearchInput{Credential: a.credential, RequestID: contracts.ID(nonce()), Deadline: deadline, SpaceID: s.m.Space, KnowledgeBaseIDs: []contracts.ID{s.m.KB}, Text: question, Limit: 10})
	if err != nil {
		return nil, err
	}
	defer corpus.Close()
	if p.health(ctx) != nil {
		return nil, errors.New("pinned local embedding model unavailable or changed")
	}
	items, vectors, err := semantic.Rank(ctx, corpus.Request, question, corpus.Candidates, *s.m.Embedding, p.embedding, s.readVectors(), 10)
	if err != nil {
		return nil, err
	}
	if corpus.Validate(ctx, items) != nil {
		return nil, errors.New("identity, publication or source changed during semantic search")
	}
	_ = s.writeVectors(vectors)
	result := map[string]any{"retrieval_mode": "semantic_scan", "embedding": s.m.Embedding, "corpus_profile_digest": s.m.ProfileDigest, "evidence": items}
	if !answer {
		return result, nil
	}
	selected := make([]contracts.Candidate, 0, 4)
	bytes := 0
	for _, item := range items {
		if len(selected) == 4 {
			break
		}
		if item.Score < 0.25 {
			continue
		}
		if bytes+len(item.Text) > 24<<10 {
			continue
		}
		bytes += len(item.Text)
		selected = append(selected, item)
	}
	if len(selected) == 0 {
		if _, err = a.scope(ctx, "knowledge.answer", ""); err != nil || corpus.Validate(ctx, nil) != nil {
			return nil, errAuthority
		}
		result["status"] = "insufficient_evidence"
		result["answer"] = "现有已发布文档不足以回答这个问题。"
		result["evidence"] = []contracts.Evidence{}
		return result, nil
	}
	scope, err := a.scope(ctx, "knowledge.answer", "")
	if err != nil || scope.PolicyVersion != corpus.Scope.PolicyVersion {
		return nil, errAuthority
	}
	evidence := make([]contracts.Evidence, 0, len(selected))
	for i, item := range selected {
		evidence = append(evidence, contracts.Evidence{Citation: item.Citation, SourceExcerpt: item.Text, Rank: i + 1})
	}
	model := p.model
	if p.generator == nil {
		return nil, modelclient.ErrModel
	}
	generator, err := p.generator()
	if err != nil {
		return nil, errors.New("DeepSeek configuration missing; set DEEPSEEK_API_KEY and a supported MEMORYS_CHAT_MODEL")
	}
	// This explicit local AI invocation has its own configuration digest. The
	// existing keyword publication pipeline is preserved as corpus_profile_digest.
	aiDigest := contracts.Digest(digest(encode([]any{"memorys.local-answer/1", s.m.ProfileDigest, s.m.Embedding, model, 1024, 4, "cosine>=0.25"})))
	end := time.Now().Add(30 * time.Second)
	for _, limit := range []time.Time{deadline, scope.ExpiresAt} {
		if limit.Before(end) {
			end = limit
		}
	}
	if corpus.Validate(ctx, selected) != nil {
		return nil, errAuthority
	}
	var generated *contracts.AnswerResult
	whitelist := make([]contracts.Citation, 0, len(evidence))
	for _, e := range evidence {
		whitelist = append(whitelist, e.Citation)
	}
	sequence, err := modulewire.NewAnswerSequence(modulewire.GenerationExpectation{RequestID: corpus.Request.RequestID, ProfileVersion: 1, ProfileDigest: aiDigest, Citations: whitelist}, modulewire.AnswerLimits{MaxEvents: 16, MaxTextBytes: 64 << 10})
	if err != nil {
		return nil, err
	}
	err = generator.Generate(ctx, contracts.GenerateRequest{Request: contracts.RequestContext{RequestID: corpus.Request.RequestID, ProfileDigest: aiDigest, Deadline: end}, Scope: scope, Question: question, Evidence: evidence, Budget: contracts.ExecutionBudget{Deadline: end, MaxOutputTokens: 1024}}, func(event contracts.AnswerEvent) error {
		if err := sequence.Accept(event); err != nil {
			return err
		}
		if event.Kind == "done" {
			var owned contracts.AnswerResult
			if json.Unmarshal(encode(event.Result), &owned) != nil {
				return modelclient.ErrModel
			}
			generated = &owned
		}
		return nil
	})
	if err != nil || generated == nil || sequence.Finish() != nil {
		return nil, modelclient.ErrModel
	}
	if corpus.Validate(ctx, selected) != nil {
		return nil, errors.New("answer withheld: current authority, publication or original changed during generation")
	}
	if fresh, err := a.scope(ctx, "knowledge.answer", ""); err != nil || fresh.PolicyVersion != scope.PolicyVersion {
		return nil, errAuthority
	}
	result["answer"] = generated.Answer
	result["status"] = generated.Status
	result["citations"] = generated.Citations
	result["evidence"] = evidence
	result["answer_profile_digest"] = aiDigest
	result["answer_provider"] = "DeepSeek"
	result["answer_model"] = model
	return result, nil
}

func (s *state) searchService(db *sql.DB) (*localruntime.Search, error) {
	search, err := localruntime.NewSearchFromFiles(db, s.m.Issuer, s.m.Audience, "user", filepath.Join(s.root, "jwks.json"), filepath.Join(s.root, "profile.json"), 15*time.Minute)
	if err != nil {
		return nil, err
	}
	if s.m.Features == 1 {
		search.WithOriginals(s.originalFactory(db))
	}
	return search, nil
}
