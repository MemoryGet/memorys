package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/ingestion"
)

type redisParseListFake struct {
	value string
	err   error
}

func (f *redisParseListFake) LPop(context.Context, string) (string, error) {
	value, err := f.value, f.err
	f.value = ""
	return value, err
}

func TestRedisParseTaskSourceDecodesBoundedTaskID(t *testing.T) {
	source := RedisParseTaskSource{Client: &redisParseListFake{value: `{"task_id":"task-1"}`}, Key: "parse:tasks", MaxMessageBytes: 4096}
	id, err := source.Next(context.Background())
	if err != nil || id != "task-1" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	empty := RedisParseTaskSource{Client: &redisParseListFake{}, Key: "parse:tasks", MaxMessageBytes: 4096}
	if _, err := empty.Next(context.Background()); !errors.Is(err, ingestion.ErrParseTaskNoWork) {
		t.Fatalf("empty queue err=%v", err)
	}
}

func TestRedisParseTaskSourceRejectsUntrustedMessage(t *testing.T) {
	for _, raw := range []string{`{"task_id":""}`, `{"task_id":"task","extra":1}`, `{"task_id":"task\n2"}`, `not-json`} {
		source := RedisParseTaskSource{Client: &redisParseListFake{value: raw}, Key: "parse:tasks", MaxMessageBytes: 4096}
		if _, err := source.Next(context.Background()); !errors.Is(err, ErrRedisParseQueue) {
			t.Fatalf("raw %q accepted: %v", raw, err)
		}
	}
}

func TestRedisBoundQueueAllowsDatabaseFallbackWhenSourceIsOmitted(t *testing.T) {
	queue := RedisBoundParseTaskQueue{Repository: ParseTaskRepository{}, Space: contracts.ID("space"), KnowledgeBase: contracts.ID("kb"), WorkerID: contracts.ID("worker")}
	if _, err := queue.ClaimTask(context.Background()); !errors.Is(err, ErrRedisParseQueue) {
		t.Fatalf("invalid queue composition returned %v", err)
	}
}
