package events

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrRunner = errors.New("event runner unavailable")

type CursorStore interface {
	Load(context.Context, contracts.ID) (string, error)
	Save(context.Context, contracts.ID, string) error
}

type RunnerConfig struct {
	Consumer     Consumer
	ConsumerID   contracts.ID
	CursorStore  CursorStore
	Limit        int
	Interval     time.Duration
	CycleTimeout time.Duration
	Cursor       string
	OnError      func(error)
}

// Runner owns only scheduling and the in-memory cursor. A host must persist
// the cursor at its chosen durability boundary before treating delivery as
// externally complete.
type Runner struct {
	consumer Consumer
	limit    int
	interval time.Duration
	timeout  time.Duration
	mu       sync.Mutex
	cursor   string
	store    CursorStore
	lastErr  error
	cancel   context.CancelFunc
	done     chan struct{}
	started  bool
	stopped  bool
	onError  func(error)
}

type Snapshot struct {
	Cursor  string
	Started bool
	Stopped bool
	LastErr error
}

func NewRunner(c RunnerConfig) (*Runner, error) {
	if c.ConsumerID == "" || c.CursorStore == nil || c.Limit < 1 || c.Limit > 100 || c.Interval < time.Millisecond || c.Interval > time.Hour || c.CycleTimeout < time.Second || c.CycleTimeout > time.Hour {
		return nil, ErrRunner
	}
	c.Consumer.ConsumerID = c.ConsumerID
	c.Consumer.Limit = c.Limit
	if c.Consumer.Bus == nil || c.Consumer.Receipts == nil || c.Consumer.Handle == nil {
		return nil, ErrRunner
	}
	return &Runner{consumer: c.Consumer, limit: c.Limit, interval: c.Interval, timeout: c.CycleTimeout, cursor: c.Cursor, store: c.CursorStore, onError: c.OnError}, nil
}

func (r *Runner) Start(ctx context.Context) error {
	if r == nil || ctx == nil {
		return ErrRunner
	}
	r.mu.Lock()
	if r.started || r.stopped {
		r.mu.Unlock()
		return ErrRunner
	}
	r.started = true
	child, cancel := context.WithCancel(ctx)
	loaded, err := r.store.Load(child, r.consumer.ConsumerID)
	if err != nil {
		cancel()
		r.started = false
		r.mu.Unlock()
		return ErrRunner
	}
	if loaded != "" {
		r.cursor = loaded
	}
	r.cancel = cancel
	r.done = make(chan struct{})
	r.mu.Unlock()
	go r.loop(child)
	return nil
}

func (r *Runner) loop(ctx context.Context) {
	defer close(r.done)
	for {
		r.mu.Lock()
		cursor := r.cursor
		r.mu.Unlock()
		request := contracts.EventConsumeRequest{ConsumerID: r.consumer.ConsumerID, Cursor: cursor, Limit: r.limit, Request: contracts.RequestContext{RequestID: "event-runner", Deadline: time.Now().Add(r.timeout)}}
		next, count, err := r.consumer.ConsumeOnce(ctx, request)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			r.report(err)
			r.mu.Lock()
			r.lastErr = err
			r.mu.Unlock()
		} else {
			if next != cursor {
				if saveErr := r.store.Save(ctx, r.consumer.ConsumerID, next); saveErr != nil {
					if ctx.Err() != nil {
						return
					}
					r.mu.Lock()
					r.lastErr = saveErr
					r.mu.Unlock()
					// A durable cursor failure is a delivery failure: the local
					// cursor must not advance, and the host needs the same alert
					// signal as a handler or bus failure.  Wait in a cancellable
					// manner so shutdown never inherits the retry interval.
					r.report(saveErr)
					timer := time.NewTimer(r.interval)
					select {
					case <-ctx.Done():
						if !timer.Stop() {
							<-timer.C
						}
						return
					case <-timer.C:
					}
					continue
				}
			}
			r.mu.Lock()
			r.cursor = next
			r.lastErr = nil
			r.mu.Unlock()
		}
		if ctx.Err() != nil {
			return
		}
		if err == nil && count > 0 {
			continue
		}
		timer := time.NewTimer(r.interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func (r *Runner) report(err error) {
	if r.onError == nil {
		return
	}
	defer func() { _ = recover() }()
	r.onError(err)
}

// Stop is idempotent and waits for the active loop. It returns the last
// delivery error observed before shutdown.
func (r *Runner) Stop() error {
	if r == nil {
		return ErrRunner
	}
	r.mu.Lock()
	if !r.started {
		r.stopped = true
		r.mu.Unlock()
		return nil
	}
	cancel := r.cancel
	done := r.done
	r.stopped = true
	r.mu.Unlock()
	cancel()
	<-done
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}

func (r *Runner) Cursor() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cursor
}

func (r *Runner) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return Snapshot{Cursor: r.cursor, Started: r.started, Stopped: r.stopped, LastErr: r.lastErr}
}
