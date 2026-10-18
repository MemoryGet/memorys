package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// DeletionControlEventBus exposes deletion control outbox events through the
// provider-neutral EventBus contract. Other event families can use separate
// adapters without widening this SQL type.
type DeletionControlEventBus struct{ DB *sql.DB }

var _ contracts.EventBus = DeletionControlEventBus{}

func (p DeletionControlEventBus) Publish(context.Context, contracts.EventEnvelope) error {
	return ErrRepository
}

func (p DeletionControlEventBus) Consume(ctx context.Context, q contracts.EventConsumeRequest) (contracts.EventPage, error) {
	var out contracts.EventPage
	if ctx == nil || p.DB == nil || q.ConsumerID == "" || q.Limit < 1 || q.Limit > 100 || q.Request.Deadline.IsZero() || len(q.Cursor) > 4096 {
		return out, ErrRepository
	}
	if q.Cursor != "" {
		if len(q.Cursor) != eventCursorWidth {
			return out, ErrRepository
		}
		if _, err := strconv.ParseInt(q.Cursor, 10, 64); err != nil {
			return out, ErrRepository
		}
	}
	ctx, cancel := context.WithDeadline(ctx, q.Request.Deadline)
	defer cancel()
	rows, err := p.DB.QueryContext(ctx, `SELECT o.event_seq,o.id,o.event_type,o.aggregate_version,o.job_id,o.space_id,a.occurred_at,o.payload FROM akos_withdraw_v1.document_delete_job_control_outbox o JOIN akos_withdraw_v1.document_delete_job_control_audit a ON a.id=o.audit_id WHERE ($1='' OR o.event_seq>$1::bigint) ORDER BY o.event_seq LIMIT $2`, q.Cursor, q.Limit)
	if err != nil {
		return out, ErrRepository
	}
	defer rows.Close()
	var lastSequence int64
	for rows.Next() {
		var event contracts.EventEnvelope
		var sequence int64
		var version uint64
		var raw []byte
		if err := rows.Scan(&sequence, &event.ID, &event.Type, &version, &event.AggregateID, &event.SpaceID, &event.OccurredAt, &raw); err != nil || sequence <= 0 || len(raw) > 65536 {
			return contracts.EventPage{}, ErrRepository
		}
		lastSequence = sequence
		event.SchemaVersion = 1
		event.AggregateVersion = version
		var payload struct {
			ResourceIDs   []contracts.ID   `json:"resource_ids"`
			RevisionID    *contracts.ID    `json:"revision_id"`
			State         string           `json:"state"`
			ContentDigest contracts.Digest `json:"content_digest"`
		}
		if json.Unmarshal(raw, &payload) != nil {
			return contracts.EventPage{}, ErrRepository
		}
		event.Payload = contracts.EventPayload{ResourceIDs: payload.ResourceIDs, RevisionID: payload.RevisionID, State: payload.State, ContentDigest: payload.ContentDigest}
		out.Events = append(out.Events, event)
	}
	if err := rows.Err(); err != nil || ctx.Err() != nil {
		return contracts.EventPage{}, ErrRepository
	}
	if len(out.Events) > 0 {
		// event_seq is an identity-backed, immutable ordering key. A fixed
		// width decimal cursor preserves the SQL monotonic-upsert invariant.
		out.NextCursor = formatEventCursor(lastSequence)
	} else {
		out.NextCursor = q.Cursor
	}
	return out, nil
}

const eventCursorWidth = 20

func formatEventCursor(sequence int64) string {
	if sequence <= 0 {
		return ""
	}
	raw := strconv.FormatInt(sequence, 10)
	if len(raw) >= eventCursorWidth {
		return raw
	}
	return "00000000000000000000"[:eventCursorWidth-len(raw)] + raw
}
