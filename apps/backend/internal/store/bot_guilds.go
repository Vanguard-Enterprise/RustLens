package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type UpsertBotGuildInput struct {
	GuildID                string
	Name                   string
	IconHash               *string
	BannerHash             *string
	OwnerDiscordID         *string
	OwnerUsername          *string
	OwnerGlobalName        *string
	OwnerAvatarHash        *string
	ApproximateMemberCount *int32
}

// UpsertBotGuildMetadata inserts or updates a row in bot_guilds and clears any
// prior "left" state. Member sync timestamp is left untouched.
func (p *PostgresStore) UpsertBotGuildMetadata(ctx context.Context, in UpsertBotGuildInput) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO bot_guilds (
			guild_id, name, icon_hash, banner_hash,
			owner_discord_id, owner_username, owner_global_name, owner_avatar_hash,
			approximate_member_count, is_installed, installed_at, left_at, metadata_updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, TRUE, NOW(), NULL, NOW())
		ON CONFLICT (guild_id) DO UPDATE SET
			name                     = EXCLUDED.name,
			icon_hash                = EXCLUDED.icon_hash,
			banner_hash              = EXCLUDED.banner_hash,
			owner_discord_id         = EXCLUDED.owner_discord_id,
			owner_username           = EXCLUDED.owner_username,
			owner_global_name        = EXCLUDED.owner_global_name,
			owner_avatar_hash        = EXCLUDED.owner_avatar_hash,
			approximate_member_count = EXCLUDED.approximate_member_count,
			is_installed             = TRUE,
			left_at                  = NULL,
			metadata_updated_at      = NOW()
	`,
		in.GuildID, in.Name, in.IconHash, in.BannerHash,
		in.OwnerDiscordID, in.OwnerUsername, in.OwnerGlobalName, in.OwnerAvatarHash,
		in.ApproximateMemberCount,
	)
	return err
}

// MarkBotGuildLeft flags the row as no longer installed. No-op if absent.
func (p *PostgresStore) MarkBotGuildLeft(ctx context.Context, guildID string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE bot_guilds
		SET is_installed = FALSE,
		    left_at = NOW()
		WHERE guild_id = $1
	`, guildID)
	return err
}

// MembersSyncedAt returns the bot_guilds.members_synced_at timestamp for a
// guild, or (nil, false) when the row does not exist.
func (p *PostgresStore) MembersSyncedAt(ctx context.Context, guildID string) (*time.Time, bool, error) {
	var ts *time.Time
	err := p.pool.QueryRow(ctx, `
		SELECT members_synced_at FROM bot_guilds WHERE guild_id = $1
	`, guildID).Scan(&ts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return ts, true, nil
}

// BulkUpsertUserGuilds inserts or reactivates user_guilds rows for the given
// discord IDs in a single statement. Caller is responsible for chunking input
// to a reasonable size (the HTTP layer caps incoming batches at 1000).
func (p *PostgresStore) BulkUpsertUserGuilds(ctx context.Context, guildID string, discordIDs []string) error {
	if len(discordIDs) == 0 {
		return nil
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO user_guilds (discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at)
		SELECT did, $1, TRUE, NOW(), NOW(), NULL FROM unnest($2::text[]) AS did
		ON CONFLICT (discord_id, guild_id) DO UPDATE
		SET is_active = TRUE,
		    last_seen_at = NOW(),
		    left_at = NULL
	`, guildID, discordIDs)
	return err
}

// SyncUserGuildInput carries the per-guild metadata captured from Discord's
// /users/@me/guilds response, used to backfill user_guilds with guild
// name/icon/member-count/owner-flag for guilds the bot isn't necessarily in.
type SyncUserGuildInput struct {
	GuildID                string
	Name                   string
	IconHash               string
	ApproximateMemberCount *int32
	IsOwner                bool
}

// SyncUserGuildsForUser reconciles the user_guilds rows for a single user
// against the authoritative list of guilds returned by Discord's
// /users/@me/guilds endpoint. Listed guilds are upserted active with their
// metadata; rows for this user in any guild NOT in the list are marked left.
func (p *PostgresStore) SyncUserGuildsForUser(ctx context.Context, discordID string, guilds []SyncUserGuildInput) error {
	if discordID == "" {
		return errors.New("discordID is required")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	guildIDs := make([]string, len(guilds))
	names := make([]string, len(guilds))
	iconHashes := make([]*string, len(guilds))
	memberCounts := make([]*int32, len(guilds))
	isOwners := make([]bool, len(guilds))
	for i, g := range guilds {
		guildIDs[i] = g.GuildID
		names[i] = g.Name
		if g.IconHash != "" {
			s := g.IconHash
			iconHashes[i] = &s
		}
		memberCounts[i] = g.ApproximateMemberCount
		isOwners[i] = g.IsOwner
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO user_guilds (
			discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at,
			name, icon_hash, approximate_member_count, is_owner
		)
		SELECT $1, t.gid, TRUE, NOW(), NOW(), NULL, t.name, t.icon, t.members, t.is_owner
		FROM unnest($2::text[], $3::text[], $4::text[], $5::int[], $6::bool[])
			AS t(gid, name, icon, members, is_owner)
		ON CONFLICT (discord_id, guild_id) DO UPDATE
		SET is_active = TRUE,
		    last_seen_at = NOW(),
		    left_at = NULL,
		    name = EXCLUDED.name,
		    icon_hash = EXCLUDED.icon_hash,
		    approximate_member_count = EXCLUDED.approximate_member_count,
		    is_owner = EXCLUDED.is_owner
	`, discordID, guildIDs, names, iconHashes, memberCounts, isOwners); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE user_guilds
		SET is_active = FALSE,
		    left_at = COALESCE(left_at, NOW())
		WHERE discord_id = $1
		  AND is_active = TRUE
		  AND guild_id <> ALL($2::text[])
	`, discordID, guildIDs); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// StampMembersSynced marks bot_guilds.members_synced_at = NOW(). Used after a
// guildCreate / clientReady member sync completes.
func (p *PostgresStore) StampMembersSynced(ctx context.Context, guildID string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE bot_guilds SET members_synced_at = NOW() WHERE guild_id = $1
	`, guildID)
	return err
}

// ReconcileShardBotGuilds marks any currently-installed bot_guilds row in the
// given shard's range as left, unless its guild_id appears in presentGuildIDs.
// Shard membership uses Discord's standard formula: (guild_id >> 22) % shard_count.
func (p *PostgresStore) ReconcileShardBotGuilds(ctx context.Context, shardID int, shardCount int, presentGuildIDs []string) error {
	if shardCount <= 0 {
		return errors.New("shardCount must be > 0")
	}
	if shardID < 0 || shardID >= shardCount {
		return errors.New("shardID out of range")
	}
	_, err := p.pool.Exec(ctx, `
		UPDATE bot_guilds
		SET is_installed = FALSE, left_at = NOW()
		WHERE is_installed = TRUE
		  AND ((guild_id::bigint >> 22) % $1::bigint) = $2::bigint
		  AND guild_id <> ALL($3::text[])
	`, shardCount, shardID, presentGuildIDs)
	return err
}

type DashboardGuildRow struct {
	GuildID                string
	Name                   string
	IconHash               *string
	BannerHash             *string
	OwnerDiscordID         *string
	OwnerUsername          *string
	OwnerGlobalName        *string
	OwnerAvatarHash        *string
	ApproximateMemberCount *int32
	TrackedServerCount     int32
}

func (p *PostgresStore) ListInstalledGuildsForUser(ctx context.Context, discordID string) ([]DashboardGuildRow, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT bg.guild_id,
		       bg.name,
		       bg.icon_hash,
		       bg.banner_hash,
		       bg.owner_discord_id,
		       bg.owner_username,
		       bg.owner_global_name,
		       bg.owner_avatar_hash,
		       bg.approximate_member_count,
		       (SELECT COUNT(*) FROM guild_tracked_servers gts WHERE gts.guild_id = bg.guild_id)::int AS tracked_server_count
		FROM user_guilds ug
		JOIN bot_guilds bg ON bg.guild_id = ug.guild_id
		WHERE ug.discord_id = $1
		  AND ug.is_active = TRUE
		  AND bg.is_installed = TRUE
		ORDER BY bg.name ASC, bg.guild_id ASC
	`, discordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]DashboardGuildRow, 0)
	for rows.Next() {
		var r DashboardGuildRow
		if err := rows.Scan(
			&r.GuildID, &r.Name, &r.IconHash, &r.BannerHash,
			&r.OwnerDiscordID, &r.OwnerUsername, &r.OwnerGlobalName, &r.OwnerAvatarHash,
			&r.ApproximateMemberCount, &r.TrackedServerCount,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AdminGuildRow is one row returned by ListAdminGuilds.
type AdminGuildRow struct {
	GuildID                string
	Name                   string
	IconHash               *string
	OwnerDiscordID         *string
	OwnerUsername          *string
	OwnerGlobalName        *string
	OwnerAvatarHash        *string
	ApproximateMemberCount *int32
	IsInstalled            bool
	LeftAt                 *time.Time
	IsConfigured           bool
	TrackedServerCount     int32
}

// ListAdminGuildsOpts are query parameters for ListAdminGuilds.
//
// Q is a free-text search applied to bot_guilds.name (substring),
// bot_guilds.guild_id (prefix), and bot_guilds.owner_username (substring), all
// case-insensitive. Empty string disables the WHERE clause.
type ListAdminGuildsOpts struct {
	Q        string
	Page     int // 1-based; values <= 0 are treated as 1
	PageSize int // values <= 0 or > 100 are clamped to 20
}

// ListAdminGuilds returns one paginated page of bot_guilds rows joined with
// guild_settings (configured flag) and guild_tracked_servers (count). Total
// is computed in a separate COUNT(*) query against the same WHERE clause so
// off-the-end pages still report the correct total.
func (p *PostgresStore) ListAdminGuilds(ctx context.Context, opts ListAdminGuildsOpts) ([]AdminGuildRow, int, error) {
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
		       OR bg.name ILIKE '%' || $1 || '%'
		       OR bg.guild_id ILIKE $1 || '%'
		       OR bg.owner_username ILIKE '%' || $1 || '%')
	`

	var total int
	if err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bot_guilds bg `+whereClause, opts.Q).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := p.pool.Query(ctx, `
		SELECT bg.guild_id, bg.name, bg.icon_hash,
		       bg.owner_discord_id, bg.owner_username, bg.owner_global_name, bg.owner_avatar_hash,
		       bg.approximate_member_count, bg.is_installed, bg.left_at,
		       (gs.guild_id IS NOT NULL) AS is_configured,
		       COALESCE(gts.cnt, 0)      AS tracked_server_count
		FROM bot_guilds bg
		LEFT JOIN guild_settings gs ON gs.guild_id = bg.guild_id
		LEFT JOIN (
			SELECT guild_id, COUNT(*)::int AS cnt
			FROM guild_tracked_servers
			GROUP BY guild_id
		) gts ON gts.guild_id = bg.guild_id
		`+whereClause+`
		ORDER BY bg.name ASC, bg.guild_id ASC
		LIMIT $2 OFFSET $3
	`, opts.Q, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]AdminGuildRow, 0)
	for rows.Next() {
		var r AdminGuildRow
		if err := rows.Scan(
			&r.GuildID, &r.Name, &r.IconHash,
			&r.OwnerDiscordID, &r.OwnerUsername, &r.OwnerGlobalName, &r.OwnerAvatarHash,
			&r.ApproximateMemberCount, &r.IsInstalled, &r.LeftAt,
			&r.IsConfigured, &r.TrackedServerCount,
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
