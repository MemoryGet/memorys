package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestFrozenViewCannotEscapeOrChangeItsBinding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	q := contracts.FrozenContentReadRequest{Request: contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Second)},
		Scope: testScope(), DocumentID: "document", RevisionID: "revision"}
	v := frozenReadView{ctx: ctx, request: q, record: contracts.FrozenContentRecord{DocumentID: q.DocumentID, RevisionID: q.RevisionID, Payload: []byte("original")}}
	one, err := v.ReadFrozenContent(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	one.Payload[0] = 'X'
	two, err := v.ReadFrozenContent(ctx, q)
	if err != nil || string(two.Payload) != "original" {
		t.Fatal("frozen view shares mutable output", err)
	}
	for _, change := range []func(*contracts.FrozenContentReadRequest){
		func(q *contracts.FrozenContentReadRequest) { q.DocumentID = "other" },
		func(q *contracts.FrozenContentReadRequest) { q.RevisionID = "other" },
		func(q *contracts.FrozenContentReadRequest) { q.Scope.Action = "knowledge.read" },
		func(q *contracts.FrozenContentReadRequest) { q.Request.RequestID = "other" },
		func(q *contracts.FrozenContentReadRequest) { q.Request.Deadline = q.Request.Deadline.Add(time.Second) },
	} {
		changed := q
		change(&changed)
		if _, err = v.ReadFrozenContent(ctx, changed); err == nil {
			t.Fatal("changed frozen binding accepted")
		}
	}
	cancel()
	if _, err = v.ReadFrozenContent(context.Background(), q); err == nil {
		t.Fatal("closed read view escaped")
	}
}
