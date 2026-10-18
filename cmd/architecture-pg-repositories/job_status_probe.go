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
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

var jobStatusSamples []map[string]any

func verifyManagementJobStatus(ctx context.Context, db *sql.DB, auth contracts.Authenticator, viewer, deleter contracts.Credential, jobID contracts.ID, profile contracts.Digest) error {
	if _, e := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES('space',6,false); INSERT INTO akos_withdraw_v1.access_policy_grants VALUES('space',6,'fixture','reader','identity-client','kb','resource.view_job'),('space',6,'fixture','historical-second-reader','identity-client','kb','kb.delete'); UPDATE akos_withdraw_v1.access_policy_snapshots SET sealed=true WHERE space_id='space' AND version=6; UPDATE akos_withdraw_v1.access_policy_heads SET version=6 WHERE space_id='space' AND version=5; INSERT INTO akos_withdraw_v1.documents(space_id,kb_id,id,publication_epoch,row_version) VALUES('space','job-foreign-kb','job-foreign-document',0,1)`); e != nil {
		return e
	}
	deadline, _ := ctx.Deadline()
	fixtureVerify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SubjectID != "job-fixture-publisher" || s.Action != "document.delete" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "job-foreign-kb" {
			return errors.New("foreign job fixture denied")
		}
		return nil
	}
	foreignRequest := contracts.DeleteDocumentRequest{DocumentID: "job-foreign-document", ExpectedDocumentVersion: 1, Command: contracts.CommandContext{Principal: contracts.Principal{SubjectID: "job-fixture-publisher", SubjectType: "user", Issuer: "fixture"}, Scope: contracts.AccessScope{ScopeID: "foreign-job-fixture", SubjectID: "job-fixture-publisher", Action: "document.delete", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"job-foreign-kb"}, PolicyVersion: 1, ExpiresAt: deadline}, Request: contracts.RequestContext{RequestID: "foreign-job-seed", Deadline: deadline, ProfileDigest: profile}, IdempotencyKey: "foreign-job-seed"}}
	fixtureAuthorize := func(c context.Context, q contracts.DeleteDocumentRequest) error {
		if q.DocumentID != "job-foreign-document" {
			return errors.New("foreign job fixture resource denied")
		}
		return fixtureVerify(c, q.Command.Scope)
	}
	foreign, e := (document.DeletionService{Store: postgres.DocumentDeletions{DB: db, Verify: fixtureVerify, AuthorizeDelete: fixtureAuthorize}, Verify: fixtureAuthorize}).Delete(ctx, foreignRequest)
	if e != nil {
		return e
	}
	authority := postgres.Authorizer{DB: db, ScopeLifetime: time.Minute}
	revokeToken, revokePermission := false, false
	checks := 0
	checked := fixtureControlAuthorize(func(c context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
		checks++
		if checks == 2 && revokeToken {
			if _, e := db.ExecContext(c, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=true WHERE issuer='fixture' AND token_id='identity-token'`); e != nil {
				return contracts.AccessScope{}, e
			}
		}
		if checks == 2 && revokePermission {
			if _, e := db.ExecContext(c, `INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES('space',7,true); UPDATE akos_withdraw_v1.access_policy_heads SET version=7 WHERE space_id='space' AND version=6`); e != nil {
				return contracts.AccessScope{}, e
			}
		}
		return authority.Authorize(c, q)
	})
	service := document.JobStatusService{Auth: auth, Reader: postgres.DeletionJobReader{DB: db, Authorizer: checked}}
	mux := http.NewServeMux()
	mux.Handle("/api/v1/knowledge-os/jobs/{id}", api.JobStatusHandler{Service: service})
	server := httptest.NewServer(mux)
	defer server.Close()
	var samples []map[string]any
	call := func(name string, id contracts.ID, credential contracts.Credential, want int) error {
		r, e := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/knowledge-os/jobs/"+string(id), nil)
		if e != nil {
			return e
		}
		r.Header.Set("Authorization", "Bearer "+string(credential.Secret))
		response, e := server.Client().Do(r)
		if e != nil {
			return e
		}
		defer response.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(response.Body, 65537))
		if e != nil || len(raw) > 65536 || response.StatusCode != want || response.Header.Get("Cache-Control") != "no-store" {
			return errors.New("management job HTTP response differs")
		}
		samples = append(samples, map[string]any{"case": name, "job_id": id, "status": response.StatusCode, "result": json.RawMessage(raw), "response_request_id": response.Header.Get("X-Request-ID")})
		return nil
	}
	if e = call("viewer-only-visible", jobID, viewer, 200); e != nil {
		return e
	}
	if e = call("delete-only-denied", jobID, deleter, 404); e != nil {
		return e
	}
	if e = call("missing-hidden", "missing-job-status", viewer, 404); e != nil {
		return e
	}
	if e = call("foreign-kb-hidden", foreign.JobID, viewer, 404); e != nil {
		return e
	}
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=true WHERE issuer='fixture' AND token_id='identity-token'`); e != nil {
		return e
	}
	tokenErr := call("revoked-token", jobID, viewer, 401)
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=false WHERE issuer='fixture' AND token_id='identity-token'`); e != nil {
		return e
	}
	if tokenErr != nil {
		return tokenErr
	}
	checks = 0
	revokeToken = true
	tokenErr = call("token-revoked-during-read", jobID, viewer, 401)
	revokeToken = false
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=false WHERE issuer='fixture' AND token_id='identity-token'`); e != nil {
		return e
	}
	if tokenErr != nil {
		return tokenErr
	}
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.document_delete_jobs SET document_id='job-foreign-document' WHERE id=$1`, jobID); e == nil {
		return errors.New("immutable job owner could be changed")
	}
	var job, foreignJob, grants []byte
	if e = db.QueryRowContext(ctx, `SELECT row_to_json(j) FROM akos_withdraw_v1.document_delete_jobs j WHERE id=$1`, jobID).Scan(&job); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, `SELECT row_to_json(j) FROM akos_withdraw_v1.document_delete_jobs j WHERE id=$1`, foreign.JobID).Scan(&foreignJob); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, `SELECT jsonb_agg(g ORDER BY subject_id,action) FROM akos_withdraw_v1.access_policy_grants g WHERE space_id='space' AND version=6`).Scan(&grants); e != nil {
		return e
	}
	checks = 0
	revokePermission = true
	e = call("permission-revoked-during-read", jobID, viewer, 404)
	revokePermission = false
	if e != nil {
		return e
	}
	if e = call("revoked-permission", jobID, viewer, 404); e != nil {
		return e
	}
	jobStatusSamples = append(jobStatusSamples, map[string]any{"http_samples": samples, "job": json.RawMessage(job), "foreign_job": json.RawMessage(foreignJob), "stored_grants": json.RawMessage(grants), "owner_mutation_rejected": true, "identity": "actual-rs256-postgres-status", "authorization": "actual-resource.view_job-sealed-acl", "foreign_creation_authority": "fixture"})
	return verifyDeletionCleanupPhase(db, auth, viewer, jobID)
}
