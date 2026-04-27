package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type TrackAnalyticsSession struct {
	StartUnix int64 `json:"start"`
	StopUnix  int64 `json:"stop"`
}

type TrackAnalyticsCacheEntry struct {
	PlayerBmID string
	ServerBmID string
	Sessions   []TrackAnalyticsSession
	FetchedAt  time.Time
}

// TrackAnalyticsMember is one tracked group member with a known BattleMetrics ID.
type TrackAnalyticsMember struct {
	SteamID string
	BmID    string
}

// ListTrackAnalyticsMembers returns the members of the given track group that
// have a resolvable BattleMetrics ID (either on the member row or on the linked
// steam account). Members without a bm_id are skipped.
func (p *PostgresStore) ListTrackAnalyticsMembers(ctx context.Context, trackGroupID int64) ([]TrackAnalyticsMember, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT
			tgm.steam_id,
			COALESCE(NULLIF(tgm.bm_id, ''), COALESCE(sa.bm_id, '')) AS bm_id
		FROM track_group_members tgm
		LEFT JOIN steam_accounts sa ON sa.steam_id = tgm.steam_id
		WHERE tgm.track_group_id = $1
	`, trackGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TrackAnalyticsMember, 0)
	for rows.Next() {
		var member TrackAnalyticsMember
		if err := rows.Scan(&member.SteamID, &member.BmID); err != nil {
			return nil, err
		}
		if member.BmID == "" {
			continue
		}
		items = append(items, member)
	}
	return items, rows.Err()
}

// GetTrackAnalyticsCache returns the cached sessions for the given (player, server)
// pair. Returns (nil, nil) if no cache entry exists.
func (p *PostgresStore) GetTrackAnalyticsCache(ctx context.Context, playerBmID, serverBmID string) (*TrackAnalyticsCacheEntry, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT player_bm_id, server_bm_id, sessions, fetched_at
		FROM track_analytics_sessions_cache
		WHERE player_bm_id = $1 AND server_bm_id = $2
	`, playerBmID, serverBmID)

	var entry TrackAnalyticsCacheEntry
	var raw []byte
	if err := row.Scan(&entry.PlayerBmID, &entry.ServerBmID, &raw, &entry.FetchedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &entry.Sessions); err != nil {
			return nil, err
		}
	}
	return &entry, nil
}

// UpsertTrackAnalyticsCache writes the sessions for the given (player, server)
// pair, refreshing fetched_at to NOW().
func (p *PostgresStore) UpsertTrackAnalyticsCache(ctx context.Context, playerBmID, serverBmID string, sessions []TrackAnalyticsSession) error {
	if sessions == nil {
		sessions = []TrackAnalyticsSession{}
	}
	encoded, err := json.Marshal(sessions)
	if err != nil {
		return err
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO track_analytics_sessions_cache (player_bm_id, server_bm_id, sessions, fetched_at)
		VALUES ($1, $2, $3::jsonb, NOW())
		ON CONFLICT (player_bm_id, server_bm_id) DO UPDATE
		SET sessions = EXCLUDED.sessions,
			fetched_at = NOW()
	`, playerBmID, serverBmID, encoded)
	return err
}
