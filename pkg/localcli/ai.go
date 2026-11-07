package localcli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/fusion"
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
	return s.aiWithLimit(ctx, db, a, question, answer, 10)
}

func (s *state) aiWithLimitSink(ctx context.Context, db *sql.DB, a *authority, question string, answer bool, limit int, sink func(contracts.AnswerEvent) error) (map[string]any, error) {
	return s.aiWithLimitSinkFiltered(ctx, db, a, question, answer, limit, answerFilter{}, sink)
}

func (s *state) aiWithLimitSinkFiltered(ctx context.Context, db *sql.DB, a *authority, question string, answer bool, limit int, filter answerFilter, sink func(contracts.AnswerEvent) error) (map[string]any, error) {
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
	return s.aiWithProvidersLimitSinkFiltered(ctx, db, a, question, answer, limit, aiProviders{embedding: embedding, health: embedding.Health, model: model, generator: func() (contracts.Generator, error) {
		return modelclient.NewDeepSeek(os.Getenv("DEEPSEEK_API_KEY"), model, 1)
	}}, filter, sink)
}

func (s *state) aiWithLimit(ctx context.Context, db *sql.DB, a *authority, question string, answer bool, limit int) (map[string]any, error) {
	return s.aiWithLimitFiltered(ctx, db, a, question, answer, limit, answerFilter{})
}

func (s *state) aiWithLimitFiltered(ctx context.Context, db *sql.DB, a *authority, question string, answer bool, limit int, filter answerFilter) (map[string]any, error) {
	if s.m.Features != 1 || s.m.Embedding == nil {
		return nil, errors.New("run enable-ai before semantic-search or ask")
	}
	if limit < 1 || limit > 100 {
		return nil, errState
	}
	embedding, err := modelclient.NewOllama(ollamaURL(), *s.m.Embedding)
	if err != nil {
		return nil, err
	}
	model := os.Getenv("MEMORYS_CHAT_MODEL")
	if model == "" {
		model = "deepseek-flash"
	}
	return s.aiWithProvidersLimitSinkFiltered(ctx, db, a, question, answer, limit, aiProviders{embedding: embedding, health: embedding.Health, model: model, generator: func() (contracts.Generator, error) {
		return modelclient.NewDeepSeek(os.Getenv("DEEPSEEK_API_KEY"), model, 1)
	}}, filter, nil)
}

func (s *state) aiWithProviders(ctx context.Context, db *sql.DB, a *authority, question string, answer bool, p aiProviders) (map[string]any, error) {
	return s.aiWithProvidersLimit(ctx, db, a, question, answer, 10, p)
}

func (s *state) aiWithProvidersLimit(ctx context.Context, db *sql.DB, a *authority, question string, answer bool, limit int, p aiProviders) (map[string]any, error) {
	return s.aiWithProvidersLimitSink(ctx, db, a, question, answer, limit, p, nil)
}

// aiWithProvidersLimitSink is the shared answer path for JSON and SSE callers.
// The optional sink receives only sequence-validated meta/delta events; the
// final result is returned after the same current-authority and citation checks.
func (s *state) aiWithProvidersLimitSink(ctx context.Context, db *sql.DB, a *authority, question string, answer bool, limit int, p aiProviders, streamSink func(contracts.AnswerEvent) error) (map[string]any, error) {
	return s.aiWithProvidersLimitSinkFiltered(ctx, db, a, question, answer, limit, p, answerFilter{}, streamSink)
}

func (s *state) aiWithProvidersLimitSinkFiltered(ctx context.Context, db *sql.DB, a *authority, question string, answer bool, limit int, p aiProviders, filter answerFilter, streamSink func(contracts.AnswerEvent) error) (map[string]any, error) {
	if s.m.Features != 1 || s.m.Embedding == nil || p.embedding == nil || p.health == nil {
		return nil, errState
	}
	if limit < 1 || limit > 100 {
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
	corpus, err := search.OpenCorpus(ctx, knowledge.SearchInput{Credential: a.credential, RequestID: contracts.ID(nonce()), Deadline: deadline, SpaceID: s.m.Space, KnowledgeBaseIDs: []contracts.ID{s.m.KB}, Text: question, Limit: limit})
	if err != nil {
		return nil, err
	}
	defer corpus.Close()
	if answer && answerFilterHasValues(filter) {
		filtered, filterErr := s.filterAnswerCandidates(ctx, db, corpus.Candidates, filter)
		if filterErr != nil {
			return nil, filterErr
		}
		corpus.Candidates = filtered
		if corpus.Validate(ctx, filtered) != nil {
			return nil, errors.New("筛选后的来源已变化，请重新检索")
		}
	}
	if p.health(ctx) != nil {
		return nil, errors.New("pinned local embedding model unavailable or changed")
	}
	items, vectors, err := semantic.Rank(ctx, corpus.Request, question, corpus.Candidates, *s.m.Embedding, p.embedding, s.readVectors(), limit)
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
	maxEvents := 16
	if streamSink != nil {
		maxEvents = 256
	}
	sequence, err := modulewire.NewAnswerSequence(modulewire.GenerationExpectation{RequestID: corpus.Request.RequestID, ProfileVersion: 1, ProfileDigest: aiDigest, Citations: whitelist}, modulewire.AnswerLimits{MaxEvents: maxEvents, MaxTextBytes: 64 << 10})
	if err != nil {
		return nil, err
	}
	generateRequest := contracts.GenerateRequest{Request: contracts.RequestContext{RequestID: corpus.Request.RequestID, ProfileDigest: aiDigest, Deadline: end}, Scope: scope, Question: question, Evidence: evidence, Budget: contracts.ExecutionBudget{Deadline: end, MaxOutputTokens: 1024}}
	handleEvent := func(event contracts.AnswerEvent) error {
		if err := sequence.Accept(event); err != nil {
			return err
		}
		if streamSink != nil && (event.Kind == "meta" || event.Kind == "answer_delta") {
			if err := streamSink(event); err != nil {
				return err
			}
		}
		if event.Kind == "done" {
			var owned contracts.AnswerResult
			if json.Unmarshal(encode(event.Result), &owned) != nil {
				return modelclient.ErrModel
			}
			generated = &owned
		}
		return nil
	}
	if streamSink != nil {
		if streaming, ok := generator.(interface {
			GenerateStream(context.Context, contracts.GenerateRequest, contracts.AnswerSink) error
		}); ok {
			err = streaming.GenerateStream(ctx, generateRequest, handleEvent)
		} else {
			err = generator.Generate(ctx, generateRequest, handleEvent)
		}
	} else {
		err = generator.Generate(ctx, generateRequest, handleEvent)
	}
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

// hybridCandidates combines the authoritative keyword route with the local
// semantic route. Each route independently rechecks current publication and
// source identity; RRF only merges those already verified candidates.
func (s *state) hybridCandidates(ctx context.Context, db *sql.DB, a *authority, question string) ([]contracts.Candidate, error) {
	return s.hybridCandidatesLimit(ctx, db, a, question, 10)
}

func (s *state) hybridCandidatesLimit(ctx context.Context, db *sql.DB, a *authority, question string, limit int) ([]contracts.Candidate, error) {
	if s.m.Features != 1 {
		return nil, errors.New("run enable-ai before hybrid-search")
	}
	if limit < 1 || limit > 100 {
		return nil, errState
	}
	search, err := s.searchService(db)
	if err != nil {
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	keyword, err := search.Search(ctx, knowledge.SearchInput{Credential: a.credential, RequestID: contracts.ID(nonce()), Deadline: deadline, SpaceID: s.m.Space, KnowledgeBaseIDs: []contracts.ID{s.m.KB}, Text: question, Limit: limit})
	if err != nil {
		return nil, err
	}
	semanticResult, err := s.aiWithLimit(ctx, db, a, question, false, limit)
	if err != nil {
		return nil, err
	}
	semantic, ok := semanticResult["evidence"].([]contracts.Candidate)
	if !ok {
		return nil, errors.New("semantic search returned an invalid candidate set")
	}
	for i := range keyword {
		keyword[i].RouteID = "keyword-route"
		keyword[i].Rank = i + 1
	}
	for i := range semantic {
		semantic[i].RouteID = "semantic-route"
		semantic[i].Rank = i + 1
	}
	request := contracts.FuseRequest{Request: contracts.RequestContext{RequestID: contracts.ID(nonce()), ProfileDigest: s.m.ProfileDigest, Deadline: deadline}, Lists: []contracts.RankedList{{RouteID: "keyword-route", Candidates: keyword}, {RouteID: "semantic-route", Candidates: semantic}}, RankConstant: s.profile.Query.Policy.RankConstant, Limit: limit}
	fused, err := (fusion.RRF{}).Fuse(ctx, request)
	if err != nil {
		return nil, err
	}
	if len(fused) == 0 {
		return fused, nil
	}
	input := contracts.RerankRequest{Request: request.Request, Query: question, Candidates: fused, Limit: limit}
	ranked, err := (localLexicalReranker{}).Rerank(ctx, input)
	if err != nil {
		return nil, err
	}
	selected := make([]contracts.Candidate, 0, len(ranked))
	used := make([]bool, len(fused))
	for _, evidence := range ranked {
		for index, candidate := range fused {
			if used[index] || !reflect.DeepEqual(candidate.Citation, evidence.Citation) || !strings.Contains(candidate.Text, evidence.SourceExcerpt) {
				continue
			}
			used[index] = true
			candidate.Rank = evidence.Rank
			selected = append(selected, candidate)
			break
		}
	}
	return selected, nil
}

func (s *state) searchService(db *sql.DB) (*localruntime.Search, error) {
	search, err := localruntime.NewSearchFromFiles(db, s.m.Issuer, s.m.Audience, "user", filepath.Join(s.root, "jwks.json"), filepath.Join(s.root, "profile.json"), 15*time.Minute)
	if err != nil {
		return nil, err
	}
	if s.m.Features == 1 {
		search.WithOriginals(s.originalFactory(db))
	}
	return search.WithSourceTags(s.m.Tags), nil
}
