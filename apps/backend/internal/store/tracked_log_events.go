package store

import (
	"context"
	"time"
)

type TrackedServerLogEvent struct {
	ID          int64
	ServerBmID  string
	ServerName  string
	PlayerBmID  string
	SteamID     string
	DiscordID   string
	BmName      string
	AvatarImage string
	EventType   string
	OccurredAt  time.Time
}

type GuildTrackedLogCursor struct {
	GuildID     string
	LastEventID int64
	UpdatedAt   time.Time
}

func (p *PostgresStore) GetGuildTrackedLogCursor(ctx context.Context, guildID string) (GuildTrackedLogCursor, error) {
	var cursor GuildTrackedLogCursor
	err := p.pool.QueryRow(ctx, `
		INSERT INTO guild_tracked_log_cursors (guild_id, last_event_id, updated_at)
		VALUES ($1, COALESCE((SELECT MAX(id) FROM player_state_events), 0), NOW())
		ON CONFLICT (guild_id) DO UPDATE
		SET guild_id = EXCLUDED.guild_id
		RETURNING guild_id, last_event_id, updated_at
	`, guildID).Scan(&cursor.GuildID, &cursor.LastEventID, &cursor.UpdatedAt)
	if err != nil {
		return GuildTrackedLogCursor{}, err
	}
	return cursor, nil
}

func (p *PostgresStore) SetGuildTrackedLogCursor(ctx context.Context, guildID string, lastEventID int64) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO guild_tracked_log_cursors (guild_id, last_event_id, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (guild_id) DO UPDATE
		SET last_event_id = GREATEST(guild_tracked_log_cursors.last_event_id, EXCLUDED.last_event_id),
			updated_at = NOW()
	`, guildID, lastEventID)
	return err
}

func (p *PostgresStore) ListGuildTrackedServerLogEvents(ctx context.Context, guildID string, afterID int64, limit int) ([]TrackedServerLogEvent, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := p.pool.Query(ctx, `
		SELECT
			e.id,
			e.server_bm_id,
			ts.name AS server_name,
			e.player_bm_id,
			sa.steam_id,
			gsl.discord_id,
			COALESCE(NULLIF(sac.bm_name, ''), NULLIF(e.bm_name, ''), '') AS bm_name,
			COALESCE(sac.avatar_image, '') AS avatar_image,
			e.event_type,
			e.occurred_at
		FROM player_state_events e
		JOIN tracked_servers ts ON ts.bm_id = e.server_bm_id
		JOIN guild_tracked_servers gts ON gts.server_bm_id = e.server_bm_id AND gts.guild_id = $1
		JOIN steam_accounts sa ON sa.bm_id = e.player_bm_id
		JOIN guild_steam_discord_links gsl ON gsl.steam_id = sa.steam_id AND gsl.guild_id = $1
		LEFT JOIN steam_account_cache sac ON sac.steam_id = sa.steam_id
		WHERE e.id > $2
		ORDER BY e.id ASC
		LIMIT $3
	`, guildID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TrackedServerLogEvent, 0)
	for rows.Next() {
		var item TrackedServerLogEvent
		if err := rows.Scan(
			&item.ID,
			&item.ServerBmID,
			&item.ServerName,
			&item.PlayerBmID,
			&item.SteamID,
			&item.DiscordID,
			&item.BmName,
			&item.AvatarImage,
			&item.EventType,
			&item.OccurredAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *PostgresStore) LatestPlayerStateEventID(ctx context.Context) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx, `SELECT COALESCE(MAX(id), 0) FROM player_state_events`).Scan(&id)
	return id, err
}
