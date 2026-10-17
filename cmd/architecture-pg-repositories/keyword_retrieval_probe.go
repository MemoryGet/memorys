package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/preparation"
	"github.com/MemoryGet/memorys/pkg/publicationprojection"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

var keywordRetrievalSamples []map[string]any
var publishedFrozenSamples []map[string]any

// Authority is deliberately fixture-supplied. The retriever still reads real
// platform publication/approval heads and actual immutable index rows.
func verifyKeywordRetrieval(ctx context.Context, db *sql.DB, scope contracts.AccessScope, verify postgres.VerifyScope, originals contracts.BlobStore) (func(contracts.ID) error, error) {
	unit := postgres.QueryReads{DB: db, Verify: verify}
	var facts []contracts.PublishedFact
	err := unit.WithinRead(ctx, scope, func(c context.Context, id contracts.QueryReadIdentity, repo contracts.QueryReadRepository) error {
		var err error
		facts, err = queryread.Collect(c, repo, scope, id, []contracts.ID{"kb"}, queryread.Limits{PageSize: 1, MaxPages: 1000, MaxFacts: 1000, MaxGenerations: 3000})
		return err
	})
	if err != nil || len(facts) < 3 {
		return nil, errors.New("published keyword fixture inventory missing")
	}
	var actual []contracts.IndexProjection
	for _, f := range facts {
		rows, err := db.QueryContext(ctx, `SELECT payload FROM akos_withdraw_v1.index_projection_rows WHERE generation_id=$1 ORDER BY chunk_id`, string(f.GenerationSet.Channels[contracts.KeywordChannel].ID))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var raw []byte
			var r contracts.IndexProjection
			if rows.Scan(&raw) != nil || json.Unmarshal(raw, &r) != nil || r.Published {
				rows.Close()
				return nil, errors.New("invalid immutable prepared row")
			}
			actual = append(actual, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if len(actual) == 0 {
		return nil, errors.New("actual search rows missing")
	}
	deadline, _ := ctx.Deadline()
	if deadline.After(scope.ExpiresAt) {
		deadline = scope.ExpiresAt
	}
	term := ""
	for _, record := range actual {
		if strings.TrimSpace(record.Chunk.Text) != "" {
			term = record.Chunk.Text
			break
		}
	}
	if term == "" {
		return nil, errors.New("no nonblank actual keyword term")
	}
	snapshot, err := referenceQuerySnapshot(1)
	if err != nil {
		return nil, err
	}
	authorized := map[contracts.Digest][]byte{}
	makeRequest := func(selected []contracts.PublishedFact) (contracts.RetrieveRequest, error) {
		q := contracts.RetrieveRequest{Request: contracts.RequestContext{RequestID: "keyword-request", ProfileDigest: snapshot.Pipeline.Digest, Deadline: deadline}, Scope: scope, Query: contracts.Query{Text: term}, BackendEpochID: selected[0].GenerationSet.BackendEpochID, RouteID: "keyword-route", Channels: []contracts.Channel{contracts.KeywordChannel}, Limit: 1000}
		r := publicationprojection.Record{SchemaVersion: 1, ProjectionID: "keyword-projection", RequestID: q.Request.RequestID, InvocationID: "keyword-invocation", ParentScopeID: "reader-parent", ChildScopeID: scope.ScopeID, SpaceID: scope.SpaceID, KnowledgeBaseID: "kb", BackendEpochID: q.BackendEpochID, RouteID: q.RouteID, ModuleInstanceID: "postgres-keyword-reader", ParentPublicationDigest: contracts.Digest(strings.Repeat("b", 64)), QueryProfileDigest: snapshot.Query.Digest, PipelineDigest: q.Request.ProfileDigest, PolicyVersion: scope.PolicyVersion, Channels: q.Channels, ExpiresAtUnixNS: deadline.UnixNano()}
		q.Publication = contracts.PublicationFilter{SpaceID: scope.SpaceID, KnowledgeBaseIDs: []contracts.ID{"kb"}}
		for _, fact := range selected {
			g := fact.GenerationSet.Channels[contracts.KeywordChannel]
			r.Entries = append(r.Entries, publicationprojection.Entry{DocumentID: fact.DocumentID, RevisionID: fact.RevisionID, PublicationVersion: fact.PublicationEpoch, GenerationSetID: fact.GenerationSet.ID, GenerationID: g.ID, Channel: contracts.KeywordChannel, IndexProfileDigest: g.ProfileDigest})
			q.Generations = append(q.Generations, g)
			q.Publication.RevisionIDs = append(q.Publication.RevisionIDs, fact.RevisionID)
			q.Publication.GenerationIDs = append(q.Publication.GenerationIDs, g.ID)
		}
		sort.Slice(q.Publication.RevisionIDs, func(i, j int) bool { return q.Publication.RevisionIDs[i] < q.Publication.RevisionIDs[j] })
		sort.Slice(q.Publication.GenerationIDs, func(i, j int) bool { return q.Publication.GenerationIDs[i] < q.Publication.GenerationIDs[j] })
		raw, err := json.Marshal(r)
		if err != nil {
			return q, err
		}
		_, q.Publication.SnapshotDigest, err = publicationprojection.ValidatedDigest(raw)
		if err == nil {
			authorized[q.Publication.SnapshotDigest] = raw
		}
		return q, err
	}
	q, err := makeRequest(facts)
	if err != nil {
		return nil, err
	}
	frozenReader := postgres.PublishedFrozenReader{Reads: unit}
	frozenRequests := map[contracts.ID]contracts.FrozenContentReadRequest{}
	for _, fact := range facts {
		request := contracts.FrozenContentReadRequest{Request: q.Request, Scope: scope, DocumentID: fact.DocumentID, RevisionID: fact.RevisionID}
		var state contracts.CurrentDocumentState
		if err := unit.WithinRead(ctx, scope, func(c context.Context, id contracts.QueryReadIdentity, repo contracts.QueryReadRepository) error {
			var err error
			state, err = repo.LoadCurrentDocument(c, fact.DocumentID)
			return err
		}); err != nil {
			return nil, err
		}
		record, err := frozenReader.ReadFrozenContent(ctx, request)
		if err != nil || state.PublicationReview == nil {
			return nil, errors.New("published native frozen content unavailable")
		}
		if err := contentverify.VerifyFrozen(ctx, frozenReader, request, state.PublicationReview.Revision); err != nil {
			return nil, errors.New("actual frozen dual hashes rejected")
		}
		wrongHash := state.PublicationReview.Revision
		wrongHash.ContentHash = contracts.Digest(strings.Repeat("0", 64))
		if err := contentverify.VerifyFrozen(ctx, frozenReader, request, wrongHash); err == nil {
			return nil, errors.New("wrong expected content hash accepted")
		}
		fixedPayload := append([]byte(nil), record.Payload...)
		record.Payload[0] = '!'
		record, err = frozenReader.ReadFrozenContent(ctx, request)
		if err != nil || !bytes.Equal(record.Payload, fixedPayload) {
			return nil, errors.New("frozen read aliases prior return value")
		}
		publishedFrozenSamples = append(publishedFrozenSamples, map[string]any{"request": request, "record": record, "expected": state.PublicationReview.Revision})
		frozenRequests[fact.DocumentID] = request
	}
	firstRead := frozenRequests[facts[0].DocumentID]
	for _, mutate := range []func(*contracts.FrozenContentReadRequest){
		func(r *contracts.FrozenContentReadRequest) { r.Scope.SpaceID = "outside-space" },
		func(r *contracts.FrozenContentReadRequest) { r.RevisionID = facts[1].RevisionID },
		func(r *contracts.FrozenContentReadRequest) { r.Scope.Action = "document.edit" },
		func(r *contracts.FrozenContentReadRequest) { r.DocumentID = "final-config-stale-document" },
	} {
		bad := firstRead
		mutate(&bad)
		if got, e := frozenReader.ReadFrozenContent(ctx, bad); e == nil || got.Payload != nil || got.DocumentID != "" {
			return nil, errors.New("unscoped/mismatched frozen read returned data")
		}
	}
	if got, e := frozenReader.ReadFrozenContent(context.Background(), firstRead); e == nil || got.Payload != nil {
		return nil, errors.New("unbounded frozen read admitted")
	}
	cancelledRead, cancelRead := context.WithCancel(ctx)
	cancelRead()
	if got, e := frozenReader.ReadFrozenContent(cancelledRead, firstRead); e == nil || got.Payload != nil {
		return nil, errors.New("cancelled frozen read returned data")
	}
	readCalls := 0
	lateDenied := postgres.PublishedFrozenReader{Reads: postgres.QueryReads{DB: db, Verify: func(c context.Context, s contracts.AccessScope) error {
		readCalls++
		if readCalls >= 4 {
			return errors.New("late frozen read authorization denied")
		}
		return verify(c, s)
	}}}
	if got, e := lateDenied.ReadFrozenContent(ctx, firstRead); e == nil || got.Payload != nil || readCalls < 4 {
		return nil, errors.New("late denial returned frozen bytes")
	}
	deny, scopeDenied := false, false
	authority := func(c context.Context, r contracts.RetrieveRequest) ([]byte, publicationprojection.CallIdentity, error) {
		if c.Err() != nil || deny || authorized[r.Publication.SnapshotDigest] == nil {
			return nil, publicationprojection.CallIdentity{}, errors.New("fixture invocation denied")
		}
		return authorized[r.Publication.SnapshotDigest], publicationprojection.CallIdentity{InvocationID: "keyword-invocation", ModuleInstanceID: "postgres-keyword-reader", Limit: r.Limit}, nil
	}
	reader := postgres.KeywordRetriever{Reads: postgres.QueryReads{DB: db, Verify: func(c context.Context, s contracts.AccessScope) error {
		if scopeDenied {
			return errors.New("current read authorization denied")
		}
		return verify(c, s)
	}}, ModuleInstanceID: "postgres-keyword-reader", Authorize: authority}
	runtime, assembled, err := assembleNativeReader(ctx, db, snapshot, reader)
	if err != nil {
		return nil, err
	}
	defer runtime.Close(context.Background())
	results, err := assembled.Search(ctx, q)

	if err != nil || len(results) == 0 {
		return nil, errors.New("actual published keyword search failed")
	}
	identity, err := runtime.Identity()
	if err != nil {
		return nil, err
	}
	if err := runtime.Close(context.Background()); err != nil {
		return nil, err
	}
	if _, err := assembled.Search(ctx, q); err == nil {
		return nil, errors.New("closed assembled reader still searched")
	}
	profileAssemblySamples = append(profileAssemblySamples, map[string]any{"profile": snapshot, "stage": "query", "identity": identity, "candidates": results, "request_profile_digest": q.Request.ProfileDigest, "route": "keyword-route", "closed_reference_rejected": true})
	requestWire, err := modulewire.EncodeRetrieveRequest(modulewire.Call{InvocationID: "keyword-invocation", ModuleInstanceID: reader.ModuleInstanceID}, q)
	if err != nil {
		return nil, err
	}
	resultWire, err := modulewire.EncodeRetrieveResult(q.Request.RequestID, "keyword-invocation", results)
	if err != nil {
		return nil, err
	}
	decoded, err := modulewire.DecodeRetrieveResult(resultWire, q.Request.RequestID, "keyword-invocation", q)
	if err != nil || !reflect.DeepEqual(decoded, results) {
		return nil, errors.New("actual SQL candidates failed module contract")
	}
	keywordRetrievalSamples = append(keywordRetrievalSamples, map[string]any{"request": q, "projection": json.RawMessage(authorized[q.Publication.SnapshotDigest]), "candidates": results, "actual_rows": actual, "request_wire": json.RawMessage(requestWire), "result_wire": json.RawMessage(resultWire)})
	limited := q
	limited.Limit = 1
	if got, e := reader.Search(ctx, limited); e != nil || len(got) != 1 || !reflect.DeepEqual(got[0], results[0]) {
		return nil, errors.New("SQL ranking/limit is unstable")
	}
	for _, term := range []string{"unlikely-no-hit-9247984", "' OR 1=1 --", "%_'"} {
		literal := q
		literal.Query.Text = term
		if got, e := reader.Search(ctx, literal); e != nil || len(got) != 0 {
			return nil, errors.New("literal SQL search broadened input")
		}
	}
	for _, mutate := range []func(*contracts.RetrieveRequest){
		func(r *contracts.RetrieveRequest) {
			r.Publication.SnapshotDigest = contracts.Digest(strings.Repeat("d", 64))
		},
		func(r *contracts.RetrieveRequest) { r.Scope.SpaceID = "outside-space" },
		func(r *contracts.RetrieveRequest) { r.RouteID = "outside-route" },
		func(r *contracts.RetrieveRequest) { r.BackendEpochID = "outside-epoch" },
		func(r *contracts.RetrieveRequest) { r.Publication.GenerationIDs = []contracts.ID{"draft-generation"} },
		func(r *contracts.RetrieveRequest) { r.Channels = []contracts.Channel{contracts.VectorChannel} },
	} {
		bad := q
		mutate(&bad)
		if got, e := reader.Search(ctx, bad); !errors.Is(e, postgres.ErrKeywordSearch) || got != nil {
			return nil, errors.New("invalid search projection admitted")
		}
	}
	deny = true
	if got, e := reader.Search(ctx, q); e == nil || got != nil {
		return nil, errors.New("denied invocation returned candidates")
	}
	deny = false
	scopeDenied = true
	if got, e := reader.Search(ctx, q); e == nil || got != nil {
		return nil, errors.New("denied scope returned candidates")
	}
	scopeDenied = false
	// Supply a fixture-authorized, structurally valid projection for an actual
	// completed preparation that never published. The primary head must win.
	var draftRaw []byte
	var draftArtifact preparation.Artifact
	if db.QueryRowContext(ctx, `SELECT a.payload FROM akos_withdraw_v1.publication_prepared_artifacts a JOIN akos_withdraw_v1.publication_intents i ON i.id=a.operation_id WHERE i.document_id='final-config-stale-document'`).Scan(&draftRaw) != nil || json.Unmarshal(draftRaw, &draftArtifact) != nil {
		return nil, errors.New("unpublished actual preparation missing")
	}
	draft, err := makeRequest([]contracts.PublishedFact{{DocumentID: "final-config-stale-document", RevisionID: draftArtifact.GenerationSet.RevisionID, PublicationEpoch: 1, GenerationSet: draftArtifact.GenerationSet}})
	if err != nil {
		return nil, err
	}
	if got, e := reader.Search(ctx, draft); e == nil || got != nil {
		return nil, errors.New("fixture projection bypassed actual unpublished head")
	}
	draftRead := firstRead
	draftRead.DocumentID, draftRead.RevisionID = "final-config-stale-document", draftArtifact.GenerationSet.RevisionID
	if got, e := frozenReader.ReadFrozenContent(ctx, draftRead); e == nil || got.Payload != nil {
		return nil, errors.New("unpublished actual frozen revision readable")
	}
	finalDenied := reader
	calls := 0
	finalDenied.Authorize = func(c context.Context, r contracts.RetrieveRequest) ([]byte, publicationprojection.CallIdentity, error) {
		calls++
		if calls == 2 {
			return nil, publicationprojection.CallIdentity{}, errors.New("final invocation revoked")
		}
		return authority(c, r)
	}
	if got, e := finalDenied.Search(ctx, q); e == nil || got != nil || calls != 2 {
		return nil, errors.New("final invocation denial returned SQL candidates")
	}
	calls = 0
	finalDenied.Authorize = func(c context.Context, r contracts.RetrieveRequest) ([]byte, publicationprojection.CallIdentity, error) {
		calls++
		if calls == 2 {
			scopeDenied = true
		}
		return authority(c, r)
	}
	if got, e := finalDenied.Search(ctx, q); e == nil || got != nil || calls != 2 {
		return nil, errors.New("late scope denial returned SQL candidates")
	}
	scopeDenied = false
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if got, e := reader.Search(cancelled, q); e == nil || got != nil {
		return nil, errors.New("cancelled search returned candidates")
	}
	assertBackendRejected, err := verifyEvidenceBackend(ctx, db, scope, verify, originals, q, facts, actual)
	if err != nil {
		return nil, err
	}
	requests := map[contracts.ID]contracts.RetrieveRequest{}
	for _, fact := range facts {
		one, err := makeRequest([]contracts.PublishedFact{fact})
		if err != nil {
			return nil, err
		}
		requests[fact.DocumentID] = one
		if fact.DocumentID == "final-readiness-lock-document" {
			if got, e := reader.Search(ctx, one); e == nil || got != nil {
				return nil, errors.New("logical chunk fence remained searchable")
			}
		}
		if strings.HasPrefix(string(fact.DocumentID), "keyword-deleted-") {
			// The backend probe deleted this generation during original projection,
			// after first proving it was ready. Confirm the actual durable fence.
			deleteScope := scope
			deleteScope.Action = "index.delete"
			cleanup := postgres.IndexCleanup{DB: db, Verify: func(context.Context, contracts.AccessScope) error { return nil }}
			state, err := cleanup.InspectGeneration(ctx, deleteScope, fact.GenerationSet.Channels[contracts.KeywordChannel].ID)
			if err != nil || !state.Deleted || state.RecordCount != 0 {
				return nil, errors.New("backend index deletion did not persist")
			}

			if got, e := reader.Search(ctx, one); e == nil || got != nil {
				return nil, errors.New("deleted actual index still searchable")
			}
		}
	}
	return func(id contracts.ID) error {
		if err := assertBackendRejected(id); err != nil {
			return err
		}
		if got, err := reader.Search(ctx, requests[id]); !errors.Is(err, postgres.ErrKeywordSearch) || got != nil {
			return errors.New("superseded actual publication remained searchable")
		}
		if got, err := frozenReader.ReadFrozenContent(ctx, frozenRequests[id]); err == nil || got.Payload != nil {
			return errors.New("superseded actual frozen revision remained readable")
		}
		return nil
	}, nil
}
