package store

import "context"

type GuildOnlineSummaryItem struct {
	GuildID    string
	DiscordID  string
	SteamID    string
	PlayerBmID string
	BmName     string
	ServerBmID string
	ServerName string
	OnlineUnix int64
}

func (p *PostgresStore) ListGuildOnlineSummary(ctx context.Context, guildID string) ([]GuildOnlineSummaryItem, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT guild_id, discord_id, steam_id, bm_id, bm_name, server_bm_id, server_name, online_unix
		FROM (
			SELECT
				gsl.guild_id,
				gsl.discord_id,
				gsl.steam_id,
				sa.bm_id,
				COALESCE(bpc.bm_name, '') AS bm_name,
				ps.server_bm_id,
				ts.name AS server_name,
				EXTRACT(EPOCH FROM ps.online_since)::BIGINT AS online_unix,
				ROW_NUMBER() OVER (
					PARTITION BY gsl.discord_id
					ORDER BY ps.online_since DESC, ps.updated_at DESC, ps.server_bm_id ASC
				) AS summary_rank
			FROM guild_steam_discord_links gsl
			JOIN steam_accounts sa ON sa.steam_id = gsl.steam_id
			LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
			JOIN player_states ps ON ps.player_bm_id = sa.bm_id
			JOIN guild_tracked_servers gts
				ON gts.guild_id = gsl.guild_id
				AND gts.server_bm_id = ps.server_bm_id
			JOIN tracked_servers ts ON ts.bm_id = ps.server_bm_id
			WHERE gsl.guild_id = $1
			  AND sa.bm_id IS NOT NULL
			  AND sa.bm_id <> ''
			  AND ps.is_online = TRUE
			  AND ps.online_since IS NOT NULL
		) summary
		WHERE summary_rank = 1
		ORDER BY server_name ASC, online_unix DESC, discord_id ASC, bm_id ASC
	`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]GuildOnlineSummaryItem, 0)
	for rows.Next() {
		var item GuildOnlineSummaryItem
		if err := rows.Scan(
			&item.GuildID,
			&item.DiscordID,
			&item.SteamID,
			&item.PlayerBmID,
			&item.BmName,
			&item.ServerBmID,
			&item.ServerName,
			&item.OnlineUnix,
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
