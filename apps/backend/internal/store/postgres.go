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
	TrackedUsersCategoryID    string
	AdminRoleID               string
}

type GuildSettingsPatch struct {
	WipeReactLogChannelID     *string
	LinkingLogChannelID       *string
	TrackedServerLogChannelID *string
	TrackedUsersCategoryID    *string
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
			tracked_users_category_id TEXT NOT NULL DEFAULT '',
			admin_role_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		DROP TABLE IF EXISTS guild_setups;

		ALTER TABLE guild_settings ADD COLUMN IF NOT EXISTS wipe_react_log_channel_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE guild_settings ADD COLUMN IF NOT EXISTS linking_log_channel_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE guild_settings ADD COLUMN IF NOT EXISTS tracked_server_log_channel_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE guild_settings ADD COLUMN IF NOT EXISTS tracked_users_category_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE guild_settings ADD COLUMN IF NOT EXISTS admin_role_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE guild_settings DROP COLUMN IF EXISTS log_channel_id;
		ALTER TABLE guild_settings DROP COLUMN IF EXISTS alerts_channel_id;

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

		CREATE TABLE IF NOT EXISTS wipe_react_schedule_posts (
			id BIGSERIAL PRIMARY KEY,
			schedule_id BIGINT NOT NULL REFERENCES wipe_react_schedules(id) ON DELETE CASCADE,
			guild_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			wipe_unix BIGINT NOT NULL,
			post_unix BIGINT NOT NULL,
			claimed_by TEXT NOT NULL DEFAULT '',
			claimed_at TIMESTAMPTZ,
			message_id TEXT NOT NULL DEFAULT '',
			posted_channel_id TEXT NOT NULL DEFAULT '',
			completed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (schedule_id, wipe_unix)
		);

		CREATE INDEX IF NOT EXISTS wipe_react_schedule_posts_claim_idx
		ON wipe_react_schedule_posts (guild_id, completed_at, post_unix, claimed_at);

		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS ask_late_reason BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS ask_declined_reason BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS channel_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS last_posted_for_unix BIGINT NOT NULL DEFAULT 0;

		CREATE TABLE IF NOT EXISTS steam_accounts (
			steam_id TEXT PRIMARY KEY,
			bm_id TEXT UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS steam_account_cache (
			steam_id TEXT PRIMARY KEY REFERENCES steam_accounts(steam_id) ON DELETE CASCADE,
			persona_name TEXT NOT NULL DEFAULT '',
			avatar_image TEXT NOT NULL DEFAULT '',
			rust_hours BIGINT,
			total_games INTEGER,
			top_games JSONB NOT NULL DEFAULT '[]'::jsonb,
			game_banned BOOLEAN NOT NULL DEFAULT FALSE,
			game_banned_at TIMESTAMPTZ,
			game_ban_checked_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		ALTER TABLE steam_account_cache ADD COLUMN IF NOT EXISTS total_games INTEGER;
		ALTER TABLE steam_account_cache ADD COLUMN IF NOT EXISTS top_games JSONB NOT NULL DEFAULT '[]'::jsonb;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS analytics_total_games;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS analytics_top_games;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS analytics_total_servers;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS analytics_top_servers;
		ALTER TABLE steam_account_cache ADD COLUMN IF NOT EXISTS game_banned BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE steam_account_cache ADD COLUMN IF NOT EXISTS game_banned_at TIMESTAMPTZ;
		ALTER TABLE steam_account_cache ADD COLUMN IF NOT EXISTS game_ban_checked_at TIMESTAMPTZ;
		ALTER TABLE steam_account_cache ADD COLUMN IF NOT EXISTS persona_name TEXT NOT NULL DEFAULT '';
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS analytics_gaps;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS analytics_updated_at;

		CREATE INDEX IF NOT EXISTS steam_account_cache_updated_idx
		ON steam_account_cache (updated_at);

		CREATE TABLE IF NOT EXISTS bm_profile_cache (
			bm_id TEXT PRIMARY KEY,
			bm_name TEXT NOT NULL DEFAULT '',
			first_seen_at TIMESTAMPTZ,
			vitality_hours BIGINT NOT NULL DEFAULT 0,
			atlas_hours BIGINT NOT NULL DEFAULT 0,
			vital_hours BIGINT NOT NULL DEFAULT 0,
			modded_hours BIGINT NOT NULL DEFAULT 0,
			official_hours BIGINT NOT NULL DEFAULT 0,
			build_hours BIGINT NOT NULL DEFAULT 0,
			bm_hours BIGINT NOT NULL DEFAULT 0,
			bm_servers_played INTEGER NOT NULL DEFAULT 0,
			top_servers JSONB NOT NULL DEFAULT '[]'::jsonb,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		ALTER TABLE bm_profile_cache DROP COLUMN IF EXISTS name_gaps;

		CREATE INDEX IF NOT EXISTS bm_profile_cache_updated_idx
		ON bm_profile_cache (updated_at);

		CREATE TABLE IF NOT EXISTS bm_name_history (
			bm_id TEXT NOT NULL,
			name TEXT NOT NULL,
			last_seen_at TIMESTAMPTZ NOT NULL,
			observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (bm_id, name, last_seen_at)
		);

		ALTER TABLE bm_name_history DROP CONSTRAINT IF EXISTS bm_name_history_bm_id_fkey;

		CREATE INDEX IF NOT EXISTS bm_name_history_bm_seen_idx
		ON bm_name_history (bm_id, last_seen_at DESC);

		DO $$
		DECLARE
			has_legacy_bm_id BOOLEAN;
		BEGIN
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'steam_account_cache' AND column_name = 'bm_id'
			) INTO has_legacy_bm_id;

			IF has_legacy_bm_id THEN
				EXECUTE '
					INSERT INTO bm_profile_cache (
						bm_id, bm_name, first_seen_at, vitality_hours, atlas_hours, vital_hours,
						modded_hours, official_hours, build_hours, bm_hours, bm_servers_played,
						top_servers, updated_at
					)
					SELECT
						sac.bm_id,
						sac.bm_name,
						sac.first_seen_at,
						sac.vitality_hours,
						sac.atlas_hours,
						sac.vital_hours,
						sac.modded_hours,
						sac.official_hours,
						sac.build_hours,
						sac.bm_hours,
						sac.bm_servers_played,
						sac.top_servers,
						sac.updated_at
					FROM steam_account_cache sac
					WHERE COALESCE(sac.bm_id, '''') <> ''''
					ON CONFLICT (bm_id) DO UPDATE
					SET bm_name = EXCLUDED.bm_name,
						first_seen_at = EXCLUDED.first_seen_at,
						vitality_hours = EXCLUDED.vitality_hours,
						atlas_hours = EXCLUDED.atlas_hours,
						vital_hours = EXCLUDED.vital_hours,
						modded_hours = EXCLUDED.modded_hours,
						official_hours = EXCLUDED.official_hours,
						build_hours = EXCLUDED.build_hours,
						bm_hours = EXCLUDED.bm_hours,
						bm_servers_played = EXCLUDED.bm_servers_played,
						top_servers = EXCLUDED.top_servers,
						updated_at = EXCLUDED.updated_at
					WHERE bm_profile_cache.updated_at < EXCLUDED.updated_at
				';
			END IF;
		END $$;

		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS bm_id;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS bm_name;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS first_seen_at;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS vitality_hours;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS atlas_hours;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS vital_hours;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS modded_hours;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS official_hours;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS build_hours;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS bm_hours;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS bm_servers_played;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS name_gaps;
		ALTER TABLE steam_account_cache DROP COLUMN IF EXISTS top_servers;

		DO $$
		DECLARE
			has_bm_name BOOLEAN;
			has_avatar_image BOOLEAN;
			has_first_seen_at BOOLEAN;
			has_updated_at BOOLEAN;
		BEGIN
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'steam_accounts' AND column_name = 'bm_name'
			) INTO has_bm_name;

			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'steam_accounts' AND column_name = 'avatar_image'
			) INTO has_avatar_image;

			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'steam_accounts' AND column_name = 'first_seen_at'
			) INTO has_first_seen_at;

			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'steam_accounts' AND column_name = 'updated_at'
			) INTO has_updated_at;

			IF has_bm_name AND has_avatar_image AND has_first_seen_at AND has_updated_at THEN
				EXECUTE '
					INSERT INTO steam_account_cache (steam_id, bm_id, bm_name, avatar_image, first_seen_at, updated_at)
					SELECT steam_id, COALESCE(bm_id, ''''), COALESCE(bm_name, ''''), COALESCE(avatar_image, ''''), first_seen_at, updated_at
					FROM steam_accounts
					WHERE COALESCE(bm_name, '''') <> '''' OR COALESCE(avatar_image, '''') <> '''' OR first_seen_at IS NOT NULL
					ON CONFLICT (steam_id) DO NOTHING
				';
			ELSIF has_bm_name AND has_first_seen_at AND has_updated_at THEN
				EXECUTE '
					INSERT INTO steam_account_cache (steam_id, bm_id, bm_name, first_seen_at, updated_at)
					SELECT steam_id, COALESCE(bm_id, ''''), COALESCE(bm_name, ''''), first_seen_at, updated_at
					FROM steam_accounts
					WHERE COALESCE(bm_name, '''') <> '''' OR first_seen_at IS NOT NULL
					ON CONFLICT (steam_id) DO NOTHING
				';
			ELSIF has_first_seen_at AND has_updated_at THEN
				EXECUTE '
					INSERT INTO steam_account_cache (steam_id, bm_id, first_seen_at, updated_at)
					SELECT steam_id, COALESCE(bm_id, ''''), first_seen_at, updated_at
					FROM steam_accounts
					WHERE first_seen_at IS NOT NULL
					ON CONFLICT (steam_id) DO NOTHING
				';
			END IF;

			IF has_bm_name THEN
				ALTER TABLE steam_accounts DROP COLUMN bm_name;
			END IF;

			IF has_avatar_image THEN
				ALTER TABLE steam_accounts DROP COLUMN avatar_image;
			END IF;

			IF has_first_seen_at THEN
				ALTER TABLE steam_accounts DROP COLUMN first_seen_at;
			END IF;

			IF has_updated_at THEN
				ALTER TABLE steam_accounts DROP COLUMN updated_at;
			END IF;
		END $$;

		CREATE TABLE IF NOT EXISTS tracked_servers (
			bm_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			is_vitality BOOLEAN NOT NULL DEFAULT FALSE,
			is_atlas BOOLEAN NOT NULL DEFAULT FALSE,
			is_vital BOOLEAN NOT NULL DEFAULT FALSE,
			is_modded BOOLEAN NOT NULL DEFAULT FALSE,
			is_official BOOLEAN NOT NULL DEFAULT FALSE,
			is_build BOOLEAN NOT NULL DEFAULT FALSE,
			high_frequency BOOLEAN NOT NULL DEFAULT FALSE,
			last_polled_at TIMESTAMPTZ,
			last_poll_error TEXT NOT NULL DEFAULT ''
		);

		ALTER TABLE tracked_servers DROP COLUMN IF EXISTS include_in_guild_settings;
		ALTER TABLE tracked_servers ADD COLUMN IF NOT EXISTS is_vitality BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE tracked_servers ADD COLUMN IF NOT EXISTS is_atlas BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE tracked_servers ADD COLUMN IF NOT EXISTS is_vital BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE tracked_servers ADD COLUMN IF NOT EXISTS is_modded BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE tracked_servers ADD COLUMN IF NOT EXISTS is_official BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE tracked_servers ADD COLUMN IF NOT EXISTS is_build BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE tracked_servers ADD COLUMN IF NOT EXISTS high_frequency BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE tracked_servers ADD COLUMN IF NOT EXISTS last_polled_at TIMESTAMPTZ;
		ALTER TABLE tracked_servers ADD COLUMN IF NOT EXISTS last_poll_error TEXT NOT NULL DEFAULT '';

		CREATE TABLE IF NOT EXISTS outbound_proxies (
			id BIGSERIAL PRIMARY KEY,
			redacted_endpoint TEXT NOT NULL,
			endpoint_ciphertext BYTEA NOT NULL,
			endpoint_nonce BYTEA NOT NULL,
			key_id TEXT NOT NULL DEFAULT 'default',
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		ALTER TABLE outbound_proxies ADD COLUMN IF NOT EXISTS redacted_endpoint TEXT NOT NULL DEFAULT '';
		ALTER TABLE outbound_proxies ADD COLUMN IF NOT EXISTS endpoint_ciphertext BYTEA NOT NULL DEFAULT '\\x'::bytea;
		ALTER TABLE outbound_proxies ADD COLUMN IF NOT EXISTS endpoint_nonce BYTEA NOT NULL DEFAULT '\\x'::bytea;
		ALTER TABLE outbound_proxies ADD COLUMN IF NOT EXISTS key_id TEXT NOT NULL DEFAULT 'default';
		ALTER TABLE outbound_proxies ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT TRUE;
		ALTER TABLE outbound_proxies ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
		ALTER TABLE outbound_proxies ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

		CREATE INDEX IF NOT EXISTS outbound_proxies_enabled_idx
		ON outbound_proxies (enabled, id);

		CREATE UNIQUE INDEX IF NOT EXISTS outbound_proxies_redacted_endpoint_idx
		ON outbound_proxies (redacted_endpoint);

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

		DROP TABLE IF EXISTS guild_tracked_log_cursors;
		DROP TABLE IF EXISTS player_state_events;

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

		CREATE TABLE IF NOT EXISTS bot_guilds (
			guild_id                 TEXT PRIMARY KEY,
			name                     TEXT NOT NULL DEFAULT '',
			icon_hash                TEXT,
			banner_hash              TEXT,
			owner_discord_id         TEXT,
			owner_username           TEXT,
			owner_global_name        TEXT,
			owner_avatar_hash        TEXT,
			approximate_member_count INTEGER,
			is_installed             BOOLEAN NOT NULL DEFAULT TRUE,
			installed_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			left_at                  TIMESTAMPTZ,
			metadata_updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			members_synced_at        TIMESTAMPTZ
		);

		CREATE INDEX IF NOT EXISTS bot_guilds_installed_idx
		ON bot_guilds (is_installed) WHERE is_installed = TRUE;

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

		CREATE TABLE IF NOT EXISTS hidden_discord_link_identities (
			discord_id TEXT PRIMARY KEY,
			reason TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

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

		CREATE TABLE IF NOT EXISTS platform_sync_state (
			platform TEXT NOT NULL CHECK (platform IN ('discord', 'steam')),
			platform_id TEXT NOT NULL,
			last_platform_sync_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (platform, platform_id)
		);

		CREATE TABLE IF NOT EXISTS track_groups (
			id BIGSERIAL PRIMARY KEY,
			guild_id TEXT NOT NULL,
			name TEXT NOT NULL,
			server_bm_id TEXT NOT NULL REFERENCES tracked_servers(bm_id) ON DELETE CASCADE,
			channel_id TEXT NOT NULL DEFAULT '',
			message_id TEXT NOT NULL DEFAULT '',
			send_updates BOOLEAN NOT NULL DEFAULT FALSE,
			online_count INTEGER NOT NULL DEFAULT 0,
			created_by TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (guild_id, name)
		);

		CREATE INDEX IF NOT EXISTS track_groups_guild_idx ON track_groups (guild_id);

		CREATE TABLE IF NOT EXISTS track_group_members (
			track_group_id BIGINT NOT NULL REFERENCES track_groups(id) ON DELETE CASCADE,
			steam_id TEXT NOT NULL,
			bm_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (track_group_id, steam_id)
		);

		CREATE INDEX IF NOT EXISTS track_group_members_group_idx ON track_group_members (track_group_id);
		CREATE INDEX IF NOT EXISTS track_group_members_bm_idx ON track_group_members (bm_id);

		CREATE TABLE IF NOT EXISTS track_analytics_sessions_cache (
			player_bm_id TEXT NOT NULL,
			server_bm_id TEXT NOT NULL,
			sessions JSONB NOT NULL DEFAULT '[]'::jsonb,
			fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (player_bm_id, server_bm_id)
		);

		CREATE INDEX IF NOT EXISTS track_analytics_sessions_cache_fetched_idx
			ON track_analytics_sessions_cache (fetched_at);

		CREATE TABLE IF NOT EXISTS server_player_count_history (
			server_bm_id TEXT NOT NULL REFERENCES tracked_servers(bm_id) ON DELETE CASCADE,
			timestamp_unix BIGINT NOT NULL,
			min INTEGER NOT NULL,
			value DOUBLE PRECISION NOT NULL,
			max INTEGER NOT NULL,
			observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (server_bm_id, timestamp_unix)
		);

		CREATE INDEX IF NOT EXISTS server_player_count_history_server_time_idx
			ON server_player_count_history (server_bm_id, timestamp_unix);

		CREATE INDEX IF NOT EXISTS server_player_count_history_observed_idx
			ON server_player_count_history (observed_at);

		CREATE TABLE IF NOT EXISTS server_player_count_history_fetches (
			server_bm_id TEXT PRIMARY KEY REFERENCES tracked_servers(bm_id) ON DELETE CASCADE,
			fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		DROP TABLE IF EXISTS server_player_count_history_cache;

		CREATE TABLE IF NOT EXISTS user_bans (
			discord_id TEXT PRIMARY KEY,
			categories TEXT[] NOT NULL DEFAULT '{}',
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

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
		    actor_user_id TEXT,
		    actor_role_snapshot TEXT[] NOT NULL DEFAULT '{}',
		    resource_type TEXT,
		    resource_id TEXT,
		    target_user_id TEXT,
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

	// app_users — Discord-only identity. discord_id is the sole login key.
	// user_emails / user_connected_accounts / user_password_credentials and the
	// email-token tables are intentionally absent; the email/password auth
	// surface was removed in the Discord-only migration. Re-running against a
	// legacy DB does NOT retroactively enforce NOT NULL/UNIQUE on existing
	// rows — wipe and re-create for a clean shape.
	if _, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS app_users (
			id                  TEXT PRIMARY KEY,
			discord_id          TEXT UNIQUE NOT NULL,
			discord_username    TEXT NOT NULL,
			discord_global_name TEXT NULL,
			discord_avatar      TEXT NULL,
			display_name        TEXT NOT NULL DEFAULT '',
			avatar_url          TEXT NOT NULL DEFAULT '',
			roles               JSONB NOT NULL DEFAULT '["user"]'::jsonb,
			created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_login_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`); err != nil {
		return fmt.Errorf("create app_users: %w", err)
	}

	if _, err := p.pool.Exec(ctx, `
		ALTER TABLE app_users
			ADD COLUMN IF NOT EXISTS discord_id          TEXT,
			ADD COLUMN IF NOT EXISTS discord_username    TEXT,
			ADD COLUMN IF NOT EXISTS discord_global_name TEXT,
			ADD COLUMN IF NOT EXISTS discord_avatar      TEXT;
	`); err != nil {
		return fmt.Errorf("alter app_users add discord columns: %w", err)
	}

	if _, err := p.pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS app_users_discord_id_idx ON app_users (discord_id)`); err != nil {
		return fmt.Errorf("create app_users_discord_id_idx: %w", err)
	}

	// Backfill display_name from Discord identity for legacy rows where it was
	// never populated (the original Discord upsert omitted display_name, so all
	// pre-fix users have an empty string here).
	if _, err := p.pool.Exec(ctx, `
		UPDATE app_users
		SET display_name = COALESCE(NULLIF(discord_global_name, ''), discord_username)
		WHERE (display_name IS NULL OR display_name = '')
		  AND discord_username IS NOT NULL
	`); err != nil {
		return fmt.Errorf("backfill app_users.display_name: %w", err)
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

	// Migrate audit_events actor/target columns from UUID to TEXT so they can
	// reference app_users(id) which is TEXT. The unmerged admin foundation has
	// no production audit data yet, so an in-place TYPE change is safe.
	if _, err := p.pool.Exec(ctx, `
		DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'audit_events' AND column_name = 'actor_user_id' AND data_type = 'uuid'
			) THEN
				ALTER TABLE audit_events ALTER COLUMN actor_user_id TYPE TEXT USING actor_user_id::text;
			END IF;
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'audit_events' AND column_name = 'target_user_id' AND data_type = 'uuid'
			) THEN
				ALTER TABLE audit_events ALTER COLUMN target_user_id TYPE TEXT USING target_user_id::text;
			END IF;
		END $$;
	`); err != nil {
		return fmt.Errorf("migrate audit_events actor/target to text: %w", err)
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

	// user_notification_preferences — per-user notification flags.
	// Flags are storage-only at this point; no delivery system reads them yet.
	if _, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS user_notification_preferences (
			user_id                       TEXT PRIMARY KEY REFERENCES app_users(id) ON DELETE CASCADE,
			notify_in_app                 BOOLEAN     NOT NULL DEFAULT TRUE,
			notify_discord_announcements  BOOLEAN     NOT NULL DEFAULT TRUE,
			updated_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create user_notification_preferences: %w", err)
	}

	if _, err := p.pool.Exec(ctx, `
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
	`); err != nil {
		return fmt.Errorf("create app auth tables: %w", err)
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
			tracked_users_category_id,
			admin_role_id
		FROM guild_settings
		WHERE guild_id = $1
	`, guildID).Scan(
		&settings.WipeReactLogChannelID,
		&settings.LinkingLogChannelID,
		&settings.TrackedServerLogChannelID,
		&settings.TrackedUsersCategoryID,
		&settings.AdminRoleID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return GuildSettings{
			GuildID:                   guildID,
			WipeReactLogChannelID:     "",
			LinkingLogChannelID:       "",
			TrackedServerLogChannelID: "",
			TrackedUsersCategoryID:    "",
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
			tracked_users_category_id = COALESCE($5, tracked_users_category_id),
			admin_role_id = COALESCE($6, admin_role_id),
			updated_at = NOW()
		WHERE guild_id = $1
	`,
		guildID,
		patch.WipeReactLogChannelID,
		patch.LinkingLogChannelID,
		patch.TrackedServerLogChannelID,
		patch.TrackedUsersCategoryID,
		patch.AdminRoleID,
	)
	if err != nil {
		return GuildSettings{}, err
	}

	return p.GetGuildSettings(ctx, guildID)
}
