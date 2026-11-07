package api

import (
	"context"
	"errors"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

// KnowledgeFeedbackApplication adapts the strict partner DTO to the
// transport-independent feedback application service.
type KnowledgeFeedbackApplication struct{ Service *knowledge.FeedbackService }

func (a KnowledgeFeedbackApplication) SubmitFeedback(ctx context.Context, in PartnerFeedbackInput) (contracts.FeedbackResult, error) {
	var empty contracts.FeedbackResult
	if a.Service == nil {
		return empty, knowledge.ErrFeedback
	}
	result, err := a.Service.SubmitFeedback(ctx, knowledge.FeedbackInput{Credential: in.Credential, RequestID: in.Request.RequestID, Deadline: in.Request.Deadline, SpaceID: in.SpaceID, Citation: in.Citation, TargetRequestID: in.TargetRequestID, Kind: in.Kind, Comment: in.Comment, IdempotencyKey: in.IdempotencyKey})
	if errors.Is(err, knowledge.ErrFeedbackUnauthenticated) {
		return empty, ErrPartnerFeedbackUnauthenticated
	}
	if errors.Is(err, knowledge.ErrFeedbackConflict) {
		return empty, ErrPartnerFeedbackConflict
	}
	return result, err
}
