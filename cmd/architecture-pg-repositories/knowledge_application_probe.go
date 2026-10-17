package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/fusion"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/profileassembly"
	"github.com/MemoryGet/memorys/pkg/publicationprojection"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

var knowledgeApplicationSamples []map[string]any
var knowledgeRerankingSamples []map[string]any
var knowledgeMultiRouteSamples []map[string]any
var partnerSearchSamples []map[string]any

type observedApplicationBackend func(context.Context, contracts.RequestContext, contracts.AccessScope, queryread.CurrentObservation, []contracts.Evidence) error

func (f observedApplicationBackend) Verify(c context.Context, r contracts.RequestContext, s contracts.AccessScope, o queryread.CurrentObservation, e []contracts.Evidence) error {
	return f(c, r, s, o, e)
}

type observedApplicationFuser func(context.Context, contracts.FuseRequest) ([]contracts.Candidate, error)

func (f observedApplicationFuser) Fuse(c context.Context, r contracts.FuseRequest) ([]contracts.Candidate, error) {
	return f(c, r)
}

type nativeReranker func(context.Context, contracts.RerankRequest) ([]contracts.Evidence, error)

func (f nativeReranker) Rerank(c context.Context, r contracts.RerankRequest) ([]contracts.Evidence, error) {
	return f(c, r)
}

type nativeSearchPlans func(context.Context, contracts.Principal, contracts.RetrieveRequest) (knowledge.SearchPlan, error)

func (f nativeSearchPlans) Prepare(c context.Context, p contracts.Principal, q contracts.RetrieveRequest) (knowledge.SearchPlan, error) {
	return f(c, p, q)
}

func verifyKnowledgeApplication(ctx context.Context, db *sql.DB, auth contracts.Authenticator, authorization contracts.Authorizer, credential contracts.Credential, facts postgres.QueryFacts, reads postgres.QueryReads, backend postgres.EvidenceBackend, published contracts.PublishedFact, store contracts.ID, text string, read api.CurrentReadApplication) error {
	snapshot, err := referenceQuerySnapshot(1)
	if err != nil {
		return err
	}
	runtimes := []*profileassembly.Runtime{}
	defer func() {
		for _, r := range runtimes {
			_ = r.Close(context.Background())
		}
	}()
	directory, ok := authorization.(knowledge.KnowledgeBaseDirectory)
	if !ok {
		return errors.New("native KB directory unavailable")
	}
	var preparedSearches atomic.Int64
	var reranker contracts.Reranker
	prepare := nativeSearchPlans(func(c context.Context, who contracts.Principal, q contracts.RetrieveRequest) (knowledge.SearchPlan, error) {
		preparedSearches.Add(1)
		if who.SubjectID != "reader" || q.Scope.SpaceID != published.SpaceID {
			return knowledge.SearchPlan{}, errors.New("application plan denied")
		}
		g := published.GenerationSet.Channels[contracts.KeywordChannel]
		q.BackendEpochID = published.GenerationSet.BackendEpochID
		q.Generations = []contracts.IndexGeneration{g}
		q.Publication = contracts.PublicationFilter{SpaceID: q.Scope.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), q.Scope.KnowledgeBaseIDs...), RevisionIDs: []contracts.ID{published.RevisionID}, GenerationIDs: []contracts.ID{g.ID}}
		record := publicationprojection.Record{SchemaVersion: 1, ProjectionID: "application-projection", RequestID: q.Request.RequestID, InvocationID: "application-invocation", ParentScopeID: "application-parent", ChildScopeID: q.Scope.ScopeID, SpaceID: q.Scope.SpaceID, KnowledgeBaseID: published.KnowledgeBaseID, BackendEpochID: q.BackendEpochID, RouteID: q.RouteID, ModuleInstanceID: "postgres-keyword-reader", ParentPublicationDigest: contracts.Digest(strings.Repeat("b", 64)), QueryProfileDigest: snapshot.Query.Digest, PipelineDigest: q.Request.ProfileDigest, PolicyVersion: q.Scope.PolicyVersion, Channels: q.Channels, ExpiresAtUnixNS: q.Request.Deadline.UnixNano(), Entries: []publicationprojection.Entry{{DocumentID: published.DocumentID, RevisionID: published.RevisionID, PublicationVersion: published.PublicationEpoch, GenerationSetID: published.GenerationSet.ID, GenerationID: g.ID, Channel: contracts.KeywordChannel, IndexProfileDigest: g.ProfileDigest}}}
		raw, err := json.Marshal(record)
		if err != nil {
			return knowledge.SearchPlan{}, err
		}
		_, q.Publication.SnapshotDigest, err = publicationprojection.ValidatedDigest(raw)
		if err != nil {
			return knowledge.SearchPlan{}, err
		}
		var fixed contracts.RetrieveRequest
		encoded, _ := json.Marshal(q)
		if json.Unmarshal(encoded, &fixed) != nil {
			return knowledge.SearchPlan{}, errors.New("application plan clone failed")
		}
		reader := postgres.KeywordRetriever{Reads: reads, ModuleInstanceID: "postgres-keyword-reader", Authorize: func(c context.Context, r contracts.RetrieveRequest) ([]byte, publicationprojection.CallIdentity, error) {
			if c.Err() != nil || !reflect.DeepEqual(r, fixed) {
				return nil, publicationprojection.CallIdentity{}, errors.New("application invocation mismatch")
			}
			return raw, publicationprojection.CallIdentity{InvocationID: "application-invocation", ModuleInstanceID: "postgres-keyword-reader", Limit: fixed.Limit}, nil
		}}
		runtime, service, err := assembleNativeReader(c, db, snapshot, reader, reranker)
		if err != nil {
			return knowledge.SearchPlan{}, err
		}
		runtimes = append(runtimes, runtime)
		var resolved contracts.Reranker
		if snapshot.Query.Policy.EnableRerank {
			resolved, err = profileassembly.Resolve[contracts.Reranker](runtime, contracts.Reranking)
			if err != nil {
				return knowledge.SearchPlan{}, err
			}
		}
		return knowledge.SearchPlan{Request: q, Retriever: service, Reranker: resolved, Expectation: queryread.CurrentExpectation{StoreEpochID: store, ScopeVersion: q.Scope.PolicyVersion, Published: []contracts.PublishedFact{published}}}, nil
	})
	service, err := knowledge.NewSearch(snapshot, knowledge.Dependencies{KnowledgeBases: directory, Auth: auth, Authorizer: authorization, Plans: prepare, Facts: facts, Reads: reads, Backend: backend})
	if err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	input := knowledge.SearchInput{Credential: credential, RequestID: "application-search", Deadline: deadline, SpaceID: published.SpaceID, KnowledgeBaseIDs: []contracts.ID{published.KnowledgeBaseID}, Text: text, Limit: 1000}
	results, err := service.Search(ctx, input)
	if err != nil || len(results) == 0 {
		return errors.New("native shared search application failed")
	}
	for _, c := range results {
		if c.Citation.DocumentID != published.DocumentID {
			return errors.New("application crossed selected publication")
		}
	}
	server := httptest.NewServer(api.ApplicationSearchHandler{Service: service})
	defer server.Close()
	args := url.Values{"q": []string{text}, "space_id": []string{string(published.SpaceID)}, "kb_id": []string{string(published.KnowledgeBaseID)}, "limit": []string{"1000"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?"+args.Encode(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+string(credential.Secret))
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != 200 || response.Header.Get("Cache-Control") != "no-store" {
		return errors.New("native REST application failed")
	}
	var output struct {
		Results []map[string]any `json:"results"`
	}
	if json.Unmarshal(body, &output) != nil || len(output.Results) != len(results) {
		return errors.New("native REST evidence missing")
	}
	if strings.Contains(string(body), string(credential.Secret)) || strings.Contains(string(body), "RetrieverID") {
		return errors.New("REST exposed private data")
	}
	partnerMux := http.NewServeMux()
	partnerMux.Handle("/api/v1/knowledge-os/search", api.PartnerSearchHandler{Service: service})
	partnerServer := httptest.NewServer(partnerMux)
	defer partnerServer.Close()
	callPartner := func(body map[string]any) (int, json.RawMessage, error) {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, partnerServer.URL+"/api/v1/knowledge-os/search", strings.NewReader(string(raw)))
		if err != nil {
			return 0, nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+string(credential.Secret))
		response, err := client.Do(request)
		if err != nil {
			return 0, nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.Header.Get("Cache-Control") != "no-store" || response.StatusCode == 401 && response.Header.Get("WWW-Authenticate") != "Bearer" {
			return 0, nil, errors.New("partner response failed")
		}
		return response.StatusCode, json.RawMessage(data), nil
	}
	partnerBody := map[string]any{"query": text, "space_id": published.SpaceID, "top_k": 1}
	status, partnerRaw, err := callPartner(partnerBody)
	if err != nil || status != 200 {
		return errors.New("partner POST search failed")
	}
	var partnerResult struct {
		Evidence []json.RawMessage `json:"evidence"`
	}
	if json.Unmarshal(partnerRaw, &partnerResult) != nil || len(partnerResult.Evidence) != 1 {
		return errors.New("omitted KB directory search failed")
	}
	partnerSearchSamples = append(partnerSearchSamples, map[string]any{"case": "omitted", "transport_path": "/api/v1/knowledge-os/search", "request": partnerBody, "response": partnerRaw, "status": status})
	if err := verifyMCPGateway(ctx, db, auth, service, credential, text, published.SpaceID, partnerRaw); err != nil {
		return err
	}
	if err := verifyPartnerReadGateway(ctx, db, auth, service, read, credential, published.DocumentID); err != nil {
		return err
	}
	if err := verifyHistoricalReadGateway(ctx, db, auth, service, read, credential); err != nil {
		return err
	}
	emptyBody := map[string]any{"query": text, "space_id": published.SpaceID, "top_k": 1, "kb_ids": []string{}}
	before := preparedSearches.Load()
	status, emptyRaw, err := callPartner(emptyBody)
	if err != nil || status != 200 {
		return errors.New("explicit empty partner search failed")
	}
	var emptyResult struct {
		Evidence []json.RawMessage `json:"evidence"`
	}
	if json.Unmarshal(emptyRaw, &emptyResult) != nil || len(emptyResult.Evidence) != 0 || preparedSearches.Load() != before {
		return errors.New("empty KBs expanded into a search")
	}
	partnerSearchSamples = append(partnerSearchSamples, map[string]any{"case": "explicit-empty", "request": emptyBody, "response": emptyRaw, "status": status, "planning_calls": 0})
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=true WHERE token_id='identity-token'`); err != nil {
		return err
	}
	denied, denyErr := service.Search(ctx, input)
	partnerStatus, partnerError, partnerErr := callPartner(partnerBody)

	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=false WHERE token_id='identity-token'`); err != nil {
		return err
	}
	if partnerErr != nil || partnerStatus != 401 || strings.Contains(string(partnerError), string(credential.Secret)) {
		return errors.New("revoked partner search response failed")
	}
	partnerSearchSamples = append(partnerSearchSamples, map[string]any{"case": "revoked", "request": partnerBody, "response": partnerError, "status": partnerStatus})
	if denyErr == nil || denied != nil {
		return errors.New("application accepted revoked credential")
	}
	knowledgeApplicationSamples = append(knowledgeApplicationSamples, map[string]any{"profile_digest": snapshot.Pipeline.Digest, "published": published, "results": results, "http_results": output.Results, "http_status": response.StatusCode, "transport": "localhost-http", "invocation_authority": "host-test-binding", "revoked_credential_rejected": true})

	baseSnapshot := snapshot
	multiRaw, err := configprofile.Encode(snapshot)
	if err != nil {
		return err
	}
	snapshot, err = configprofile.Decode(multiRaw)
	if err != nil {
		return err
	}
	snapshot.Query.Version = 200
	snapshot.Pipeline.Version = 200
	snapshot.Pipeline.QueryProfile.Version = 200
	snapshot.Query.Routes[0].CandidateLimit = 500
	second := snapshot.Query.Routes[0]
	second.ID = "keyword-route-secondary"
	second.Required = true
	snapshot.Query.Routes = append(snapshot.Query.Routes, second)
	snapshot, err = configprofile.Seal(snapshot)
	if err != nil {
		return err
	}
	multiRaw, err = configprofile.Encode(snapshot)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.query_profile_catalog VALUES($1,$2)`, snapshot.Pipeline.Digest, multiRaw); err != nil {
		return err
	}
	if err := activateNativeProfile(ctx, db, published.SpaceID, published.KnowledgeBaseID, snapshot.Pipeline.Digest); err != nil {
		return err
	}
	backendChecks, preChecks, fuserCalls := 0, 0, 0
	observedBackend := observedApplicationBackend(func(c context.Context, r contracts.RequestContext, s contracts.AccessScope, o queryread.CurrentObservation, e []contracts.Evidence) error {
		backendChecks++
		return backend.Verify(c, r, s, o, e)
	})
	observedFuser := observedApplicationFuser(func(c context.Context, r contracts.FuseRequest) ([]contracts.Candidate, error) {
		fuserCalls++
		preChecks = backendChecks
		if preChecks < 1 {
			return nil, errors.New("fuser received unverified evidence")
		}
		return (fusion.RRF{}).Fuse(c, r)
	})
	multiService, err := knowledge.NewSearch(snapshot, knowledge.Dependencies{KnowledgeBases: directory, Auth: auth, Authorizer: authorization, Plans: prepare, Facts: facts, Reads: reads, Backend: observedBackend, Fuser: observedFuser})
	if err != nil {
		return err
	}
	fused, err := multiService.Search(ctx, input)
	if err != nil || len(fused) == 0 || preChecks != 1 || backendChecks != 2 || fuserCalls != 1 {
		return fmt.Errorf("native multi-route application failed: pre=%d total=%d fuser=%d results=%d error=%v", preChecks, backendChecks, fuserCalls, len(fused), err)
	}
	for _, c := range fused {
		if len(c.FusionOrigins) != 2 {
			return errors.New("configured route was not fused")
		}
	}
	failing := nativeSearchPlans(func(c context.Context, p contracts.Principal, r contracts.RetrieveRequest) (knowledge.SearchPlan, error) {
		if r.RouteID == "keyword-route-secondary" {
			return knowledge.SearchPlan{}, errors.New("required route failed")
		}
		return prepare.Prepare(c, p, r)
	})
	failedService, err := knowledge.NewSearch(snapshot, knowledge.Dependencies{KnowledgeBases: directory, Auth: auth, Authorizer: authorization, Plans: failing, Facts: facts, Reads: reads, Backend: backend})
	if err != nil {
		return err
	}
	if output, err := failedService.Search(ctx, input); err == nil || output != nil {
		return errors.New("required route failure returned partial output")
	}
	knowledgeMultiRouteSamples = append(knowledgeMultiRouteSamples, map[string]any{"profile": snapshot, "candidates": fused, "input_candidates": results, "route_failure_rejected": true, "provider": "same-postgres-reader-two-routes", "fuser_calls": fuserCalls, "pre_fusion_backend_checks": preChecks, "total_backend_checks": backendChecks})
	if err := activateNativeProfile(ctx, db, published.SpaceID, published.KnowledgeBaseID, baseSnapshot.Pipeline.Digest); err != nil {
		return err
	}
	snapshot = baseSnapshot
	encoded, err := configprofile.Encode(snapshot)
	if err != nil {
		return err
	}
	snapshot, err = configprofile.Decode(encoded)
	if err != nil {
		return err
	}
	snapshot.Query.Version = 100
	snapshot.Pipeline.Version = 100
	snapshot.Pipeline.QueryProfile.Version = 100
	snapshot.Query.Policy.EnableRerank = true
	snapshot.Query.Policy.RerankTimeoutMS = 1000
	snapshot.Query.Bindings[contracts.Reranking] = contracts.ModuleBinding{InstanceID: "native-reranker", Capability: contracts.Reranking, ContractVersion: "1.0"}
	snapshot, err = configprofile.Seal(snapshot)
	if err != nil {
		return err
	}
	encoded, err = configprofile.Encode(snapshot)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.query_profile_catalog VALUES($1,$2)`, snapshot.Pipeline.Digest, encoded); err != nil {
		return err
	}
	if err := activateNativeProfile(ctx, db, published.SpaceID, published.KnowledgeBaseID, snapshot.Pipeline.Digest); err != nil {
		return err
	}
	mode, calls := "valid", 0
	var rerankInput []contracts.Candidate
	rerankInputRequest := input
	rerankInputRequest.Limit = 1
	rerankInputRequest.Text = "文"
	reranker = nativeReranker(func(c context.Context, r contracts.RerankRequest) ([]contracts.Evidence, error) {
		calls++
		out := make([]contracts.Evidence, 0, len(r.Candidates))
		if mode == "valid" {
			encoded, _ := json.Marshal(r.Candidates)
			if json.Unmarshal(encoded, &rerankInput) != nil {
				return nil, errors.New("rerank sample copy failed")
			}
		}
		for i := len(r.Candidates) - 1; i >= 0 && len(out) < r.Limit; i-- {
			candidate := r.Candidates[i]
			out = append(out, contracts.Evidence{Citation: candidate.Citation, SourceExcerpt: candidate.Text, Rank: len(out) + 1})
		}
		if mode == "fabricate" && len(out) > 0 {
			out[0].SourceExcerpt = "模型编造的无来源事实"
		}
		if mode == "revoke" {
			if _, err := db.ExecContext(c, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=true WHERE token_id='identity-token'`); err != nil {
				return nil, err
			}
		}
		return out, nil
	})
	rankedService, err := knowledge.NewSearch(snapshot, knowledge.Dependencies{KnowledgeBases: directory, Auth: auth, Authorizer: authorization, Plans: prepare, Facts: facts, Reads: reads, Backend: backend})
	if err != nil {
		return err
	}
	ranked, err := rankedService.Search(ctx, rerankInputRequest)
	if err != nil || len(ranked) != 1 || len(rerankInput) < 2 {
		return errors.New("native rerank application failed")
	}
	mode = "fabricate"
	if output, err := rankedService.Search(ctx, rerankInputRequest); err == nil || output != nil {
		return errors.New("fabricated rerank output escaped")
	}
	mode = "revoke"
	revokedOutput, revokedErr := rankedService.Search(ctx, rerankInputRequest)
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=false WHERE token_id='identity-token'`); err != nil {
		return err
	}
	if revokedErr == nil || revokedOutput != nil || calls != 3 {
		return errors.New("rerank final credential revocation ignored")
	}
	knowledgeRerankingSamples = append(knowledgeRerankingSamples, map[string]any{"profile": snapshot, "input_candidates": rerankInput, "requested_limit": rerankInputRequest.Limit, "query": rerankInputRequest.Text, "ranked_candidates": ranked, "reranker_calls": calls, "provider": "test-reverse-order", "fabrication_rejected": true, "final_revocation_rejected": true})
	if err := activateNativeProfile(ctx, db, published.SpaceID, published.KnowledgeBaseID, baseSnapshot.Pipeline.Digest); err != nil {
		return err
	}
	snapshot = baseSnapshot
	return nil
}
