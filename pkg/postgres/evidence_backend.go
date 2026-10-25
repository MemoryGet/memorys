package postgres

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/preparation"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// EvidenceBackend is the native keyword implementation of CurrentBackend.
// CurrentControl/identity remain separate. This provider verifies actual index,
// frozen body and supported quote sources, not a network send/revoke barrier.
type EvidenceBackend struct {
	Reads           QueryReads
	Originals       contracts.BlobStore
	OriginalControl contentverify.OriginalReadControl
	Projector       contentverify.OriginalTextProjector
}

var _ queryread.CurrentBackend = EvidenceBackend{}

func (p EvidenceBackend) Verify(ctx context.Context, request contracts.RequestContext, scope contracts.AccessScope, input queryread.CurrentObservation, evidence []contracts.Evidence) error {
	if ctx == nil || !commandID(string(request.RequestID)) || !commandDigest(request.ProfileDigest) || request.Deadline.IsZero() || len(input.Documents) < 1 || len(input.Documents) > 1000 || len(evidence) > 1000 {
		return queryread.ErrCheck
	}
	if _, ok := ctx.Deadline(); !ok {
		return queryread.ErrCheck
	}
	for _, e := range evidence {
		if !utf8.ValidString(e.SourceExcerpt) || !commandID(string(e.Citation.DocumentID)) || !commandID(string(e.Citation.RevisionID)) || !commandID(string(e.Citation.BlockID)) {
			return queryread.ErrCheck
		}
	}
	for _, s := range input.Documents {
		if !commandID(string(s.Document.ID)) || !commandID(string(s.Document.SpaceID)) || !commandID(string(s.Document.KnowledgeBaseID)) {
			return queryread.ErrCheck
		}
	}
	observed, err := copyQuery(input)
	if err != nil {
		return queryread.ErrCheck
	}
	quotes, err := copyQuery(evidence)
	if err != nil {
		return queryread.ErrCheck
	}
	scope = cloneScope(scope)
	ctx, cancel := context.WithDeadline(ctx, request.Deadline)
	defer cancel()
	expected := queryread.CurrentExpectation{StoreEpochID: observed.Identity.StoreEpochID, ScopeVersion: scope.PolicyVersion}
	policies := map[contracts.ID]uint64{}
	byID := map[contracts.ID]contracts.CurrentDocumentState{}
	for _, state := range observed.Documents {
		d := state.Document
		if !commandID(string(d.ID)) || d.ActiveRevisionID == nil || d.ActiveGenerationSetID == nil || state.GenerationSet == nil || state.PublicationReview == nil {
			return queryread.ErrCheck
		}
		if _, duplicate := byID[d.ID]; duplicate {
			return queryread.ErrCheck
		}
		byID[d.ID] = state
		r := state.PublicationReview
		expected.Published = append(expected.Published, contracts.PublishedFact{DocumentID: d.ID, SpaceID: d.SpaceID, KnowledgeBaseID: d.KnowledgeBaseID, RevisionID: *d.ActiveRevisionID, RowVersion: d.RowVersion, PublicationEpoch: d.PublicationEpoch, ApprovalID: r.Approval.ID, ContentDigest: r.Revision.ContentHash, GenerationSet: *state.GenerationSet})
		if old, ok := policies[d.KnowledgeBaseID]; ok && old != r.Validity.ReviewPolicyVersion {
			return queryread.ErrCheck
		}
		policies[d.KnowledgeBaseID] = r.Validity.ReviewPolicyVersion
	}
	if queryread.MatchCurrentReview(scope, expected, observed, policies) != nil {
		return queryread.ErrCheck
	}
	groups := map[contracts.ID][]contracts.Evidence{}
	for _, e := range quotes {
		state, ok := byID[e.Citation.DocumentID]
		if !ok || e.Citation.RevisionID != state.PublicationReview.Revision.ID || e.SourceExcerpt == "" || e.Rank < 1 {
			return queryread.ErrCheck
		}
		groups[e.Citation.DocumentID] = append(groups[e.Citation.DocumentID], e)
	}
	inspect := func() error {
		return p.Reads.withinQuery(ctx, scope, func(c context.Context, id contracts.QueryReadIdentity, repo *queryRepository) error {
			current := queryread.CurrentObservation{Identity: id}
			remaining, count := 16<<20, 0
			ids := make([]contracts.ID, 0, len(expected.Published))
			for _, fact := range expected.Published {
				ids = append(ids, fact.DocumentID)
			}
			if err := repo.prefetch(c, ids); err != nil {
				return err
			}
			for _, fact := range expected.Published {
				state, err := repo.load(c, fact.DocumentID)
				if err != nil {
					return err
				}
				current.Documents = append(current.Documents, state)
			}
			if queryread.MatchCurrentReview(scope, expected, current, policies) != nil {
				return queryread.ErrCheck
			}
			if err := repo.prefetchSnapshots(c, expected.Published); err != nil {
				return err
			}
			for _, fact := range expected.Published {
				if len(fact.GenerationSet.Channels) != 1 || fact.GenerationSet.Channels[contracts.KeywordChannel].ID == "" {
					return queryread.ErrCheck
				}
				snapshot, err := readPublishedSnapshot(c, repo, fact)
				if err != nil {
					return err
				}
				for _, r := range snapshot.Records {
					raw, err := json.Marshal(r)
					count++
					if err != nil || len(raw) > remaining || count > 10000 {
						return queryread.ErrCheck
					}
					remaining -= len(raw)
				}
				for _, e := range groups[fact.DocumentID] {
					matched := false
					for _, r := range snapshot.Records {
						if reflect.DeepEqual(r.Citation, e.Citation) && strings.Contains(r.Chunk.Text, e.SourceExcerpt) {
							matched = true
							break
						}
					}
					if !matched {
						return queryread.ErrCheck
					}
				}
			}
			// Both inspections freshly read all frozen bodies in their own view.
			// Quote verification also checks both hashes; it need not rehash the
			// same body a second time within that view. Original projection calls
			// below continue to use independent PublishedFrozenReader passes.
			return repo.frozenRevisions(c, expected.Published, func(fact contracts.PublishedFact, payload []byte) error {
				revision := byID[fact.DocumentID].PublicationReview.Revision
				q := contracts.FrozenContentReadRequest{Request: request, Scope: scope, DocumentID: fact.DocumentID, RevisionID: fact.RevisionID}
				reader := frozenReadView{ctx: c, request: q, record: contracts.FrozenContentRecord{DocumentID: fact.DocumentID, RevisionID: fact.RevisionID, Payload: payload}}
				var quotes []contracts.Evidence
				for _, e := range groups[fact.DocumentID] {
					switch e.Citation.Source.Kind {
					case "revision":
						quotes = append(quotes, e)
					case "original":
					default:
						return queryread.ErrCheck
					}
				}
				if len(quotes) > 0 {
					return contentverify.VerifyTextEvidence(c, reader, q, revision, quotes)
				}
				return contentverify.VerifyFrozen(c, reader, q, revision)
			})
		})
	}
	if inspect() != nil {
		return queryread.ErrCheck
	}
	reader := PublishedFrozenReader{Reads: p.Reads}
	bounded := &evidenceBlobBudget{BlobStore: p.Originals, remaining: 16 << 20}
	for _, fact := range expected.Published {
		r := byID[fact.DocumentID].PublicationReview.Revision
		q := contracts.FrozenContentReadRequest{Request: request, Scope: scope, DocumentID: fact.DocumentID, RevisionID: fact.RevisionID}
		originals := map[contracts.ID][]contracts.Evidence{}
		for _, e := range groups[fact.DocumentID] {
			switch e.Citation.Source.Kind {
			case "revision":
				// Already checked against actual bytes in inspect's current view.
			case "original":
				originals[e.Citation.Source.BlobID] = append(originals[e.Citation.Source.BlobID], e)
			default:
				return queryread.ErrCheck
			}
		}
		// Sixteen MiB of original reads across the entire batch, including
		// the second post-projection pass. Each file remains capped at 8MiB.
		for _, batch := range originals {
			if contentverify.VerifyOriginalTextEvidence(ctx, reader, bounded, p.OriginalControl, p.Projector, q, r, batch) != nil {
				return queryread.ErrCheck
			}
		}
	}
	if inspect() != nil || ctx.Err() != nil {
		return queryread.ErrCheck
	}
	return nil
}

type evidenceBlobBudget struct {
	contracts.BlobStore
	remaining int64
}

func (b *evidenceBlobBudget) Open(c context.Context, q contracts.BlobReadRequest) (io.ReadCloser, error) {
	if b.BlobStore == nil || q.Blob.Size < 0 || q.Blob.Size > b.remaining {
		return nil, queryread.ErrCheck
	}
	b.remaining -= q.Blob.Size
	return b.BlobStore.Open(c, q)
}

func readPublishedSnapshot(ctx context.Context, repo *queryRepository, fact contracts.PublishedFact) (contracts.IndexSnapshot, error) {
	var empty contracts.IndexSnapshot
	g := fact.GenerationSet.Channels[contracts.KeywordChannel]
	if snapshot, ok := repo.snapshots[g.ID]; ok {
		prepared, ok := repo.prepared[fact.DocumentID]
		if !ok || !reflect.DeepEqual(prepared.artifact.GenerationSet, fact.GenerationSet) {
			return empty, ErrRepository
		}
		return copyQuery(snapshot)
	}
	var planRaw, artifactRaw []byte
	if repo.tx.QueryRowContext(ctx, `SELECT i.payload,a.payload FROM akos_withdraw_v1.publication_prepared_artifacts a JOIN akos_withdraw_v1.publication_intents i ON i.id=a.operation_id WHERE a.space_id=$1::text AND a.kb_id=$2::text AND a.generation_set_id=$3::text AND a.generation_id=$4::text`, string(repo.scope.SpaceID), string(fact.KnowledgeBaseID), string(fact.GenerationSet.ID), string(g.ID)).Scan(&planRaw, &artifactRaw) != nil || len(planRaw) > 262144 || len(artifactRaw) > 1<<20 {
		return empty, ErrRepository
	}
	var plan document.PublicationStartPlan
	var artifact preparation.Artifact
	if json.Unmarshal(planRaw, &plan) != nil || json.Unmarshal(artifactRaw, &artifact) != nil || !reflect.DeepEqual(artifact.GenerationSet, fact.GenerationSet) || plan.SpaceID != fact.SpaceID || plan.KnowledgeBaseID != fact.KnowledgeBaseID || plan.Result.Operation.DocumentID != fact.DocumentID || plan.Result.Operation.RevisionID != fact.RevisionID || artifact.OperationID != plan.Result.Operation.ID {
		return empty, ErrRepository
	}
	if digest, err := preparation.ArtifactDigest(artifact); err != nil || digest != artifact.Digest {
		return empty, ErrRepository
	}
	snapshot, err := loadPreparationSnapshot(ctx, repo.tx, plan, artifact, false, false)
	if err != nil || snapshot.Checksum != artifact.SnapshotDigest {
		return empty, ErrRepository
	}
	return snapshot, nil
}
