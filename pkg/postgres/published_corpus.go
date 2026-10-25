package postgres

import (
	"context"
	"reflect"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// PublishedCorpusSnapshot is a bounded, authorized materialization of native
// published chunk projections. It is not a second authoritative vector index.
type PublishedCorpusSnapshot struct {
	Observation queryread.CurrentObservation
	Facts       []contracts.PublishedFact
	Candidates  []contracts.Candidate
}

func (p QueryReads) LoadPublishedCorpus(ctx context.Context, scope contracts.AccessScope) (PublishedCorpusSnapshot, error) {
	var out PublishedCorpusSnapshot
	err := p.withinQuery(ctx, scope, func(c context.Context, identity contracts.QueryReadIdentity, repo *queryRepository) error {
		out.Observation.Identity = identity
		facts, err := queryread.Collect(c, repo, scope, identity, scope.KnowledgeBaseIDs, queryread.Limits{PageSize: 100, MaxPages: 10, MaxFacts: 1000, MaxGenerations: 3000})
		if err != nil {
			return err
		}
		if len(facts) > 0 {
			if err := repo.prefetchSnapshots(c, facts); err != nil {
				return err
			}
		}
		bytes := 0
		for _, fact := range facts {
			state, err := repo.load(c, fact.DocumentID)
			if err != nil {
				return err
			}
			current, err := queryread.PublishedCurrentFact(scope, identity, state)
			if err != nil || !reflect.DeepEqual(current, fact) {
				return queryread.ErrCheck
			}
			out.Facts = append(out.Facts, fact)
			out.Observation.Documents = append(out.Observation.Documents, state)
			snapshot, err := readPublishedSnapshot(c, repo, fact)
			if err != nil {
				return err
			}
			for _, r := range snapshot.Records {
				bytes += len(r.Chunk.Text)
				if len(out.Candidates) >= 512 || bytes > 512<<10 {
					return queryread.ErrLimit
				}
				out.Candidates = append(out.Candidates, contracts.Candidate{Citation: r.Citation, ChunkID: r.Chunk.ID, RouteID: "local-semantic", RetrieverID: "local-published-scan", KnowledgeBaseID: r.KnowledgeBaseID, Rank: len(out.Candidates) + 1, ScoreKind: "unranked", GenerationIDs: []contracts.ID{r.GenerationID}, Text: r.Chunk.Text})
			}
		}
		return nil
	})
	if err != nil {
		return PublishedCorpusSnapshot{}, err
	}
	return out, nil
}
