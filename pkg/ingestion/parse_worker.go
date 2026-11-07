package ingestion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrParseWorker = errors.New("parse worker unavailable")
var ErrParseTaskNoWork = errors.New("parse worker has no queued work")

type ParseTaskLease struct {
	Space, KnowledgeBase contracts.ID
	JobID                contracts.ID
	JobVersion           uint64
	Lease                contracts.Lease
	Payload              []byte
}

type ParseTaskQueue interface {
	ClaimTask(context.Context) (ParseTaskLease, error)
	CompleteTask(context.Context, ParseTaskLease, contracts.ID) error
}

// ParseTaskFailureQueue is optional so lightweight queues can keep the lease
// for takeover, while durable queues can record a fenced failed state. It is
// deliberately separate from CompleteTask: a failed parser attempt must not
// be confused with a successfully committed candidate.
type ParseTaskFailureQueue interface {
	FailTask(context.Context, ParseTaskLease, contracts.PublicError) error
}

// ParseWorker composes durable task claiming with CandidateService. Structural
// and preparation failures are durably fenced when the queue supports it;
// commit/completion errors leave the lease for takeover because their effect
// may be unknown.
type ParseWorker struct {
	Queue   ParseTaskQueue
	Decode  func([]byte) (DraftParseCommand, error)
	Service CandidateService
}

func (w ParseWorker) RunOnce(ctx context.Context) error {
	if ctx == nil || w.Queue == nil || w.Decode == nil {
		return ErrParseWorker
	}
	lease, err := w.Queue.ClaimTask(ctx)
	if err != nil {
		return err
	}
	if lease.JobID == "" || lease.JobVersion == 0 || lease.Lease.JobID != lease.JobID || lease.Lease.WorkerID == "" || lease.Lease.AttemptID == "" || lease.Lease.FencingToken == 0 || len(lease.Payload) == 0 {
		return ErrParseWorker
	}
	command, err := w.Decode(lease.Payload)
	if err != nil {
		return w.fail(ctx, lease, ErrParseWorker)
	}
	prepared, err := w.Service.Prepare(ctx, command)
	if err != nil {
		return w.fail(ctx, lease, err)
	}
	receipt, err := w.Service.Commit(ctx, prepared)
	if err != nil {
		return err
	}
	if receipt.CandidateID == "" || receipt.DraftID != command.DraftID || receipt.DocumentID != command.DocumentID {
		return w.fail(ctx, lease, ErrParseWorker)
	}
	if err := w.Queue.CompleteTask(ctx, lease, receipt.CandidateID); err != nil {
		return err
	}
	return nil
}

func (w ParseWorker) fail(ctx context.Context, lease ParseTaskLease, cause error) error {
	if cause == nil || ctx == nil || ctx.Err() != nil {
		return cause
	}
	queue, ok := w.Queue.(ParseTaskFailureQueue)
	if !ok {
		return cause
	}
	safe := contracts.PublicError{Code: "dependency_unavailable", SafeMessage: "解析任务失败，可重试。", Retryable: true, RequestID: lease.Lease.AttemptID}
	if err := queue.FailTask(ctx, lease, safe); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (w ParseWorker) Run(ctx context.Context, interval time.Duration, onError func(error)) error {
	if ctx == nil || w.Queue == nil || w.Decode == nil || interval < time.Millisecond || interval > time.Minute {
		return ErrParseWorker
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := w.RunOnce(ctx)
		if err != nil && !errors.Is(err, ErrParseTaskNoWork) && onError != nil {
			onError(err)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func DecodeParseTaskPayload(raw []byte) (DraftParseCommand, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return DraftParseCommand{}, ErrParseWorker
	}
	var command DraftParseCommand
	decoder := json.NewDecoder(bytes.NewReader(append([]byte(nil), raw...)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&command) != nil || command.DocumentID == "" || command.DraftID == "" || command.CandidateID == "" {
		return DraftParseCommand{}, ErrParseWorker
	}
	return command, nil
}
