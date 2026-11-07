package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type answerAuth struct{ principal contracts.Principal }

func (a answerAuth) Authenticate(context.Context, contracts.Credential) (contracts.Principal, error) {
	return a.principal, nil
}

type answerAuthorizer struct{}

func (answerAuthorizer) Authorize(_ context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
	return contracts.AccessScope{ScopeID: "scope", SubjectID: q.Principal.SubjectID, Action: q.Action, SpaceID: q.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), q.KnowledgeBaseIDs...), PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

type answerSearch struct{ calls int }

func (s *answerSearch) Search(context.Context, SearchInput) ([]contracts.Candidate, error) {
	s.calls++
	return []contracts.Candidate{{Citation: contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block"}, Text: "证据", Rank: 1, Score: 1}}, nil
}
func (answerSearch) ProfileIdentity() (uint64, contracts.Digest, error) {
	return 1, contracts.Digest(strings.Repeat("a", 64)), nil
}

type answerGenerator func(context.Context, contracts.GenerateRequest, contracts.AnswerSink) error

func (f answerGenerator) Generate(c context.Context, q contracts.GenerateRequest, sink contracts.AnswerSink) error {
	return f(c, q, sink)
}

func TestAnswerServiceRevalidatesAndBindsCitations(t *testing.T) {
	search := &answerSearch{}
	service, err := NewAnswer(AnswerDependencies{Search: search, Auth: answerAuth{principal: contracts.Principal{SubjectID: "user", SubjectType: "user", Issuer: "issuer"}}, Authorizer: answerAuthorizer{}, Generator: answerGenerator(func(_ context.Context, q contracts.GenerateRequest, sink contracts.AnswerSink) error {
		return sink(contracts.AnswerEvent{Kind: "done", Result: &contracts.AnswerResult{RequestID: q.Request.RequestID, ProfileVersion: 1, ProfileDigest: q.Request.ProfileDigest, Answer: "答案 [1]", Status: "answered", Citations: []contracts.Citation{q.Evidence[0].Citation}}})
	})})
	if err != nil {
		t.Fatal(err)
	}
	var done *contracts.AnswerResult
	err = service.Ask(context.Background(), AnswerInput{Credential: contracts.Credential{Kind: "access_token", Secret: []byte("token")}, RequestID: "request", Deadline: time.Now().Add(time.Second), SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Question: "问题"}, func(event contracts.AnswerEvent) error {
		if event.Kind == "done" {
			done = event.Result
		}
		return nil
	})
	if err != nil || done == nil || done.Status != "answered" || search.calls != 2 {
		t.Fatalf("answer failed: err=%v done=%+v calls=%d", err, done, search.calls)
	}
}

func TestAnswerServiceRejectsUnsupportedReflectionAndInvalidGeneratorCitation(t *testing.T) {
	search := &answerSearch{}
	service, err := NewAnswer(AnswerDependencies{Search: search, Auth: answerAuth{principal: contracts.Principal{SubjectID: "user", SubjectType: "user", Issuer: "issuer"}}, Authorizer: answerAuthorizer{}, Generator: answerGenerator(func(_ context.Context, q contracts.GenerateRequest, sink contracts.AnswerSink) error {
		return sink(contracts.AnswerEvent{Kind: "done", Result: &contracts.AnswerResult{RequestID: q.Request.RequestID, ProfileVersion: 1, ProfileDigest: q.Request.ProfileDigest, Answer: "答案 [1]", Status: "answered", Citations: []contracts.Citation{{DocumentID: "other", RevisionID: "rev", BlockID: "block"}}}})
	})})
	if err != nil {
		t.Fatal(err)
	}
	input := AnswerInput{Credential: contracts.Credential{Kind: "access_token", Secret: []byte("token")}, RequestID: "request", Deadline: time.Now().Add(time.Second), SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Question: "问题", Reflection: true}
	if err := service.Ask(context.Background(), input, func(contracts.AnswerEvent) error { return nil }); !errors.Is(err, ErrAnswer) {
		t.Fatal("reflection was accepted", err)
	}
	input.Reflection = false
	if err := service.Ask(context.Background(), input, func(contracts.AnswerEvent) error { return nil }); !errors.Is(err, ErrAnswer) {
		t.Fatal("forged citation was accepted", err)
	}
}
