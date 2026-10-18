package cleanup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

// QueueBinding binds an internal cursor to a currently verified worker scope.
// It provides context binding, not authenticity; scope/resource checks remain
// mandatory. Ephemeral scope IDs/expirations are deliberately excluded.
func QueueBinding(scope contracts.AccessScope, worker contracts.ID, limit int) (contracts.Digest, error) {
	if scope.Action != "publication.cleanup.execute" || scope.SubjectID != worker || !validID(worker) || !validID(scope.ScopeID) || !validID(scope.SpaceID) || len(scope.KnowledgeBaseIDs) != 1 || !validID(scope.KnowledgeBaseIDs[0]) || scope.PolicyVersion == 0 || !scope.ExpiresAt.After(time.Now()) || limit < 1 || limit > 100 {
		return "", ErrRejected
	}
	raw, e := json.Marshal(map[string]any{"format": "akos.delete.queue-cursor/1", "subject": scope.SubjectID, "worker": worker, "space": scope.SpaceID, "kb": scope.KnowledgeBaseIDs[0], "action": scope.Action, "policy": strconv.FormatUint(scope.PolicyVersion, 10), "limit": limit})
	if e != nil {
		return "", ErrRejected
	}
	raw, e = canonical.JSON(raw)
	if e != nil {
		return "", ErrRejected
	}
	sum := sha256.Sum256(append([]byte("akos.delete.queue-cursor/1\n"), raw...))
	return contracts.Digest(hex.EncodeToString(sum[:])), nil
}

type DeletionPollConfig struct {
	Queue  contracts.DeletionCleanupQueue
	Runner contracts.DeletionJobRunner
	// Required trusted provider: renew actual worker authority every cycle.
	Scope                                          func(context.Context) (contracts.AccessScope, error)
	WorkerID, SpaceID, KnowledgeBaseID             contracts.ID
	PageSize, MaxPages                             int
	CycleBudget, PollInterval, TargetLeaseDuration time.Duration
}
type DeletionPollReport struct {
	Discovered, Attempted, Completed, Failed, WorkErrors int
	SweepCompleted                                       bool
}
type DeletionPoller struct {
	config  DeletionPollConfig
	gate    chan struct{}
	cursor  *contracts.DeletionQueueCursor
	binding contracts.Digest
	running atomic.Bool
}

func NewDeletionPoller(c DeletionPollConfig) (*DeletionPoller, error) {
	if c.Queue == nil || c.Runner == nil || c.Scope == nil || !validID(c.WorkerID) || !validID(c.SpaceID) || !validID(c.KnowledgeBaseID) || c.PageSize < 1 || c.PageSize > 100 || c.MaxPages < 1 || c.MaxPages > 1000 || c.CycleBudget < time.Millisecond || c.CycleBudget > 30*time.Second || c.PollInterval < time.Millisecond || c.PollInterval > time.Minute || c.TargetLeaseDuration < time.Millisecond || c.TargetLeaseDuration > 30*time.Second || c.TargetLeaseDuration%time.Millisecond != 0 {
		return nil, ErrRejected
	}
	p := &DeletionPoller{config: c, gate: make(chan struct{}, 1)}
	p.gate <- struct{}{}
	return p, nil
}
func ownPollValue[T any](v T) (T, error) {
	var out T
	raw, e := json.Marshal(v)
	if e != nil || len(raw) > 1<<20 {
		return out, ErrRejected
	}
	if json.Unmarshal(raw, &out) != nil {
		return out, ErrRejected
	}
	return out, nil
}

func (p *DeletionPoller) RunOnce(ctx context.Context) (DeletionPollReport, error) {
	var report DeletionPollReport
	if ctx == nil || p == nil || p.gate == nil {
		return report, ErrRejected
	}
	select {
	case <-ctx.Done():
		return report, ctx.Err()
	case <-p.gate:
	}
	defer func() { p.gate <- struct{}{} }()
	ctx, cancel := context.WithTimeout(ctx, p.config.CycleBudget)
	defer cancel()
	if ctx.Err() != nil {
		return report, ctx.Err()
	}
	scope, e := p.config.Scope(ctx)
	if e != nil {
		return report, e
	}
	if ctx.Err() != nil {
		return report, ctx.Err()
	}
	scope, e = ownPollValue(scope)
	if e != nil || scope.SpaceID != p.config.SpaceID || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] != p.config.KnowledgeBaseID {
		return report, ErrRejected
	}
	binding, e := QueueBinding(scope, p.config.WorkerID, p.config.PageSize)
	if e != nil {
		return report, e
	}
	if p.binding != binding {
		p.cursor = nil
		p.binding = binding
	}
	ctx, expire := context.WithDeadline(ctx, scope.ExpiresAt)
	defer expire()
	deadline, _ := ctx.Deadline()
	var nonce [16]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		return report, e
	}
	request := contracts.RequestContext{RequestID: contracts.ID("delete-poll-" + hex.EncodeToString(nonce[:])), Deadline: deadline}
	for pageNumber := 0; pageNumber < p.config.MaxPages; pageNumber++ {
		q, e := ownPollValue(contracts.DeletionQueueRequest{Request: request, Scope: scope, WorkerID: p.config.WorkerID, Limit: p.config.PageSize, Cursor: p.cursor})
		if e != nil {
			return report, e
		}
		page, e := p.config.Queue.DiscoverDeletionCleanup(ctx, q)
		if e != nil {
			return report, e
		}
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		page, e = ownPollValue(page)
		if e != nil || len(page.Items) > p.config.PageSize {
			return report, ErrRejected
		}
		after, upper := contracts.ID(""), contracts.ID("")
		if p.cursor != nil {
			after, upper = p.cursor.AfterJobID, p.cursor.UpperJobID
		}
		if len(page.Items) > 0 && !validID(page.UpperJobID) || upper != "" && page.UpperJobID != upper {
			return report, ErrRejected
		}
		if page.UpperJobID != "" {
			upper = page.UpperJobID
		}
		if page.Next != nil {
			if len(page.Items) == 0 || page.Next.Binding != binding || !validID(page.Next.UpperJobID) || page.Next.AfterJobID != page.Items[len(page.Items)-1].JobID || page.Next.AfterJobID >= page.Next.UpperJobID || upper != "" && page.Next.UpperJobID != upper {
				return report, ErrRejected
			}
			upper = page.Next.UpperJobID
		}
		// Validate the whole page before allowing any side effect.
		for _, ticket := range page.Items {
			if !validID(ticket.JobID) || !validID(ticket.ResourceID) || ticket.SpaceID != scope.SpaceID || ticket.KnowledgeBaseID != scope.KnowledgeBaseIDs[0] || !validDigest(ticket.ProfileDigest) || ticket.JobID <= after || upper != "" && ticket.JobID > upper {
				return report, ErrRejected
			}
			after = ticket.JobID
		}
		report.Discovered += len(page.Items)
		for _, ticket := range page.Items {
			if ctx.Err() != nil {
				return report, ctx.Err()
			}
			input, e := ownPollValue(contracts.DeletionCleanupRequest{Request: contracts.RequestContext{RequestID: request.RequestID, Deadline: deadline, ProfileDigest: ticket.ProfileDigest}, Scope: scope, JobID: ticket.JobID, WorkerID: p.config.WorkerID, TargetLeaseDuration: p.config.TargetLeaseDuration})
			if e != nil {
				return report, e
			}
			result, workError := p.config.Runner.RunOne(ctx, input)
			report.Attempted++
			// Persist position even on a per-job error: terminal failures are excluded
			// by discovery, and busy targets must not starve later jobs.
			p.cursor = &contracts.DeletionQueueCursor{AfterJobID: ticket.JobID, UpperJobID: upper, Binding: binding}
			if workError != nil {
				report.WorkErrors++
			}
			if result.Job.ID == "" && workError != nil {
				return report, workError
			}
			if result.Job.ID != ticket.JobID || result.ResourceID != ticket.ResourceID || result.Job.SpaceID != ticket.SpaceID || result.Job.ProfileDigest != ticket.ProfileDigest {
				return report, ErrRejected
			}
			if result.Job.State == "completed" {
				report.Completed++
			}
			if result.Job.State == "failed" {
				report.Failed++
			}
			if ctx.Err() != nil {
				return report, ctx.Err()
			}
		}
		if page.Next == nil {
			p.cursor = nil
			report.SweepCompleted = true
			return report, nil
		}
	}
	return report, nil
}

// Run does not outlive its caller and always waits between cycles, including
// work-error or empty cycles. Scope/discovery errors stop the loop fail-closed.
func (p *DeletionPoller) Run(ctx context.Context, observe func(DeletionPollReport)) error {
	if ctx == nil || p == nil || p.gate == nil || !p.running.CompareAndSwap(false, true) {
		return ErrRejected
	}
	defer p.running.Store(false)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		report, e := p.RunOnce(ctx)
		if observe != nil {
			observe(report)
		}
		if e != nil {
			return e
		}
		timer := time.NewTimer(p.config.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
