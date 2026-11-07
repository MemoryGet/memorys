package preparation

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type RedisList interface {
	LPop(context.Context, string) (string, error)
	RPush(context.Context, string, string) error
}

// RedisPublicationSource is deliberately SDK-neutral. A host can wrap
// go-redis or another Redis client with these two methods without leaking the
// vendor type into preparation contracts.
type RedisPublicationSource struct {
	Client          RedisList
	Key             string
	MaxMessageBytes int
}

// NewRedisPublicationService composes the SDK-neutral Redis delivery source
// with the durable preparation worker lifecycle. The host only needs to wrap
// its Redis client as RedisList and inject the worker's real task store,
// preparer, and completion store.
func NewRedisPublicationService(runner Runner, client RedisList, key string, maxMessageBytes int, interval time.Duration, onError func(error)) (*Service, error) {
	source := RedisPublicationSource{Client: client, Key: key, MaxMessageBytes: maxMessageBytes}
	return NewService(runner, source.Next, interval, onError)
}

func (s RedisPublicationSource) Next(ctx context.Context) (RunOnceRequest, error) {
	var empty RunOnceRequest
	if ctx == nil || s.Client == nil || s.Key == "" {
		return empty, ErrRunner
	}
	limit := s.MaxMessageBytes
	if limit == 0 {
		limit = 128 << 10
	}
	if limit < 1024 || limit > 1<<20 {
		return empty, ErrRunner
	}
	raw, err := s.Client.LPop(ctx, s.Key)
	if err != nil {
		return empty, err
	}
	if raw == "" {
		return empty, ErrRunnerNoWork
	}
	if len(raw) > limit {
		return empty, ErrRunner
	}
	var request RunOnceRequest
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || request.CommandID == "" || request.Claim.JobID == "" || request.Claim.WorkerID == "" || request.Claim.Scope.Action != "publication.prepare" {
		return empty, ErrRunner
	}
	return request, nil
}

func (s RedisPublicationSource) Requeue(ctx context.Context, request RunOnceRequest) error {
	if ctx == nil || s.Client == nil || s.Key == "" || request.CommandID == "" || request.Claim.JobID == "" {
		return ErrRunner
	}
	raw, err := json.Marshal(request)
	if err != nil || len(raw) > s.limit() {
		return ErrRunner
	}
	if err := s.Client.RPush(ctx, s.Key, string(raw)); err != nil {
		return err
	}
	return nil
}

func (s RedisPublicationSource) limit() int {
	if s.MaxMessageBytes == 0 {
		return 128 << 10
	}
	return s.MaxMessageBytes
}
