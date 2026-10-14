package main

import (
	"context"
	"database/sql"
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/withdrawal"
	"strings"
	"time"
)

// All values belong to a disposable fixture; these statements are not API DML.
func verifyPlanRollback(ctx context.Context, db *sql.DB, scope contracts.AccessScope, verify postgres.VerifyScope) error {
	seed := []string{
		`INSERT INTO akos_withdraw_v1.documents VALUES('space','kb','go-plan-doc',1,1,NULL,NULL)`,
		`INSERT INTO akos_withdraw_v1.revisions VALUES('space','kb','go-plan-doc','go-plan-rev',repeat('a',64))`,
		`INSERT INTO akos_withdraw_v1.generation_sets VALUES('space','kb','go-plan-doc','go-plan-rev','go-plan-set','go-plan-epoch')`,
		`INSERT INTO akos_withdraw_v1.index_generations VALUES('space','kb','go-plan-doc','go-plan-rev','go-plan-set','go-plan-gen','go-plan-epoch','keyword',repeat('a',64),'engine')`,
		`INSERT INTO akos_withdraw_v1.binding_definitions VALUES('space','kb','go-plan-binding','engine',repeat('a',64),'index','{"generation_id":"go-plan-gen"}')`,
		`INSERT INTO akos_withdraw_v1.binding_definitions VALUES('space','kb','go-cache-binding','cache-engine',repeat('a',64),'cache','{"source_epoch":1}')`,
		`UPDATE akos_withdraw_v1.documents SET active_revision_id='go-plan-rev',active_generation_set_id='go-plan-set' WHERE id='go-plan-doc'`,
		`INSERT INTO akos_withdraw_v1.publication_cleanup_dependencies VALUES('space','kb','go-plan-doc','go-plan-rev','go-plan-set',1,'go-cache-binding','cache-engine',repeat('a',64),'cache')`,
	}
	for _, statement := range seed {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return errors.New("plan fixture seed failed")
		}
	}
	rev, set, gen, epoch := contracts.ID("go-plan-rev"), contracts.ID("go-plan-set"), contracts.ID("go-plan-gen"), contracts.ID("go-plan-epoch")
	channel := contracts.KeywordChannel
	digest := contracts.Digest(strings.Repeat("a", 64))
	now := time.Now().UTC()
	failure := errors.New("after plan insert failure")
	history := contracts.PublicationHistory{ID: "go-plan-history", OperationID: "go-plan-op", SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "go-plan-doc", PreviousEpoch: 1, PublicationEpoch: 2, Action: "withdraw", PreviousRevisionID: &rev, PreviousGenerationSetID: &set, ActorID: "actor", Actor: contracts.Principal{SubjectID: "actor", SubjectType: "user", Issuer: "test-fixture"}, Reason: "fixture", OccurredAt: now, CommandDigest: contracts.Digest(strings.Repeat("c", 64))}
	for _, mode := range []string{"callback", "bad-target", "stale", "outbox-failure", "audit-failure", "success"} {
		badTarget := mode == "bad-target"
		run := func(callback func(context.Context, contracts.WithdrawalPorts) error) error {
			if mode != "success" {
				return postgres.WithinWithdrawal(ctx, db, scope, verify, callback)
			}
			result, err := probeApplicationWithdraw(ctx, db, scope, verify, withdrawal.Preparation{OperationID: history.OperationID, HistoryID: history.ID, PlanID: "go-plan", EventID: "go-plan-event", AuditID: "go-plan-audit", ActorSemanticDigest: contracts.Digest(strings.Repeat("b", 64)), CommandDigest: history.CommandDigest, OccurredAt: now})
			if err != nil {
				return err
			}
			if result.HistoryID != history.ID || result.PublicationEpoch != 2 {
				return errors.New("application withdrawal result mismatch")
			}
			return nil
		}
		err := run(func(c context.Context, ports postgres.WithdrawalPorts) error {
			reservation, err := ports.Reserve(c, scope, postgres.WithdrawalCommand{OperationID: "go-plan-op", DocumentID: "go-plan-doc", Key: "go-plan-key", ActorSemanticDigest: contracts.Digest(strings.Repeat("b", 64)), CommandDigest: history.CommandDigest})
			if err != nil {
				return err
			}
			if !reservation.Created || reservation.OperationID != history.OperationID {
				return errors.New("new command not reserved")
			}
			current, err := ports.LoadCurrent(c, scope, "go-plan-doc")
			if err != nil || len(current.Targets) != 2 || current.PublicationEpoch != 1 || current.RevisionID != "go-plan-rev" {
				return errors.New("current target snapshot mismatch")
			}
			h, p := ports.History, ports.Plans
			if err := h.Append(c, scope, history); err != nil {
				return err
			}
			plan := contracts.CleanupPlan{ID: "go-plan", HistoryID: history.ID, OperationID: history.OperationID, SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "go-plan-doc", RevisionID: rev, GenerationSetID: set, PublicationEpoch: 1, ProfileDigest: digest, CreatedAt: now, Targets: []contracts.CleanupTarget{
				{ID: "go-index", Kind: "index", ModuleInstanceID: "engine", BindingReference: "go-plan-binding", ProfileDigest: digest, GenerationID: &gen, BackendEpochID: &epoch, Channel: &channel},
				{ID: "go-cache", Kind: "cache", ModuleInstanceID: "cache-engine", BindingReference: "go-cache-binding", ProfileDigest: digest},
			}}
			if badTarget {
				plan.Targets[1].BindingReference = "unknown-binding"
			}
			err = p.Append(c, scope, plan)
			if badTarget {
				if err == nil {
					return errors.New("invalid second target inserted")
				}
				return err
			}
			if err != nil {
				return err
			}
			loaded, err := p.Load(c, scope, plan.ID)
			if err != nil || len(loaded.Targets) != 2 {
				return errors.New("inserted plan round trip failed")
			}
			version, event := uint64(1), contracts.ID("go-plan-event")
			if mode == "stale" {
				version = 2
			}
			if mode == "outbox-failure" {
				event = contracts.ID(strings.Repeat("x", 201))
			}
			completion := postgres.WithdrawalCompletion{PlanID: plan.ID, ExpectedRowVersion: version, EventID: event, AuditID: "go-plan-audit", RequestID: "go-plan-request"}
			if mode == "audit-failure" {
				completion.AuditID = contracts.ID(strings.Repeat("x", 201))
			}
			completeErr := ports.Complete(c, scope, completion)
			if mode == "stale" || mode == "outbox-failure" || mode == "audit-failure" {
				if completeErr == nil {
					return errors.New("invalid completion accepted")
				}
				return nil // Intentionally swallow the error: transaction must still abort.
			}
			if completeErr != nil {
				return completeErr
			}
			if mode == "success" {
				return nil
			}
			return failure
		})
		if mode == "callback" && !errors.Is(err, failure) {
			return errors.New("plan callback failure lost")
		}
		if (badTarget || mode == "stale" || mode == "outbox-failure" || mode == "audit-failure") && !errors.Is(err, postgres.ErrRepository) {
			return errors.New("bad target error not propagated")
		}
		if mode == "success" && err != nil {
			return err
		}
		var epoch, version int
		var active sql.NullString
		if db.QueryRowContext(ctx, "SELECT publication_epoch,row_version,active_revision_id FROM akos_withdraw_v1.documents WHERE id='go-plan-doc'").Scan(&epoch, &version, &active) != nil {
			return errors.New("head check failed")
		}
		wantEpoch, wantCount := 1, 0
		if mode == "success" {
			wantEpoch, wantCount = 2, 1
		}
		if epoch != wantEpoch || version != wantEpoch || active.Valid != (mode != "success") {
			return errors.New("publication head atomicity failed")
		}
		for _, query := range []string{
			"SELECT count(*) FROM akos_withdraw_v1.publication_history WHERE id='go-plan-history'",
			"SELECT count(*) FROM akos_withdraw_v1.cleanup_plans WHERE id='go-plan'",
			"SELECT count(DISTINCT plan_id) FROM akos_withdraw_v1.cleanup_targets WHERE plan_id='go-plan'",
			"SELECT count(*) FROM akos_withdraw_v1.withdraw_outbox WHERE plan_id='go-plan'",
			"SELECT count(*) FROM akos_withdraw_v1.withdraw_audit WHERE history_id='go-plan-history'",
			"SELECT count(DISTINCT plan_id) FROM akos_withdraw_v1.cleanup_progress WHERE plan_id='go-plan'",
			"SELECT count(*) FROM akos_withdraw_v1.command_operations WHERE id='go-plan-op' AND state='committed'",
			"SELECT count(*) FROM akos_withdraw_v1.command_operations WHERE id='go-plan-op'",
		} {
			var count int
			if db.QueryRowContext(ctx, query).Scan(&count) != nil || count != wantCount {
				return errors.New("partial cleanup plan survived rollback")
			}
		}
	}
	return verifyCommandReplay(ctx, db, scope, verify)
}
