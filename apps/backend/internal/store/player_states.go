package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrPlayerStateNotFound = errors.New("player state not found")

type PlayerState struct {
	ServerBmID   string
	ServerName   string
	PlayerBmID   string
	IsOnline     bool
	OnlineSince  *time.Time
	OfflineSince *time.Time
	UpdatedAt    time.Time
}

type PlayerPresenceInput struct {
	PlayerBmID string
	BmName     string
}

type PlayerSearchResult struct {
	ServerBmID string
	ServerName string
	PlayerBmID string
	SteamID    string
	BmName     string
	IsOnline   bool
}

func scanPlayerState(scanner rowScanner) (PlayerState, error) {
	var state PlayerState
	err := scanner.Scan(
		&state.ServerBmID,
		&state.ServerName,
		&state.PlayerBmID,
		&state.IsOnline,
		&state.OnlineSince,
		&state.OfflineSince,
		&state.UpdatedAt,
	)
	if err != nil {
		return PlayerState{}, err
	}

	return state, nil
}

func (p *PostgresStore) ListOnlinePlayerStatesByServer(ctx context.Context, serverBmID string) ([]PlayerState, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT ps.server_bm_id, ts.name, ps.player_bm_id, ps.is_online, ps.online_since, ps.offline_since, ps.updated_at
		FROM player_states ps
		JOIN tracked_servers ts ON ts.bm_id = ps.server_bm_id
		WHERE ps.server_bm_id = $1 AND ps.is_online = TRUE
		ORDER BY ps.online_since DESC NULLS LAST, ps.updated_at DESC, ps.player_bm_id ASC
	`, serverBmID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlayerState, 0)
	for rows.Next() {
		item, err := scanPlayerState(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (p *PostgresStore) ListPlayerStatesByPlayer(ctx context.Context, playerBmID string) ([]PlayerState, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT ps.server_bm_id, ts.name, ps.player_bm_id, ps.is_online, ps.online_since, ps.offline_since, ps.updated_at
		FROM player_states ps
		JOIN tracked_servers ts ON ts.bm_id = ps.server_bm_id
		WHERE ps.player_bm_id = $1
		ORDER BY ps.is_online DESC, ps.online_since DESC NULLS LAST, ps.updated_at DESC, ps.server_bm_id ASC
	`, playerBmID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlayerState, 0)
	for rows.Next() {
		item, err := scanPlayerState(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (p *PostgresStore) GetPlayerState(ctx context.Context, serverBmID string, playerBmID string) (PlayerState, error) {
	state, err := scanPlayerState(p.pool.QueryRow(ctx, `
		SELECT ps.server_bm_id, ts.name, ps.player_bm_id, ps.is_online, ps.online_since, ps.offline_since, ps.updated_at
		FROM player_states ps
		JOIN tracked_servers ts ON ts.bm_id = ps.server_bm_id
		WHERE ps.server_bm_id = $1 AND ps.player_bm_id = $2
	`, serverBmID, playerBmID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PlayerState{}, ErrPlayerStateNotFound
	}
	return state, err
}

func (p *PostgresStore) SearchPlayerStatesByName(ctx context.Context, serverBmID string, query string, limit int) ([]PlayerSearchResult, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := p.pool.Query(ctx, `
		SELECT
			ps.server_bm_id,
			ts.name,
			ps.player_bm_id,
			sa.steam_id,
			COALESCE(sac.bm_name, '') AS bm_name,
			ps.is_online
		FROM player_states ps
		JOIN tracked_servers ts ON ts.bm_id = ps.server_bm_id
		JOIN steam_accounts sa ON sa.bm_id = ps.player_bm_id
		LEFT JOIN steam_account_cache sac ON sac.steam_id = sa.steam_id
		WHERE ps.server_bm_id = $1
			AND ps.is_online = TRUE
			AND COALESCE(sac.bm_name, '') ILIKE '%' || $2 || '%'
		ORDER BY COALESCE(sac.bm_name, '') ASC, sa.steam_id ASC
		LIMIT $3
	`, serverBmID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlayerSearchResult, 0)
	for rows.Next() {
		var item PlayerSearchResult
		if err := rows.Scan(
			&item.ServerBmID,
			&item.ServerName,
			&item.PlayerBmID,
			&item.SteamID,
			&item.BmName,
			&item.IsOnline,
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

func (p *PostgresStore) SyncPlayerStates(ctx context.Context, serverBmID string, players []PlayerPresenceInput, observedAt time.Time) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT player_bm_id, is_online
		FROM player_states
		WHERE server_bm_id = $1
		FOR UPDATE
	`, serverBmID)
	if err != nil {
		return err
	}

	type existingState struct {
		IsOnline bool
	}

	existing := make(map[string]existingState)
	for rows.Next() {
		var playerBmID string
		var isOnline bool
		if err := rows.Scan(&playerBmID, &isOnline); err != nil {
			rows.Close()
			return err
		}
		existing[playerBmID] = existingState{IsOnline: isOnline}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	seen := make(map[string]struct{}, len(players))
	for _, player := range players {
		if player.PlayerBmID == "" {
			continue
		}

		seen[player.PlayerBmID] = struct{}{}

		if player.BmName != "" {
			if _, err := tx.Exec(ctx, `
				INSERT INTO steam_account_cache (steam_id, bm_id, bm_name, updated_at)
				SELECT steam_id, $1, $2, 'epoch'::timestamptz
				FROM steam_accounts
				WHERE bm_id = $1
				ON CONFLICT (steam_id) DO UPDATE
				SET bm_id = EXCLUDED.bm_id,
					bm_name = EXCLUDED.bm_name
				WHERE steam_account_cache.bm_name IS DISTINCT FROM EXCLUDED.bm_name
			`, player.PlayerBmID, player.BmName); err != nil {
				return err
			}
		}

		current, exists := existing[player.PlayerBmID]
		switch {
		case !exists:
			if _, err := tx.Exec(ctx, `
				INSERT INTO player_states (server_bm_id, player_bm_id, is_online, online_since, offline_since, updated_at)
				VALUES ($1, $2, TRUE, $3, NULL, $3)
			`, serverBmID, player.PlayerBmID, observedAt); err != nil {
				return err
			}
			existing[player.PlayerBmID] = existingState{IsOnline: true}
		case current.IsOnline:
			if _, err := tx.Exec(ctx, `
				UPDATE player_states
				SET updated_at = $3
				WHERE server_bm_id = $1 AND player_bm_id = $2
			`, serverBmID, player.PlayerBmID, observedAt); err != nil {
				return err
			}
			existing[player.PlayerBmID] = existingState{IsOnline: true}
		default:
			if _, err := tx.Exec(ctx, `
				UPDATE player_states
				SET is_online = TRUE,
					online_since = $3,
					offline_since = NULL,
					updated_at = $3
				WHERE server_bm_id = $1 AND player_bm_id = $2
			`, serverBmID, player.PlayerBmID, observedAt); err != nil {
				return err
			}
			existing[player.PlayerBmID] = existingState{IsOnline: true}
		}
	}

	for playerBmID, current := range existing {
		if !current.IsOnline {
			continue
		}
		if _, ok := seen[playerBmID]; ok {
			continue
		}

		if _, err := tx.Exec(ctx, `
			UPDATE player_states
			SET is_online = FALSE,
				offline_since = $3,
				updated_at = $3
			WHERE server_bm_id = $1 AND player_bm_id = $2
		`, serverBmID, playerBmID, observedAt); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
