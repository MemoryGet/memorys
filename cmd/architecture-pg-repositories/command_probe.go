package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/withdrawal"
	"time"
)

func verifyCommandReplay(ctx context.Context, db *sql.DB, scope contracts.AccessScope, verify postgres.VerifyScope) error {
	// A later head proves that replay comes from original history, not the head.
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.documents SET publication_epoch=3,row_version=3 WHERE id='go-plan-doc'`); err != nil {
		return err
	}
	semantic, semanticErr := withdrawal.SemanticCommitments(fixtureWithdrawRequest(ctx, scope))
	if semanticErr != nil {
		return semanticErr
	}
	command := postgres.WithdrawalCommand{OperationID: "retry-new-id", DocumentID: "go-plan-doc", Key: "go-plan-key", ActorSemanticDigest: semantic.ActorDigest, CommandDigest: semantic.CommandDigest}
	err := postgres.WithinWithdrawal(ctx, db, scope, verify, func(c context.Context, ports postgres.WithdrawalPorts) error {
		r, err := ports.Reserve(c, scope, command)
		if err != nil {
			return err
		}
		if r.Created || r.OperationID != "go-plan-op" || r.HistoryID != "go-plan-history" || r.PublicationEpoch != 2 {
			return errors.New("replay did not return original history")
		}
		return nil
	})
	if err != nil {
		return err
	}
	appResult, appErr := probeApplicationWithdraw(ctx, db, scope, verify, withdrawal.Preparation{OperationID: "retry-app-id", HistoryID: "unused-app-history", PlanID: "unused-app-plan", EventID: "unused-app-event", AuditID: "unused-app-audit", ActorSemanticDigest: command.ActorSemanticDigest, CommandDigest: command.CommandDigest, OccurredAt: time.Now().UTC()})
	if appErr != nil || appResult.HistoryID != "go-plan-history" || appResult.PublicationEpoch != 2 {
		return errors.New("application replay did not return original result")
	}
	command.CommandDigest = contracts.Digest(strings.Repeat("d", 64))
	err = postgres.WithinWithdrawal(ctx, db, scope, verify, func(c context.Context, ports postgres.WithdrawalPorts) error {
		_, err := ports.Reserve(c, scope, command)
		return err
	})
	if !errors.Is(err, postgres.ErrCommandConflict) {
		return errors.New("changed command not rejected")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.command_operations VALUES('pending-probe','space','kb','go-plan-doc',$1::text,'actor','withdraw','pending-key',$2::text,'pending',NULL)`, string(semantic.ActorDigest), string(semantic.CommandDigest)); err != nil {
		return err
	}
	command.Key, command.CommandDigest = "pending-key", semantic.CommandDigest
	err = postgres.WithinWithdrawal(ctx, db, scope, verify, func(c context.Context, ports postgres.WithdrawalPorts) error {
		_, err := ports.Reserve(c, scope, command)
		return err
	})
	if !errors.Is(err, postgres.ErrCommandPending) {
		return errors.New("pending command taken over")
	}
	for _, query := range []string{
		"SELECT count(*) FROM akos_withdraw_v1.publication_history WHERE document_id='go-plan-doc'",
		"SELECT count(*) FROM akos_withdraw_v1.cleanup_plans WHERE document_id='go-plan-doc'",
		"SELECT count(*) FROM akos_withdraw_v1.withdraw_outbox WHERE document_id='go-plan-doc'",
		"SELECT count(*) FROM akos_withdraw_v1.withdraw_audit WHERE document_id='go-plan-doc'",
		"SELECT count(*) FROM akos_withdraw_v1.command_operations WHERE id='retry-new-id'",
	} {
		var count int
		want := 1
		if strings.Contains(query, "retry-new-id") {
			want = 0
		}
		if db.QueryRowContext(ctx, query).Scan(&count) != nil || count != want {
			return errors.New("replay duplicated command effects")
		}
	}
	return nil
}
