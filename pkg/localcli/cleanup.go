package localcli

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

// localCleanupProvider owns the short-lived service credential used by the
// cleanup poller. It rotates the credential when its current scope expires;
// cleanup work never reuses the owner's web credential.
type localCleanupProvider struct {
	db *sql.DB
	s  *state

	mu     sync.Mutex
	worker *cleanup.WorkerAuthority
	closed bool
}

func newLocalCleanupProvider(db *sql.DB, s *state) *localCleanupProvider {
	return &localCleanupProvider{db: db, s: s}
}

func (p *localCleanupProvider) current() *cleanup.WorkerAuthority {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	return p.worker
}

func (p *localCleanupProvider) rotate(ctx context.Context) (*cleanup.WorkerAuthority, error) {
	if ctx == nil || p == nil || p.db == nil || p.s == nil {
		return nil, cleanup.ErrRejected
	}
	source, err := p.s.identity(ctx, p.db, true)
	if err != nil {
		return nil, err
	}
	defer source.close()
	authorizer := postgres.Authorizer{DB: p.db, ScopeLifetime: time.Minute}
	tasks := postgres.CleanupTasks{DB: p.db, Verify: p.verifyScope}
	worker, err := cleanup.NewWorkerAuthority(ctx, cleanup.WorkerAuthorityConfig{
		Credential:      source.credential,
		Auth:            source.auth,
		Authorizer:      authorizer,
		WorkerID:        p.s.m.Worker,
		SpaceID:         p.s.m.Space,
		KnowledgeBaseID: p.s.m.KB,
		VerifyOperation: tasks.VerifyDeletionOperation,
	})
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		worker.Close()
		return nil, cleanup.ErrRejected
	}
	old := p.worker
	p.worker = worker
	p.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return worker, nil
}

func (p *localCleanupProvider) active(ctx context.Context) (*cleanup.WorkerAuthority, error) {
	worker := p.current()
	if worker != nil {
		if _, err := worker.CurrentScope(ctx); err == nil {
			return worker, nil
		}
	}
	return p.rotate(ctx)
}

func (p *localCleanupProvider) scope(ctx context.Context) (contracts.AccessScope, error) {
	worker, err := p.active(ctx)
	if err != nil {
		return contracts.AccessScope{}, err
	}
	return worker.Scope(ctx)
}

func (p *localCleanupProvider) verifyScope(ctx context.Context, scope contracts.AccessScope) error {
	worker := p.current()
	if worker == nil {
		return cleanup.ErrRejected
	}
	return worker.VerifyScope(ctx, scope)
}

func (p *localCleanupProvider) authorizeJob(ctx context.Context, scope contracts.AccessScope, record contracts.ResourceJob) error {
	worker := p.current()
	if worker == nil {
		return cleanup.ErrRejected
	}
	return worker.AuthorizeJob(ctx, scope, record)
}

func (p *localCleanupProvider) indexRequest(ctx context.Context, op contracts.CleanupOperation) (contracts.IndexDeleteRequest, error) {
	worker := p.current()
	if worker == nil {
		return contracts.IndexDeleteRequest{}, cleanup.ErrRejected
	}
	return worker.IndexRequest(ctx, op)
}

type localCleanupTargets struct{ worker cleanup.Worker }

func (t localCleanupTargets) RunOne(ctx context.Context, scope contracts.AccessScope, plan, target, worker contracts.ID, duration time.Duration) error {
	return t.worker.RunOne(ctx, scope, plan, target, worker, duration)
}

func (p *localCleanupProvider) queue() postgres.DeletionCleanupQueue {
	return postgres.DeletionCleanupQueue{DB: p.db, Verify: p.verifyScope, AuthorizeJob: p.authorizeJob}
}

func (p *localCleanupProvider) runner() cleanup.DeletionWorker {
	tasks := postgres.CleanupTasks{DB: p.db, Verify: p.verifyScope}
	index := postgres.IndexCleanup{DB: p.db, Verify: p.verifyScope}
	binding := p.s.profile.Index.Bindings[contracts.IndexWriting]
	executor := adapters.IndexCleanup{Index: index, ModuleInstanceID: binding.InstanceID, ProfileDigest: p.s.profile.Index.Digest, Authorize: p.indexRequest}
	worker := cleanup.Worker{
		Tasks:       tasks,
		VerifyLease: tasks.VerifyLease,
		Resolve: func(ctx context.Context, op contracts.CleanupOperation) (contracts.CleanupExecutor, error) {
			if op.Target.Kind != "index" {
				return nil, cleanup.ErrRejected
			}
			return executor, nil
		},
		AuthorizeExecution: func(ctx context.Context, _ contracts.AccessScope, _ contracts.CleanupLease, op contracts.CleanupOperation) error {
			_, err := p.indexRequest(ctx, op)
			return err
		},
		VerifyReceipt: executor.VerifyReceipt,
	}
	jobs := postgres.DeletionCleanupJobs{DB: p.db, Verify: p.verifyScope, AuthorizeJob: p.authorizeJob}
	return cleanup.DeletionWorker{Jobs: jobs, Targets: localCleanupTargets{worker: worker}}
}

func (p *localCleanupProvider) start(ctx context.Context) error {
	if p == nil || ctx == nil {
		return cleanup.ErrRejected
	}
	boot, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := p.scope(boot); err != nil {
		return errors.New("本地清理 Worker 无法获得当前服务权限")
	}
	poller, err := cleanup.NewDeletionPoller(cleanup.DeletionPollConfig{
		Queue:               p.queue(),
		Runner:              p.runner(),
		Scope:               p.scope,
		WorkerID:            p.s.m.Worker,
		SpaceID:             p.s.m.Space,
		KnowledgeBaseID:     p.s.m.KB,
		PageSize:            10,
		MaxPages:            10,
		CycleBudget:         20 * time.Second,
		PollInterval:        2 * time.Second,
		TargetLeaseDuration: 10 * time.Second,
	})
	if err != nil {
		return err
	}
	go func() { _ = poller.Run(ctx, nil) }()
	return nil
}

func (p *localCleanupProvider) close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.closed = true
	worker := p.worker
	p.worker = nil
	p.mu.Unlock()
	if worker != nil {
		worker.Close()
	}
}
