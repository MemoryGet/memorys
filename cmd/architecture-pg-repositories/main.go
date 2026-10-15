// Offline integration probe for the isolated SQL fixture, never a business API.
package main

import (
	"context"
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
	db, err := openFixtureDB()
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
	if err := verifyDraftCandidates(ctx, db, scope); err != nil {
		return err
	}
	if err := verifyCandidateApplication(ctx, db, scope); err != nil {
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
	json.NewEncoder(os.Stdout).Encode(map[string]any{"passed": true, "production_acceptance": false, "authentication": "fixture verifier", "review_contract_samples": reviewContractSamples, "publication_contract_samples": publicationContractSamples, "publication_task_samples": publicationTaskSamples, "query_read_samples": queryReadSamples, "finalization_results": finalizationResults, "finalization_requests": finalizationRequests, "finalization_contract_samples": finalizationContractSamples, "completion_samples": completionSamples, "completion_job_samples": completionJobSamples, "preparation_artifacts": preparationArtifactSamples, "preparation_wire_samples": preparationWireSamples, "preparation_snapshots": preparationSnapshotSamples, "publication_lease_samples": publicationLeaseSamples, "publication_policy_samples": publicationPolicySamples, "checks": []string{"query_reads_real_repeatable_read_pages", "query_reads_actual_current_approval_epoch", "query_reads_withdrawal_between_pages", "query_reads_fresh_withdrawal_check", "query_reads_callback_handle_closed", "query_reads_cursor_view_scope_binding", "query_reads_detached_return_values", "query_reads_current_authority_recheck", "query_reads_authority_failure_scope_denial", "query_reads_cancellation", "query_reads_store_rotation_fence", "query_reads_current_approval_revocation", "finalization_actual_completed_index", "finalization_atomic_history_head", "finalization_cold_unknown_replay", "finalization_semantic_conflict", "finalization_current_publisher_authority", "finalization_replacement_cas_single_winner", "finalization_old_publication_preserved_on_failure", "finalization_fixed_old_cleanup_isolation", "finalization_withdrawal_interoperation", "finalization_replay_after_withdrawal_no_resurrection", "finalization_deleted_index_revoked_approval", "finalization_audit_outbox_atomicity", "finalization_deferred_event_and_head_gates", "finalization_precommit_revocation", "finalization_readiness_write_barrier", "finalization_configuration_superseded", "completion_actual_projection_reinspection", "completion_immutable_success_closes_lease", "completion_cold_unknown_replay_after_expiry", "completion_exact_command_binding", "completion_expired_takeover_denied", "completion_deleted_projection_rejected", "completion_replay_worker_publisher_authority", "completion_precommit_revocation", "completion_precommit_expiry", "completion_configuration_superseded", "completion_effects_atomicity", "completion_deferred_effects_and_head_gate", "completion_concurrent_idempotency", "completion_no_publication_cutover", "preparation_actual_keyword_projection", "preparation_durable_cold_replay", "preparation_independent_live_inspection", "preparation_scoped_query_filters", "preparation_sealed_rows_and_delete_fence", "preparation_rejects_echo_and_source_mutation", "preparation_audit_outbox_atomicity", "preparation_deferred_event_gate", "preparation_current_authority_and_lease_fence", "preparation_attempt_takeover_isolation", "preparation_concurrent_idempotency", "preparation_no_publication_cutover", "publication_tasks_durable_claim_replay", "publication_tasks_renewal_unknown_replay", "publication_tasks_expiry_takeover_fence", "publication_tasks_owned_frozen_content", "publication_tasks_worker_and_publisher_authority", "publication_tasks_precommit_revocation", "publication_tasks_atomic_audit_outbox", "publication_tasks_deferred_event_gate", "publication_tasks_concurrent_claim", "publication_tasks_configuration_superseded", "publication_tasks_review_epoch_superseded", "publication_tasks_no_publication", "publication_tasks_receipt_head_binding", "publication_start_atomic_task_outbox", "publication_start_current_approval_policy", "publication_start_backend_binding", "publication_start_cold_replay", "publication_start_authority_recheck", "publication_start_effects_rollback", "publication_start_deferred_event_gate", "publication_start_head_write_fences", "publication_start_concurrent_key", "publication_start_no_head_cutover", "review_atomic_approve_reject", "review_immutable_revoke", "review_lost_response_cold_replay", "review_idempotency_and_authority", "review_failed_effects_rollback", "review_deferred_effect_gate", "review_precommit_authority", "review_concurrent_single_decision", "review_no_publication", "submission_atomic_revision_review", "submission_lost_response_cold_replay", "submission_immutable_version", "submission_audit_outbox_rollback", "submission_current_authority_recheck", "submission_concurrent_single_version", "submission_does_not_publish", "submission_configuration_and_key_scope", "submission_deferred_effect_gate", "application_native_frozen_candidate_content", "application_real_file_parser_pg", "application_lost_commit_cold_retry", "application_persisted_unicode_source", "application_human_edit_late_candidate", "application_no_publication_or_duplicate_effects", "candidate_atomic_commit", "candidate_durable_replay", "candidate_late_and_submitted_retained", "candidate_audit_failure_rollback", "candidate_commit_authority_revocation", "history_plan_load", "uint64_nanosecond_decode", "cross_scope_rejected", "escaped_handle_closed", "history_append_rollback", "authority_denied", "commit_scope_revocation_rollback", "plan_targets_append_roundtrip", "partial_target_failure_rollback", "withdraw_head_outbox_atomic_commit", "withdraw_complete_rollback", "stale_head_swallowed_error_rollback", "outbox_failure_rollback", "command_reservation_rollback", "original_history_replay", "command_digest_conflict", "pending_command_not_taken_over", "replay_no_duplicate_effects", "withdraw_audit_atomic_commit", "audit_failure_rollback", "audit_replay_no_duplicate", "current_head_and_registered_targets", "application_withdraw_real_transaction", "application_original_history_replay", "application_canonical_preparer", "cleanup_claim_exclusive", "cleanup_expired_lease_reclaim", "cleanup_stale_fence_rejected", "cleanup_receipt_durable_replay", "cleanup_unknown_not_retried", "cleanup_concurrent_claim_single_owner", "cleanup_worker_real_store_fixture_executor", "cleanup_lease_binding_verification", "cleanup_worker_native_memory_index", "cleanup_old_generation_isolation", "cleanup_late_native_write_rejected", "cleanup_persistent_index_backend", "cleanup_persistent_reconnect_receipt", "cleanup_repeatable_snapshot_fenced", "index_same_data_roundtrip", "index_transfer_failure_rollback", "index_transfer_restores_generation_chunk_fences", "index_configured_provider_switch", "index_configured_close_rejects_old_port"}})
}
