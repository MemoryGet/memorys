package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

var managementDeleteSamples []map[string]any
var runManagementDeletionProbe func(context.Context) error

func verifyManagementDeletion(ctx context.Context, db *sql.DB, auth contracts.Authenticator, reader, deleter contracts.Credential) error {
	// The second real signed identity receives ONLY deletion permission. Existing
	// read grants of the first actor must not enable management deletion.
	if _, e := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES('space',4,false); INSERT INTO akos_withdraw_v1.access_policy_grants VALUES('space',4,'fixture','historical-second-reader','identity-client','kb','kb.delete'),('space',4,'fixture','reader','identity-client','kb','knowledge.read'); UPDATE akos_withdraw_v1.access_policy_snapshots SET sealed=true WHERE space_id='space' AND version=4; UPDATE akos_withdraw_v1.access_policy_heads SET version=4 WHERE space_id='space' AND version=3; INSERT INTO akos_withdraw_v1.documents(space_id,kb_id,id,publication_epoch,row_version) VALUES('space','kb','management-delete-document',0,1)`); e != nil {
		return e
	}
	var payload []byte
	if e := db.QueryRowContext(ctx, `SELECT c.payload FROM akos_withdraw_v1.query_profile_heads h JOIN akos_withdraw_v1.query_profile_bindings b USING(space_id,kb_id,version) JOIN akos_withdraw_v1.query_profile_catalog c ON c.profile_digest=b.profile_digest WHERE h.space_id='space' AND h.kb_id='kb'`).Scan(&payload); e != nil {
		return e
	}
	snapshot, e := configprofile.Decode(payload)
	if e != nil {
		return e
	}
	app := document.AuthenticatedDeletion{Auth: auth, Authority: postgres.DeletionAuthority{Authorizer: postgres.Authorizer{DB: db, ScopeLifetime: time.Minute}}, Stores: func(control document.DeletionControl) (contracts.DocumentDeletionStore, error) {
		return postgres.DocumentDeletions{DB: db, Verify: control.VerifyScope, AuthorizeDelete: control.AuthorizeDelete}, nil
	}}
	profile := lifecycleProfile{version: snapshot.Pipeline.Version, digest: snapshot.Pipeline.Digest}
	mux := http.NewServeMux()
	mux.Handle("/api/v1/knowledge-os/documents/{id}", api.DocumentDeleteHandler{Service: app, Profile: profile})
	server := httptest.NewServer(mux)
	defer server.Close()
	var samples []map[string]any
	call := func(name, doc, version, key string, credential contracts.Credential, want int) (json.RawMessage, error) {
		request, e := http.NewRequestWithContext(ctx, "DELETE", server.URL+"/api/v1/knowledge-os/documents/"+doc+"?expected_document_version="+version, nil)
		if e != nil {
			return nil, e
		}
		request.Header.Set("Authorization", "Bearer "+string(credential.Secret))
		request.Header.Set("Idempotency-Key", key)
		response, e := server.Client().Do(request)
		if e != nil {
			return nil, e
		}
		defer response.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(response.Body, 65537))
		if e != nil || len(raw) > 65536 || response.StatusCode != want || response.Header.Get("Cache-Control") != "no-store" {
			return nil, errors.New("management deletion HTTP status/body differs")
		}
		samples = append(samples, map[string]any{"case": name, "document_id": doc, "expected_document_version": version, "idempotency_key": key, "status": response.StatusCode, "result": json.RawMessage(raw), "response_request_id": response.Header.Get("X-Request-ID")})
		return raw, nil
	}
	if _, e = call("read-only-denied", "management-delete-document", "1", "denied", reader, 404); e != nil {
		return e
	}
	if _, e = call("missing-hidden", "missing-management-document", "1", "denied", reader, 404); e != nil {
		return e
	}
	first, e := call("delete-only-accepted", "management-delete-document", "1", "management-delete", deleter, 202)
	if e != nil {
		return e
	}
	replay, e := call("committed-replay", "management-delete-document", "1", "management-delete", deleter, 202)
	if e != nil || string(first) != string(replay) {
		return errors.New("management deletion replay changed accepted projection")
	}
	if _, e = call("version-conflict", "management-delete-document", "2", "management-delete", deleter, 409); e != nil {
		return e
	}
	if _, e = call("deleted-new-key-conflict", "management-delete-document", "2", "another-delete", deleter, 409); e != nil {
		return e
	}
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=true WHERE issuer='fixture' AND token_id='historical-second-token'`); e != nil {
		return e
	}
	_, tokenErr := call("revoked-token", "management-delete-document", "1", "management-delete", deleter, 401)
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=false WHERE issuer='fixture' AND token_id='historical-second-token'`); e != nil {
		return e
	}
	if tokenErr != nil {
		return tokenErr
	}
	var receipt, job, head, grant []byte
	if e = db.QueryRowContext(ctx, `SELECT row_to_json(r),row_to_json(j),row_to_json(d) FROM akos_withdraw_v1.document_delete_receipts r JOIN akos_withdraw_v1.document_delete_jobs j ON j.id=r.job_id JOIN akos_withdraw_v1.documents d ON d.id=r.document_id WHERE r.document_id='management-delete-document'`).Scan(&receipt, &job, &head); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, `SELECT row_to_json(g) FROM akos_withdraw_v1.access_policy_grants g WHERE space_id='space' AND version=4 AND subject_id='historical-second-reader'`).Scan(&grant); e != nil {
		return e
	}
	var deleterGrants, receipts, jobs int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.access_policy_grants WHERE space_id='space' AND version=4 AND subject_id='historical-second-reader'`).Scan(&deleterGrants); e != nil || deleterGrants != 1 {
		return errors.New("delete-only actor has extra grants")
	}
	if e = db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM akos_withdraw_v1.document_delete_receipts WHERE document_id='management-delete-document'),(SELECT count(*) FROM akos_withdraw_v1.document_delete_jobs WHERE document_id='management-delete-document')`).Scan(&receipts, &jobs); e != nil || receipts != 1 || jobs != 1 {
		return errors.New("management deletion replay duplicated job/receipt")
	}
	// A committed receipt does not bypass a newer explicit permission revocation.
	if _, e = db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES('space',5,true); UPDATE akos_withdraw_v1.access_policy_heads SET version=5 WHERE space_id='space' AND version=4`); e != nil {
		return e
	}
	if _, e = call("revoked-permission", "management-delete-document", "1", "management-delete", deleter, 404); e != nil {
		return e
	}
	managementDeleteSamples = append(managementDeleteSamples, map[string]any{"http_samples": samples, "receipt": json.RawMessage(receipt), "job": json.RawMessage(job), "head": json.RawMessage(head), "stored_grant": json.RawMessage(grant), "deleter_grant_count": deleterGrants, "receipt_count": receipts, "job_count": jobs, "profile_digest": snapshot.Pipeline.Digest, "identity": "actual-rs256-postgres-status", "authorization": "actual-kb.delete-sealed-acl"})
	var persisted struct {
		ID contracts.ID `json:"id"`
	}
	if json.Unmarshal(job, &persisted) != nil {
		return errors.New("management job identity missing")
	}
	return verifyManagementJobStatus(ctx, db, auth, reader, deleter, persisted.ID, snapshot.Pipeline.Digest)
}
