package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/ingestion"
)

// RedisParseList is the provider-neutral subset needed by the parser queue.
// A host can wrap go-redis, rueidis, or another client without bringing that
// dependency into the repository or contracts packages.
type RedisParseList interface {
	LPop(context.Context, string) (string, error)
}

var ErrRedisParseQueue = errors.New("redis parse queue unavailable")

// RedisParseTaskSource carries only a task identity. The durable PostgreSQL
// row remains authoritative for payload, state, lease and fencing.
type RedisParseTaskSource struct {
	Client          RedisParseList
	Key             string
	MaxMessageBytes int
}

func (s RedisParseTaskSource) Next(ctx context.Context) (contracts.ID, error) {
	if ctx == nil || s.Client == nil || strings.TrimSpace(s.Key) == "" {
		return "", ErrRedisParseQueue
	}
	limit := s.MaxMessageBytes
	if limit == 0 {
		limit = 16 << 10
	}
	if limit < 128 || limit > 1<<20 {
		return "", ErrRedisParseQueue
	}
	raw, err := s.Client.LPop(ctx, s.Key)
	if err != nil {
		return "", err
	}
	if raw == "" {
		return "", ingestion.ErrParseTaskNoWork
	}
	if len(raw) > limit {
		return "", ErrRedisParseQueue
	}
	var message struct {
		TaskID contracts.ID `json:"task_id"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&message) != nil || !validRedisTaskID(message.TaskID) {
		return "", ErrRedisParseQueue
	}
	return message.TaskID, nil
}

func validRedisTaskID(id contracts.ID) bool {
	return id != "" && utf8.ValidString(string(id)) && utf8.RuneCountInString(string(id)) <= 200 && !strings.ContainsAny(string(id), "\r\n")
}

// RedisBoundParseTaskQueue combines Redis task hints with the durable parser
// repository. When Redis has no hint it scans PostgreSQL for queued work, so a
// lost notification does not make a task permanently invisible.
type RedisBoundParseTaskQueue struct {
	Repository    ParseTaskRepository
	Source        RedisParseTaskSource
	Space         contracts.ID
	KnowledgeBase contracts.ID
	WorkerID      contracts.ID
	LeaseDuration time.Duration
}

var _ ingestion.ParseTaskQueue = RedisBoundParseTaskQueue{}
var _ ingestion.ParseTaskFailureQueue = RedisBoundParseTaskQueue{}

func (q RedisBoundParseTaskQueue) ClaimTask(ctx context.Context) (ingestion.ParseTaskLease, error) {
	if q.LeaseDuration == 0 {
		q.LeaseDuration = 30 * time.Second
	}
	if q.Repository.DB == nil || q.Space == "" || q.KnowledgeBase == "" || q.WorkerID == "" || q.LeaseDuration < time.Millisecond || q.LeaseDuration > 30*time.Second {
		return ingestion.ParseTaskLease{}, ErrRedisParseQueue
	}
	var id contracts.ID
	var err error
	if q.Source.Client == nil || strings.TrimSpace(q.Source.Key) == "" {
		id, err = q.Repository.NextQueued(ctx, q.Space, q.KnowledgeBase)
	} else {
		id, err = q.Source.Next(ctx)
	}
	if errors.Is(err, ingestion.ErrParseTaskNoWork) {
		id, err = q.Repository.NextQueued(ctx, q.Space, q.KnowledgeBase)
	}
	if err != nil {
		return ingestion.ParseTaskLease{}, err
	}
	return q.Repository.ClaimTask(ctx, q.Space, q.KnowledgeBase, id, q.WorkerID, q.LeaseDuration)
}

func (q RedisBoundParseTaskQueue) CompleteTask(ctx context.Context, lease ingestion.ParseTaskLease, result contracts.ID) error {
	return q.Repository.CompleteTask(ctx, q.Space, q.KnowledgeBase, lease.Lease, lease.JobVersion, result)
}

func (q RedisBoundParseTaskQueue) FailTask(ctx context.Context, lease ingestion.ParseTaskLease, safe contracts.PublicError) error {
	return q.Repository.FailTask(ctx, q.Space, q.KnowledgeBase, lease.Lease, lease.JobVersion, safe)
}
