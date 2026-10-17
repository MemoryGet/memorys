package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// HistoricalFrozenReader reads only previously published, still approved
// targets while the document has a live effective publication. Old search
// indexes are not required. Both explicit resource grants are reauthorized.
type HistoricalFrozenReader struct {
	Reads      QueryReads
	Authorizer contracts.Authorizer
}

func (p HistoricalFrozenReader) ReadHistoricalContent(ctx context.Context, input contracts.HistoricalContentReadRequest) (contracts.HistoricalContentRecord, error) {
	var empty contracts.HistoricalContentRecord
	q, err := copyQuery(input)
	if err != nil || queryread.ValidateHistoricalScopes(ctx, q) != nil || p.Authorizer == nil || !commandID(string(q.Base.DocumentID)) || !commandID(string(q.Base.RevisionID)) || !commandDigest(q.Base.Request.ProfileDigest) {
		return empty, queryread.ErrRead
	}
	ctx, cancel := context.WithDeadline(ctx, q.Base.Request.Deadline)
	defer cancel()
	ctx, expire := context.WithDeadline(ctx, q.HistoryScope.ExpiresAt)
	defer expire()
	ctx, expireRead := context.WithDeadline(ctx, q.Base.Scope.ExpiresAt)
	defer expireRead()
	authorize := func() error {
		for _, scope := range []contracts.AccessScope{q.Base.Scope, q.HistoryScope} {
			who, e := copyQuery(q.Principal)
			if e != nil {
				return e
			}
			fresh, e := p.Authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: scope.Action, SpaceID: scope.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), scope.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{q.Base.DocumentID}})
			if e != nil || ctx.Err() != nil || fresh.ScopeID == "" || fresh.SubjectID != scope.SubjectID || fresh.Action != scope.Action || fresh.SpaceID != scope.SpaceID || fresh.PolicyVersion != scope.PolicyVersion || !reflect.DeepEqual(fresh.KnowledgeBaseIDs, scope.KnowledgeBaseIDs) || !fresh.ExpiresAt.After(time.Now()) {
				return queryread.ErrRead
			}
		}
		return nil
	}
	load := func(withBody bool) (contracts.HistoricalContentRecord, error) {
		var out contracts.HistoricalContentRecord
		if authorize() != nil {
			return empty, queryread.ErrRead
		}
		err := p.Reads.withinQuery(ctx, q.Base.Scope, func(c context.Context, identity contracts.QueryReadIdentity, repo *queryRepository) error {
			state, e := repo.load(c, q.Base.DocumentID)
			if e != nil {
				return e
			}
			current, e := queryread.PublishedCurrentFact(q.Base.Scope, identity, state)
			if e != nil || current.RevisionID == q.Base.RevisionID {
				return queryread.ErrRead
			}
			var payload []byte
			var epoch string
			e = repo.tx.QueryRowContext(c, `SELECT h.id,h.publication_epoch::text,i.payload FROM akos_withdraw_v1.publication_finalizations f
 JOIN akos_withdraw_v1.publication_history h ON h.id=f.history_id AND h.operation_id=f.operation_id
 JOIN akos_withdraw_v1.publication_intents i ON i.id=f.operation_id
 WHERE f.space_id=$1 AND f.kb_id=$2 AND f.document_id=$3 AND h.action='publish' AND h.revision_id=$4 ORDER BY h.publication_epoch DESC LIMIT 1`, q.Base.Scope.SpaceID, current.KnowledgeBaseID, q.Base.DocumentID, q.Base.RevisionID).Scan(&out.PublicationID, &epoch, &payload)
			if e != nil || len(payload) > 262144 {
				return queryread.ErrRead
			}
			out.PublicationEpoch, e = strconv.ParseUint(epoch, 10, 64)
			if e != nil {
				return queryread.ErrRead
			}
			var withdrawn bool
			if repo.tx.QueryRowContext(c, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_history WHERE space_id=$1 AND kb_id=$2 AND document_id=$3 AND previous_revision_id=$4 AND publication_epoch>$5::numeric AND action IN('withdraw','delete'))`, q.Base.Scope.SpaceID, current.KnowledgeBaseID, q.Base.DocumentID, q.Base.RevisionID, epoch).Scan(&withdrawn) != nil || withdrawn {
				return queryread.ErrRead
			}
			var plan document.PublicationStartPlan
			if json.Unmarshal(payload, &plan) != nil || plan.SpaceID != current.SpaceID || plan.KnowledgeBaseID != current.KnowledgeBaseID || plan.Result.Operation.DocumentID != q.Base.DocumentID || plan.Result.Operation.RevisionID != q.Base.RevisionID {
				return queryread.ErrRead
			}
			out.Review, e = repo.loadReview(c, current.SpaceID, current.KnowledgeBaseID, q.Base.DocumentID, q.Base.RevisionID, plan.Result.Operation.ApprovalID, plan.Review)
			if e != nil {
				return e
			}
			out.Content = contracts.FrozenContentRecord{DocumentID: q.Base.DocumentID, RevisionID: q.Base.RevisionID}
			out.Current, out.StoreEpochID = current, identity.StoreEpochID
			if queryread.ValidateHistoricalContent(q, out, out.Review.Validity.ReviewPolicyVersion) != nil {
				return queryread.ErrRead
			}
			if withBody {
				_, out.Content.Payload, e = readFrozenRevision(c, repo.tx, current.SpaceID, current.KnowledgeBaseID, out.Review.Revision)
				if e != nil {
					return e
				}
			}
			return nil
		})
		if err != nil || authorize() != nil {
			return empty, queryread.ErrRead
		}
		return out, nil
	}
	result, err := load(true)
	if err != nil {
		return empty, queryread.ErrRead
	}
	fresh, err := load(false)
	if err != nil {
		return empty, queryread.ErrRead
	}
	expected := result
	expected.Content.Payload = nil
	if !reflect.DeepEqual(expected, fresh) || ctx.Err() != nil {
		return empty, queryread.ErrRead
	}
	return result, nil
}

var _ contracts.HistoricalContentReader = HistoricalFrozenReader{}
