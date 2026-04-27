package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrTrackedServerNotFound = errors.New("tracked server not found")
var ErrTrackedServerAlreadyExists = errors.New("tracked server already exists")

type TrackedServer struct {
	BmID          string
	Name          string
	IncludeInGuildSettings bool
	IsVitality    bool
	IsAtlas       bool
	IsVital       bool
	IsModded      bool
	IsOfficial    bool
	IsBuild       bool
	LastPolledAt  *time.Time
	LastPollError string
}

func (p *PostgresStore) ListTrackedServers(ctx context.Context) ([]TrackedServer, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT bm_id, name, include_in_guild_settings, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, last_polled_at, last_poll_error
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
		if err := rows.Scan(
			&item.BmID,
			&item.Name,
			&item.IncludeInGuildSettings,
			&item.IsVitality,
			&item.IsAtlas,
			&item.IsVital,
			&item.IsModded,
			&item.IsOfficial,
			&item.IsBuild,
			&item.LastPolledAt,
			&item.LastPollError,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (p *PostgresStore) CreateTrackedServer(ctx context.Context, bmID string, name string, includeInGuildSettings bool, isVitality bool, isAtlas bool, isVital bool, isModded bool, isOfficial bool, isBuild bool) (TrackedServer, error) {
	row := p.pool.QueryRow(ctx, `
		INSERT INTO tracked_servers (
			bm_id,
			name,
			include_in_guild_settings,
			is_vitality,
			is_atlas,
			is_vital,
			is_modded,
			is_official,
			is_build
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (bm_id) DO NOTHING
		RETURNING bm_id, name, include_in_guild_settings, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, last_polled_at, last_poll_error
	`, bmID, name, includeInGuildSettings, isVitality, isAtlas, isVital, isModded, isOfficial, isBuild)

	var item TrackedServer
	err := row.Scan(
		&item.BmID,
		&item.Name,
		&item.IncludeInGuildSettings,
		&item.IsVitality,
		&item.IsAtlas,
		&item.IsVital,
		&item.IsModded,
		&item.IsOfficial,
		&item.IsBuild,
		&item.LastPolledAt,
		&item.LastPollError,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackedServer{}, ErrTrackedServerAlreadyExists
	}
	if err != nil {
		return TrackedServer{}, err
	}

	return item, nil
}

func (p *PostgresStore) UpdateTrackedServer(
	ctx context.Context,
	currentBmID string,
	nextBmID string,
	name string,
	includeInGuildSettings bool,
	isVitality bool,
	isAtlas bool,
	isVital bool,
	isModded bool,
	isOfficial bool,
	isBuild bool,
) (TrackedServer, error) {
	if currentBmID == nextBmID {
		row := p.pool.QueryRow(ctx, `
			UPDATE tracked_servers
			SET name = $2,
				include_in_guild_settings = $3,
				is_vitality = $4,
				is_atlas = $5,
				is_vital = $6,
				is_modded = $7,
				is_official = $8,
				is_build = $9
			WHERE bm_id = $1
			RETURNING bm_id, name, include_in_guild_settings, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, last_polled_at, last_poll_error
		`, currentBmID, name, includeInGuildSettings, isVitality, isAtlas, isVital, isModded, isOfficial, isBuild)

		var item TrackedServer
		err := row.Scan(
			&item.BmID,
			&item.Name,
			&item.IncludeInGuildSettings,
			&item.IsVitality,
			&item.IsAtlas,
			&item.IsVital,
			&item.IsModded,
			&item.IsOfficial,
			&item.IsBuild,
			&item.LastPolledAt,
			&item.LastPollError,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return TrackedServer{}, ErrTrackedServerNotFound
		}
		if err != nil {
			return TrackedServer{}, err
		}

		if !includeInGuildSettings {
			if _, err := p.pool.Exec(ctx, `
				DELETE FROM guild_tracked_servers
				WHERE server_bm_id = $1
			`, currentBmID); err != nil {
				return TrackedServer{}, err
			}
		}

		return item, nil
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return TrackedServer{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var existing TrackedServer
	row := p.pool.QueryRow(ctx, `
		SELECT bm_id, name, include_in_guild_settings, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, last_polled_at, last_poll_error
		FROM tracked_servers
		WHERE bm_id = $1
	`, currentBmID)
	err = row.Scan(
		&existing.BmID,
		&existing.Name,
		&existing.IncludeInGuildSettings,
		&existing.IsVitality,
		&existing.IsAtlas,
		&existing.IsVital,
		&existing.IsModded,
		&existing.IsOfficial,
		&existing.IsBuild,
		&existing.LastPolledAt,
		&existing.LastPollError,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackedServer{}, ErrTrackedServerNotFound
	}
	if err != nil {
		return TrackedServer{}, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO tracked_servers (
			bm_id,
			name,
			include_in_guild_settings,
			is_vitality,
			is_atlas,
			is_vital,
			is_modded,
			is_official,
			is_build,
			last_polled_at,
			last_poll_error
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, nextBmID, name, includeInGuildSettings, isVitality, isAtlas, isVital, isModded, isOfficial, isBuild, existing.LastPolledAt, existing.LastPollError)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return TrackedServer{}, ErrTrackedServerAlreadyExists
		}
		return TrackedServer{}, err
	}

	if includeInGuildSettings {
		if _, err := tx.Exec(ctx, `
			UPDATE guild_tracked_servers
			SET server_bm_id = $2
			WHERE server_bm_id = $1
		`, currentBmID, nextBmID); err != nil {
			return TrackedServer{}, err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			DELETE FROM guild_tracked_servers
			WHERE server_bm_id = $1
		`, currentBmID); err != nil {
			return TrackedServer{}, err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE player_states
		SET server_bm_id = $2
		WHERE server_bm_id = $1
	`, currentBmID, nextBmID); err != nil {
		return TrackedServer{}, err
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM tracked_servers
		WHERE bm_id = $1
	`, currentBmID); err != nil {
		return TrackedServer{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TrackedServer{}, err
	}

	return TrackedServer{
		BmID:          nextBmID,
		Name:          name,
		IncludeInGuildSettings: includeInGuildSettings,
		IsVitality:    isVitality,
		IsAtlas:       isAtlas,
		IsVital:       isVital,
		IsModded:      isModded,
		IsOfficial:    isOfficial,
		IsBuild:       isBuild,
		LastPolledAt:  existing.LastPolledAt,
		LastPollError: existing.LastPollError,
	}, nil
}

func (p *PostgresStore) GetTrackedServer(ctx context.Context, bmID string) (TrackedServer, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT bm_id, name, include_in_guild_settings, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, last_polled_at, last_poll_error
		FROM tracked_servers
		WHERE bm_id = $1
	`, bmID)

	var item TrackedServer
	err := row.Scan(
		&item.BmID,
		&item.Name,
		&item.IncludeInGuildSettings,
		&item.IsVitality,
		&item.IsAtlas,
		&item.IsVital,
		&item.IsModded,
		&item.IsOfficial,
		&item.IsBuild,
		&item.LastPolledAt,
		&item.LastPollError,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackedServer{}, ErrTrackedServerNotFound
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
		SELECT bm_id, name, include_in_guild_settings, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, last_polled_at, last_poll_error
		FROM tracked_servers
		ORDER BY last_polled_at NULLS FIRST, bm_id ASC
		LIMIT 1
	`)

	var item TrackedServer
	err := row.Scan(
		&item.BmID,
		&item.Name,
		&item.IncludeInGuildSettings,
		&item.IsVitality,
		&item.IsAtlas,
		&item.IsVital,
		&item.IsModded,
		&item.IsOfficial,
		&item.IsBuild,
		&item.LastPolledAt,
		&item.LastPollError,
	)
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
