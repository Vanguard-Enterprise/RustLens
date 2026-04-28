# Backend Agent Instructions

This file provides backend-specific guidance. It supplements the root `AGENTS.md`.

## Stack

- **Runtime**: Go 1.24
- **Router**: chi v5
- **Database**: pgx v5 (Postgres 16)
- **Cache**: go-redis v9 (Redis 7)
- **Auth**: Self-signed JWT (`BOT_SERVICE_JWT_SECRET`)

## Database Schema

Auto-migrated at startup via `store.EnsureSchema()` (no migration tool). All IDs stored as `TEXT` (Discord snowflakes).

### `bot_worker_events`

Append-only log of shard worker lifecycle events.

| Column | Type | Notes |
|---|---|---|
| `id` | `BIGSERIAL` | PK |
| `worker_id` | `TEXT NOT NULL` | |
| `event_type` | `TEXT NOT NULL` | |
| `payload` | `JSONB NOT NULL DEFAULT '{}'` | |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

### `guild_settings`

Per-guild bot settings for logs, the tracked-users category, and command authorization.

| Column | Type | Notes |
|---|---|---|
| `guild_id` | `TEXT` | PK |
| `wipe_react_log_channel_id` | `TEXT NOT NULL DEFAULT ''` | Channel snowflake |
| `linking_log_channel_id` | `TEXT NOT NULL DEFAULT ''` | Channel snowflake |
| `tracked_server_log_channel_id` | `TEXT NOT NULL DEFAULT ''` | Channel snowflake |
| `tracked_users_category_id` | `TEXT NOT NULL DEFAULT ''` | Discord category snowflake — parent for /track group channels. Replaces the legacy `alerts_channel_id` column, which is dropped on migration. |
| `admin_role_id` | `TEXT NOT NULL DEFAULT ''` | Role snowflake |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |
| `updated_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

### `wipe_reacts`

Persisted wipe-react events with reaction user lists.

| Column | Type | Notes |
|---|---|---|
| `message_id` | `TEXT` | PK (Discord message snowflake) |
| `guild_id` | `TEXT NOT NULL` | |
| `channel_id` | `TEXT NOT NULL` | |
| `creator_id` | `TEXT NOT NULL` | |
| `name` | `TEXT NOT NULL` | |
| `description` | `TEXT NOT NULL DEFAULT ''` | |
| `time_zone` | `TEXT NOT NULL` | IANA timezone |
| `scheduled_unix` | `BIGINT NOT NULL` | Epoch seconds |
| `ask_late_reason` | `BOOLEAN NOT NULL DEFAULT FALSE` | |
| `ask_declined_reason` | `BOOLEAN NOT NULL DEFAULT FALSE` | |
| `accepted_user_ids` | `JSONB NOT NULL DEFAULT '[]'` | String array |
| `declined_user_ids` | `JSONB NOT NULL DEFAULT '[]'` | String array |
| `late_user_ids` | `JSONB NOT NULL DEFAULT '[]'` | String array |
| `embed_timestamp` | `TEXT NOT NULL` | ISO 8601 for embed display |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |
| `updated_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

**Index**: `wipe_reacts_guild_scheduled_idx ON (guild_id, scheduled_unix DESC)` — used by `ListActiveWipeReacts`.

### `wipe_react_schedules`

Recurring wipe-react schedule configs (max 3 per guild).

| Column | Type | Notes |
|---|---|---|
| `id` | `BIGSERIAL` | PK |
| `guild_id` | `TEXT NOT NULL` | |
| `days` | `JSONB NOT NULL DEFAULT '[]'` | String array of weekday values |
| `includes_force_wipe` | `BOOLEAN NOT NULL DEFAULT FALSE` | First Thursday of month, 19:00 Europe/London |
| `time` | `TEXT NOT NULL DEFAULT ''` | HH:MM, empty if force-wipe-only |
| `time_zone` | `TEXT NOT NULL` | IANA timezone |
| `repeat` | `TEXT NOT NULL DEFAULT ''` | weekly/biweekly/monthly, empty if force-wipe-only |
| `post_before` | `TEXT NOT NULL` | 1d/2d/3d/4d/1w/2w |
| `created_by` | `TEXT NOT NULL` | User snowflake |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |
| `updated_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

**Index**: `wipe_react_schedules_guild_idx ON (guild_id)`.

**Limit**: 3 schedules per guild, enforced in `CreateWipeReactSchedule` via `SELECT COUNT(*)` before insert.

### `wipe_react_schedule_posts`

Backend-owned queue of due wipe-react schedule posts waiting for a bot shard to claim and send in Discord.

| Column | Type | Notes |
|---|---|---|
| `id` | `BIGSERIAL` | PK |
| `schedule_id` | `BIGINT NOT NULL` | FK to `wipe_react_schedules.id` |
| `guild_id` | `TEXT NOT NULL` | Discord guild snowflake |
| `channel_id` | `TEXT NOT NULL` | Target Discord channel |
| `wipe_unix` | `BIGINT NOT NULL` | Scheduled wipe-react time |
| `post_unix` | `BIGINT NOT NULL` | When the post becomes due |
| `claimed_by` | `TEXT NOT NULL DEFAULT ''` | Bot worker ID that claimed the job |
| `claimed_at` | `TIMESTAMPTZ` | Claim timestamp, nullable |
| `message_id` | `TEXT NOT NULL DEFAULT ''` | Discord message ID after successful send |
| `posted_channel_id` | `TEXT NOT NULL DEFAULT ''` | Discord channel ID after successful send |
| `completed_at` | `TIMESTAMPTZ` | Completion timestamp, nullable |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

**Indexes**:
- `wipe_react_schedule_posts_claim_idx ON (guild_id, completed_at, post_unix, claimed_at)`
- `UNIQUE (schedule_id, wipe_unix)` to prevent duplicate queue entries for the same occurrence.

### `steam_accounts`

Known Steam player accounts with optional BattleMetrics ID.

| Column | Type | Notes |
|---|---|---|
| `steam_id` | `TEXT` | PK (17-digit Steam ID) |
| `bm_id` | `TEXT UNIQUE` | Nullable, BattleMetrics player ID |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

### `steam_account_cache`

Volatile Steam/BattleMetrics profile data cached for lazy `/player` and `/view user` profile rendering. The profile route treats rows with `updated_at` newer than 24 hours as fresh and skips Steam/BattleMetrics API calls; if refresh fails for a stale row, the stale row is returned without changing `updated_at`. The same table also stores Steam game-ban cache state with a separate `game_ban_checked_at` timestamp used by the guild game-ban scan endpoint.

| Column | Type | Notes |
|---|---|---|
| `steam_id` | `TEXT` | PK, FK to `steam_accounts.steam_id` |
| `bm_id` | `TEXT NOT NULL DEFAULT ''` | BattleMetrics player ID used when the cache was built |
| `bm_name` | `TEXT NOT NULL DEFAULT ''` | Latest cached BattleMetrics display name |
| `avatar_image` | `TEXT NOT NULL DEFAULT ''` | Latest cached Steam avatar URL |
| `first_seen_at` | `TIMESTAMPTZ` | Cached BattleMetrics player creation / first-seen time |
| `vitality_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours for tracked Vitality servers |
| `atlas_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours for tracked Atlas servers |
| `vital_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours for tracked Vital servers |
| `modded_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours for tracked Modded servers |
| `official_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours for tracked Official servers |
| `build_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours for tracked Build servers |
| `bm_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours across all BattleMetrics-returned servers |
| `rust_hours` | `BIGINT` | Cached Steam Rust playtime, nullable when unavailable/private |
| `game_banned` | `BOOLEAN NOT NULL DEFAULT FALSE` | Cached whether Steam reported any game bans |
| `game_banned_at` | `TIMESTAMPTZ` | Approximate game-ban timestamp derived from `DaysSinceLastBan`, null when no game ban exists |
| `game_ban_checked_at` | `TIMESTAMPTZ` | Last Steam game-ban refresh timestamp used for the 6-hour TTL |
| `updated_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | Cache refresh timestamp |

### `tracked_servers`

Global BattleMetrics servers the bot should track.

| Column | Type | Notes |
|---|---|---|
| `bm_id` | `TEXT` | PK |
| `name` | `TEXT NOT NULL` | Display name |
| `is_vitality` | `BOOLEAN NOT NULL DEFAULT FALSE` | Global category flag |
| `is_atlas` | `BOOLEAN NOT NULL DEFAULT FALSE` | Global category flag |
| `is_vital` | `BOOLEAN NOT NULL DEFAULT FALSE` | Global category flag |
| `is_modded` | `BOOLEAN NOT NULL DEFAULT FALSE` | Global category flag |
| `is_official` | `BOOLEAN NOT NULL DEFAULT FALSE` | Global category flag |
| `is_build` | `BOOLEAN NOT NULL DEFAULT FALSE` | Global category flag |
| `last_polled_at` | `TIMESTAMPTZ` | Nullable, last successful or failed poll attempt time |
| `last_poll_error` | `TEXT NOT NULL DEFAULT ''` | Most recent poll error, empty on success |

### `outbound_proxies`

Owner-managed outbound proxy endpoints for proxied BattleMetrics requests. Full URLs are encrypted with AES-GCM using `PROXY_ENCRYPTION_KEY`; only credential-free endpoints are returned or logged.

| Column | Type | Notes |
|---|---|---|
| `id` | `BIGSERIAL` | PK |
| `redacted_endpoint` | `TEXT NOT NULL` | `scheme://host:port`, unique display/removal value |
| `endpoint_ciphertext` | `BYTEA NOT NULL` | Encrypted full proxy URL including username/password |
| `endpoint_nonce` | `BYTEA NOT NULL` | AES-GCM nonce |
| `key_id` | `TEXT NOT NULL DEFAULT 'default'` | Key identifier for future rotation |
| `enabled` | `BOOLEAN NOT NULL DEFAULT TRUE` | Included in runtime proxy pool when true |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |
| `updated_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

**Indexes**:
- `outbound_proxies_enabled_idx ON (enabled, id)`
- `outbound_proxies_redacted_endpoint_idx ON (redacted_endpoint)` unique

### `guild_tracked_servers`

Guild-specific selection of servers from the global `tracked_servers` list.

| Column | Type | Notes |
|---|---|---|
| `guild_id` | `TEXT NOT NULL` | Discord guild snowflake |
| `server_bm_id` | `TEXT NOT NULL` | FK to `tracked_servers.bm_id` |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

**PK**: `(guild_id, server_bm_id)`.

**Index**:
- `guild_tracked_servers_guild_idx ON (guild_id)` — used to load one guild's selected tracked servers.

### `player_states`

BattleMetrics presence state per tracked server and player.

| Column | Type | Notes |
|---|---|---|
| `server_bm_id` | `TEXT NOT NULL` | FK to `tracked_servers.bm_id` |
| `player_bm_id` | `TEXT NOT NULL` | BattleMetrics player ID |
| `is_online` | `BOOLEAN NOT NULL DEFAULT FALSE` | Current presence state for this server |
| `online_since` | `TIMESTAMPTZ` | Start of current online streak |
| `offline_since` | `TIMESTAMPTZ` | Start of current offline streak |
| `updated_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | Last sync update time |

**PK**: `(server_bm_id, player_bm_id)`.

**Indexes**:
- `player_states_player_idx ON (player_bm_id)` — reverse lookup by BattleMetrics player.
- `player_states_server_online_idx ON (server_bm_id, is_online, online_since DESC, updated_at DESC)` — current online players for a tracked server.

### Guild Online Summary

`online_summary.go` joins `guild_steam_discord_links`, `guild_tracked_servers`, `steam_accounts`, `steam_account_cache`, `player_states`, and `tracked_servers` to produce the guild online summary used by the bot's `/online` command. The query only returns rows where the linked Steam account has a non-null `bm_id`, the matching player state is currently online, and the server is selected for that guild.

### `steam_discord_links`

Maps Steam accounts to Discord users. A pair can have both soft and hard links.

| Column | Type | Notes |
|---|---|---|
| `steam_id` | `TEXT NOT NULL` | FK to `steam_accounts.steam_id` |
| `discord_id` | `TEXT NOT NULL` | Discord snowflake |
| `type` | `TEXT NOT NULL` | `soft` or `hard` (CHECK constraint) |
| `source` | `TEXT NOT NULL DEFAULT ''` | Origin: `bot_command`, `frontend`, `battlemetrics` |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

**PK**: `(steam_id, discord_id, type)` — allows both link types per pair.

**Index**: `steam_discord_links_discord_idx ON (discord_id)` — reverse lookups by Discord user.

**Link types:**
- **soft** — user self-reported, unverified. Admins can delete.
- **hard** — from trusted source (BattleMetrics), authoritatively verified. Cannot be deleted via API.

### `track_analytics_sessions_cache`

Per-(player, server) cache of raw BattleMetrics player session windows used by the track-group analytics endpoint. Entries are refreshed lazily by `GET /api/internal/bot/setup/guilds/{guildID}/track-groups/{groupID}/analytics` after a 24-hour TTL.

| Column | Type | Notes |
|---|---|---|
| `player_bm_id` | `TEXT NOT NULL` | BattleMetrics player ID (composite PK) |
| `server_bm_id` | `TEXT NOT NULL` | BattleMetrics server ID (composite PK) |
| `sessions` | `JSONB NOT NULL DEFAULT '[]'::jsonb` | Array of `{start, stop}` unix-second pairs |
| `fetched_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | Last refresh time, drives the 24-hour TTL |

**PK**: `(player_bm_id, server_bm_id)`.

**Index**: `track_analytics_sessions_cache_fetched_idx ON (fetched_at)` for future TTL sweeps.

### `guild_steam_discord_links`

Guild-scoped Steam-Discord associations managed by guild admins.

| Column | Type | Notes |
|---|---|---|
| `guild_id` | `TEXT NOT NULL` | Discord guild snowflake |
| `discord_id` | `TEXT NOT NULL` | Discord user snowflake |
| `steam_id` | `TEXT NOT NULL` | 17-digit Steam ID |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

**PK**: `(guild_id, discord_id, steam_id)`.

**Indexes**:
- `guild_steam_discord_links_guild_discord_idx ON (guild_id, discord_id)` — lookup by guild + Discord user.
- `guild_steam_discord_links_guild_steam_idx ON (guild_id, steam_id)` — lookup by guild + Steam account.

## OpenAPI

The OpenAPI spec is built programmatically in `internal/http/openapi.go` as a Go map literal. **When adding, modifying, or removing any HTTP endpoint, always update the OpenAPI document to match.** This includes adding path entries, request/response schemas, and keeping parameter descriptions accurate. The spec is served at `/api/docs/openapi.json` and powers the interactive docs at `/api/docs`.

## Conventions

- Parameterized queries only (`$1`, `$2`, ...) — no string interpolation
- `EnsureSchema()` uses `CREATE TABLE IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS` — idempotent
- Handlers validate all snowflake inputs via `snowflakePattern` regex before touching the DB
- Sentinel errors (e.g. `ErrWipeReactNotFound`) mapped to HTTP 404 in handlers
- Reaction mutations use `SELECT ... FOR UPDATE` row locking to prevent races
- `cmd/server/main.go` imports `github.com/joho/godotenv/autoload`, so local backend runs automatically read `apps/backend/.env` before config loading.
- The BattleMetrics poller queries `GET https://api.battlemetrics.com/servers/{server_id}?include=player` on a rolling interval, authenticates with `Authorization: Bearer <BATTLEMETRICS_API_KEY>`, and updates `player_states` inside a single store transaction per server.
- Steam profile resolution lives in `internal/steamapi/`. The bot uses `POST /api/internal/bot/setup/steam/resolve` to turn a raw 17-digit Steam ID or a supported `steamcommunity.com/profiles/...` / `steamcommunity.com/id/...` URL into a canonical Steam ID. Vanity URLs are resolved live through `STEAM_API_KEY`, are not cached separately, and always go direct without using the backend proxy pool.
- Bot-internal account lookup routes under `/api/internal/bot/setup/steam/` can backfill missing `steam_accounts` rows through direct, authorized BattleMetrics requests with `BATTLEMETRICS_API_KEY`. These fallback requests are sent directly and are never proxied.
- Bot-internal `GET /api/internal/bot/setup/steam-accounts/count` returns the current `steam_accounts` row count for bot presence updates.
- Steam game-ban scans use `GET /api/internal/bot/setup/guilds/{guildID}/game-bans`, which batches guild-linked Steam IDs through `ISteamUser/GetPlayerBans`, refreshes stale cache rows after 6 hours, ignores VAC-only bans, and returns only game bans newer than 90 days.
- Outbound BattleMetrics HTTP requests prefer enabled `outbound_proxies` rows managed by owner-guild-only `/proxy add|list|remove`; DB proxy URLs are encrypted at rest with `PROXY_ENCRYPTION_KEY`, are live-reloaded after add/delete, and request logs only include credential-free `scheme://host:port` endpoints. If no DB proxies exist, legacy `PROXY_ENABLED=true` + comma-separated `PROXY_ENDPOINTS` is still used as a fallback. Steam API requests are always sent directly.
- The bot-internal steam profile route lazily refreshes `steam_account_cache` after a 24-hour TTL. Fresh cache rows skip Steam/BattleMetrics API calls; stale rows are returned without changing `updated_at` if refresh fails. Steam requests are sent directly. The response includes optional current/last tracked-server status from `player_states` joined to `tracked_servers`.
- The bot-internal steam analytics route shares the same `steam_account_cache.updated_at` TTL and refresh path as the bot-internal steam profile route. Fresh cache rows skip Steam/BattleMetrics API calls for both routes; stale cache rows are returned if refresh fails. Name-gap data is shared between the profile risk calculation and analytics gap display. If Steam summary reports `communityvisibilitystate = 1`, RustLens skips `GetOwnedGames` and leaves game totals/playtime private.

## Commands

```bash
cd apps/backend && go test ./...          # Run all tests
cd apps/backend && go vet ./...           # Lint
cd apps/backend && go build ./cmd/server  # Build
```
