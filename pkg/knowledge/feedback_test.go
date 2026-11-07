package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type feedbackAuth struct{}

func (feedbackAuth) Authenticate(context.Context, contracts.Credential) (contracts.Principal, error) {
	return contracts.Principal{SubjectID: "user", SubjectType: "user", Issuer: "issuer"}, nil
}

type feedbackAuthorizer struct{}

func (feedbackAuthorizer) Authorize(_ context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
	return contracts.AccessScope{ScopeID: "scope", SubjectID: q.Principal.SubjectID, Action: q.Action, SpaceID: q.SpaceID, KnowledgeBaseIDs: q.KnowledgeBaseIDs, PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

type feedbackTargets struct{}

func (feedbackTargets) ResolveCitation(context.Context, contracts.ID, contracts.Citation) (contracts.FeedbackTarget, error) {
	return contracts.FeedbackTarget{SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "doc"}, nil
}
func (feedbackTargets) ResolveRequest(context.Context, contracts.ID, contracts.ID) (contracts.FeedbackTarget, error) {
	return contracts.FeedbackTarget{SpaceID: "space", KnowledgeBaseID: "kb"}, nil
}

type feedbackStore struct{ record contracts.FeedbackRecord }

func (s *feedbackStore) Submit(_ context.Context, record contracts.FeedbackRecord) (contracts.FeedbackResult, error) {
	s.record = record
	return contracts.FeedbackResult{RequestID: record.RequestID, FeedbackID: "feedback-1", State: "received"}, nil
}

func TestFeedbackServiceAuthorizesResolvedCitationAndPersists(t *testing.T) {
	store := &feedbackStore{}
	service, err := NewFeedback(FeedbackDependencies{Auth: feedbackAuth{}, Authorizer: feedbackAuthorizer{}, Targets: feedbackTargets{}, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	citation := &contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block"}
	result, err := service.SubmitFeedback(context.Background(), FeedbackInput{Credential: contracts.Credential{Kind: "access_token", Secret: []byte("secret")}, RequestID: "request", Deadline: time.Now().Add(time.Second), SpaceID: "space", Citation: citation, Kind: "incorrect", Comment: "不准确", IdempotencyKey: "key-1"})
	if err != nil || result.FeedbackID != "feedback-1" || store.record.Target.KnowledgeBaseID != "kb" || store.record.Request.Command.Scope.Action != "knowledge.feedback" {
		t.Fatalf("feedback failed: %+v %v", result, err)
	}
}

func TestFeedbackServiceRejectsAmbiguousTarget(t *testing.T) {
	service, err := NewFeedback(FeedbackDependencies{Auth: feedbackAuth{}, Authorizer: feedbackAuthorizer{}, Targets: feedbackTargets{}, Store: &feedbackStore{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SubmitFeedback(context.Background(), FeedbackInput{Credential: contracts.Credential{Kind: "access_token", Secret: []byte("secret")}, RequestID: "request", Deadline: time.Now().Add(time.Second), SpaceID: "space", Kind: "helpful", Comment: "ok", IdempotencyKey: "key"})
	if !errors.Is(err, ErrFeedback) {
		t.Fatal("ambiguous feedback accepted", err)
	}
}
