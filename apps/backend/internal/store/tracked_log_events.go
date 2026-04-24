package store

import (
	"context"
	"time"
)

type TrackedServerLogState struct {
	ServerBmID     string
	ServerName     string
	PlayerBmID     string
	SteamID        string
	DiscordID      string
	BmName         string
	AvatarImage    string
	IsOnline       bool
	OnlineSince    *time.Time
	OfflineSince   *time.Time
	StateChangedAt time.Time
}

func (p *PostgresStore) ListGuildTrackedServerLogStates(ctx context.Context, guildID string) ([]TrackedServerLogState, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT
			ps.server_bm_id,
			ts.name AS server_name,
			ps.player_bm_id,
			sa.steam_id,
			gsl.discord_id,
			COALESCE(sac.bm_name, '') AS bm_name,
			COALESCE(sac.avatar_image, '') AS avatar_image,
			ps.is_online,
			ps.online_since,
			ps.offline_since,
			COALESCE(ps.online_since, ps.offline_since, ps.updated_at) AS state_changed_at
		FROM player_states ps
		JOIN tracked_servers ts ON ts.bm_id = ps.server_bm_id
		JOIN guild_tracked_servers gts ON gts.server_bm_id = ps.server_bm_id AND gts.guild_id = $1
		JOIN steam_accounts sa ON sa.bm_id = ps.player_bm_id
		JOIN guild_steam_discord_links gsl ON gsl.steam_id = sa.steam_id AND gsl.guild_id = $1
		LEFT JOIN steam_account_cache sac ON sac.steam_id = sa.steam_id
		ORDER BY ts.name ASC, gsl.discord_id ASC, sa.steam_id ASC, ps.player_bm_id ASC
	`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TrackedServerLogState, 0)
	for rows.Next() {
		var item TrackedServerLogState
		if err := rows.Scan(
			&item.ServerBmID,
			&item.ServerName,
			&item.PlayerBmID,
			&item.SteamID,
			&item.DiscordID,
			&item.BmName,
			&item.AvatarImage,
			&item.IsOnline,
			&item.OnlineSince,
			&item.OfflineSince,
			&item.StateChangedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}
