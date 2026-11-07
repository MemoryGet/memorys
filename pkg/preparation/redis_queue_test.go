package preparation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type redisListFixture struct {
	values []string
	pushes []string
}

func (f *redisListFixture) LPop(context.Context, string) (string, error) {
	if len(f.values) == 0 {
		return "", nil
	}
	value := f.values[0]
	f.values = f.values[1:]
	return value, nil
}
func (f *redisListFixture) RPush(_ context.Context, _ string, value string) error {
	f.pushes = append(f.pushes, value)
	return nil
}

func TestRedisPublicationSourceDecodesAndRequeuesBoundedRequest(t *testing.T) {
	request := RunOnceRequest{Claim: document.PublicationClaimRequest{ClaimRequest: contracts.ClaimRequest{Scope: contracts.AccessScope{Action: "publication.prepare"}, JobID: "job", WorkerID: "worker"}}, CommandID: "command"}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &redisListFixture{values: []string{string(raw)}}
	source := RedisPublicationSource{Client: fixture, Key: "publication:prepare", MaxMessageBytes: 4096}
	got, err := source.Next(context.Background())
	if err != nil || got.CommandID != request.CommandID || got.Claim.JobID != request.Claim.JobID {
		t.Fatalf("redis request decode failed: %+v %v", got, err)
	}
	if err := source.Requeue(context.Background(), got); err != nil || len(fixture.pushes) != 1 {
		t.Fatalf("redis request requeue failed: %v", err)
	}
}

func TestRedisPublicationSourceRejectsEmptyAndMalformedMessages(t *testing.T) {
	fixture := &redisListFixture{values: []string{"", "{}"}}
	source := RedisPublicationSource{Client: fixture, Key: "queue"}
	if _, err := source.Next(context.Background()); !errors.Is(err, ErrRunnerNoWork) {
		t.Fatalf("empty queue was not reported as no work: %v", err)
	}
	if _, err := source.Next(context.Background()); err == nil {
		t.Fatal("malformed queue message accepted")
	}
}
