package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type PlatformSyncState struct {
	Platform           string
	PlatformID         string
	LastPlatformSyncAt time.Time
}

// GetPlatformSyncState returns the last platformsync attempt for the given
// (platform, platform_id) pair. Returns ErrSteamAccountNotFound when no row
// exists so callers can treat the missing case the same way as a stale row.
func (p *PostgresStore) GetPlatformSyncState(ctx context.Context, platform, platformID string) (PlatformSyncState, error) {
	var record PlatformSyncState
	err := p.pool.QueryRow(ctx, `
		SELECT platform, platform_id, last_platform_sync_at
		FROM platform_sync_state
		WHERE platform = $1 AND platform_id = $2
	`, platform, platformID).Scan(&record.Platform, &record.PlatformID, &record.LastPlatformSyncAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlatformSyncState{}, ErrSteamAccountNotFound
	}
	if err != nil {
		return PlatformSyncState{}, err
	}
	return record, nil
}

// UpsertPlatformSyncState records that we just queried platformsync for the
// given (platform, platform_id) pair. last_platform_sync_at is set to NOW().
func (p *PostgresStore) UpsertPlatformSyncState(ctx context.Context, platform, platformID string) (PlatformSyncState, error) {
	var record PlatformSyncState
	err := p.pool.QueryRow(ctx, `
		INSERT INTO platform_sync_state (platform, platform_id, last_platform_sync_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (platform, platform_id) DO UPDATE
		SET last_platform_sync_at = NOW()
		RETURNING platform, platform_id, last_platform_sync_at
	`, platform, platformID).Scan(&record.Platform, &record.PlatformID, &record.LastPlatformSyncAt)
	if err != nil {
		return PlatformSyncState{}, err
	}
	return record, nil
}
