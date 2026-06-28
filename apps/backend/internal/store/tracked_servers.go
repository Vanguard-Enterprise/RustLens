package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrTrackedServerNotFound = errors.New("tracked server not found")
var ErrTrackedServerAlreadyExists = errors.New("tracked server already exists")

type TrackedServer struct {
	BmID          string
	Name          string
	IsVitality    bool
	IsAtlas       bool
	IsVital       bool
	IsModded      bool
	IsOfficial    bool
	IsBuild       bool
	HighFrequency bool
	LastPolledAt  *time.Time
	LastPollError string
}

type TrackedServerFilter struct {
	Q             string
	HighFrequency *bool
	HasError      *bool
	IsVitality    *bool
	IsAtlas       *bool
	IsVital       *bool
	IsModded      *bool
	IsOfficial    *bool
	IsBuild       *bool
	SortBy        string // name | bmId | highFrequency | lastPolledAt | pollStatus
	SortOrder     string // asc | desc
}

func trackedServerOrderClause(sortBy, sortOrder string) string {
	desc := strings.EqualFold(sortOrder, "desc")
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	// Stable tie-breaker on bm_id.
	switch sortBy {
	case "bmId":
		return "ORDER BY bm_id " + dir
	case "highFrequency":
		return "ORDER BY high_frequency " + dir + ", name ASC, bm_id ASC"
	case "lastPolledAt":
		nulls := "NULLS LAST"
		if desc {
			nulls = "NULLS LAST"
		} else {
			nulls = "NULLS FIRST"
		}
		return "ORDER BY last_polled_at " + dir + " " + nulls + ", name ASC, bm_id ASC"
	case "pollStatus":
		// "error" rows (last_poll_error <> '') first when DESC, healthy first when ASC.
		return "ORDER BY (last_poll_error <> '') " + dir + ", name ASC, bm_id ASC"
	case "name", "":
		return "ORDER BY name " + dir + ", bm_id ASC"
	default:
		return "ORDER BY name ASC, bm_id ASC"
	}
}

func (p *PostgresStore) ListTrackedServersPage(ctx context.Context, filter TrackedServerFilter, limit, offset int) ([]TrackedServer, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	args := []any{}
	conds := []string{}
	addArg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	if q := strings.TrimSpace(filter.Q); q != "" {
		ph := addArg("%" + q + "%")
		conds = append(conds, "(name ILIKE "+ph+" OR bm_id ILIKE "+ph+")")
	}
	if filter.HighFrequency != nil {
		ph := addArg(*filter.HighFrequency)
		conds = append(conds, "high_frequency = "+ph)
	}
	if filter.HasError != nil {
		if *filter.HasError {
			conds = append(conds, "last_poll_error <> ''")
		} else {
			conds = append(conds, "last_poll_error = ''")
		}
	}
	addBoolFilter := func(column string, value *bool) {
		if value == nil {
			return
		}
		ph := addArg(*value)
		conds = append(conds, column+" = "+ph)
	}
	addBoolFilter("is_vitality", filter.IsVitality)
	addBoolFilter("is_atlas", filter.IsAtlas)
	addBoolFilter("is_vital", filter.IsVital)
	addBoolFilter("is_modded", filter.IsModded)
	addBoolFilter("is_official", filter.IsOfficial)
	addBoolFilter("is_build", filter.IsBuild)

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	if err := p.pool.QueryRow(ctx, "SELECT COUNT(*) FROM tracked_servers "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	order := trackedServerOrderClause(filter.SortBy, filter.SortOrder)
	limitPh := addArg(limit)
	offsetPh := addArg(offset)
	rows, err := p.pool.Query(ctx, `
		SELECT bm_id, name, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, high_frequency, last_polled_at, last_poll_error
		FROM tracked_servers
		`+where+`
		`+order+`
		LIMIT `+limitPh+` OFFSET `+offsetPh+`
	`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]TrackedServer, 0)
	for rows.Next() {
		var item TrackedServer
		if err := rows.Scan(
			&item.BmID,
			&item.Name,
			&item.IsVitality,
			&item.IsAtlas,
			&item.IsVital,
			&item.IsModded,
			&item.IsOfficial,
			&item.IsBuild,
			&item.HighFrequency,
			&item.LastPolledAt,
			&item.LastPollError,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

func (p *PostgresStore) ListTrackedServers(ctx context.Context) ([]TrackedServer, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT bm_id, name, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, high_frequency, last_polled_at, last_poll_error
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
			&item.IsVitality,
			&item.IsAtlas,
			&item.IsVital,
			&item.IsModded,
			&item.IsOfficial,
			&item.IsBuild,
			&item.HighFrequency,
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

func (p *PostgresStore) CreateTrackedServer(ctx context.Context, bmID string, name string, isVitality bool, isAtlas bool, isVital bool, isModded bool, isOfficial bool, isBuild bool, highFrequency bool) (TrackedServer, error) {
	row := p.pool.QueryRow(ctx, `
		INSERT INTO tracked_servers (
			bm_id,
			name,
			is_vitality,
			is_atlas,
			is_vital,
			is_modded,
			is_official,
			is_build,
			high_frequency
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (bm_id) DO NOTHING
		RETURNING bm_id, name, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, high_frequency, last_polled_at, last_poll_error
	`, bmID, name, isVitality, isAtlas, isVital, isModded, isOfficial, isBuild, highFrequency)

	var item TrackedServer
	err := row.Scan(
		&item.BmID,
		&item.Name,
		&item.IsVitality,
		&item.IsAtlas,
		&item.IsVital,
		&item.IsModded,
		&item.IsOfficial,
		&item.IsBuild,
		&item.HighFrequency,
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
	isVitality bool,
	isAtlas bool,
	isVital bool,
	isModded bool,
	isOfficial bool,
	isBuild bool,
	highFrequency bool,
) (TrackedServer, error) {
	if currentBmID == nextBmID {
		row := p.pool.QueryRow(ctx, `
			UPDATE tracked_servers
			SET name = $2,
				is_vitality = $3,
				is_atlas = $4,
				is_vital = $5,
				is_modded = $6,
				is_official = $7,
				is_build = $8,
				high_frequency = $9
			WHERE bm_id = $1
			RETURNING bm_id, name, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, high_frequency, last_polled_at, last_poll_error
		`, currentBmID, name, isVitality, isAtlas, isVital, isModded, isOfficial, isBuild, highFrequency)

		var item TrackedServer
		err := row.Scan(
			&item.BmID,
			&item.Name,
			&item.IsVitality,
			&item.IsAtlas,
			&item.IsVital,
			&item.IsModded,
			&item.IsOfficial,
			&item.IsBuild,
			&item.HighFrequency,
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

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return TrackedServer{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var existing TrackedServer
	row := tx.QueryRow(ctx, `
		SELECT bm_id, name, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, high_frequency, last_polled_at, last_poll_error
		FROM tracked_servers
		WHERE bm_id = $1
	`, currentBmID)
	err = row.Scan(
		&existing.BmID,
		&existing.Name,
		&existing.IsVitality,
		&existing.IsAtlas,
		&existing.IsVital,
		&existing.IsModded,
		&existing.IsOfficial,
		&existing.IsBuild,
		&existing.HighFrequency,
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
			is_vitality,
			is_atlas,
			is_vital,
			is_modded,
			is_official,
			is_build,
			high_frequency,
			last_polled_at,
			last_poll_error
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, nextBmID, name, isVitality, isAtlas, isVital, isModded, isOfficial, isBuild, highFrequency, existing.LastPolledAt, existing.LastPollError)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return TrackedServer{}, ErrTrackedServerAlreadyExists
		}
		return TrackedServer{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE guild_tracked_servers
		SET server_bm_id = $2
		WHERE server_bm_id = $1
	`, currentBmID, nextBmID); err != nil {
		return TrackedServer{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE player_states
		SET server_bm_id = $2
		WHERE server_bm_id = $1
	`, currentBmID, nextBmID); err != nil {
		return TrackedServer{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE track_groups
		SET server_bm_id = $2
		WHERE server_bm_id = $1
	`, currentBmID, nextBmID); err != nil {
		return TrackedServer{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE server_player_count_history
		SET server_bm_id = $2
		WHERE server_bm_id = $1
	`, currentBmID, nextBmID); err != nil {
		return TrackedServer{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE server_player_count_history_fetches
		SET server_bm_id = $2
		WHERE server_bm_id = $1
	`, currentBmID, nextBmID); err != nil {
		return TrackedServer{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE track_analytics_sessions_cache
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
		IsVitality:    isVitality,
		IsAtlas:       isAtlas,
		IsVital:       isVital,
		IsModded:      isModded,
		IsOfficial:    isOfficial,
		IsBuild:       isBuild,
		HighFrequency: highFrequency,
		LastPolledAt:  existing.LastPolledAt,
		LastPollError: existing.LastPollError,
	}, nil
}

func (p *PostgresStore) GetTrackedServer(ctx context.Context, bmID string) (TrackedServer, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT bm_id, name, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, high_frequency, last_polled_at, last_poll_error
		FROM tracked_servers
		WHERE bm_id = $1
	`, bmID)

	var item TrackedServer
	err := row.Scan(
		&item.BmID,
		&item.Name,
		&item.IsVitality,
		&item.IsAtlas,
		&item.IsVital,
		&item.IsModded,
		&item.IsOfficial,
		&item.IsBuild,
		&item.HighFrequency,
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

func (p *PostgresStore) SetTrackedServerHighFrequency(ctx context.Context, bmID string, highFrequency bool) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE tracked_servers
		SET high_frequency = $2
		WHERE bm_id = $1
	`, bmID, highFrequency)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTrackedServerNotFound
	}
	return nil
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

func (p *PostgresStore) NextTrackedServerToPoll(ctx context.Context, highFrequency bool) (*TrackedServer, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT bm_id, name, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, high_frequency, last_polled_at, last_poll_error
		FROM tracked_servers
		WHERE high_frequency = $1
		ORDER BY last_polled_at NULLS FIRST, bm_id ASC
		LIMIT 1
	`, highFrequency)

	var item TrackedServer
	err := row.Scan(
		&item.BmID,
		&item.Name,
		&item.IsVitality,
		&item.IsAtlas,
		&item.IsVital,
		&item.IsModded,
		&item.IsOfficial,
		&item.IsBuild,
		&item.HighFrequency,
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

func (p *PostgresStore) ListTrackedServersToPoll(ctx context.Context, highFrequency bool, limit int) ([]TrackedServer, error) {
	if limit <= 0 {
		return nil, nil
	}

	rows, err := p.pool.Query(ctx, `
		SELECT bm_id, name, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, high_frequency, last_polled_at, last_poll_error
		FROM tracked_servers
		WHERE high_frequency = $1
		ORDER BY last_polled_at NULLS FIRST, bm_id ASC
		LIMIT $2
	`, highFrequency, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TrackedServer, 0, limit)
	for rows.Next() {
		var item TrackedServer
		if err := rows.Scan(
			&item.BmID,
			&item.Name,
			&item.IsVitality,
			&item.IsAtlas,
			&item.IsVital,
			&item.IsModded,
			&item.IsOfficial,
			&item.IsBuild,
			&item.HighFrequency,
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

func (p *PostgresStore) RecordTrackedServerPollResult(ctx context.Context, bmID string, polledAt time.Time, pollError string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE tracked_servers
		SET last_polled_at = $2,
			last_poll_error = $3
		WHERE bm_id = $1
	`, bmID, polledAt, pollError)
	return err
}

func (p *PostgresStore) IncrementTrackedServerEmptyPolls(ctx context.Context, bmID string) (int, error) {
	var count int
	err := p.pool.QueryRow(ctx, `
		UPDATE tracked_servers
		SET empty_player_poll_count = empty_player_poll_count + 1
		WHERE bm_id = $1
		RETURNING empty_player_poll_count
	`, bmID).Scan(&count)
	return count, err
}

func (p *PostgresStore) ResetTrackedServerEmptyPolls(ctx context.Context, bmID string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE tracked_servers
		SET empty_player_poll_count = 0
		WHERE bm_id = $1 AND empty_player_poll_count <> 0
	`, bmID)
	return err
}
