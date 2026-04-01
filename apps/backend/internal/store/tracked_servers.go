package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrTrackedServerNotFound = errors.New("tracked server not found")
var ErrTrackedServerAlreadyExists = errors.New("tracked server already exists")

type TrackedServer struct {
	BmID          string
	Name          string
	LastPolledAt  *time.Time
	LastPollError string
}

func (p *PostgresStore) ListTrackedServers(ctx context.Context) ([]TrackedServer, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT bm_id, name, last_polled_at, last_poll_error
		FROM tracked_servers
		ORDER BY name ASC, bm_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TrackedServer, 0)
	for rows.Next() {
		var item TrackedServer
		if err := rows.Scan(&item.BmID, &item.Name, &item.LastPolledAt, &item.LastPollError); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (p *PostgresStore) CreateTrackedServer(ctx context.Context, bmID string, name string) (TrackedServer, error) {
	row := p.pool.QueryRow(ctx, `
		INSERT INTO tracked_servers (bm_id, name)
		VALUES ($1, $2)
		ON CONFLICT (bm_id) DO NOTHING
		RETURNING bm_id, name, last_polled_at, last_poll_error
	`, bmID, name)

	var item TrackedServer
	err := row.Scan(&item.BmID, &item.Name, &item.LastPolledAt, &item.LastPollError)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackedServer{}, ErrTrackedServerAlreadyExists
	}
	if err != nil {
		return TrackedServer{}, err
	}

	return item, nil
}

func (p *PostgresStore) DeleteTrackedServer(ctx context.Context, bmID string) error {
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM tracked_servers
		WHERE bm_id = $1
	`, bmID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTrackedServerNotFound
	}

	return nil
}

func (p *PostgresStore) NextTrackedServerToPoll(ctx context.Context) (*TrackedServer, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT bm_id, name, last_polled_at, last_poll_error
		FROM tracked_servers
		ORDER BY last_polled_at NULLS FIRST, bm_id ASC
		LIMIT 1
	`)

	var item TrackedServer
	err := row.Scan(&item.BmID, &item.Name, &item.LastPolledAt, &item.LastPollError)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &item, nil
}

func (p *PostgresStore) RecordTrackedServerPollResult(ctx context.Context, bmID string, polledAt time.Time, pollError string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE tracked_servers
		SET last_polled_at = $2,
			last_poll_error = $3
		WHERE bm_id = $1
	`, bmID, polledAt, pollError)
	return err
}
