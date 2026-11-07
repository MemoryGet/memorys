package ingestion

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ParseWorkerRuntime owns the lifetime of one durable parser loop. The host
// supplies the queue, parser and authorization-bound CandidateService through
// ParseWorker; this type only provides start/stop/readiness semantics.
//
// A stopped runtime is never ready again. An unexpected worker error is kept
// as a readiness failure so a supervisor cannot advertise a dead parser as
// healthy.
type ParseWorkerRuntime struct {
	worker   ParseWorker
	interval time.Duration
	onError  func(error)

	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	started bool
	running bool
	stopped bool
	lastErr error
}

var ErrParseWorkerRuntime = errors.New("parse worker runtime unavailable")

func NewParseWorkerRuntime(worker ParseWorker, interval time.Duration, onError func(error)) (*ParseWorkerRuntime, error) {
	if worker.Queue == nil || worker.Decode == nil || interval < time.Millisecond || interval > time.Minute {
		return nil, ErrParseWorkerRuntime
	}
	return &ParseWorkerRuntime{worker: worker, interval: interval, onError: onError}, nil
}

func (r *ParseWorkerRuntime) Start(ctx context.Context) error {
	if r == nil || ctx == nil || ctx.Err() != nil {
		return ErrParseWorkerRuntime
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.stopped {
		return ErrParseWorkerRuntime
	}
	child, cancel := context.WithCancel(ctx)
	r.cancel, r.done, r.started, r.running = cancel, make(chan struct{}), true, true
	go r.run(child)
	return nil
}

func (r *ParseWorkerRuntime) run(ctx context.Context) {
	err := r.worker.Run(ctx, r.interval, r.onError)
	r.mu.Lock()
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		r.lastErr = err
	}
	r.running = false
	close(r.done)
	r.mu.Unlock()
}

func (r *ParseWorkerRuntime) Stop() error {
	if r == nil {
		return ErrParseWorkerRuntime
	}
	r.mu.Lock()
	if !r.started {
		r.mu.Unlock()
		return nil
	}
	cancel, done := r.cancel, r.done
	r.stopped = true
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}

func (r *ParseWorkerRuntime) Ready() error {
	if r == nil {
		return ErrParseWorkerRuntime
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started || !r.running || r.stopped || r.lastErr != nil {
		return ErrParseWorkerRuntime
	}
	return nil
}
