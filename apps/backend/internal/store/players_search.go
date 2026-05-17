package store

import (
	"context"
	"time"
)

// PlayerSearchHit is the row returned by SearchPlayersByName. Used by the
// /api/app/players/search name-mode response.
type PlayerSearchHit struct {
	SteamID         string
	BmID            *string
	BmName          *string
	AvatarImage     *string
	CurrentServerBm *string
	CurrentServer   *string
	IsOnline        bool
	LastSeenAt      *string // ISO 8601 (RFC3339, UTC)
}

// SearchPlayersByName runs an ILIKE search across all globally tracked
// servers, dedupes by Steam ID, prefers online rows, and caps at 20.
//
// The search matches against bm_profile_cache.bm_name (preferred) OR
// steam_account_cache.persona_name. includeOffline=true also returns rows
// where offline_since is within the last 24 hours.
func (p *PostgresStore) SearchPlayersByName(ctx context.Context, q string, includeOffline bool) ([]PlayerSearchHit, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT DISTINCT ON (sa.steam_id)
		       sa.steam_id,
		       sa.bm_id,
		       NULLIF(bpc.bm_name, '') AS bm_name,
		       NULLIF(sac.avatar_image, '') AS avatar_image,
		       ts.bm_id      AS server_bm_id,
		       ts.name       AS server_name,
		       ps.is_online,
		       COALESCE(ps.online_since, ps.offline_since, ps.updated_at) AS last_seen
		  FROM steam_accounts        sa
		  JOIN player_states         ps  ON ps.player_bm_id = sa.bm_id
		  JOIN tracked_servers       ts  ON ts.bm_id        = ps.server_bm_id
		  LEFT JOIN bm_profile_cache bpc ON bpc.bm_id       = sa.bm_id
		  LEFT JOIN steam_account_cache sac ON sac.steam_id = sa.steam_id
		 WHERE (
		        bpc.bm_name      ILIKE '%' || $1 || '%'
		     OR sac.persona_name ILIKE '%' || $1 || '%'
		       )
		   AND (
		        ps.is_online
		     OR ($2 AND ps.offline_since IS NOT NULL AND ps.offline_since > now() - interval '24 hours')
		       )
		 ORDER BY sa.steam_id,
		          ps.is_online DESC,
		          COALESCE(ps.online_since, ps.offline_since, ps.updated_at) DESC
		 LIMIT 20
	`, q, includeOffline)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hits := make([]PlayerSearchHit, 0, 20)
	for rows.Next() {
		var (
			hit      PlayerSearchHit
			lastSeen *time.Time
		)
		if err := rows.Scan(
			&hit.SteamID,
			&hit.BmID,
			&hit.BmName,
			&hit.AvatarImage,
			&hit.CurrentServerBm,
			&hit.CurrentServer,
			&hit.IsOnline,
			&lastSeen,
		); err != nil {
			return nil, err
		}
		if lastSeen != nil {
			s := lastSeen.UTC().Format(time.RFC3339)
			hit.LastSeenAt = &s
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}
