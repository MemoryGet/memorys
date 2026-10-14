// Offline integration probe for the isolated SQL fixture, never a business API.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
	"strings"
	"time"
)

func probe() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", "postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable")
	if err != nil {
		return errors.New("driver open failed")
	}
	defer db.Close()
	if db.PingContext(ctx) != nil {
		return errors.New("fixture database unavailable")
	}
	scope := contracts.AccessScope{ScopeID: "fixture-scope", SubjectID: "actor", Action: "publication.withdraw", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: time.Now().Add(20 * time.Second)}
	verify := func(ctx context.Context, s contracts.AccessScope) error {
		if ctx.Err() != nil || s.SubjectID != "actor" || s.SpaceID != "space" || s.PolicyVersion != 1 {
			return errors.New("fixture scope denied")
		}
		return nil
	}
	var escaped contracts.PublicationHistoryRepository
	err = postgres.WithinPublication(ctx, db, scope, verify, func(c context.Context, h contracts.PublicationHistoryRepository, p contracts.CleanupPlanRepository) error {
		escaped = h
		history, err := h.Load(c, scope, "history-success")
		if err != nil {
			return err
		}
		if history.PublicationEpoch != 2 || history.PreviousEpoch != 1 || history.OccurredAt.Nanosecond() != 1 || history.Actor.Issuer != "test-fixture" {
			return errors.New("history decode mismatch")
		}
		plan, err := p.Load(c, scope, "plan-success")
		if err != nil {
			return err
		}
		if len(plan.Targets) != 2 || plan.PublicationEpoch != 1 {
			return errors.New("plan decode mismatch")
		}
		wrong := scope
		wrong.SpaceID = "other"
		if _, err := h.Load(c, wrong, "history-success"); err == nil {
			return errors.New("cross scope load accepted")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if _, err := escaped.Load(ctx, scope, "history-success"); err == nil {
		return errors.New("escaped transaction handle usable")
	}
	// Inject application failure after a real history INSERT and verify rollback.
	_, err = db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.command_operations VALUES('go-operation','space','kb','doc',$1::text,'actor','delete','go-rollback',$2::text,'pending',NULL)`, strings.Repeat("b", 64), strings.Repeat("c", 64))
	if err != nil {
		return errors.New("fixture command seed failed")
	}
	writeScope := scope
	writeScope.Action = "document.delete"
	record := contracts.PublicationHistory{ID: "go-history", OperationID: "go-operation", SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "doc", PreviousEpoch: 2, PublicationEpoch: 3, Action: "delete", ActorID: "actor", Actor: contracts.Principal{SubjectID: "actor", SubjectType: "user", Issuer: "test-fixture"}, Reason: "fixture delete", OccurredAt: time.Now().UTC(), CommandDigest: contracts.Digest(strings.Repeat("c", 64))}
	failure := errors.New("injected callback failure")
	err = postgres.WithinPublication(ctx, db, writeScope, verify, func(c context.Context, h contracts.PublicationHistoryRepository, _ contracts.CleanupPlanRepository) error {
		if err := h.Append(c, writeScope, record); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		return errors.New("callback failure lost")
	}
	var count int
	if db.QueryRowContext(ctx, "SELECT count(*) FROM akos_withdraw_v1.publication_history WHERE id='go-history'").Scan(&count) != nil || count != 0 {
		return errors.New("history insert escaped rollback")
	}

	calls := 0
	beforeCommit := func(c context.Context, s contracts.AccessScope) error {
		calls++
		if calls == 3 {
			return errors.New("scope revoked before commit")
		}
		return verify(c, s)
	}
	record.ID = "go-late-history"
	err = postgres.WithinPublication(ctx, db, writeScope, beforeCommit, func(c context.Context, h contracts.PublicationHistoryRepository, _ contracts.CleanupPlanRepository) error {
		return h.Append(c, writeScope, record)
	})
	if !errors.Is(err, postgres.ErrRepository) || calls != 3 {
		return errors.New("commit boundary did not reject revoked scope")
	}
	if db.QueryRowContext(ctx, "SELECT count(*) FROM akos_withdraw_v1.publication_history WHERE id='go-late-history'").Scan(&count) != nil || count != 0 {
		return errors.New("revoked transaction history persisted")
	}
	if err := verifyPlanRollback(ctx, db, scope, verify); err != nil {
		return err
	}
	if err := verifyCleanupLeases(ctx, db, scope); err != nil {
		return err
	}
	if err := verifyCleanupWorker(ctx, db, scope); err != nil {
		return err
	}
	if err := verifyNativeIndexCleanup(ctx, db, scope); err != nil {
		return err
	}
	if err := verifyPersistentIndex(ctx, db, scope); err != nil {
		return err
	}
	if err := verifyIndexTransfer(ctx, db, scope); err != nil {
		return err
	}
	deny := func(context.Context, contracts.AccessScope) error { return errors.New("revoked") }
	called := false
	err = postgres.WithinPublication(ctx, db, scope, deny, func(context.Context, contracts.PublicationHistoryRepository, contracts.CleanupPlanRepository) error {
		called = true
		return nil
	})
	if err == nil || called {
		return errors.New("denied authority began work")
	}
	return nil
}
func main() {
	if err := probe(); err != nil {
		fmt.Fprintln(os.Stderr, "PostgreSQL repository probe failed:", err)
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"passed": true, "production_acceptance": false, "authentication": "fixture verifier", "checks": []string{"history_plan_load", "uint64_nanosecond_decode", "cross_scope_rejected", "escaped_handle_closed", "history_append_rollback", "authority_denied", "commit_scope_revocation_rollback", "plan_targets_append_roundtrip", "partial_target_failure_rollback", "withdraw_head_outbox_atomic_commit", "withdraw_complete_rollback", "stale_head_swallowed_error_rollback", "outbox_failure_rollback", "command_reservation_rollback", "original_history_replay", "command_digest_conflict", "pending_command_not_taken_over", "replay_no_duplicate_effects", "withdraw_audit_atomic_commit", "audit_failure_rollback", "audit_replay_no_duplicate", "current_head_and_registered_targets", "application_withdraw_real_transaction", "application_original_history_replay", "application_canonical_preparer", "cleanup_claim_exclusive", "cleanup_expired_lease_reclaim", "cleanup_stale_fence_rejected", "cleanup_receipt_durable_replay", "cleanup_unknown_not_retried", "cleanup_concurrent_claim_single_owner", "cleanup_worker_real_store_fixture_executor", "cleanup_lease_binding_verification", "cleanup_worker_native_memory_index", "cleanup_old_generation_isolation", "cleanup_late_native_write_rejected", "cleanup_persistent_index_backend", "cleanup_persistent_reconnect_receipt", "cleanup_repeatable_snapshot_fenced", "index_same_data_roundtrip", "index_transfer_failure_rollback", "index_transfer_restores_generation_chunk_fences"}})
}
