package preparation

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrWorkerService = errors.New("publication worker service unavailable")

// QueueNext is the narrow discovery boundary used by the host. PostgreSQL,
// Redis, or another durable queue can implement it without becoming coupled
// to worker lifecycle or HTTP composition.
type QueueNext func(context.Context) (RunOnceRequest, error)

// Service owns one long-running preparation worker. Runner still owns claim,
// fencing and completion semantics; Service only owns process lifecycle and
// readiness. It deliberately does not retry a failed step itself—the queue
// and durable lease expiry remain the retry authority.
type Service struct {
	runner   Runner
	next     QueueNext
	interval time.Duration
	onError  func(error)

	mu      sync.Mutex
	started bool
	stopped bool
	err     error
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewService(runner Runner, next QueueNext, interval time.Duration, onError func(error)) (*Service, error) {
	if runner.Tasks == nil || runner.Prepare == nil || runner.Complete == nil || next == nil || interval < time.Millisecond || interval > time.Minute {
		return nil, ErrWorkerService
	}
	return &Service{runner: runner, next: next, interval: interval, onError: onError}, nil
}

func (s *Service) Start(ctx context.Context) error {
	if s == nil || ctx == nil {
		return ErrWorkerService
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return ErrWorkerService
	}
	child, cancel := context.WithCancel(ctx)
	s.started, s.stopped, s.err, s.cancel, s.done = true, false, nil, cancel, make(chan struct{})
	done := s.done
	go func() {
		err := s.runner.Run(child, s.next, s.interval, s.onError)
		s.mu.Lock()
		s.err = err
		s.stopped = true
		close(done)
		s.mu.Unlock()
	}()
	return nil
}

func (s *Service) Stop() error {
	if s == nil {
		return ErrWorkerService
	}
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	cancel()
	<-done
	s.mu.Lock()
	err := s.err
	s.mu.Unlock()
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func (s *Service) Ready() error {
	if s == nil {
		return ErrWorkerService
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped || s.err != nil {
		return ErrWorkerService
	}
	return nil
}
