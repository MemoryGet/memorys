package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/publicationprojection"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

var ErrKeywordSearch = errors.New("published keyword retrieval rejected")

// RetrievalAuthority must resolve a trusted application projection and current
// invocation permission. A client-supplied digest or DTO is not authorization.
type RetrievalAuthority func(context.Context, contracts.RetrieveRequest) ([]byte, publicationprojection.CallIdentity, error)

// KeywordRetriever is the native PostgreSQL implementation of the standard
// Retriever port. It supports one KB/keyword route per call. Candidates still
// require final evidence/current-authority checks before user/model output.
type KeywordRetriever struct {
	Reads            QueryReads
	ModuleInstanceID contracts.ID
	Authorize        RetrievalAuthority
}

var _ contracts.Retriever = KeywordRetriever{}

func (p KeywordRetriever) Search(ctx context.Context, input contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	if input.Scope.Action != "knowledge.search" || ctx == nil || p.Authorize == nil || !commandID(string(p.ModuleInstanceID)) {
		return nil, ErrKeywordSearch
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrKeywordSearch
	}
	q, err := copyQuery(input)
	if err != nil || q.Request.Deadline.IsZero() || !q.Request.Deadline.After(time.Now()) || len(q.Query.Vector) != 0 || q.Query.EmbeddingSpec != nil || !utf8.ValidString(q.Query.Text) || len(q.Query.Text) > 4096 || strings.TrimSpace(q.Query.Text) == "" || strings.ContainsRune(q.Query.Text, 0) || len(q.Channels) != 1 || q.Channels[0] != contracts.KeywordChannel || len(q.Scope.KnowledgeBaseIDs) != 1 || q.Limit < 1 || q.Limit > 1000 {
		return nil, ErrKeywordSearch
	}
	ctx, cancel := context.WithDeadline(ctx, q.Request.Deadline)
	defer cancel()
	authorize := func() ([]byte, publicationprojection.CallIdentity, error) {
		owned, err := copyQuery(q)
		if err != nil || ctx.Err() != nil {
			return nil, publicationprojection.CallIdentity{}, ErrKeywordSearch
		}
		raw, call, err := p.Authorize(ctx, owned)
		if err != nil || len(raw) > 1<<20 || ctx.Err() != nil || call.ModuleInstanceID != p.ModuleInstanceID || publicationprojection.ValidateRequest(raw, call, q) != nil {
			return nil, call, ErrKeywordSearch
		}
		return append([]byte(nil), raw...), call, nil
	}
	raw, call, err := authorize()
	if err != nil {
		return nil, ErrKeywordSearch
	}
	projection, _, err := publicationprojection.ValidatedDigest(raw)
	if err != nil {
		return nil, ErrKeywordSearch
	}
	expires := time.Unix(0, projection.ExpiresAtUnixNS)
	ctx, stop := context.WithDeadline(ctx, expires)
	defer stop()
	var results []contracts.Candidate
	var expected queryread.CurrentExpectation
	policies := map[contracts.ID]uint64{}
	err = p.Reads.withinQuery(ctx, q.Scope, func(c context.Context, identity contracts.QueryReadIdentity, repo *queryRepository) error {
		if err := repo.inventory(c); err != nil {
			return err
		}
		facts := map[contracts.ID]contracts.PublishedFact{}
		for _, fact := range repo.facts {
			facts[fact.DocumentID] = fact
		}
		declared := map[contracts.ID]contracts.IndexGeneration{}
		for _, g := range q.Generations {
			declared[g.ID] = g
		}
		expected = queryread.CurrentExpectation{StoreEpochID: identity.StoreEpochID, ScopeVersion: q.Scope.PolicyVersion}
		rowsByID := map[[2]contracts.ID]contracts.IndexProjection{}
		remaining := 16 << 20
		if len(repo.facts) > 0 {
			if err := repo.prefetchSnapshots(c, repo.facts); err != nil {
				return err
			}
		}
		for _, entry := range projection.Entries {
			fact, present := facts[entry.DocumentID]
			g := fact.GenerationSet.Channels[contracts.KeywordChannel]
			if !present || fact.RevisionID != entry.RevisionID || fact.PublicationEpoch != entry.PublicationVersion || fact.GenerationSet.ID != entry.GenerationSetID || g.ID != entry.GenerationID || !reflect.DeepEqual(g, declared[g.ID]) || g.BackendEpochID != q.BackendEpochID || g.ProfileDigest != entry.IndexProfileDigest {
				return ErrKeywordSearch
			}
			state, err := repo.load(c, fact.DocumentID)
			if err != nil || state.PublicationReview == nil {
				return ErrKeywordSearch
			}
			policies[fact.KnowledgeBaseID] = state.PublicationReview.Validity.ReviewPolicyVersion
			expected.Published = append(expected.Published, fact)
			snapshot, err := readPublishedSnapshot(c, repo, fact)
			if err != nil {
				return err
			}

			for _, record := range snapshot.Records {
				encoded, err := json.Marshal(record)
				if err != nil || len(encoded) > remaining || len(rowsByID) >= 10000 {
					return ErrKeywordSearch
				}
				remaining -= len(encoded)
				rowsByID[[2]contracts.ID{record.GenerationID, record.Chunk.ID}] = record
			}
		}
		// Publication authority is the current platform head, not the immutable
		// projection's historical Published flag. All constraints precede LIMIT.
		rows, err := repo.tx.QueryContext(c, `SELECT r.generation_id,r.chunk_id,
 ts_rank_cd(r.keyword_vector,plainto_tsquery('simple'::regconfig,$5::text))::float8 + CASE WHEN strpos(lower(r.payload->'Chunk'->>'Text'),lower($5::text))>0 THEN 1::float8 ELSE 0::float8 END AS score
 FROM akos_withdraw_v1.index_projection_rows r JOIN akos_withdraw_v1.index_generations g ON g.id=r.generation_id
 JOIN akos_withdraw_v1.documents d ON d.id=g.document_id AND d.space_id=g.space_id AND d.kb_id=g.kb_id
 WHERE g.space_id=$1::text AND g.kb_id=$2::text AND g.backend_epoch_id=$3::text AND g.id=ANY($4::text[]) AND g.channel='keyword'
 AND NOT d.deleted AND d.active_revision_id=g.revision_id AND d.active_generation_set_id=g.generation_set_id
 AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=g.id)
 AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.index_chunk_deletion_fences f WHERE f.generation_id=g.id AND f.chunk_id=r.chunk_id)
 AND (r.keyword_vector @@ plainto_tsquery('simple'::regconfig,$5::text) OR strpos(lower(r.payload->'Chunk'->>'Text'),lower($5::text))>0)
 ORDER BY score DESC,g.id COLLATE "C",r.chunk_id COLLATE "C" LIMIT $6`, string(q.Scope.SpaceID), string(q.Scope.KnowledgeBaseIDs[0]), string(q.BackendEpochID), queryKBs(q.Publication.GenerationIDs), q.Query.Text, q.Limit)
		if err != nil {
			return ErrKeywordSearch
		}
		defer rows.Close()
		results = make([]contracts.Candidate, 0)
		for rows.Next() {
			var gid, chunk contracts.ID
			var score float64
			if rows.Scan(&gid, &chunk, &score) != nil || math.IsNaN(score) || math.IsInf(score, 0) || score < 0 {
				return ErrKeywordSearch
			}
			record, ok := rowsByID[[2]contracts.ID{gid, chunk}]
			if !ok {
				return ErrKeywordSearch
			}
			results = append(results, contracts.Candidate{Citation: record.Citation, ChunkID: chunk, RouteID: q.RouteID, RetrieverID: p.ModuleInstanceID, KnowledgeBaseID: record.KnowledgeBaseID, Rank: len(results) + 1, Score: score, ScoreKind: "postgres_keyword_rank", GenerationIDs: []contracts.ID{gid}, Text: record.Chunk.Text})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, ErrKeywordSearch
	}
	lastRaw, lastCall, err := authorize()
	_, lastDigest, digestErr := publicationprojection.ValidatedDigest(lastRaw)
	if err != nil || digestErr != nil || lastCall != call || lastDigest != q.Publication.SnapshotDigest || ctx.Err() != nil {
		return nil, ErrKeywordSearch
	}
	// Candidates from an obsolete metadata snapshot are never returned as a
	// successful search. This remains an observation, not a network send fence.
	current, err := queryread.ObserveCurrent(ctx, p.Reads, q.Scope, documentIDs(expected.Published), queryread.CurrentLimits{MaxDocuments: 1000, MaxBytes: 4 << 20})
	if err != nil || queryread.MatchCurrentReview(q.Scope, expected, current, policies) != nil {
		return nil, ErrKeywordSearch
	}
	return results, nil
}

func documentIDs(facts []contracts.PublishedFact) []contracts.ID {
	ids := make([]contracts.ID, 0, len(facts))
	for _, fact := range facts {
		ids = append(ids, fact.DocumentID)
	}
	return ids
}
