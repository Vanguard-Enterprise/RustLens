package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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

func (p *PostgresStore) Pool() *pgxpool.Pool { return p.pool }

func (p *PostgresStore) Close() {
	p.pool.Close()
}

func (p *PostgresStore) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *PostgresStore) EnsureSchema(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `
		CREATE EXTENSION IF NOT EXISTS pgcrypto;

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

		CREATE TABLE IF NOT EXISTS admin_permissions (
		    key TEXT PRIMARY KEY,
		    description TEXT NOT NULL,
		    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS admin_roles (
		    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		    key TEXT UNIQUE NOT NULL,
		    name TEXT NOT NULL,
		    description TEXT,
		    is_system BOOLEAN NOT NULL DEFAULT FALSE,
		    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS admin_role_permissions (
		    role_id UUID NOT NULL REFERENCES admin_roles(id) ON DELETE CASCADE,
		    permission_key TEXT NOT NULL REFERENCES admin_permissions(key) ON DELETE CASCADE,
		    PRIMARY KEY (role_id, permission_key)
		);

		CREATE TABLE IF NOT EXISTS audit_events (
		    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		    action_type TEXT NOT NULL,
		    actor_user_id UUID,
		    actor_role_snapshot TEXT[] NOT NULL DEFAULT '{}',
		    resource_type TEXT,
		    resource_id TEXT,
		    target_user_id UUID,
		    params JSONB NOT NULL DEFAULT '{}',
		    reason TEXT,
		    result TEXT NOT NULL,
		    error_message TEXT,
		    ip_address INET,
		    user_agent TEXT,
		    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS audit_events_actor_idx ON audit_events (actor_user_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS audit_events_resource_idx ON audit_events (resource_type, resource_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS audit_events_action_idx ON audit_events (action_type, created_at DESC);
		CREATE INDEX IF NOT EXISTS audit_events_target_user_idx ON audit_events (target_user_id, created_at DESC);
	`); err != nil {
		return err
	}

	// app_users and the login/connected-account/email tables are created by the
	// appauth subsystem in production, but we mirror them here as IF-NOT-EXISTS
	// so a fresh test database can install the full admin + app-auth schema
	// from a single EnsureSchema call.
	if _, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS app_users (
			id            TEXT PRIMARY KEY,
			display_name  TEXT NOT NULL DEFAULT '',
			avatar_url    TEXT NOT NULL DEFAULT '',
			roles         JSONB NOT NULL DEFAULT '["user"]'::jsonb,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_login_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS user_emails (
			email       TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			is_primary  BOOLEAN NOT NULL DEFAULT FALSE,
			is_verified BOOLEAN NOT NULL DEFAULT FALSE,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS user_emails_user_idx ON user_emails (user_id);
		CREATE TABLE IF NOT EXISTS user_connected_accounts (
			user_id             TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			provider            TEXT NOT NULL,
			provider_account_id TEXT NOT NULL,
			username            TEXT NOT NULL DEFAULT '',
			display_name        TEXT NOT NULL DEFAULT '',
			avatar_url          TEXT NOT NULL DEFAULT '',
			profile_email       TEXT NOT NULL DEFAULT '',
			created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (provider, provider_account_id),
			UNIQUE (user_id, provider)
		);
		CREATE INDEX IF NOT EXISTS user_connected_accounts_user_idx ON user_connected_accounts (user_id);
	`); err != nil {
		return fmt.Errorf("create app_users and related tables: %w", err)
	}

	// Drop orphaned legacy users(UUID) table if empty — it's been supplanted by
	// app_users (TEXT id). CASCADE removes stale FK constraints from audit_events.
	if _, err := p.pool.Exec(ctx, `
		DO $$
		DECLARE
			row_count INT;
		BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'users') THEN
				EXECUTE 'SELECT COUNT(*) FROM users' INTO row_count;
				IF row_count = 0 THEN
					DROP TABLE users CASCADE;
				END IF;
			END IF;
		END $$;
	`); err != nil {
		return fmt.Errorf("drop legacy users(uuid): %w", err)
	}

	// admin_user_roles: rekeyed to app_users(id) TEXT. A one-time DROP removes
	// the old UUID-shaped table if present (no production data).
	if _, err := p.pool.Exec(ctx, `
		DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'admin_user_roles' AND column_name = 'user_id' AND data_type = 'uuid'
			) THEN
				DROP TABLE admin_user_roles;
			END IF;
		END $$;
	`); err != nil {
		return fmt.Errorf("drop legacy admin_user_roles (uuid): %w", err)
	}
	if _, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS admin_user_roles (
			user_id    TEXT        NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			role_id    UUID        NOT NULL REFERENCES admin_roles(id) ON DELETE CASCADE,
			granted_by TEXT        NULL     REFERENCES app_users(id),
			granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (user_id, role_id)
		)
	`); err != nil {
		return fmt.Errorf("create admin_user_roles: %w", err)
	}

	// app_users lifecycle columns — suspension and soft-delete tracking
	// plus a session epoch counter used to invalidate active sessions.
	if _, err := p.pool.Exec(ctx, `
		ALTER TABLE app_users
			ADD COLUMN IF NOT EXISTS suspended_at   TIMESTAMPTZ NULL,
			ADD COLUMN IF NOT EXISTS suspended_by   TEXT        NULL REFERENCES app_users(id),
			ADD COLUMN IF NOT EXISTS suspend_reason TEXT        NULL,
			ADD COLUMN IF NOT EXISTS deleted_at     TIMESTAMPTZ NULL,
			ADD COLUMN IF NOT EXISTS deleted_by     TEXT        NULL REFERENCES app_users(id),
			ADD COLUMN IF NOT EXISTS session_epoch  INT         NOT NULL DEFAULT 0
	`); err != nil {
		return fmt.Errorf("alter app_users lifecycle columns: %w", err)
	}
	if _, err := p.pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS app_users_active_idx
			ON app_users (created_at DESC)
			WHERE deleted_at IS NULL
	`); err != nil {
		return fmt.Errorf("create app_users_active_idx: %w", err)
	}
	return nil
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
