package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/mcpgateway"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

var historicalReadSamples []map[string]any
var historicalReadSource *observedHistorySource

type observedHistorySource struct {
	inner         contracts.HistoricalContentReader
	request       contracts.HistoricalContentReadRequest
	record        contracts.HistoricalContentRecord
	calls         int
	secondRequest contracts.HistoricalContentReadRequest
}

func (s *observedHistorySource) ReadHistoricalContent(c context.Context, q contracts.HistoricalContentReadRequest) (contracts.HistoricalContentRecord, error) {
	s.calls++
	out, err := s.inner.ReadHistoricalContent(c, q)
	if err == nil && q.Principal.SubjectID == "historical-second-reader" && s.secondRequest.Principal.SubjectID == "" {
		raw, _ := json.Marshal(q)
		_ = json.Unmarshal(raw, &s.secondRequest)
	}
	if err == nil && s.record.PublicationID == "" {
		raw, _ := json.Marshal(q)
		_ = json.Unmarshal(raw, &s.request)
		raw, _ = json.Marshal(out)
		_ = json.Unmarshal(raw, &s.record)
	}
	return out, err
}

func verifyHistoricalReadGateway(ctx context.Context, db *sql.DB, auth contracts.Authenticator, profile api.PartnerSearchApplication, read api.CurrentReadApplication, credential contracts.Credential) error {
	if historicalTarget.Document == "" || historicalReadSource == nil {
		return errors.New("historical source fixture missing")
	}
	_, digest, err := profile.ProfileIdentity()
	if err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	cleanupScope := contracts.AccessScope{ScopeID: "historical-old-index-cleanup", SubjectID: "fixture-cleanup", Action: "index.delete", SpaceID: historicalTarget.OldHistory.SpaceID, KnowledgeBaseIDs: []contracts.ID{historicalTarget.OldHistory.KnowledgeBaseID}, PolicyVersion: 1, ExpiresAt: deadline}
	cleanup := postgres.IndexCleanup{DB: db, Verify: func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SubjectID != "fixture-cleanup" || s.Action != "index.delete" {
			return errors.New("historical cleanup fixture denied")
		}
		return nil
	}}
	if historicalTarget.OldGeneration == "" {
		return errors.New("historical old index fixture missing")
	}
	if err := cleanup.Delete(ctx, contracts.IndexDeleteRequest{Request: contracts.RequestContext{RequestID: "historical-old-index-delete", ProfileDigest: digest, Deadline: deadline}, Scope: cleanupScope, GenerationID: historicalTarget.OldGeneration}); err != nil {
		return err
	}
	oldIndex, err := cleanup.InspectGeneration(ctx, cleanupScope, historicalTarget.OldGeneration)
	if err != nil || !oldIndex.Deleted || oldIndex.RecordCount != 0 {
		return errors.New("historical old index still exists")
	}
	g, err := mcpgateway.New(mcpgateway.Config{Auth: auth, Search: profile, Read: read})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/read", api.PartnerReadHandler{Service: read, Profile: profile})
	mux.Handle("/mcp", g)
	server := httptest.NewServer(mux)
	defer server.Close()
	postAs := func(actor contracts.Credential, path string, value any) (json.RawMessage, error) {
		body, _ := json.Marshal(value)
		r, e := http.NewRequestWithContext(ctx, "POST", server.URL+path, bytes.NewReader(body))
		if e != nil {
			return nil, e
		}
		r.Header.Set("Authorization", "Bearer "+string(actor.Secret))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("Mcp-Protocol-Version", mcpgateway.ProtocolVersion)
		res, e := server.Client().Do(r)
		if e != nil {
			return nil, e
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
		if e != nil || res.StatusCode != 200 || len(raw) > 4<<20 {
			return nil, errors.New("historical HTTP read unavailable")
		}
		return raw, nil
	}
	post := func(path string, value any) (json.RawMessage, error) {
		return postAs(credential, path, value)
	}
	args := map[string]any{"document_id": historicalTarget.Document, "revision_id": historicalTarget.OldRevision}
	rest, err := post("/read", args)
	if err != nil {
		return err
	}
	var first map[string]any
	if json.Unmarshal(rest, &first) != nil || first["revision_id"] != string(historicalTarget.OldRevision) {
		return errors.New("historical target returned current revision")
	}
	cursor, ok := first["next_cursor"].(string)
	if !ok || cursor == "" {
		return errors.New("historical paging cursor missing")
	}
	rpc := func(method string, params any) (json.RawMessage, error) {
		return post("/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	}
	_, err = rpc("initialize", map[string]any{"protocolVersion": mcpgateway.ProtocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "historical-native-probe", "version": "1"}})
	if err != nil {
		return err
	}
	continued := map[string]any{"document_id": historicalTarget.Document, "revision_id": historicalTarget.OldRevision, "cursor": cursor}
	mcp, err := rpc("tools/call", map[string]any{"name": "knowledge_read", "arguments": continued})
	if err != nil {
		return err
	}
	var result struct {
		Result struct {
			Error      bool           `json:"isError"`
			Structured map[string]any `json:"structuredContent"`
			Content    []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(mcp, &result) != nil || result.Result.Error || result.Result.Structured["revision_id"] != string(historicalTarget.OldRevision) || result.Result.Structured["next_cursor"] != nil {
		return errors.New("historical MCP continuation failed")
	}
	paged, ok := read.(api.PagedReadApplication)
	if !ok {
		return errors.New("historical paged application missing")
	}
	in := knowledge.CurrentReadInput{Credential: credential, DocumentID: historicalTarget.Document, RevisionID: historicalTarget.OldRevision, Request: contracts.RequestContext{RequestID: "historical-denial-probe", Deadline: deadline, ProfileDigest: digest}}
	secondPrincipal, err := auth.Authenticate(ctx, historicalSecondCredential)
	if err != nil || secondPrincipal.SubjectID != "historical-second-reader" || secondPrincipal.SubjectID == historicalReadSource.request.Principal.SubjectID {
		return errors.New("historical second signed identity missing")
	}
	secondREST, err := postAs(historicalSecondCredential, "/read", args)
	if err != nil {
		return err
	}
	var secondFirst map[string]any
	if json.Unmarshal(secondREST, &secondFirst) != nil || secondFirst["revision_id"] != string(historicalTarget.OldRevision) || secondFirst["next_cursor"] == nil {
		return errors.New("independently authorized second historical reader failed")
	}
	secondInput := in
	secondInput.Credential = historicalSecondCredential
	if out, e := paged.ReadPage(ctx, secondInput, knowledge.ReadPageOptions{Cursor: cursor}); !errors.Is(e, knowledge.ErrReadCursor) || len(out.Record.Payload) != 0 || out.NextCursor != "" {
		return errors.New("historical cursor not bound to authenticated subject")
	}
	secondQuery := historicalReadSource.secondRequest
	if secondQuery.Principal.SubjectID != secondPrincipal.SubjectID {
		return errors.New("second historical reader did not reach native source")
	}
	secondGrants, err := historicalGrantRows(ctx, db, secondQuery)
	if err != nil {
		return err
	}
	var secondToken json.RawMessage
	if err := db.QueryRowContext(ctx, `SELECT row_to_json(t) FROM akos_withdraw_v1.identity_tokens t WHERE issuer=$1 AND subject_id=$2 AND client_id=$3`, secondPrincipal.Issuer, secondPrincipal.SubjectID, secondPrincipal.ClientID).Scan(&secondToken); err != nil {
		return err
	}
	changed := in
	changed.RevisionID = historicalTarget.NewRevision
	if out, e := paged.ReadPage(ctx, changed, knowledge.ReadPageOptions{Cursor: cursor}); e == nil || len(out.Record.Payload) != 0 {
		return errors.New("historical cursor reused for current revision")
	}
	foreign := in
	foreign.RevisionID = "missing-historical-revision"
	if out, e := paged.ReadPage(ctx, foreign, knowledge.ReadPageOptions{}); e == nil || len(out.Record.Payload) != 0 {
		return errors.New("never-published target returned content")
	}
	unpublished := in
	unpublished.RevisionID = historicalTarget.UnpublishedRevision
	if out, e := paged.ReadPage(ctx, unpublished, knowledge.ReadPageOptions{}); e == nil || len(out.Record.Payload) != 0 {
		return errors.New("approved unpublished revision returned historical content")
	}
	var unpublishedPublications int
	if db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.publication_history WHERE document_id=$1 AND revision_id=$2 AND action='publish'`, historicalTarget.Document, historicalTarget.UnpublishedRevision).Scan(&unpublishedPublications) != nil || unpublishedPublications != 0 {
		return errors.New("unpublished historical fixture was actually published")
	}
	source := historicalReadSource.record
	grants, err := historicalGrantRows(ctx, db, historicalReadSource.request)
	if err != nil {
		return err
	}
	command := contracts.CommandContext{Request: in.Request, Scope: historicalReadSource.request.HistoryScope, Principal: contracts.Principal{SubjectID: "reviewer", SubjectType: "user", Issuer: "fixture"}, IdempotencyKey: "historical-revoke-original-approval"}
	command.Scope.SubjectID = "reviewer"
	command.Scope.Action = "revision.approve"
	verifier := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SubjectID != "reviewer" || s.Action != "revision.approve" {
			return errors.New("historical reviewer fixture denied")
		}
		return nil
	}
	reviews := postgres.Reviews{DB: db, Verify: verifier, ProfileDigest: digest, ReviewPolicyVersion: source.Review.Validity.ReviewPolicyVersion, AuthorizeDecision: func(c context.Context, q document.ReviewCommand) error {
		if c.Err() != nil || q.DocumentID != historicalTarget.Document {
			return errors.New("historical revoke fixture denied")
		}
		return nil
	}}
	revoked, err := reviews.DecideReview(ctx, document.ReviewCommand{DocumentID: historicalTarget.Document, Request: contracts.ApprovalDecisionRequest{Command: command, RevisionID: historicalTarget.OldRevision, Decision: "revoke", ExpectedApprovalVersion: source.Review.Approval.RowVersion, Reason: "历史版本输出前撤销批准"}})
	if err != nil {
		return err
	}
	if out, e := paged.ReadPage(ctx, in, knowledge.ReadPageOptions{Cursor: cursor}); e == nil || len(out.Record.Payload) != 0 {
		return errors.New("revoked historical approval returned content")
	}
	historicalReadSamples = append(historicalReadSamples, map[string]any{"second_principal": secondPrincipal, "second_token_status": secondToken, "second_source_request": secondQuery, "second_stored_grants": secondGrants, "second_rest_result": secondREST, "cross_subject_cursor_rejected": true, "request": args, "continuation": continued, "rest_result": rest, "mcp_result": mcp, "source_request": historicalReadSource.request, "source_record": source, "replaced_by": historicalTarget.NewRevision, "old_publication": historicalTarget.OldHistory, "new_publication": historicalTarget.NewHistory, "stored_grants": grants, "old_index_cleanup": oldIndex, "old_generation_id": historicalTarget.OldGeneration, "unpublished_revision": historicalTarget.UnpublishedRevision, "unpublished_approval": historicalTarget.UnpublishedApproval, "unpublished_publication_count": unpublishedPublications, "unpublished_target_rejected": true, "revoked_approval": revoked, "revoked_target_rejected": true, "changed_target_cursor_rejected": true, "missing_target_rejected": true, "provider_calls": historicalReadSource.calls, "authority": "actual-rs256-postgres-read-and-history", "original_grant": "fixture-independent-read"})
	return nil
}

func historicalGrantRows(ctx context.Context, db *sql.DB, query contracts.HistoricalContentReadRequest) ([]json.RawMessage, error) {
	rows, err := db.QueryContext(ctx, `SELECT row_to_json(g) FROM akos_withdraw_v1.access_policy_grants g WHERE space_id=$1 AND version=$2::numeric AND issuer=$3 AND subject_id=$4 AND client_id=$5 AND kb_id=$6 AND action IN('knowledge.read','knowledge.history.read') ORDER BY action`, query.Base.Scope.SpaceID, query.HistoryScope.PolicyVersion, query.Principal.Issuer, query.Principal.SubjectID, query.Principal.ClientID, query.Base.Scope.KnowledgeBaseIDs[0])
	if err != nil {
		return nil, err
	}
	var grants []json.RawMessage
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil {
			rows.Close()
			return nil, errors.New("historical grant sample unavailable")
		}
		grants = append(grants, append(json.RawMessage(nil), raw...))
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(grants) != 2 {
		return nil, errors.New("historical dual grant rows missing")
	}
	return grants, nil
}
