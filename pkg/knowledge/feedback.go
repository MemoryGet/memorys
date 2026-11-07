package knowledge

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var (
	ErrFeedback                = errors.New("knowledge feedback unavailable")
	ErrFeedbackUnauthenticated = errors.New("knowledge feedback authentication required")
	ErrFeedbackConflict        = contracts.ErrFeedbackConflict
)

type FeedbackInput struct {
	Credential      contracts.Credential
	RequestID       contracts.ID
	Deadline        time.Time
	SpaceID         contracts.ID
	Citation        *contracts.Citation
	TargetRequestID *contracts.ID
	Kind            string
	Comment         string
	IdempotencyKey  string
}

type FeedbackDependencies struct {
	Auth       contracts.Authenticator
	Authorizer contracts.Authorizer
	Targets    contracts.FeedbackTargetResolver
	Store      contracts.FeedbackStore
}

type FeedbackService struct{ deps FeedbackDependencies }

func NewFeedback(d FeedbackDependencies) (*FeedbackService, error) {
	if missing(d.Auth) || missing(d.Authorizer) || missing(d.Targets) || missing(d.Store) {
		return nil, ErrFeedback
	}
	return &FeedbackService{deps: d}, nil
}

func (s *FeedbackService) SubmitFeedback(ctx context.Context, in FeedbackInput) (contracts.FeedbackResult, error) {
	var empty contracts.FeedbackResult
	if s == nil || ctx == nil || ctx.Err() != nil || in.RequestID == "" || in.SpaceID == "" || in.Deadline.IsZero() || !in.Deadline.After(time.Now()) || in.Credential.Kind == "" || len(in.Credential.Secret) == 0 || len(in.Credential.Secret) > 16384 || !validFeedbackKind(in.Kind) || !validFeedbackComment(in.Comment) || in.IdempotencyKey == "" || len(in.IdempotencyKey) > 200 || strings.ContainsAny(in.IdempotencyKey, "\x00\r\n") || (in.Citation == nil) == (in.TargetRequestID == nil) {
		return empty, ErrFeedback
	}
	ctx, cancel := context.WithDeadline(ctx, in.Deadline)
	defer cancel()
	credential := contracts.Credential{Kind: in.Credential.Kind, Secret: append([]byte(nil), in.Credential.Secret...)}
	who, err := s.deps.Auth.Authenticate(ctx, credential)
	clear(credential.Secret)
	if err != nil || ctx.Err() != nil {
		return empty, ErrFeedbackUnauthenticated
	}
	var target contracts.FeedbackTarget
	var targetRequestID *contracts.ID
	if in.Citation != nil {
		citation := *in.Citation
		target, err = s.deps.Targets.ResolveCitation(ctx, in.SpaceID, citation)
	} else {
		targetRequestID = cloneID(in.TargetRequestID)
		target, err = s.deps.Targets.ResolveRequest(ctx, in.SpaceID, *in.TargetRequestID)
	}
	if err != nil || target.SpaceID != in.SpaceID || target.KnowledgeBaseID == "" || target.DocumentID == "" && in.Citation != nil {
		return empty, ErrFeedback
	}
	scope, err := s.deps.Authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "knowledge.feedback", SpaceID: in.SpaceID, KnowledgeBaseIDs: []contracts.ID{target.KnowledgeBaseID}, ResourceIDs: optionalResource(target.DocumentID)})
	if err != nil || scope.Action != "knowledge.feedback" || scope.SpaceID != in.SpaceID || scope.SubjectID != who.SubjectID || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] != target.KnowledgeBaseID || !scope.ExpiresAt.After(time.Now()) {
		return empty, ErrFeedback
	}
	request := contracts.FeedbackRequest{Command: contracts.CommandContext{Request: contracts.RequestContext{RequestID: in.RequestID, Deadline: in.Deadline}, Principal: who, Scope: scope, IdempotencyKey: in.IdempotencyKey}, SpaceID: in.SpaceID, Citation: cloneCitation(in.Citation), TargetRequestID: targetRequestID, Kind: in.Kind, Comment: strings.TrimSpace(in.Comment)}
	result, err := s.deps.Store.Submit(ctx, contracts.FeedbackRecord{Request: request, Principal: who, Target: target, RequestID: in.RequestID, TargetRequestID: targetRequestID, IdempotencyKey: in.IdempotencyKey})
	if errors.Is(err, ErrFeedbackConflict) {
		return empty, ErrFeedbackConflict
	}
	if err != nil || result.FeedbackID == "" {
		return empty, ErrFeedback
	}
	if result.RequestID != "" && result.RequestID != in.RequestID {
		return empty, ErrFeedback
	}
	return result, nil
}

func validFeedbackKind(value string) bool {
	switch value {
	case "incorrect", "missing", "outdated", "helpful", "other":
		return true
	default:
		return false
	}
}

func validFeedbackComment(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && len(value) <= 10000
}

func optionalResource(value contracts.ID) []contracts.ID {
	if value == "" {
		return nil
	}
	return []contracts.ID{value}
}

func cloneID(value *contracts.ID) *contracts.ID {
	if value == nil {
		return nil
	}
	owned := *value
	return &owned
}

func cloneCitation(value *contracts.Citation) *contracts.Citation {
	if value == nil {
		return nil
	}
	owned := *value
	return &owned
}
