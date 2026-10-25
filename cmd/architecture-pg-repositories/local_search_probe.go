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
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/localruntime"
)

// Keep this proof separate from the full evidence fixtures: local-host samples
// retain statuses and counts only, never credentials or source bodies.
var localRuntimeSearchSamples []map[string]any

func verifyLocalRuntimeSearch(ctx context.Context, db *sql.DB, auth contracts.Authenticator, credential contracts.Credential) (resultErr error) {
	previousLimit := db.Stats().MaxOpenConnections
	db.SetMaxOpenConns(2)
	defer db.SetMaxOpenConns(previousLimit)
	profile, err := referenceQuerySnapshot(1)
	if err != nil {
		return err
	}
	service, err := localruntime.NewSearch(db, auth, profile)
	if err != nil {
		return err
	}
	loadSource := func(document contracts.ID, kind string, chinese bool) (contracts.IndexProjection, error) {
		var raw []byte
		var projection contracts.IndexProjection
		err := db.QueryRowContext(ctx, `SELECT r.payload
 FROM akos_withdraw_v1.index_projection_rows r
 JOIN akos_withdraw_v1.index_generations g ON g.id=r.generation_id
 JOIN akos_withdraw_v1.documents d ON d.id=g.document_id AND d.active_generation_set_id=g.generation_set_id AND d.active_revision_id=g.revision_id
 WHERE d.id=$1 AND NOT d.deleted AND g.channel='keyword'
 AND r.payload->'Citation'->'Source'->>'Kind'=$2
 AND btrim(r.payload->'Chunk'->>'Text')<>''
 AND (NOT $3::boolean OR r.payload->'Citation'->>'BlockID'='historical-current-block')
 ORDER BY r.chunk_id COLLATE "C" LIMIT 1`, document, kind, chinese).Scan(&raw)
		if err != nil || len(raw) > 4<<20 || json.Unmarshal(raw, &projection) != nil || projection.DocumentID != document || projection.Citation.Source.Kind != kind || strings.TrimSpace(projection.Chunk.Text) == "" || len(projection.Chunk.Text) > 4096 {
			return projection, errors.New("local search " + kind + " current source fixture unavailable")
		}
		return projection, nil
	}
	// The historical replacement is a human-edited revision published by the
	// actual submit/review/prepare/complete/finalize flow, not a forged SQL head.
	revision, err := loadSource(historicalTarget.Document, "revision", true)
	if err != nil {
		return err
	}
	original, err := loadSource("evidence-original-document", "original", false)
	if err != nil {
		return err
	}
	const endpoint = "/api/v1/knowledge-os/search"
	mux := http.NewServeMux()
	mux.Handle(endpoint, api.PartnerSearchHandler{Service: service})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	type evidence struct {
		Citation struct {
			DocumentID contracts.ID `json:"document_id"`
			RevisionID contracts.ID `json:"revision_id"`
			Source     struct {
				Kind string `json:"kind"`
			} `json:"source_anchor"`
		} `json:"citation"`
		Excerpt string `json:"source_excerpt"`
	}
	type response struct {
		Code     string     `json:"code"`
		Evidence []evidence `json:"evidence"`
	}
	call := func(query string, token []byte) (int, response, error) {
		var output response
		body, err := json.Marshal(map[string]any{"query": query, "space_id": "space", "kb_ids": []string{"kb"}, "top_k": 100})
		if err != nil {
			return 0, output, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+endpoint, bytes.NewReader(body))
		if err != nil {
			return 0, output, err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+string(token))
		reply, err := client.Do(request)
		if err != nil {
			return 0, output, errors.New("local search HTTP transport failed")
		}
		data, readErr := io.ReadAll(io.LimitReader(reply.Body, (4<<20)+1))
		closeErr := reply.Body.Close()
		if readErr != nil || closeErr != nil || len(data) > 4<<20 || json.Unmarshal(data, &output) != nil || reply.Header.Get("Cache-Control") != "no-store" || bytes.Contains(data, credential.Secret) || reply.StatusCode == 401 && reply.Header.Get("WWW-Authenticate") != "Bearer" {
			return 0, response{}, errors.New("local search response contract rejected")
		}
		if reply.StatusCode != 200 && (len(output.Evidence) != 0 || bytes.Contains(data, []byte("source_excerpt")) || bytes.Contains(data, []byte(revision.Chunk.Text)) || bytes.Contains(data, []byte(original.Chunk.Text))) {
			return 0, response{}, errors.New("local search failure exposed source evidence")
		}
		return reply.StatusCode, output, nil
	}
	status, successful, err := call(strings.TrimSpace(revision.Chunk.Text), credential.Secret)
	if err != nil || status != http.StatusOK || len(successful.Evidence) == 0 {
		return errors.New("local search revision source did not succeed")
	}
	for _, item := range successful.Evidence {
		if item.Citation.DocumentID != revision.DocumentID || item.Citation.RevisionID != revision.Citation.RevisionID || item.Citation.Source.Kind != "revision" || !strings.Contains(revision.Chunk.Text, item.Excerpt) || item.Excerpt == "" {
			return errors.New("local search revision citation or excerpt differs")
		}
	}
	// A request's SQL read and current-authority check share a two-connection
	// pool. Queued requests must not retain a transaction and starve the check.
	parallelResults := make(chan error, 4)
	for range 4 {
		go func() {
			code, result, err := call(strings.TrimSpace(revision.Chunk.Text), credential.Secret)
			if err != nil || code != http.StatusOK || len(result.Evidence) == 0 {
				parallelResults <- errors.New("local search small-pool concurrency failed")
				return
			}
			parallelResults <- nil
		}()
	}
	var parallelErr error
	for range 4 {
		if err := <-parallelResults; err != nil {
			parallelErr = err
		}
	}
	if parallelErr != nil {
		return parallelErr
	}
	localRuntimeSearchSamples = append(localRuntimeSearchSamples, map[string]any{"case": "small-pool-concurrency", "requests": 4, "connections": 2, "all_status": http.StatusOK})
	badStatus, bad, err := call(strings.TrimSpace(revision.Chunk.Text), []byte("invalid-local-access-token"))
	if err != nil || badStatus != http.StatusUnauthorized || bad.Code != "unauthenticated" {
		return errors.New("local search invalid credential was not rejected")
	}
	var wasRevoked bool
	if db.QueryRowContext(ctx, `SELECT revoked FROM akos_withdraw_v1.identity_tokens WHERE issuer='fixture' AND token_id='identity-token'`).Scan(&wasRevoked) != nil || wasRevoked {
		return errors.New("local search token fixture unavailable")
	}
	changed, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=true WHERE issuer='fixture' AND token_id='identity-token'`)
	if err != nil {
		return errors.New("local search token revocation failed")
	}
	// Always restore the shared fixture token, including a failed HTTP request
	// or an expired phase context, before later probes reuse the credential.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanup, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=false WHERE issuer='fixture' AND token_id='identity-token'`); err != nil && resultErr == nil {
			resultErr = errors.New("local search token restoration failed")
		}
	}()
	if count, err := changed.RowsAffected(); err != nil || count != 1 {
		return errors.New("local search token revocation count differs")
	}
	revokedStatus, revoked, err := call(strings.TrimSpace(revision.Chunk.Text), credential.Secret)
	if err != nil || revokedStatus != http.StatusUnauthorized || revoked.Code != "unauthenticated" {
		return errors.New("local search revoked credential was not rejected")
	}
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=false WHERE issuer='fixture' AND token_id='identity-token'`); err != nil {
		return errors.New("local search token restoration failed")
	}
	originalStatus, originalFailure, err := call(strings.TrimSpace(original.Chunk.Text), credential.Secret)
	if err != nil || originalStatus != http.StatusServiceUnavailable || originalFailure.Code != "dependency_unavailable" {
		return errors.New("local search original source lacked independent read denial")
	}
	localRuntimeSearchSamples = append(localRuntimeSearchSamples, map[string]any{"transport": "localhost-http", "transport_path": endpoint, "profile_digest": profile.Pipeline.Digest, "revision_status": status, "revision_evidence_count": len(successful.Evidence), "revision_source_kind": "revision", "invalid_credential_status": badStatus, "revoked_credential_status": revokedStatus, "original_without_grant_status": originalStatus, "source_evidence_on_failure": false, "authentication": "actual-rs256-current-status", "authorization": "postgres-sealed-acl", "projection_authority": "configured-private-invocation"})
	return nil
}
