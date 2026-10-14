package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/withdrawal"
)

// ID/time and authority sources remain fixtures; semantic commitments use real code.
func probeApplicationWithdraw(ctx context.Context, db *sql.DB, scope contracts.AccessScope, verify postgres.VerifyScope, prepared withdrawal.Preparation) (contracts.WithdrawResult, error) {
	ids := []contracts.ID{prepared.OperationID, prepared.HistoryID, prepared.PlanID, prepared.EventID, prepared.AuditID}
	ordinal := 0
	preparer := withdrawal.Preparer{Verify: func(c context.Context, r contracts.WithdrawRequest) error {
		if r.DocumentID != "go-plan-doc" || r.Command.Principal.SubjectID != "actor" || r.Command.Principal.SubjectType != "user" || r.Command.Principal.Issuer != "test-fixture" || r.Command.Principal.Delegation != nil {
			return errors.New("fixture principal/resource denied")
		}
		return verify(c, r.Command.Scope)
	}, NewID: func(context.Context) (contracts.ID, error) {
		if ordinal >= len(ids) {
			return "", errors.New("fixture IDs exhausted")
		}
		id := ids[ordinal]
		ordinal++
		return id, nil
	}, Now: func() time.Time { return prepared.OccurredAt }}
	service := withdrawal.Service{Transactions: postgres.WithdrawalAdapter{DB: db, Verify: verify}, Prepare: preparer.Prepare}
	return service.Withdraw(ctx, fixtureWithdrawRequest(ctx, scope))
}
func fixtureWithdrawRequest(ctx context.Context, scope contracts.AccessScope) contracts.WithdrawRequest {
	deadline, _ := ctx.Deadline()
	return contracts.WithdrawRequest{DocumentID: "go-plan-doc", ExpectedEpoch: 1, Reason: "fixture", Command: contracts.CommandContext{Scope: scope, Principal: contracts.Principal{SubjectID: "actor", SubjectType: "user", Issuer: "test-fixture"}, IdempotencyKey: "go-plan-key", Request: contracts.RequestContext{RequestID: "go-plan-request", ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), Deadline: deadline}}}
}
