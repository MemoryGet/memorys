package api

import (
	"context"
	"errors"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

// KnowledgeAnswerApplication adapts the transport DTO boundary to the
// transport-independent answer service. It keeps bearer parsing and response
// formatting in api while the application owns authorization, retrieval and
// citation revalidation.
type KnowledgeAnswerApplication struct{ Service *knowledge.AnswerService }

func (a KnowledgeAnswerApplication) Ask(ctx context.Context, in PartnerAskInput, sink contracts.AnswerSink) error {
	if a.Service == nil {
		return knowledge.ErrAnswer
	}
	err := a.Service.Ask(ctx, knowledge.AnswerInput{Credential: in.Credential, RequestID: in.Request.RequestID, Deadline: in.Request.Deadline, SpaceID: in.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), in.KnowledgeBaseIDs...), Question: in.Question, Reflection: in.Reflection}, sink)
	if errors.Is(err, knowledge.ErrAnswerUnauthenticated) {
		return ErrPartnerAskUnauthenticated
	}
	return err
}
