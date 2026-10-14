// Package queryread contains query authority helpers, not a database adapter.
package queryread

import (
	"context"
	"errors"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrInvalid = errors.New("invalid publication inventory")
var ErrLimit = errors.New("publication inventory limit exceeded")
var ErrRead = errors.New("publication inventory read failed")

type Limits struct{ PageSize, MaxPages, MaxFacts, MaxGenerations int }

// Collect must run inside one QueryReadUnit callback. A complete result is
// returned only after an explicit terminal page. Repository snapshots must
// transfer independent data ownership; this helper does not prove provenance.
func Collect(ctx context.Context, repository contracts.QueryReadRepository, scope contracts.AccessScope, identity contracts.QueryReadIdentity, knowledgeBaseIDs []contracts.ID, limits Limits) ([]contracts.PublishedFact, error) {
	if ctx == nil || repository == nil {
		return nil, ErrInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrInvalid
	}
	if limits.PageSize < 1 || limits.PageSize > 1000 || limits.MaxPages < 1 || limits.MaxPages > 1000 || limits.MaxFacts < 1 || limits.MaxFacts > 10000 || limits.MaxGenerations < 1 || limits.MaxGenerations > 30000 {
		return nil, ErrInvalid
	}
	scope.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	kbs := append([]contracts.ID(nil), knowledgeBaseIDs...)
	initial := contracts.PublishedPageRequest{Identity: identity, KnowledgeBaseIDs: kbs, Limit: limits.PageSize}
	if contracts.ValidatePublishedPage(contracts.PublishedPage{Identity: identity}, initial, scope, "") != nil {
		return nil, ErrInvalid
	}
	var facts []contracts.PublishedFact
	var previous contracts.ID
	cursor := ""
	seenCursors := map[string]bool{"": true}
	revisions := map[contracts.ID]bool{}
	sets := map[contracts.ID]bool{}
	generations := map[contracts.ID]bool{}
	for pageNumber := 0; pageNumber < limits.MaxPages; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := limits.MaxFacts - len(facts)
		if remaining == 0 {
			return nil, ErrLimit
		}
		count := limits.PageSize
		if count > remaining {
			count = remaining
		}
		request := contracts.PublishedPageRequest{Identity: identity, KnowledgeBaseIDs: kbs, Cursor: cursor, Limit: count}
		sent := request
		sent.KnowledgeBaseIDs = append([]contracts.ID(nil), kbs...)
		page, err := repository.ReadPublishedPage(ctx, sent)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, ErrRead
		}
		if contracts.ValidatePublishedPage(page, request, scope, previous) != nil {
			return nil, ErrInvalid
		}
		for _, fact := range page.Facts {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if revisions[fact.RevisionID] || sets[fact.GenerationSet.ID] {
				return nil, ErrInvalid
			}
			revisions[fact.RevisionID] = true
			sets[fact.GenerationSet.ID] = true
			for _, g := range fact.GenerationSet.Channels {
				if generations[g.ID] {
					return nil, ErrInvalid
				}
				generations[g.ID] = true
				if len(generations) > limits.MaxGenerations {
					return nil, ErrLimit
				}
			}
			facts = append(facts, fact)
			previous = fact.DocumentID
		}
		if page.NextCursor == "" {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return facts, nil
		}
		if len(page.NextCursor) > 4096 || seenCursors[page.NextCursor] {
			return nil, ErrInvalid
		}
		seenCursors[page.NextCursor] = true
		cursor = page.NextCursor
	}
	return nil, ErrLimit
}
