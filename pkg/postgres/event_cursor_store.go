package postgres

import (
	"context"
	"database/sql"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// EventCursorStore persists one cursor per consumer. It deliberately has no
// relationship to deletion-job leases or worker fencing tokens.
type EventCursorStore struct{ DB *sql.DB }

func (p EventCursorStore) Load(ctx context.Context, consumerID contracts.ID) (string, error) {
	if ctx == nil || p.DB == nil || consumerID == "" {
		return "", ErrRepository
	}
	var cursor string
	err := p.DB.QueryRowContext(ctx, `SELECT cursor FROM akos_withdraw_v1.event_consumer_cursors WHERE consumer_id=$1`, consumerID).Scan(&cursor)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil || len(cursor) > 4096 {
		return "", ErrRepository
	}
	return cursor, nil
}

func (p EventCursorStore) Save(ctx context.Context, consumerID contracts.ID, cursor string) error {
	if ctx == nil || p.DB == nil || consumerID == "" || len(cursor) > 4096 {
		return ErrRepository
	}
	result, err := p.DB.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.event_consumer_cursors(consumer_id,cursor) VALUES($1,$2) ON CONFLICT(consumer_id) DO UPDATE SET cursor=EXCLUDED.cursor,updated_at=clock_timestamp() WHERE akos_withdraw_v1.event_consumer_cursors.cursor<=EXCLUDED.cursor`, consumerID, cursor)
	if err != nil {
		return ErrRepository
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return ErrRepository
	}
	return nil
}
