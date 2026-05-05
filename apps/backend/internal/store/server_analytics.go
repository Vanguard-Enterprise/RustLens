package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type ServerPlayerCountPoint struct {
	TimestampUnix int64   `json:"timestampUnix"`
	Min           int     `json:"min"`
	Value         float64 `json:"value"`
	Max           int     `json:"max"`
}

type ServerPlayerCountHistoryCacheEntry struct {
	ServerBmID string
	Points     []ServerPlayerCountPoint
	FetchedAt  time.Time
}

func (p *PostgresStore) GetServerPlayerCountHistory(ctx context.Context, serverBmID string) (*ServerPlayerCountHistoryCacheEntry, error) {
	fetchedAt, err := p.getServerPlayerCountHistoryFetchedAt(ctx, serverBmID)
	if err != nil {
		return nil, err
	}
	if fetchedAt == nil {
		return nil, nil
	}

	rows, err := p.pool.Query(ctx, `
		SELECT timestamp_unix, min, value, max
		FROM server_player_count_history
		WHERE server_bm_id = $1
		ORDER BY timestamp_unix ASC
	`, serverBmID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entry := ServerPlayerCountHistoryCacheEntry{ServerBmID: serverBmID, FetchedAt: *fetchedAt}
	for rows.Next() {
		var point ServerPlayerCountPoint
		if err := rows.Scan(&point.TimestampUnix, &point.Min, &point.Value, &point.Max); err != nil {
			return nil, err
		}
		entry.Points = append(entry.Points, point)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &entry, nil
}

func (p *PostgresStore) getServerPlayerCountHistoryFetchedAt(ctx context.Context, serverBmID string) (*time.Time, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT fetched_at
		FROM server_player_count_history_fetches
		WHERE server_bm_id = $1
	`, serverBmID)

	var fetchedAt time.Time
	if err := row.Scan(&fetchedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &fetchedAt, nil
}

func (p *PostgresStore) UpsertServerPlayerCountHistory(ctx context.Context, serverBmID string, points []ServerPlayerCountPoint) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, point := range points {
		if point.TimestampUnix == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO server_player_count_history (server_bm_id, timestamp_unix, min, value, max, observed_at)
			VALUES ($1, $2, $3, $4, $5, NOW())
			ON CONFLICT (server_bm_id, timestamp_unix) DO UPDATE
			SET min = EXCLUDED.min,
				value = EXCLUDED.value,
				max = EXCLUDED.max,
				observed_at = NOW()
		`, serverBmID, point.TimestampUnix, point.Min, point.Value, point.Max); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO server_player_count_history_fetches (server_bm_id, fetched_at)
		VALUES ($1, NOW())
		ON CONFLICT (server_bm_id) DO UPDATE
		SET fetched_at = NOW()
	`, serverBmID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
