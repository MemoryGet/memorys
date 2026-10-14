package queryread

import (
	"context"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type guardedUnit struct{ backend contracts.QueryReadUnit }

// ProtectUnit installs automatic handle cleanup around a separately admitted
// read unit. It does not implement transactions or authenticate the scope.
func ProtectUnit(backend contracts.QueryReadUnit) (contracts.QueryReadUnit, error) {
	if backend == nil {
		return nil, ErrInvalid
	}
	return &guardedUnit{backend: backend}, nil
}

func (u *guardedUnit) WithinRead(ctx context.Context, scope contracts.AccessScope, callback func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
	if ctx == nil || callback == nil {
		return ErrInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if scope.ScopeID == "" || scope.SubjectID == "" || scope.SpaceID == "" || scope.Action != "knowledge.search" || scope.PolicyVersion == 0 || len(scope.KnowledgeBaseIDs) < 1 || len(scope.KnowledgeBaseIDs) > 256 {
		return ErrInvalid
	}
	if !scope.ExpiresAt.After(time.Now()) {
		return context.DeadlineExceeded
	}
	seen := map[contracts.ID]bool{}
	for _, kb := range scope.KnowledgeBaseIDs {
		if kb == "" || seen[kb] {
			return ErrInvalid
		}
		seen[kb] = true
	}
	scope.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	view, cancel := context.WithDeadline(ctx, scope.ExpiresAt)
	defer cancel()
	sentScope := scope
	sentScope.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	var mu sync.Mutex
	started, finished, ended, bad := false, false, false, false
	var handle *Guard
	var callbackErr error
	err := u.backend.WithinRead(view, sentScope, func(inner context.Context, id contracts.QueryReadIdentity, repo contracts.QueryReadRepository) (result error) {
		mu.Lock()
		if ended || started {
			bad = true
			mu.Unlock()
			return ErrInvalid
		}
		started = true
		mu.Unlock()
		defer func() { mu.Lock(); finished = true; callbackErr = result; mu.Unlock() }()
		if inner == nil {
			return ErrInvalid
		}
		deadline, _ := view.Deadline()
		combined, stop := context.WithDeadline(inner, deadline)
		defer stop()
		unlink := context.AfterFunc(view, stop)
		defer unlink()
		g, err := NewGuard(combined, repo, scope, id)
		if err != nil {
			return err
		}
		defer g.Close()
		mu.Lock()
		handle = g
		abort := ended
		mu.Unlock()
		if abort {
			return ErrInvalid
		}
		result = callback(g.ctx, id, g)
		if result == nil {
			result = g.ctx.Err()
		}
		return result
	})
	mu.Lock()
	ended = true
	valid := started && finished && !bad
	g := handle
	cbErr := callbackErr
	mu.Unlock()
	if !valid {
		cancel()
		if g != nil {
			g.Close()
		}
		return ErrInvalid
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if view.Err() != nil {
		return view.Err()
	}
	deadline, _ := view.Deadline()
	if !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	if cbErr != nil {
		return cbErr
	}
	if err != nil {
		return ErrRead
	}
	return nil
}
