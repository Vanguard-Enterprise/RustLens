package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, connectionString string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return &PostgresStore{pool: pool}, nil
}

func (p *PostgresStore) Close() {
	p.pool.Close()
}

func (p *PostgresStore) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *PostgresStore) EnsureSchema(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS bot_worker_events (
			id BIGSERIAL PRIMARY KEY,
			worker_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	return err
}

func (p *PostgresStore) InsertWorkerEvent(ctx context.Context, workerID string, eventType string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO bot_worker_events (worker_id, event_type, payload)
		VALUES ($1, $2, $3::jsonb)
	`, workerID, eventType, string(encoded))

	return err
}
