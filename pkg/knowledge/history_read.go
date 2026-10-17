package knowledge

import (
	"context"
	"reflect"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type HistoryReadDependencies struct {
	Authorizer contracts.Authorizer
	Content    contracts.HistoricalContentReader
	Verifier   contracts.HistoricalContentVerifier
}

func (s *CurrentReadService) historyRequest(ctx context.Context, in CurrentReadInput, who contracts.Principal, location contracts.ReadableDocument) (contracts.HistoricalContentReadRequest, error) {
	var empty contracts.HistoricalContentReadRequest
	p, err := copyValue(who)
	if err != nil {
		return empty, ErrRead
	}
	q := contracts.AuthorizeRequest{Principal: p, Action: contracts.HistoricalReadAction, SpaceID: location.SpaceID, KnowledgeBaseIDs: []contracts.ID{location.KnowledgeBaseID}, ResourceIDs: []contracts.ID{in.DocumentID}}
	scope, err := s.deps.History.Authorizer.Authorize(ctx, q)
	if err != nil {
		return empty, ErrRead
	}
	scope, err = copyValue(scope)
	if err != nil {
		return empty, ErrRead
	}
	result := contracts.HistoricalContentReadRequest{Base: contracts.FrozenContentReadRequest{Request: in.Request, Scope: location.Scope, DocumentID: in.DocumentID, RevisionID: in.RevisionID}, Principal: who, HistoryScope: scope}
	if queryread.ValidateHistoricalScopes(ctx, result) != nil {
		return empty, ErrRead
	}
	return copyValue(result)
}

func validateHistoryResult(q contracts.HistoricalContentReadRequest, history contracts.HistoricalContentRecord, current contracts.PublishedFact, store contracts.ID, policy uint64) error {
	if queryread.ValidateHistoricalContent(q, history, policy) != nil || history.StoreEpochID != store || !reflect.DeepEqual(history.Current, current) || len(history.Content.Payload) > 4<<20 {
		return ErrRead
	}
	h, err := canonical.Frozen(history.Content.Payload)
	if err != nil || h.Format != history.Review.Revision.HashFormat || contracts.Digest(h.ContentHash) != history.Review.Revision.ContentHash || contracts.Digest(h.BlocksHash) != history.Review.Revision.BlocksHash {
		return ErrRead
	}
	return nil
}

func historyMeta(h contracts.HistoricalContentRecord) contracts.HistoricalContentRecord {
	h.Content.Payload = nil
	return h
}
