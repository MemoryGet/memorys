// Package postgres adapts the isolated withdrawal schema prototype. It is not
// the full metadata UnitOfWork provider or a production bootstrap.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"reflect"
	"sync"
	"time"
)

var ErrRepository = errors.New("publication repository unavailable or rejected")
var ErrCommitUnknown = errors.New("publication transaction outcome requires reconciliation")

type VerifyScope func(context.Context, contracts.AccessScope) error
type session struct {
	mu           sync.Mutex
	tx           *sql.Tx
	ctx          context.Context
	scope        contracts.AccessScope
	verify       VerifyScope
	closed       bool
	failed       bool
	reservations map[contracts.ID]WithdrawalCommand
}

func cloneScope(s contracts.AccessScope) contracts.AccessScope {
	s.KnowledgeBaseIDs = append([]contracts.ID(nil), s.KnowledgeBaseIDs...)
	return s
}

// WithinPublication supplies only History/Plan ports. Never register it as the
// complete contracts.UnitOfWork: metadata/audit/outbox/journal ports are absent.
func WithinPublication(ctx context.Context, db *sql.DB, scope contracts.AccessScope, verify VerifyScope, fn func(context.Context, contracts.PublicationHistoryRepository, contracts.CleanupPlanRepository) error) error {
	if fn == nil || scope.Action == "document.parse" || scope.Action == "document.candidate.read" || scope.Action == "document.submit" || scope.Action == "revision.approve" || scope.Action == "publication.start" || scope.Action == "publication.prepare" {
		return ErrRepository
	}
	return withinSession(ctx, db, scope, verify, func(ctx context.Context, s *session) error {
		return fn(ctx, historyRepo{s}, planRepo{s})
	})
}

func withinSession(ctx context.Context, db *sql.DB, scope contracts.AccessScope, verify VerifyScope, fn func(context.Context, *session) error) error {
	if ctx == nil || db == nil || verify == nil || fn == nil || scope.ScopeID == "" || scope.SubjectID == "" || scope.SpaceID == "" || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] == "" || scope.PolicyVersion == 0 || scope.Action == "" || !scope.ExpiresAt.After(time.Now()) {
		return ErrRepository
	}
	switch scope.Action {
	case "publication.withdraw", "publication.publish", "document.delete", "document.parse", "document.candidate.read", "document.submit", "revision.approve", "publication.start", "publication.prepare", "publication.history.read", "publication.cleanup.read", "publication.cleanup.execute", "index.delete", "index.write", "index.export", "index.import":
	default:
		return ErrRepository
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrRepository
	}
	ctx, cancel := context.WithDeadline(ctx, scope.ExpiresAt)
	defer cancel()
	scope = cloneScope(scope)
	if verify(ctx, cloneScope(scope)) != nil {
		return ErrRepository
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ErrRepository
	}
	defer tx.Rollback()
	s := &session{tx: tx, ctx: ctx, scope: scope, verify: verify}
	err = fn(ctx, s)
	// Closing gates joins any cooperative in-flight repository method.
	s.mu.Lock()
	s.closed = true
	failed := s.failed
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if failed {
		return ErrRepository
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if verify(ctx, cloneScope(scope)) != nil {
		return ErrRepository
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if tx.Commit() != nil {
		return ErrCommitUnknown
	}
	return nil
}
func (s *session) enter(ctx context.Context, scope contracts.AccessScope) error {
	s.mu.Lock()
	keep := false
	defer func() {
		if !keep {
			s.mu.Unlock()
		}
	}()
	if s.closed || s.failed || ctx == nil || s.ctx.Err() != nil || ctx.Err() != nil || !reflect.DeepEqual(scope, s.scope) {
		return ErrRepository
	}
	caller, ok := ctx.Deadline()
	parent, _ := s.ctx.Deadline()
	if !ok || caller.After(parent) {
		return ErrRepository
	}
	if s.verify(ctx, cloneScope(scope)) != nil {
		return ErrRepository
	}
	// A verifier may finish successfully after cancellation; do not begin SQL
	// merely because its return value is nil.
	if s.ctx.Err() != nil || ctx.Err() != nil {
		return ErrRepository
	}
	keep = true
	return nil
}

func nullableID(id *contracts.ID) any {
	if id == nil {
		return nil
	}
	return string(*id)
}
func idPointer(value sql.NullString) *contracts.ID {
	if !value.Valid {
		return nil
	}
	id := contracts.ID(value.String)
	return &id
}

const timeFormat = "2006-01-02T15:04:05.000000000Z"
