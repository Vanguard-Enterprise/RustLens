-- RustLens baseline schema (distilled end-state, Go-removal B1).
--
-- Ground truth: the Go backend's store.EnsureSchema() run against a fresh
-- Postgres 16, dumped with `pg_dump --schema-only --no-owner --no-privileges`,
-- then distilled into a fully idempotent, dependency-ordered form:
--   * pgcrypto extension first (gen_random_uuid).
--   * Every object uses IF NOT EXISTS; foreign keys, unique keys, and check
--     constraints are folded inline with the same auto-generated names Postgres
--     (and therefore pg_dump) assigns, so a re-dump matches the Go dump.
--   * Idempotent data seeds Go performs at startup are kept (cached_counts).
--   * Dropped: all legacy DROP/backfill/ALTER-TYPE migration history, the
--     runtime RBAC role/permission seed (Go seeds it at server boot, not in
--     EnsureSchema; the Rust admin service owns that), the pro-entitlement
--     backfill (a no-op on a fresh DB), and Go's internal `schema_migrations`
--     bookkeeping table (sqlx uses `_sqlx_migrations`).
--
-- Applying this against a database already migrated by Go is a safe no-op: every
-- statement self-gates. Applying against an empty database produces a schema
-- equivalent to the Go dump.
--
-- IMPORTANT: this file is frozen once landed. sqlx records its checksum in
-- `_sqlx_migrations`; editing it after any environment has applied it breaks
-- migration verification. All future schema changes are new `0002_*.sql` files.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ---------------------------------------------------------------------------
-- App users and admin RBAC
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS app_users (
    id                  text PRIMARY KEY,
    discord_id          text NOT NULL UNIQUE,
    discord_username    text NOT NULL,
    discord_global_name text,
    discord_avatar      text,
    display_name        text NOT NULL DEFAULT '',
    avatar_url          text NOT NULL DEFAULT '',
    roles               jsonb NOT NULL DEFAULT '["user"]'::jsonb,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    last_login_at       timestamptz NOT NULL DEFAULT now(),
    suspended_at        timestamptz,
    suspended_by        text REFERENCES app_users(id),
    suspend_reason      text,
    deleted_at          timestamptz,
    deleted_by          text REFERENCES app_users(id),
    session_epoch       integer NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS app_users_discord_id_idx ON app_users USING btree (discord_id);
CREATE INDEX IF NOT EXISTS app_users_active_idx ON app_users USING btree (created_at DESC) WHERE (deleted_at IS NULL);

CREATE TABLE IF NOT EXISTS admin_permissions (
    key         text PRIMARY KEY,
    description text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS admin_roles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key         text NOT NULL UNIQUE,
    name        text NOT NULL,
    description text,
    is_system   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS admin_role_permissions (
    role_id        uuid NOT NULL REFERENCES admin_roles(id) ON DELETE CASCADE,
    permission_key text NOT NULL REFERENCES admin_permissions(key) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_key)
);

CREATE TABLE IF NOT EXISTS admin_user_roles (
    user_id    text NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
    role_id    uuid NOT NULL REFERENCES admin_roles(id) ON DELETE CASCADE,
    granted_by text REFERENCES app_users(id),
    granted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS api_keys (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name               text NOT NULL,
    prefix             text NOT NULL UNIQUE,
    last4              text NOT NULL,
    secret_hash        bytea NOT NULL,
    created_by_user_id text NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
    permissions        text[] NOT NULL DEFAULT '{}'::text[],
    expires_at         timestamptz,
    revoked_at         timestamptz,
    revoked_reason     text,
    last_used_at       timestamptz,
    last_used_ip       inet,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS api_keys_created_by_idx ON api_keys USING btree (created_by_user_id, revoked_at);

CREATE TABLE IF NOT EXISTS audit_events (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    action_type         text NOT NULL,
    actor_user_id       text,
    actor_role_snapshot text[] NOT NULL DEFAULT '{}'::text[],
    resource_type       text,
    resource_id         text,
    target_user_id      text,
    params              jsonb NOT NULL DEFAULT '{}'::jsonb,
    reason              text,
    result              text NOT NULL,
    error_message       text,
    ip_address          inet,
    user_agent          text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    via_api_key_id      uuid REFERENCES api_keys(id) ON DELETE SET NULL,
    via_api_key_name    text
);

CREATE INDEX IF NOT EXISTS audit_events_action_idx ON audit_events USING btree (action_type, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_events_actor_idx ON audit_events USING btree (actor_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_events_resource_idx ON audit_events USING btree (resource_type, resource_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_events_target_user_idx ON audit_events USING btree (target_user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS app_audit_events (
    id             bigserial PRIMARY KEY,
    actor_user_id  text,
    target_user_id text,
    event_type     text NOT NULL,
    payload        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS app_audit_events_created_idx ON app_audit_events USING btree (created_at DESC);

CREATE TABLE IF NOT EXISTS user_entitlements (
    user_id    text NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
    key        text NOT NULL,
    granted_by text REFERENCES app_users(id),
    granted_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz,
    PRIMARY KEY (user_id, key)
);

CREATE INDEX IF NOT EXISTS user_entitlements_key_idx ON user_entitlements USING btree (key, user_id);

CREATE TABLE IF NOT EXISTS user_notification_preferences (
    user_id                      text PRIMARY KEY REFERENCES app_users(id) ON DELETE CASCADE,
    notify_in_app                boolean NOT NULL DEFAULT true,
    notify_discord_announcements boolean NOT NULL DEFAULT true,
    updated_at                   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS discord_role_connections (
    user_id             text PRIMARY KEY REFERENCES app_users(id) ON DELETE CASCADE,
    discord_id          text NOT NULL,
    access_token_ct     bytea NOT NULL,
    access_token_nonce  bytea NOT NULL,
    refresh_token_ct    bytea NOT NULL,
    refresh_token_nonce bytea NOT NULL,
    access_expires_at   timestamptz NOT NULL,
    scopes              text NOT NULL,
    linked_at           timestamptz NOT NULL DEFAULT now(),
    last_pushed_at      timestamptz,
    last_pushed_hash    text,
    last_push_error     text,
    revoked_at          timestamptz
);

CREATE INDEX IF NOT EXISTS discord_role_connections_active_idx ON discord_role_connections USING btree (revoked_at) WHERE (revoked_at IS NULL);

-- ---------------------------------------------------------------------------
-- Command usage
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS command_usage (
    id               bigserial PRIMARY KEY,
    app_user_id      text NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
    discord_id       text NOT NULL,
    guild_id         text,
    channel_id       text,
    interaction_type text NOT NULL,
    command          text NOT NULL,
    subcommand       text,
    target           text,
    options          jsonb NOT NULL DEFAULT '{}'::jsonb,
    outcome          text NOT NULL,
    error_code       text,
    latency_ms       integer NOT NULL,
    correlation_id   text,
    parent_usage_id  bigint REFERENCES command_usage(id) ON DELETE SET NULL,
    channel_kind     text,
    channel_name     text,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS command_usage_user_created_idx ON command_usage USING btree (app_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS command_usage_created_idx ON command_usage USING btree (created_at DESC);
CREATE INDEX IF NOT EXISTS command_usage_correlation_idx ON command_usage USING btree (app_user_id, correlation_id) WHERE (correlation_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS command_usage_parent_idx ON command_usage USING btree (parent_usage_id) WHERE (parent_usage_id IS NOT NULL);

CREATE TABLE IF NOT EXISTS command_usage_targets (
    id               bigserial PRIMARY KEY,
    command_usage_id bigint NOT NULL REFERENCES command_usage(id) ON DELETE CASCADE,
    kind             text NOT NULL,
    snowflake        text NOT NULL,
    display_name     text,
    avatar_hash      text,
    relation         text NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT command_usage_targets_kind_chk CHECK ((kind = ANY (ARRAY['user'::text, 'role'::text, 'channel'::text, 'guild'::text, 'steam'::text]))),
    CONSTRAINT command_usage_targets_relation_chk CHECK ((relation = ANY (ARRAY['recipient'::text, 'scope'::text, 'subject'::text])))
);

CREATE INDEX IF NOT EXISTS command_usage_targets_usage_idx ON command_usage_targets USING btree (command_usage_id);
CREATE INDEX IF NOT EXISTS command_usage_targets_snowflake_idx ON command_usage_targets USING btree (snowflake);

-- ---------------------------------------------------------------------------
-- Bot guilds, user installs, and user-guild membership
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS bot_guilds (
    guild_id                 text PRIMARY KEY,
    name                     text NOT NULL DEFAULT '',
    icon_hash                text,
    banner_hash              text,
    owner_discord_id         text,
    owner_username           text,
    owner_global_name        text,
    owner_avatar_hash        text,
    approximate_member_count integer,
    is_installed             boolean NOT NULL DEFAULT true,
    installed_at             timestamptz NOT NULL DEFAULT now(),
    left_at                  timestamptz,
    metadata_updated_at      timestamptz NOT NULL DEFAULT now(),
    members_synced_at        timestamptz
);

CREATE INDEX IF NOT EXISTS bot_guilds_installed_idx ON bot_guilds USING btree (is_installed) WHERE (is_installed = true);

CREATE TABLE IF NOT EXISTS bot_user_installs (
    discord_id          text PRIMARY KEY,
    username            text NOT NULL DEFAULT '',
    global_name         text,
    avatar_hash         text,
    scopes              text[] NOT NULL DEFAULT '{}'::text[],
    is_installed        boolean NOT NULL DEFAULT true,
    installed_at        timestamptz NOT NULL DEFAULT now(),
    left_at             timestamptz,
    last_seen_at        timestamptz,
    metadata_updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS bot_user_installs_installed_idx ON bot_user_installs USING btree (is_installed) WHERE (is_installed = true);

CREATE TABLE IF NOT EXISTS user_guilds (
    discord_id               text NOT NULL,
    guild_id                 text NOT NULL,
    is_active                boolean NOT NULL DEFAULT true,
    first_seen_at            timestamptz NOT NULL DEFAULT now(),
    last_seen_at             timestamptz NOT NULL DEFAULT now(),
    left_at                  timestamptz,
    name                     text NOT NULL DEFAULT '',
    icon_hash                text,
    approximate_member_count integer,
    is_owner                 boolean NOT NULL DEFAULT false,
    PRIMARY KEY (discord_id, guild_id)
);

CREATE INDEX IF NOT EXISTS user_guilds_guild_idx ON user_guilds USING btree (guild_id);

-- ---------------------------------------------------------------------------
-- Guild settings and coordination
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS guild_settings (
    guild_id                      text PRIMARY KEY,
    wipe_react_log_channel_id     text NOT NULL DEFAULT '',
    linking_log_channel_id        text NOT NULL DEFAULT '',
    tracked_server_log_channel_id text NOT NULL DEFAULT '',
    tracked_users_category_id     text NOT NULL DEFAULT '',
    admin_role_id                 text NOT NULL DEFAULT '',
    alert_role_id                 text NOT NULL DEFAULT '',
    created_at                    timestamptz NOT NULL DEFAULT now(),
    updated_at                    timestamptz NOT NULL DEFAULT now(),
    limited                       boolean NOT NULL DEFAULT false
);

-- ---------------------------------------------------------------------------
-- Tracked servers and everything that references them
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS tracked_servers (
    bm_id                   text PRIMARY KEY,
    name                    text NOT NULL,
    is_vitality             boolean NOT NULL DEFAULT false,
    is_atlas                boolean NOT NULL DEFAULT false,
    is_vital                boolean NOT NULL DEFAULT false,
    is_modded               boolean NOT NULL DEFAULT false,
    is_official             boolean NOT NULL DEFAULT false,
    is_build                boolean NOT NULL DEFAULT false,
    high_frequency          boolean NOT NULL DEFAULT false,
    last_polled_at          timestamptz,
    last_poll_error         text NOT NULL DEFAULT '',
    empty_player_poll_count integer NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS guild_tracked_servers (
    guild_id     text NOT NULL,
    server_bm_id text NOT NULL REFERENCES tracked_servers(bm_id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (guild_id, server_bm_id)
);

CREATE INDEX IF NOT EXISTS guild_tracked_servers_guild_idx ON guild_tracked_servers USING btree (guild_id);

CREATE TABLE IF NOT EXISTS player_states (
    server_bm_id  text NOT NULL REFERENCES tracked_servers(bm_id) ON DELETE CASCADE,
    player_bm_id  text NOT NULL,
    is_online     boolean NOT NULL DEFAULT false,
    online_since  timestamptz,
    offline_since timestamptz,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (server_bm_id, player_bm_id)
);

CREATE INDEX IF NOT EXISTS player_states_player_idx ON player_states USING btree (player_bm_id);
CREATE INDEX IF NOT EXISTS player_states_server_online_idx ON player_states USING btree (server_bm_id, is_online, online_since DESC, updated_at DESC);

CREATE TABLE IF NOT EXISTS server_player_count_history (
    server_bm_id   text NOT NULL REFERENCES tracked_servers(bm_id) ON DELETE CASCADE,
    timestamp_unix bigint NOT NULL,
    min            integer NOT NULL,
    value          double precision NOT NULL,
    max            integer NOT NULL,
    observed_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (server_bm_id, timestamp_unix)
);

CREATE INDEX IF NOT EXISTS server_player_count_history_observed_idx ON server_player_count_history USING btree (observed_at);
CREATE INDEX IF NOT EXISTS server_player_count_history_server_time_idx ON server_player_count_history USING btree (server_bm_id, timestamp_unix);

CREATE TABLE IF NOT EXISTS server_player_count_history_fetches (
    server_bm_id text PRIMARY KEY REFERENCES tracked_servers(bm_id) ON DELETE CASCADE,
    fetched_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS track_groups (
    id                       bigserial PRIMARY KEY,
    guild_id                 text NOT NULL,
    name                     text NOT NULL,
    server_bm_id             text NOT NULL REFERENCES tracked_servers(bm_id) ON DELETE CASCADE,
    channel_id               text NOT NULL DEFAULT '',
    message_id               text NOT NULL DEFAULT '',
    send_updates             boolean NOT NULL DEFAULT false,
    ping_alert_role_on_empty boolean NOT NULL DEFAULT false,
    online_count             integer NOT NULL DEFAULT 0,
    created_by               text NOT NULL DEFAULT '',
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    UNIQUE (guild_id, name)
);

CREATE INDEX IF NOT EXISTS track_groups_guild_idx ON track_groups USING btree (guild_id);

CREATE TABLE IF NOT EXISTS track_group_members (
    track_group_id bigint NOT NULL REFERENCES track_groups(id) ON DELETE CASCADE,
    steam_id       text NOT NULL,
    bm_id          text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (track_group_id, steam_id)
);

CREATE INDEX IF NOT EXISTS track_group_members_bm_idx ON track_group_members USING btree (bm_id);
CREATE INDEX IF NOT EXISTS track_group_members_group_idx ON track_group_members USING btree (track_group_id);

CREATE TABLE IF NOT EXISTS track_analytics_sessions_cache (
    player_bm_id text NOT NULL,
    server_bm_id text NOT NULL,
    sessions     jsonb NOT NULL DEFAULT '[]'::jsonb,
    fetched_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (player_bm_id, server_bm_id)
);

CREATE INDEX IF NOT EXISTS track_analytics_sessions_cache_fetched_idx ON track_analytics_sessions_cache USING btree (fetched_at);

-- ---------------------------------------------------------------------------
-- Steam accounts, caches, and Steam<->Discord links
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS steam_accounts (
    steam_id   text PRIMARY KEY,
    bm_id      text UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS steam_account_cache (
    steam_id            text PRIMARY KEY REFERENCES steam_accounts(steam_id) ON DELETE CASCADE,
    persona_name        text NOT NULL DEFAULT '',
    avatar_image        text NOT NULL DEFAULT '',
    rust_hours          bigint,
    total_games         integer,
    top_games           jsonb NOT NULL DEFAULT '[]'::jsonb,
    game_banned         boolean NOT NULL DEFAULT false,
    game_banned_at      timestamptz,
    game_ban_checked_at timestamptz,
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS steam_account_cache_updated_idx ON steam_account_cache USING btree (updated_at);

CREATE TABLE IF NOT EXISTS bm_profile_cache (
    bm_id             text PRIMARY KEY,
    bm_name           text NOT NULL DEFAULT '',
    first_seen_at     timestamptz,
    vitality_hours    bigint NOT NULL DEFAULT 0,
    atlas_hours       bigint NOT NULL DEFAULT 0,
    vital_hours       bigint NOT NULL DEFAULT 0,
    modded_hours      bigint NOT NULL DEFAULT 0,
    official_hours    bigint NOT NULL DEFAULT 0,
    build_hours       bigint NOT NULL DEFAULT 0,
    bm_hours          bigint NOT NULL DEFAULT 0,
    bm_servers_played integer NOT NULL DEFAULT 0,
    top_servers       jsonb NOT NULL DEFAULT '[]'::jsonb,
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS bm_profile_cache_updated_idx ON bm_profile_cache USING btree (updated_at);

CREATE TABLE IF NOT EXISTS bm_name_history (
    bm_id        text NOT NULL,
    name         text NOT NULL,
    last_seen_at timestamptz NOT NULL,
    observed_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (bm_id, name, last_seen_at)
);

CREATE INDEX IF NOT EXISTS bm_name_history_bm_seen_idx ON bm_name_history USING btree (bm_id, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS steam_discord_links (
    steam_id   text NOT NULL REFERENCES steam_accounts(steam_id),
    discord_id text NOT NULL,
    type       text NOT NULL,
    source     text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (steam_id, discord_id, type),
    CONSTRAINT steam_discord_links_type_check CHECK ((type = ANY (ARRAY['soft'::text, 'hard'::text])))
);

CREATE INDEX IF NOT EXISTS steam_discord_links_discord_idx ON steam_discord_links USING btree (discord_id);

CREATE TABLE IF NOT EXISTS guild_steam_discord_links (
    guild_id   text NOT NULL,
    discord_id text NOT NULL,
    steam_id   text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (guild_id, discord_id, steam_id)
);

CREATE INDEX IF NOT EXISTS guild_steam_discord_links_guild_discord_idx ON guild_steam_discord_links USING btree (guild_id, discord_id);
CREATE INDEX IF NOT EXISTS guild_steam_discord_links_guild_steam_idx ON guild_steam_discord_links USING btree (guild_id, steam_id);

CREATE TABLE IF NOT EXISTS hidden_discord_link_identities (
    discord_id text PRIMARY KEY,
    reason     text NOT NULL DEFAULT '',
    created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Platform sync, outbound proxies, user bans
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS platform_sync_state (
    platform              text NOT NULL,
    platform_id           text NOT NULL,
    last_platform_sync_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (platform, platform_id),
    CONSTRAINT platform_sync_state_platform_check CHECK ((platform = ANY (ARRAY['discord'::text, 'steam'::text])))
);

CREATE TABLE IF NOT EXISTS outbound_proxies (
    id                  bigserial PRIMARY KEY,
    redacted_endpoint   text NOT NULL,
    endpoint_ciphertext bytea NOT NULL,
    endpoint_nonce      bytea NOT NULL,
    key_id              text NOT NULL DEFAULT 'default',
    enabled             boolean NOT NULL DEFAULT true,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    auto_disabled_at    timestamptz,
    CONSTRAINT outbound_proxies_ciphertext_nonempty CHECK (((octet_length(endpoint_ciphertext) > 0) AND (octet_length(endpoint_nonce) > 0)))
);

CREATE INDEX IF NOT EXISTS outbound_proxies_enabled_idx ON outbound_proxies USING btree (enabled, id);
CREATE UNIQUE INDEX IF NOT EXISTS outbound_proxies_redacted_endpoint_idx ON outbound_proxies USING btree (redacted_endpoint);

CREATE TABLE IF NOT EXISTS user_bans (
    discord_id text PRIMARY KEY,
    categories text[] NOT NULL DEFAULT '{}'::text[],
    updated_by text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Wipe reacts and schedules
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS wipe_reacts (
    message_id          text PRIMARY KEY,
    guild_id            text NOT NULL,
    channel_id          text NOT NULL,
    creator_id          text NOT NULL,
    name                text NOT NULL,
    description         text NOT NULL DEFAULT '',
    time_zone           text NOT NULL,
    scheduled_unix      bigint NOT NULL,
    ask_late_reason     boolean NOT NULL DEFAULT false,
    ask_declined_reason boolean NOT NULL DEFAULT false,
    accepted_user_ids   jsonb NOT NULL DEFAULT '[]'::jsonb,
    declined_user_ids   jsonb NOT NULL DEFAULT '[]'::jsonb,
    late_user_ids       jsonb NOT NULL DEFAULT '[]'::jsonb,
    embed_timestamp     text NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS wipe_reacts_guild_scheduled_idx ON wipe_reacts USING btree (guild_id, scheduled_unix DESC);

CREATE TABLE IF NOT EXISTS wipe_react_schedules (
    id                   bigserial PRIMARY KEY,
    guild_id             text NOT NULL,
    days                 jsonb NOT NULL DEFAULT '[]'::jsonb,
    includes_force_wipe  boolean NOT NULL DEFAULT false,
    "time"               text NOT NULL DEFAULT '',
    time_zone            text NOT NULL,
    repeat               text NOT NULL DEFAULT '',
    post_before          text NOT NULL,
    name                 text NOT NULL DEFAULT '',
    description          text NOT NULL DEFAULT '',
    ask_late_reason      boolean NOT NULL DEFAULT false,
    ask_declined_reason  boolean NOT NULL DEFAULT false,
    channel_id           text NOT NULL DEFAULT '',
    last_posted_for_unix bigint NOT NULL DEFAULT 0,
    created_by           text NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS wipe_react_schedules_guild_idx ON wipe_react_schedules USING btree (guild_id);

CREATE TABLE IF NOT EXISTS wipe_react_schedule_posts (
    id                bigserial PRIMARY KEY,
    schedule_id       bigint NOT NULL REFERENCES wipe_react_schedules(id) ON DELETE CASCADE,
    guild_id          text NOT NULL,
    channel_id        text NOT NULL,
    wipe_unix         bigint NOT NULL,
    post_unix         bigint NOT NULL,
    claimed_by        text NOT NULL DEFAULT '',
    claimed_at        timestamptz,
    message_id        text NOT NULL DEFAULT '',
    posted_channel_id text NOT NULL DEFAULT '',
    completed_at      timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (schedule_id, wipe_unix)
);

CREATE INDEX IF NOT EXISTS wipe_react_schedule_posts_claim_idx ON wipe_react_schedule_posts USING btree (guild_id, completed_at, post_unix, claimed_at);

-- ---------------------------------------------------------------------------
-- Cached counts (idempotent runtime seed the Go backend performs)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS cached_counts (
    name       text PRIMARY KEY,
    value      bigint NOT NULL,
    updated_at timestamptz NOT NULL
);

INSERT INTO cached_counts (name, value, updated_at)
VALUES ('steam_accounts', 0, 'epoch')
ON CONFLICT (name) DO NOTHING;
