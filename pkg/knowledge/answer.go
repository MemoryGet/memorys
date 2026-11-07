package knowledge

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var (
	ErrAnswer                = errors.New("knowledge answer unavailable")
	ErrAnswerUnauthenticated = errors.New("knowledge answer authentication required")
)

// AnswerSearcher is deliberately narrower than SearchService so hosts can
// provide a configured runtime without exposing its planner internals.
type AnswerSearcher interface {
	Search(context.Context, SearchInput) ([]contracts.Candidate, error)
	ProfileIdentity() (uint64, contracts.Digest, error)
}

type AnswerInput struct {
	Credential       contracts.Credential
	RequestID        contracts.ID
	Deadline         time.Time
	SpaceID          contracts.ID
	KnowledgeBaseIDs []contracts.ID
	Question         string
	Reflection       bool
	Limit            int
}

type AnswerDependencies struct {
	Search         AnswerSearcher
	Auth           contracts.Authenticator
	Authorizer     contracts.Authorizer
	KnowledgeBases KnowledgeBaseDirectory
	Generator      contracts.Generator
}

type AnswerService struct{ deps AnswerDependencies }

func NewAnswer(d AnswerDependencies) (*AnswerService, error) {
	if missing(d.Search) || missing(d.Auth) || missing(d.Authorizer) || missing(d.Generator) {
		return nil, ErrAnswer
	}
	return &AnswerService{deps: d}, nil
}

func (s *AnswerService) Ask(ctx context.Context, in AnswerInput, sink contracts.AnswerSink) error {
	if s == nil || ctx == nil || sink == nil || ctx.Err() != nil || in.RequestID == "" || in.SpaceID == "" || in.Deadline.IsZero() || !in.Deadline.After(time.Now()) || strings.TrimSpace(in.Question) == "" || !utf8.ValidString(in.Question) || utf8.RuneCountInString(in.Question) > 10000 || in.Credential.Kind == "" || len(in.Credential.Secret) == 0 || len(in.Credential.Secret) > 16384 || in.Reflection {
		return ErrAnswer
	}
	limit := in.Limit
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 100 {
		return ErrAnswer
	}
	ctx, cancel := context.WithDeadline(ctx, in.Deadline)
	defer cancel()
	version, digest, err := s.deps.Search.ProfileIdentity()
	if err != nil || version == 0 || !validDigest(digest) {
		return ErrAnswer
	}
	credential := contracts.Credential{Kind: in.Credential.Kind, Secret: append([]byte(nil), in.Credential.Secret...)}
	who, authErr := s.deps.Auth.Authenticate(ctx, credential)
	clear(credential.Secret)
	if authErr != nil || ctx.Err() != nil {
		return ErrAnswerUnauthenticated
	}
	ids := append([]contracts.ID(nil), in.KnowledgeBaseIDs...)
	if ids == nil {
		if s.deps.KnowledgeBases == nil {
			return ErrAnswer
		}
		ids, err = s.deps.KnowledgeBases.ListKnowledgeBases(ctx, who, in.SpaceID)
		if err != nil {
			return ErrAnswer
		}
	}
	if len(ids) == 0 {
		return emitInsufficient(sink, in.RequestID, version, digest)
	}
	seen := map[contracts.ID]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return ErrAnswer
		}
		seen[id] = true
	}
	scope, err := s.deps.Authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "knowledge.answer", SpaceID: in.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), ids...)})
	if err != nil || scope.Action != "knowledge.answer" || scope.SpaceID != in.SpaceID || !reflect.DeepEqual(scope.KnowledgeBaseIDs, ids) || scope.SubjectID != who.SubjectID || !scope.ExpiresAt.After(time.Now()) {
		return ErrAnswer
	}
	searchInput := SearchInput{Credential: in.Credential, RequestID: in.RequestID, Deadline: in.Deadline, SpaceID: in.SpaceID, KnowledgeBaseIDs: ids, Text: in.Question, Limit: limit}
	candidates, err := s.deps.Search.Search(ctx, searchInput)
	if err != nil || ctx.Err() != nil {
		return ErrAnswer
	}
	evidence := selectAnswerEvidence(candidates)
	if len(evidence) == 0 {
		return emitInsufficient(sink, in.RequestID, version, digest)
	}
	deadline := in.Deadline
	if scope.ExpiresAt.Before(deadline) {
		deadline = scope.ExpiresAt
	}
	request := contracts.GenerateRequest{Request: contracts.RequestContext{RequestID: in.RequestID, ProfileDigest: digest, Deadline: deadline}, Scope: scope, Question: in.Question, Evidence: evidence, Budget: contracts.ExecutionBudget{Deadline: deadline, MaxOutputTokens: 1024}}
	var generated *contracts.AnswerResult
	var emittedCitations []contracts.Citation
	handle := func(event contracts.AnswerEvent) error {
		switch event.Kind {
		case "meta":
			return sink(event)
		case "answer_delta":
			if event.Text == "" || !utf8.ValidString(event.Text) {
				return ErrAnswer
			}
			return sink(event)
		case "citation":
			if event.Citation == nil {
				return ErrAnswer
			}
			emittedCitations = append(emittedCitations, *event.Citation)
			return nil
		case "done":
			if event.Result == nil {
				return ErrAnswer
			}
			owned := *event.Result
			generated = &owned
			return nil
		default:
			return ErrAnswer
		}
	}
	if err := s.deps.Generator.Generate(ctx, request, handle); err != nil || generated == nil || ctx.Err() != nil {
		return ErrAnswer
	}
	if !validAnswer(generated, in.RequestID, version, digest, evidence) {
		return ErrAnswer
	}
	if len(emittedCitations) > 0 {
		for _, citation := range emittedCitations {
			if !containsCitation(generated.Citations, citation) {
				return ErrAnswer
			}
		}
	}
	if err := s.revalidate(ctx, searchInput, evidence); err != nil {
		return err
	}
	for _, citation := range generated.Citations {
		value := citation
		if err := sink(contracts.AnswerEvent{Kind: "citation", Citation: &value}); err != nil {
			return err
		}
	}
	return sink(contracts.AnswerEvent{Kind: "done", Result: generated})
}

func (s *AnswerService) revalidate(ctx context.Context, in SearchInput, evidence []contracts.Evidence) error {
	fresh, err := s.deps.Search.Search(ctx, in)
	if err != nil || ctx.Err() != nil {
		return ErrAnswer
	}
	allowed := map[string]bool{}
	for _, candidate := range fresh {
		allowed[citationKey(candidate.Citation)] = true
	}
	for _, item := range evidence {
		if !allowed[citationKey(item.Citation)] {
			return ErrAnswer
		}
	}
	return nil
}

func selectAnswerEvidence(candidates []contracts.Candidate) []contracts.Evidence {
	out := make([]contracts.Evidence, 0, 5)
	bytes := 0
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if len(out) == 5 || candidate.Text == "" || !utf8.ValidString(candidate.Text) || bytes+len(candidate.Text) > 24<<10 {
			continue
		}
		key := citationKey(candidate.Citation)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		bytes += len(candidate.Text)
		out = append(out, contracts.Evidence{Citation: candidate.Citation, SourceExcerpt: candidate.Text, Rank: len(out) + 1})
	}
	return out
}

func emitInsufficient(sink contracts.AnswerSink, requestID contracts.ID, version uint64, digest contracts.Digest) error {
	if err := sink(contracts.AnswerEvent{Kind: "meta"}); err != nil {
		return err
	}
	return sink(contracts.AnswerEvent{Kind: "done", Result: &contracts.AnswerResult{RequestID: requestID, Answer: "现有已发布文档不足以回答这个问题。", Status: "insufficient_evidence", ProfileVersion: version, ProfileDigest: digest, TimingsMS: map[string]float64{}, Citations: []contracts.Citation{}, Degraded: []contracts.Degradation{}}})
}

func validAnswer(result *contracts.AnswerResult, requestID contracts.ID, version uint64, digest contracts.Digest, evidence []contracts.Evidence) bool {
	if result == nil || result.RequestID != requestID || result.ProfileVersion != version || result.ProfileDigest != digest || result.Answer == "" || !utf8.ValidString(result.Answer) || result.Status != "answered" && result.Status != "insufficient_evidence" || !validDigest(result.ProfileDigest) {
		return false
	}
	allowed := map[string]bool{}
	for _, item := range evidence {
		allowed[citationKey(item.Citation)] = true
	}
	seen := map[string]bool{}
	for _, citation := range result.Citations {
		key := citationKey(citation)
		if key == "" || !allowed[key] || seen[key] {
			return false
		}
		seen[key] = true
	}
	return result.Status == "insufficient_evidence" && len(result.Citations) == 0 || result.Status == "answered" && len(result.Citations) > 0
}

func containsCitation(values []contracts.Citation, target contracts.Citation) bool {
	key := citationKey(target)
	for _, value := range values {
		if citationKey(value) == key {
			return true
		}
	}
	return false
}

func citationKey(value contracts.Citation) string {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 1<<20 {
		return ""
	}
	return string(raw)
}

func validDigest(value contracts.Digest) bool {
	decoded, err := hex.DecodeString(string(value))
	return err == nil && len(decoded) == 32 && strings.ToLower(string(value)) == string(value)
}
