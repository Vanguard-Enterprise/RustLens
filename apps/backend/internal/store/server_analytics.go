package store

import (
	"context"
	"encoding/json"
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

func (p *PostgresStore) GetServerPlayerCountHistoryCache(ctx context.Context, serverBmID string) (*ServerPlayerCountHistoryCacheEntry, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT server_bm_id, points, fetched_at
		FROM server_player_count_history_cache
		WHERE server_bm_id = $1
	`, serverBmID)

	var entry ServerPlayerCountHistoryCacheEntry
	var raw []byte
	if err := row.Scan(&entry.ServerBmID, &raw, &entry.FetchedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &entry.Points); err != nil {
			return nil, err
		}
	}
	return &entry, nil
}

func (p *PostgresStore) UpsertServerPlayerCountHistoryCache(ctx context.Context, serverBmID string, points []ServerPlayerCountPoint) error {
	if points == nil {
		points = []ServerPlayerCountPoint{}
	}
	encoded, err := json.Marshal(points)
	if err != nil {
		return err
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO server_player_count_history_cache (server_bm_id, points, fetched_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (server_bm_id) DO UPDATE
		SET points = EXCLUDED.points,
			fetched_at = NOW()
	`, serverBmID, encoded)
	return err
}
