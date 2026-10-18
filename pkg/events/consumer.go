// Package events contains transport-neutral, bounded event consumption.
// Delivery is at-least-once: handlers must be idempotent and receipts are
// written only after the handler succeeds.
package events

import (
	"context"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrConsumer = errors.New("event consumer unavailable")

type Handler func(context.Context, contracts.EventEnvelope) error

type Consumer struct {
	Bus        contracts.EventBus
	Receipts   contracts.ConsumerReceiptRepository
	ConsumerID contracts.ID
	Handle     Handler
	Limit      int
	Now        func() time.Time
}

func (c Consumer) ConsumeOnce(ctx context.Context, request contracts.EventConsumeRequest) (string, uint64, error) {
	if ctx == nil || c.Bus == nil || c.Receipts == nil || c.Handle == nil || c.ConsumerID == "" || request.ConsumerID != c.ConsumerID || request.Limit < 1 || request.Limit > 100 || request.Request.Deadline.IsZero() {
		return request.Cursor, 0, ErrConsumer
	}
	ctx, cancel := context.WithDeadline(ctx, request.Request.Deadline)
	defer cancel()
	page, err := c.Bus.Consume(ctx, request)
	if err != nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			return request.Cursor, 0, ctx.Err()
		}
		return request.Cursor, 0, ErrConsumer
	}
	processed := uint64(0)
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	for _, event := range page.Events {
		if !valid(event) {
			return request.Cursor, processed, ErrConsumer
		}
		seen, err := c.Receipts.Contains(ctx, c.ConsumerID, event.ID)
		if err != nil {
			return request.Cursor, processed, ErrConsumer
		}
		if !seen {
			if err = c.Handle(ctx, event); err != nil {
				return request.Cursor, processed, err
			}
			if err = c.Receipts.Append(ctx, contracts.ConsumerReceipt{ConsumerID: c.ConsumerID, EventID: event.ID, ProcessedAt: now().UTC()}); err != nil {
				return request.Cursor, processed, ErrConsumer
			}
		}
		processed++
	}
	return page.NextCursor, processed, nil
}

func valid(event contracts.EventEnvelope) bool {
	return event.ID != "" && event.Type != "" && event.SchemaVersion > 0 && event.AggregateID != "" && event.AggregateVersion > 0 && event.SpaceID != "" && !event.OccurredAt.IsZero()
}
