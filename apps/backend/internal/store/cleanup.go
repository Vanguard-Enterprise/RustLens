package store

import (
	"context"
	"time"
)

// PurgeWipeReactsBefore removes wipe-react rows whose scheduled time is older
// than the provided cutoff. The cutoff is interpreted as the absolute moment
// before which a wipe react is considered safe to delete; callers typically
// pass `now - retentionWindow` (e.g. 7 days) so recent passed wipe reacts
// remain available to /compare and /wipereact missing.
//
// Returns the number of rows deleted.
func (p *PostgresStore) PurgeWipeReactsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM wipe_reacts
		WHERE scheduled_unix < $1
	`, cutoff.Unix())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PurgeTrackAnalyticsSessionsCacheBefore removes cached BattleMetrics session
// windows that haven't been refreshed since the cutoff. The cache is read with
// a 24h freshness rule by the analytics handler, so any cutoff older than that
// (e.g. 48h) keeps the working set warm without unbounded growth.
//
// Returns the number of rows deleted.
func (p *PostgresStore) PurgeTrackAnalyticsSessionsCacheBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM track_analytics_sessions_cache
		WHERE fetched_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PurgeExpiredAuthTokensBefore removes consumed or expired rows from the
// short-lived auth-token tables. Consumed tokens cannot be reused regardless
// of expiry, so they're safe to drop unconditionally.
//
// Returns the number of rows deleted across all three tables.
func (p *PostgresStore) PurgeExpiredAuthTokensBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	var total int64
	for _, table := range []string{
		"email_sign_in_tokens",
		"password_reset_tokens",
		"email_verification_tokens",
	} {
		tag, err := p.pool.Exec(ctx, `
			DELETE FROM `+table+`
			WHERE consumed_at IS NOT NULL OR expires_at < $1
		`, cutoff)
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
	}
	return total, nil
}
