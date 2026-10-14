package main

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

func verifyCleanupLeases(ctx context.Context, db *sql.DB, scope contracts.AccessScope) error {
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.Action != "publication.cleanup.execute" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" || s.SubjectID != "worker-a" && s.SubjectID != "worker-b" {
			return errors.New("fixture cleanup scope denied")
		}
		return nil
	}
	a, b := scope, scope
	a.SubjectID = "worker-a"
	b.SubjectID = "worker-b"
	a.Action = "publication.cleanup.execute"
	b.Action = a.Action
	store := postgres.CleanupTasks{DB: db, Verify: verify}
	first, err := store.Claim(ctx, a, "go-plan", "go-plan-binding", "worker-a", 2*time.Second)
	if err != nil {
		return err
	}
	if first.FencingToken != 1 || first.Attempt != 1 || first.RowVersion != 2 || first.PublicationEpoch != 1 || first.Target.GenerationID == nil || *first.Target.GenerationID != "go-plan-gen" {
		return errors.New("initial cleanup lease mismatch")
	}
	if _, err := store.Claim(ctx, b, "go-plan", "go-plan-binding", "worker-b", 2*time.Second); !errors.Is(err, postgres.ErrCleanupLease) {
		return errors.New("active lease taken over")
	}
	// An isolated fixture time transition avoids sleeps; not an application API.
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.cleanup_progress SET lease_until=clock_timestamp()-interval '1 second' WHERE plan_id='go-plan' AND target_id='go-plan-binding'`); err != nil {
		return err
	}
	second, err := store.Claim(ctx, b, "go-plan", "go-plan-binding", "worker-b", 2*time.Second)
	if err != nil {
		return err
	}
	if second.FencingToken != 2 || second.Attempt != 2 || second.RowVersion != 3 {
		return errors.New("reclaimed lease did not advance fence")
	}
	if err := store.VerifyLease(ctx, b, second); err != nil {
		return err
	}
	if err := store.VerifyLease(ctx, a, first); !errors.Is(err, postgres.ErrCleanupLease) {
		return errors.New("stale lease passed ownership verification")
	}
	forged := second
	wrong := contracts.ID("other-generation")
	forged.Target.GenerationID = &wrong
	if err := store.VerifyLease(ctx, b, forged); !errors.Is(err, postgres.ErrCleanupLease) {
		return errors.New("modified fixed target passed verification")
	}
	if err := store.Finish(ctx, a, first, "succeeded", "old-receipt"); !errors.Is(err, postgres.ErrCleanupLease) {
		return errors.New("stale worker committed result")
	}
	if err := store.Finish(ctx, b, second, "succeeded", "receipt"); err != nil {
		return err
	}
	// Recreate the adapter and reconnect to show that completion is durable.
	other, err := sql.Open("pgx", "postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable")
	if err != nil {
		return err
	}
	defer other.Close()
	restarted := postgres.CleanupTasks{DB: other, Verify: verify}
	if err := restarted.Finish(ctx, b, second, "succeeded", "receipt"); err != nil {
		return err
	}
	if err := restarted.Finish(ctx, b, second, "succeeded", "different-receipt"); !errors.Is(err, postgres.ErrCleanupLease) {
		return errors.New("terminal receipt overwritten")
	}
	if _, err := restarted.Claim(ctx, b, "go-plan", "go-plan-binding", "worker-b", 2*time.Second); !errors.Is(err, postgres.ErrCleanupLease) {
		return errors.New("successful target reclaimed")
	}
	lease, err := store.Claim(ctx, a, "go-plan", "go-cache-binding", "worker-a", 2*time.Second)
	if err != nil {
		return err
	}
	if err := store.Finish(ctx, a, lease, "unknown", ""); err != nil {
		return err
	}
	if _, err := restarted.Claim(ctx, b, "go-plan", "go-cache-binding", "worker-b", 2*time.Second); !errors.Is(err, postgres.ErrCleanupLease) {
		return errors.New("unknown external effect blindly retried")
	}
	// Competing transactions on a separate pending target must yield one owner.
	type claimResult struct {
		lease contracts.CleanupLease
		err   error
	}
	results := make(chan claimResult, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, workerScope := range []contracts.AccessScope{a, b} {
		workers.Add(1)
		go func(s contracts.AccessScope) {
			defer workers.Done()
			<-start
			lease, err := store.Claim(ctx, s, "plan-success", "target", s.SubjectID, 2*time.Second)
			results <- claimResult{lease, err}
		}(workerScope)
	}
	close(start)
	workers.Wait()
	close(results)
	winners := 0
	var winner contracts.CleanupLease
	for r := range results {
		if r.err == nil {
			winners++
			winner = r.lease
		}
	}
	if winners != 1 {
		return errors.New("concurrent claim did not yield exactly one owner")
	}
	var fence int
	var owner string
	if db.QueryRowContext(ctx, `SELECT fencing_token,worker_id FROM akos_withdraw_v1.cleanup_progress WHERE plan_id='plan-success' AND target_id='target'`).Scan(&fence, &owner) != nil || fence != 1 || owner != string(winner.WorkerID) {
		return errors.New("concurrent ownership not durable")
	}
	return nil
}
