package events

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type busFunc func(context.Context, contracts.EventConsumeRequest) (contracts.EventPage, error)

func (f busFunc) Publish(context.Context, contracts.EventEnvelope) error { return nil }
func (f busFunc) Consume(c context.Context, q contracts.EventConsumeRequest) (contracts.EventPage, error) {
	return f(c, q)
}

type receiptStore struct {
	seen     map[contracts.ID]bool
	appended []contracts.ConsumerReceipt
}

func (r *receiptStore) Contains(context.Context, contracts.ID, contracts.ID) (bool, error) {
	return r.seen["event"], nil
}
func (r *receiptStore) Append(_ context.Context, q contracts.ConsumerReceipt) error {
	r.seen[q.EventID] = true
	r.appended = append(r.appended, q)
	return nil
}

func validEvent() contracts.EventEnvelope {
	return contracts.EventEnvelope{ID: "event", Type: "document.delete.cleanup.cancelled", SchemaVersion: 1, AggregateID: "job", AggregateVersion: 2, SpaceID: "space", OccurredAt: time.Unix(1, 0)}
}

func TestConsumerAcknowledgesOnlyAfterHandlerAndSkipsReceipt(t *testing.T) {
	r := &receiptStore{seen: map[contracts.ID]bool{}}
	calls := 0
	c := Consumer{Bus: busFunc(func(context.Context, contracts.EventConsumeRequest) (contracts.EventPage, error) {
		return contracts.EventPage{Events: []contracts.EventEnvelope{validEvent()}, NextCursor: "next"}, nil
	}), Receipts: r, ConsumerID: "consumer", Limit: 1, Handle: func(context.Context, contracts.EventEnvelope) error { calls++; return nil }, Now: func() time.Time { return time.Unix(2, 0) }}
	deadline := time.Now().Add(time.Minute)
	q := contracts.EventConsumeRequest{ConsumerID: "consumer", Request: contracts.RequestContext{RequestID: "request", Deadline: deadline}, Limit: 1}
	cursor, n, err := c.ConsumeOnce(context.Background(), q)
	if err != nil || cursor != "next" || n != 1 || calls != 1 || len(r.appended) != 1 {
		t.Fatalf("first delivery failed: %v %s %d %d", err, cursor, n, calls)
	}
	_, _, err = c.ConsumeOnce(context.Background(), q)
	if err != nil || calls != 1 || len(r.appended) != 1 {
		t.Fatal("duplicate event was redelivered")
	}
}

func TestConsumerDoesNotAdvanceCursorWhenHandlerFails(t *testing.T) {
	r := &receiptStore{seen: map[contracts.ID]bool{}}
	want := errors.New("handler failed")
	c := Consumer{Bus: busFunc(func(context.Context, contracts.EventConsumeRequest) (contracts.EventPage, error) {
		return contracts.EventPage{Events: []contracts.EventEnvelope{validEvent()}, NextCursor: "next"}, nil
	}), Receipts: r, ConsumerID: "consumer", Handle: func(context.Context, contracts.EventEnvelope) error { return want }}
	q := contracts.EventConsumeRequest{ConsumerID: "consumer", Request: contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Minute)}, Limit: 1}
	cursor, n, err := c.ConsumeOnce(context.Background(), q)
	if !errors.Is(err, want) || cursor != "" || n != 0 || len(r.appended) != 0 {
		t.Fatalf("failed delivery advanced: %v %s %d", err, cursor, n)
	}
}
