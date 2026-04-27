package store

import "context"

// OwnerStatsCounts groups the database-derived counts surfaced by the
// owner-only /owner stats command.
type OwnerStatsCounts struct {
	ConfiguredGuilds     int64
	SteamAccounts        int64
	HardLinks            int64
	UniqueLinkedDiscords int64
	TrackedServers       int64
	OnlineTrackedPlayers int64
	ActiveWipeReacts     int64
}

// OwnerStats runs all the COUNT queries needed for /owner stats in a single
// call. Each query targets an indexed column and is cheap on production-sized
// data.
func (p *PostgresStore) OwnerStats(ctx context.Context, nowUnix int64) (OwnerStatsCounts, error) {
	var stats OwnerStatsCounts

	queries := []struct {
		query string
		args  []any
		dest  *int64
	}{
		{`SELECT COUNT(*) FROM guild_settings`, nil, &stats.ConfiguredGuilds},
		{`SELECT COUNT(*) FROM steam_accounts`, nil, &stats.SteamAccounts},
		{`SELECT COUNT(*) FROM guild_steam_discord_links`, nil, &stats.HardLinks},
		{`SELECT COUNT(DISTINCT discord_id) FROM guild_steam_discord_links`, nil, &stats.UniqueLinkedDiscords},
		{`SELECT COUNT(*) FROM tracked_servers`, nil, &stats.TrackedServers},
		{`SELECT COUNT(*) FROM player_states WHERE is_online`, nil, &stats.OnlineTrackedPlayers},
		{`SELECT COUNT(*) FROM wipe_reacts WHERE scheduled_unix > $1`, []any{nowUnix}, &stats.ActiveWipeReacts},
	}

	for _, q := range queries {
		if err := p.pool.QueryRow(ctx, q.query, q.args...).Scan(q.dest); err != nil {
			return OwnerStatsCounts{}, err
		}
	}

	return stats, nil
}
