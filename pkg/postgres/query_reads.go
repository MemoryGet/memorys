package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/preparation"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// QueryReads provides real, primary-database, read-only Repeatable Read views.
// Its inventory covers current publications made by the native finalizer. Old
// fixture sources without verifiable approval/ready metadata never enter it.
// Readiness here is metadata; callers must verify actual backend effects.
type QueryReads struct {
	DB     *sql.DB
	Verify VerifyScope
}

var _ contracts.QueryReadUnit = QueryReads{}

func validQueryScope(s contracts.AccessScope) bool {
	if !contracts.IsCurrentContentReadAction(s.Action) || s.PolicyVersion == 0 || !s.ExpiresAt.After(time.Now()) || !commandID(string(s.ScopeID)) || !commandID(string(s.SubjectID)) || !commandID(string(s.SpaceID)) || len(s.KnowledgeBaseIDs) < 1 || len(s.KnowledgeBaseIDs) > 256 {
		return false
	}
	seen := map[contracts.ID]bool{}
	for _, kb := range s.KnowledgeBaseIDs {
		if !commandID(string(kb)) || seen[kb] {
			return false
		}
		seen[kb] = true
	}
	return true
}
func (p QueryReads) WithinRead(ctx context.Context, scope contracts.AccessScope, fn func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
	if fn == nil {
		return queryread.ErrInvalid
	}
	return p.withinQuery(ctx, scope, func(c context.Context, id contracts.QueryReadIdentity, repo *queryRepository) error {
		guard, err := queryread.NewGuard(c, repo, repo.scope, id)
		if err != nil {
			return err
		}
		defer guard.Close()
		return fn(c, id, guard)
	})
}

// withinQuery keeps adapter-private SQL and authority metadata in one read view.
// Native repositories must not escape this callback.
func (p QueryReads) withinQuery(ctx context.Context, scope contracts.AccessScope, fn func(context.Context, contracts.QueryReadIdentity, *queryRepository) error) error {
	if ctx == nil || p.DB == nil || p.Verify == nil || fn == nil || !validQueryScope(scope) {
		return queryread.ErrInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return queryread.ErrInvalid
	}
	scope = cloneScope(scope)
	ctx, cancel := context.WithDeadline(ctx, scope.ExpiresAt)
	defer cancel()
	verify := func(c context.Context) error {
		if c.Err() != nil || p.Verify(c, cloneScope(scope)) != nil || c.Err() != nil {
			return queryread.ErrRead
		}
		return nil
	}
	if verify(ctx) != nil {
		return queryread.ErrRead
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return queryread.ErrRead
	}
	defer tx.Rollback()
	var epoch contracts.ID
	var snapshot, storeVersion string
	if tx.QueryRowContext(ctx, `SELECT store_epoch_id,row_version::text,pg_current_snapshot()::text FROM akos_withdraw_v1.query_store_head WHERE singleton`).Scan(&epoch, &storeVersion, &snapshot) != nil || !commandID(string(epoch)) {
		return queryread.ErrRead
	}
	readID, err := submissionID("query-view-")
	if err != nil {
		return queryread.ErrRead
	}
	sum := sha256.Sum256([]byte(string(epoch) + "\x00" + storeVersion + "\x00" + snapshot))
	identity := contracts.QueryReadIdentity{StoreEpochID: epoch, ReadVersion: string(readID), Watermark: hex.EncodeToString(sum[:])}
	repo := &queryRepository{tx: tx, scope: scope, identity: identity, verify: verify, cursors: map[string]queryCursor{}, states: map[contracts.ID]contracts.CurrentDocumentState{}}
	err = fn(ctx, identity, repo)
	if err != nil {
		return err
	}
	if verify(ctx) != nil {
		return queryread.ErrRead
	}
	if tx.Commit() != nil {
		return queryread.ErrRead
	}
	// This second statement is outside the old view and after its connection is
	// released, so a one-connection pool cannot deadlock the rotation check.
	var current contracts.ID
	var version string
	if p.DB.QueryRowContext(ctx, `SELECT store_epoch_id,row_version::text FROM akos_withdraw_v1.query_store_head WHERE singleton`).Scan(&current, &version) != nil || current != epoch || version != storeVersion || verify(ctx) != nil {
		return queryread.ErrRead
	}
	return nil
}

type queryCursor struct {
	after contracts.ID
	kbs   string
}
type queryRepository struct {
	tx         *sql.Tx
	scope      contracts.AccessScope
	identity   contracts.QueryReadIdentity
	verify     func(context.Context) error
	cursors    map[string]queryCursor
	states     map[contracts.ID]contracts.CurrentDocumentState
	facts      []contracts.PublishedFact
	loaded     bool
	stateBytes int
	prepared   map[contracts.ID]preparedQueryArtifact
	snapshots  map[contracts.ID]contracts.IndexSnapshot
}

// Verified immutable preparation metadata belongs to one SQL read view only.
type preparedQueryArtifact struct {
	plan     document.PublicationStartPlan
	artifact preparation.Artifact
}

func copyQuery[T any](v T) (T, error) {
	var result T
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > 4<<20 {
		return result, queryread.ErrLimit
	}
	if json.Unmarshal(raw, &result) != nil {
		return result, queryread.ErrRead
	}
	return result, nil
}
func queryKBs(ids []contracts.ID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return out
}

// takeState owns a fresh adapter-decoded value. Only load/batch return copies;
// private preparation metadata may share it inside this immutable SQL view.
func (r *queryRepository) takeState(id contracts.ID, state contracts.CurrentDocumentState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return queryread.ErrRead
	}
	if len(raw) > 4<<20-r.stateBytes {
		return queryread.ErrLimit
	}
	r.states[id] = state
	r.stateBytes += len(raw)
	return nil
}
func (r *queryRepository) allowed(kb contracts.ID) bool {
	for _, v := range r.scope.KnowledgeBaseIDs {
		if kb == v {
			return true
		}
	}
	return false
}

func (r *queryRepository) backend(ctx context.Context, kb contracts.ID) (document.PublicationBackendSnapshot, error) {
	var b document.PublicationBackendSnapshot
	var raw []byte
	var id string
	if !r.allowed(kb) || r.tx.QueryRowContext(ctx, `SELECT e.payload,h.backend_epoch_id FROM akos_withdraw_v1.publication_backend_heads h JOIN akos_withdraw_v1.publication_backend_epochs e ON e.space_id=h.space_id AND e.kb_id=h.kb_id AND e.id=h.backend_epoch_id WHERE h.space_id=$1::text AND h.kb_id=$2::text`, string(r.scope.SpaceID), string(kb)).Scan(&raw, &id) != nil || len(raw) > 65536 || json.Unmarshal(raw, &b) != nil || string(b.EpochID) != id || b.Number == 0 || !commandID(b.CutoverWatermark) || !commandDigest(b.ProfileDigest) {
		return b, queryread.ErrRead
	}
	return b, nil
}

func (r *queryRepository) load(ctx context.Context, id contracts.ID) (contracts.CurrentDocumentState, error) {
	if state, present := r.states[id]; present {
		return copyAdmittedState(state), nil
	}
	if err := r.prefetch(ctx, []contracts.ID{id}); err != nil {
		return contracts.CurrentDocumentState{}, err
	}
	return copyAdmittedState(r.states[id]), nil
}

func (r *queryRepository) inventory(ctx context.Context) error {
	if r.loaded {
		return nil
	}
	rows, err := r.tx.QueryContext(ctx, `SELECT id FROM akos_withdraw_v1.documents WHERE space_id=$1::text AND kb_id=ANY($2::text[]) AND NOT deleted AND active_revision_id IS NOT NULL ORDER BY id COLLATE "C" LIMIT 1001`, string(r.scope.SpaceID), queryKBs(r.scope.KnowledgeBaseIDs))
	if err != nil {
		return queryread.ErrRead
	}
	var ids []contracts.ID
	for rows.Next() {
		var id contracts.ID
		if rows.Scan(&id) != nil {
			rows.Close()
			return queryread.ErrRead
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return queryread.ErrRead
	}
	if len(ids) > 1000 {
		return queryread.ErrLimit
	}
	var facts []contracts.PublishedFact
	if len(ids) > 0 {
		if err := r.prefetch(ctx, ids); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		s, err := r.load(ctx, id)
		if err != nil {
			return err
		}
		if s.GenerationSet == nil || s.PublicationReview == nil || s.PublicationReview.Validity.State != "effective" || s.ActiveBackendEpochID != s.GenerationSet.BackendEpochID {
			continue
		}
		d, review := s.Document, s.PublicationReview
		facts = append(facts, contracts.PublishedFact{DocumentID: d.ID, SpaceID: d.SpaceID, KnowledgeBaseID: d.KnowledgeBaseID, RevisionID: *d.ActiveRevisionID, RowVersion: d.RowVersion, PublicationEpoch: d.PublicationEpoch, ApprovalID: review.Approval.ID, ContentDigest: review.Revision.ContentHash, GenerationSet: *s.GenerationSet})
	}
	owned, err := copyQuery(facts)
	if err != nil {
		return err
	}
	r.facts = owned
	r.loaded = true
	return nil
}

func (r *queryRepository) ReadPublishedPage(ctx context.Context, q contracts.PublishedPageRequest) (contracts.PublishedPage, error) {
	var page contracts.PublishedPage
	if r.verify(ctx) != nil || q.Identity != r.identity || contracts.ValidatePublishedPage(contracts.PublishedPage{Identity: r.identity}, q, r.scope, "") != nil {
		return page, queryread.ErrRead
	}
	if err := r.inventory(ctx); err != nil {
		return page, err
	}
	kbs := append([]contracts.ID(nil), q.KnowledgeBaseIDs...)
	sort.Slice(kbs, func(i, j int) bool { return kbs[i] < kbs[j] })
	raw, _ := json.Marshal(kbs)
	binding := string(raw)
	var after contracts.ID
	if q.Cursor != "" {
		cursor, ok := r.cursors[q.Cursor]
		if !ok || cursor.kbs != binding {
			return page, queryread.ErrInvalid
		}
		after = cursor.after
	}
	selected := map[contracts.ID]bool{}
	for _, kb := range kbs {
		selected[kb] = true
	}
	page.Identity = r.identity
	for _, f := range r.facts {
		if f.DocumentID <= after || !selected[f.KnowledgeBaseID] {
			continue
		}
		if len(page.Facts) == q.Limit {
			if len(r.cursors) >= 1000 {
				return contracts.PublishedPage{}, queryread.ErrLimit
			}
			cursor, err := submissionID("query-cursor-")
			if err != nil {
				return contracts.PublishedPage{}, queryread.ErrRead
			}
			page.NextCursor = string(cursor)
			r.cursors[page.NextCursor] = queryCursor{after: page.Facts[len(page.Facts)-1].DocumentID, kbs: binding}
			break
		}
		page.Facts = append(page.Facts, f)
	}
	if contracts.ValidatePublishedPage(page, q, r.scope, after) != nil {
		return contracts.PublishedPage{}, queryread.ErrRead
	}
	return copyQuery(page)
}
func (r *queryRepository) LoadCurrentDocument(ctx context.Context, id contracts.ID) (contracts.CurrentDocumentState, error) {
	if r.verify(ctx) != nil {
		return contracts.CurrentDocumentState{}, queryread.ErrRead
	}
	return r.load(ctx, id)
}
func (r *queryRepository) LoadActiveEpoch(ctx context.Context, kb contracts.ID) (contracts.BackendEpoch, error) {
	var epoch contracts.BackendEpoch
	if r.verify(ctx) != nil {
		return epoch, queryread.ErrRead
	}
	b, err := r.backend(ctx, kb)
	if err != nil {
		return epoch, err
	}
	if err := r.inventory(ctx); err != nil {
		return epoch, err
	}
	epoch = contracts.BackendEpoch{ID: b.EpochID, KnowledgeBaseID: kb, Number: b.Number, IndexProfile: b.IndexProfile, ProfileDigest: b.ProfileDigest, State: "active", CutoverWatermark: b.CutoverWatermark, RevisionGenerationSets: map[contracts.ID]contracts.ID{}}
	for _, f := range r.facts {
		if f.KnowledgeBaseID == kb {
			if f.GenerationSet.BackendEpochID != b.EpochID {
				return contracts.BackendEpoch{}, queryread.ErrRead
			}
			epoch.RevisionGenerationSets[f.RevisionID] = f.GenerationSet.ID
		}
	}
	return copyQuery(epoch)
}
