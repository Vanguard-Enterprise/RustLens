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
