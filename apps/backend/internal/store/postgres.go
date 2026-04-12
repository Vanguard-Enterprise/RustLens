package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

type GuildSettings struct {
	GuildID                   string
	WipeReactLogChannelID     string
	LinkingLogChannelID       string
	TrackedServerLogChannelID string
	AlertsChannelID           string
	AdminRoleID               string
}

type GuildSettingsPatch struct {
	WipeReactLogChannelID     *string
	LinkingLogChannelID       *string
	TrackedServerLogChannelID *string
	AlertsChannelID           *string
	AdminRoleID               *string
}

func NewPostgresStore(ctx context.Context, connectionString string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return &PostgresStore{pool: pool}, nil
}

func (p *PostgresStore) Close() {
	p.pool.Close()
}

func (p *PostgresStore) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *PostgresStore) EnsureSchema(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS bot_worker_events (
			id BIGSERIAL PRIMARY KEY,
			worker_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS guild_settings (
			guild_id TEXT PRIMARY KEY,
			wipe_react_log_channel_id TEXT NOT NULL DEFAULT '',
			linking_log_channel_id TEXT NOT NULL DEFAULT '',
			tracked_server_log_channel_id TEXT NOT NULL DEFAULT '',
			alerts_channel_id TEXT NOT NULL DEFAULT '',
			admin_role_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		DROP TABLE IF EXISTS guild_setups;

		ALTER TABLE guild_settings ADD COLUMN IF NOT EXISTS wipe_react_log_channel_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE guild_settings ADD COLUMN IF NOT EXISTS linking_log_channel_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE guild_settings ADD COLUMN IF NOT EXISTS tracked_server_log_channel_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE guild_settings ADD COLUMN IF NOT EXISTS alerts_channel_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE guild_settings ADD COLUMN IF NOT EXISTS admin_role_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE guild_settings DROP COLUMN IF EXISTS log_channel_id;

		CREATE TABLE IF NOT EXISTS wipe_reacts (
			message_id TEXT PRIMARY KEY,
			guild_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			creator_id TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			time_zone TEXT NOT NULL,
			scheduled_unix BIGINT NOT NULL,
			ask_late_reason BOOLEAN NOT NULL DEFAULT FALSE,
			ask_declined_reason BOOLEAN NOT NULL DEFAULT FALSE,
			accepted_user_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
			declined_user_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
			late_user_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
			embed_timestamp TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS wipe_reacts_guild_scheduled_idx
		ON wipe_reacts (guild_id, scheduled_unix DESC);

		CREATE TABLE IF NOT EXISTS wipe_react_schedules (
			id BIGSERIAL PRIMARY KEY,
			guild_id TEXT NOT NULL,
			days JSONB NOT NULL DEFAULT '[]'::jsonb,
			includes_force_wipe BOOLEAN NOT NULL DEFAULT FALSE,
			time TEXT NOT NULL DEFAULT '',
			time_zone TEXT NOT NULL,
			repeat TEXT NOT NULL DEFAULT '',
			post_before TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			ask_late_reason BOOLEAN NOT NULL DEFAULT FALSE,
			ask_declined_reason BOOLEAN NOT NULL DEFAULT FALSE,
			channel_id TEXT NOT NULL DEFAULT '',
			last_posted_for_unix BIGINT NOT NULL DEFAULT 0,
			created_by TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS wipe_react_schedules_guild_idx
		ON wipe_react_schedules (guild_id);

		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS ask_late_reason BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS ask_declined_reason BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS channel_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS last_posted_for_unix BIGINT NOT NULL DEFAULT 0;

		CREATE TABLE IF NOT EXISTS steam_accounts (
			steam_id TEXT PRIMARY KEY,
			bm_id TEXT UNIQUE,
			bm_name TEXT NOT NULL DEFAULT '',
			avatar_image TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		ALTER TABLE steam_accounts ADD COLUMN IF NOT EXISTS bm_name TEXT NOT NULL DEFAULT '';

		CREATE TABLE IF NOT EXISTS tracked_servers (
			bm_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			last_polled_at TIMESTAMPTZ,
			last_poll_error TEXT NOT NULL DEFAULT ''
		);

		ALTER TABLE tracked_servers ADD COLUMN IF NOT EXISTS last_polled_at TIMESTAMPTZ;
		ALTER TABLE tracked_servers ADD COLUMN IF NOT EXISTS last_poll_error TEXT NOT NULL DEFAULT '';

		CREATE TABLE IF NOT EXISTS player_states (
			server_bm_id TEXT NOT NULL REFERENCES tracked_servers(bm_id) ON DELETE CASCADE,
			player_bm_id TEXT NOT NULL,
			is_online BOOLEAN NOT NULL DEFAULT FALSE,
			online_since TIMESTAMPTZ,
			offline_since TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (server_bm_id, player_bm_id)
		);

		CREATE INDEX IF NOT EXISTS player_states_player_idx
		ON player_states (player_bm_id);

		CREATE INDEX IF NOT EXISTS player_states_server_online_idx
		ON player_states (server_bm_id, is_online, online_since DESC, updated_at DESC);

		CREATE TABLE IF NOT EXISTS guild_tracked_servers (
			guild_id TEXT NOT NULL,
			server_bm_id TEXT NOT NULL REFERENCES tracked_servers(bm_id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (guild_id, server_bm_id)
		);

		CREATE INDEX IF NOT EXISTS guild_tracked_servers_guild_idx
		ON guild_tracked_servers (guild_id);

		CREATE TABLE IF NOT EXISTS user_guilds (
			discord_id TEXT NOT NULL,
			guild_id TEXT NOT NULL,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			left_at TIMESTAMPTZ,
			PRIMARY KEY (discord_id, guild_id)
		);

		ALTER TABLE user_guilds ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;
		ALTER TABLE user_guilds ADD COLUMN IF NOT EXISTS first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
		ALTER TABLE user_guilds ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
		ALTER TABLE user_guilds ADD COLUMN IF NOT EXISTS left_at TIMESTAMPTZ;

		DO $$
		BEGIN
			IF EXISTS (
				SELECT 1
				FROM information_schema.columns
				WHERE table_name = 'user_guilds' AND column_name = 'created_at'
			) THEN
				EXECUTE '
					UPDATE user_guilds
					SET first_seen_at = COALESCE(first_seen_at, created_at),
						last_seen_at = COALESCE(last_seen_at, created_at)
					WHERE TRUE
				';

				EXECUTE 'ALTER TABLE user_guilds DROP COLUMN created_at';
			END IF;
		END
		$$;

		CREATE INDEX IF NOT EXISTS user_guilds_guild_idx
		ON user_guilds (guild_id);

		CREATE TABLE IF NOT EXISTS steam_discord_links (
			steam_id TEXT NOT NULL REFERENCES steam_accounts(steam_id),
			discord_id TEXT NOT NULL,
			type TEXT NOT NULL CHECK (type IN ('soft', 'hard')),
			source TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (steam_id, discord_id, type)
		);

		CREATE INDEX IF NOT EXISTS steam_discord_links_discord_idx
		ON steam_discord_links (discord_id);

		CREATE TABLE IF NOT EXISTS guild_steam_discord_links (
			guild_id TEXT NOT NULL,
			discord_id TEXT NOT NULL,
			steam_id TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (guild_id, discord_id, steam_id)
		);

		CREATE INDEX IF NOT EXISTS guild_steam_discord_links_guild_discord_idx
		ON guild_steam_discord_links (guild_id, discord_id);

		CREATE INDEX IF NOT EXISTS guild_steam_discord_links_guild_steam_idx
		ON guild_steam_discord_links (guild_id, steam_id);

		CREATE TABLE IF NOT EXISTS app_users (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL DEFAULT '',
			avatar_url TEXT NOT NULL DEFAULT '',
			roles JSONB NOT NULL DEFAULT '["user"]'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_login_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		ALTER TABLE app_users ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
		ALTER TABLE app_users ADD COLUMN IF NOT EXISTS avatar_url TEXT NOT NULL DEFAULT '';
		ALTER TABLE app_users ADD COLUMN IF NOT EXISTS roles JSONB NOT NULL DEFAULT '["user"]'::jsonb;
		ALTER TABLE app_users ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
		ALTER TABLE app_users ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

		CREATE TABLE IF NOT EXISTS user_connected_accounts (
			user_id TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			provider TEXT NOT NULL,
			provider_account_id TEXT NOT NULL,
			username TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL DEFAULT '',
			avatar_url TEXT NOT NULL DEFAULT '',
			profile_email TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (provider, provider_account_id),
			UNIQUE (user_id, provider)
		);

		CREATE INDEX IF NOT EXISTS user_connected_accounts_user_idx
		ON user_connected_accounts (user_id);

		CREATE TABLE IF NOT EXISTS user_emails (
			email TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			is_primary BOOLEAN NOT NULL DEFAULT FALSE,
			is_verified BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS user_emails_user_idx
		ON user_emails (user_id);

		CREATE TABLE IF NOT EXISTS user_password_credentials (
			user_id TEXT PRIMARY KEY REFERENCES app_users(id) ON DELETE CASCADE,
			password_hash TEXT NOT NULL,
			password_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS email_sign_in_tokens (
			token_hash TEXT PRIMARY KEY,
			email TEXT NOT NULL REFERENCES user_emails(email) ON DELETE CASCADE,
			expires_at TIMESTAMPTZ NOT NULL,
			consumed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS email_sign_in_tokens_email_idx
		ON email_sign_in_tokens (email);

		CREATE TABLE IF NOT EXISTS password_reset_tokens (
			token_hash TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			email TEXT NOT NULL REFERENCES user_emails(email) ON DELETE CASCADE,
			expires_at TIMESTAMPTZ NOT NULL,
			consumed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS password_reset_tokens_user_idx
		ON password_reset_tokens (user_id);

		CREATE TABLE IF NOT EXISTS email_verification_tokens (
			token_hash TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			email TEXT NOT NULL REFERENCES user_emails(email) ON DELETE CASCADE,
			expires_at TIMESTAMPTZ NOT NULL,
			consumed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS email_verification_tokens_user_idx
		ON email_verification_tokens (user_id);

		CREATE TABLE IF NOT EXISTS app_audit_events (
			id BIGSERIAL PRIMARY KEY,
			actor_user_id TEXT,
			target_user_id TEXT,
			event_type TEXT NOT NULL,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS app_audit_events_created_idx
		ON app_audit_events (created_at DESC);
	`)
	return err
}

func (p *PostgresStore) InsertWorkerEvent(ctx context.Context, workerID string, eventType string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO bot_worker_events (worker_id, event_type, payload)
		VALUES ($1, $2, $3::jsonb)
	`, workerID, eventType, string(encoded))

	return err
}

func (p *PostgresStore) GetGuildSettings(ctx context.Context, guildID string) (GuildSettings, error) {
	var settings GuildSettings

	err := p.pool.QueryRow(ctx, `
		SELECT wipe_react_log_channel_id,
			linking_log_channel_id,
			tracked_server_log_channel_id,
			alerts_channel_id,
			admin_role_id
		FROM guild_settings
		WHERE guild_id = $1
	`, guildID).Scan(
		&settings.WipeReactLogChannelID,
		&settings.LinkingLogChannelID,
		&settings.TrackedServerLogChannelID,
		&settings.AlertsChannelID,
		&settings.AdminRoleID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return GuildSettings{
			GuildID:                   guildID,
			WipeReactLogChannelID:     "",
			LinkingLogChannelID:       "",
			TrackedServerLogChannelID: "",
			AlertsChannelID:           "",
			AdminRoleID:               "",
		}, nil
	}
	if err != nil {
		return GuildSettings{}, err
	}

	settings.GuildID = guildID
	return settings, nil
}

func (p *PostgresStore) UpdateGuildSettings(ctx context.Context, guildID string, patch GuildSettingsPatch) (GuildSettings, error) {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO guild_settings (guild_id)
		VALUES ($1)
		ON CONFLICT (guild_id) DO NOTHING
	`, guildID)
	if err != nil {
		return GuildSettings{}, err
	}

	_, err = p.pool.Exec(ctx, `
		UPDATE guild_settings
		SET wipe_react_log_channel_id = COALESCE($2, wipe_react_log_channel_id),
			linking_log_channel_id = COALESCE($3, linking_log_channel_id),
			tracked_server_log_channel_id = COALESCE($4, tracked_server_log_channel_id),
			alerts_channel_id = COALESCE($5, alerts_channel_id),
			admin_role_id = COALESCE($6, admin_role_id),
			updated_at = NOW()
		WHERE guild_id = $1
	`,
		guildID,
		patch.WipeReactLogChannelID,
		patch.LinkingLogChannelID,
		patch.TrackedServerLogChannelID,
		patch.AlertsChannelID,
		patch.AdminRoleID,
	)
	if err != nil {
		return GuildSettings{}, err
	}

	return p.GetGuildSettings(ctx, guildID)
}
