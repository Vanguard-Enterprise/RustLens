package store

import (
	"context"
	"time"
)

// UpsertBotUserInstallInput is the metadata captured from an
// APPLICATION_AUTHORIZED webhook event with integration_type=1 (user install).
type UpsertBotUserInstallInput struct {
	DiscordID  string
	Username   string
	GlobalName *string
	AvatarHash *string
	Scopes     []string
}

// UpsertBotUserInstall inserts or reactivates a row in bot_user_installs.
// Mirrors UpsertBotGuildMetadata: clears prior left_at on re-authorization.
func (p *PostgresStore) UpsertBotUserInstall(ctx context.Context, in UpsertBotUserInstallInput) error {
	scopes := in.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO bot_user_installs (
			discord_id, username, global_name, avatar_hash, scopes,
			is_installed, installed_at, left_at, metadata_updated_at
		)
		VALUES ($1, $2, $3, $4, $5, TRUE, NOW(), NULL, NOW())
		ON CONFLICT (discord_id) DO UPDATE SET
			username            = EXCLUDED.username,
			global_name         = EXCLUDED.global_name,
			avatar_hash         = EXCLUDED.avatar_hash,
			scopes              = EXCLUDED.scopes,
			is_installed        = TRUE,
			left_at             = NULL,
			metadata_updated_at = NOW()
	`, in.DiscordID, in.Username, in.GlobalName, in.AvatarHash, scopes)
	return err
}

// MarkBotUserInstallRemoved flags a row as deauthorized. No-op if absent.
func (p *PostgresStore) MarkBotUserInstallRemoved(ctx context.Context, discordID string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE bot_user_installs
		SET is_installed = FALSE, left_at = NOW()
		WHERE discord_id = $1
	`, discordID)
	return err
}

// TouchBotUserInstallSeen is the interaction-backfill heartbeat. Called by the
// bot when an interaction arrives with authorizing_integration_owners["1"]
// matching this user. If the row exists, last_seen_at is bumped; if it does
// not (we missed the webhook), we insert it active so the user shows up in
// admin even without an APPLICATION_AUTHORIZED event.
func (p *PostgresStore) TouchBotUserInstallSeen(ctx context.Context, discordID string) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO bot_user_installs (discord_id, is_installed, installed_at, last_seen_at, metadata_updated_at)
		VALUES ($1, TRUE, NOW(), NOW(), NOW())
		ON CONFLICT (discord_id) DO UPDATE
		SET last_seen_at = NOW()
	`, discordID)
	return err
}

// AdminUserInstallRow is one row returned by ListAdminUserInstalls.
type AdminUserInstallRow struct {
	DiscordID   string
	Username    string
	GlobalName  *string
	AvatarHash  *string
	Scopes      []string
	IsInstalled bool
	InstalledAt time.Time
	LeftAt      *time.Time
	LastSeenAt  *time.Time
}

// ListAdminUserInstallsOpts are query parameters for ListAdminUserInstalls.
//
// Q is a free-text search applied to username (substring), global_name
// (substring), and discord_id (prefix), all case-insensitive. Empty disables
// the WHERE clause.
type ListAdminUserInstallsOpts struct {
	Q        string
	Page     int // 1-based; values <= 0 are treated as 1
	PageSize int // values <= 0 or > 100 are clamped to 20
}

// ListAdminUserInstalls returns one paginated page of bot_user_installs rows.
// Total is computed in a separate COUNT(*) query against the same WHERE clause
// so off-the-end pages still report the correct total.
func (p *PostgresStore) ListAdminUserInstalls(ctx context.Context, opts ListAdminUserInstallsOpts) ([]AdminUserInstallRow, int, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	const whereClause = `
		WHERE ($1 = ''
		       OR username ILIKE '%' || $1 || '%'
		       OR global_name ILIKE '%' || $1 || '%'
		       OR discord_id ILIKE $1 || '%')
	`

	var total int
	if err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bot_user_installs `+whereClause, opts.Q).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := p.pool.Query(ctx, `
		SELECT discord_id, username, global_name, avatar_hash, scopes,
		       is_installed, installed_at, left_at, last_seen_at
		FROM bot_user_installs
		`+whereClause+`
		ORDER BY installed_at DESC, discord_id ASC
		LIMIT $2 OFFSET $3
	`, opts.Q, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]AdminUserInstallRow, 0)
	for rows.Next() {
		var r AdminUserInstallRow
		if err := rows.Scan(
			&r.DiscordID, &r.Username, &r.GlobalName, &r.AvatarHash, &r.Scopes,
			&r.IsInstalled, &r.InstalledAt, &r.LeftAt, &r.LastSeenAt,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
