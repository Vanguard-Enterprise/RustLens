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

Per-guild bot settings for logs, the tracked-users category, command authorization, and alert pings.

| Column | Type | Notes |
|---|---|---|
| `guild_id` | `TEXT` | PK |
| `wipe_react_log_channel_id` | `TEXT NOT NULL DEFAULT ''` | Channel snowflake |
| `linking_log_channel_id` | `TEXT NOT NULL DEFAULT ''` | Channel snowflake |
| `tracked_server_log_channel_id` | `TEXT NOT NULL DEFAULT ''` | Channel snowflake |
| `tracked_users_category_id` | `TEXT NOT NULL DEFAULT ''` | Discord category snowflake — parent for /track group channels. Replaces the legacy `alerts_channel_id` column, which is dropped on migration. |
| `admin_role_id` | `TEXT NOT NULL DEFAULT ''` | Role snowflake |
| `alert_role_id` | `TEXT NOT NULL DEFAULT ''` | Role snowflake pinged by enabled alert notifications. |
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

Steam-keyed volatile profile data — only fields that require a Steam ID live here. BattleMetrics-derived data has moved to `bm_profile_cache` (keyed by `bm_id`). The profile route treats rows with `updated_at` newer than 24 hours as fresh and skips Steam API calls; if refresh fails for a stale row, the stale row is returned without changing `updated_at`. The same table also stores Steam game-ban cache state with a separate `game_ban_checked_at` timestamp used by the guild game-ban scan endpoint.

| Column | Type | Notes |
|---|---|---|
| `steam_id` | `TEXT` | PK, FK to `steam_accounts.steam_id` |
| `avatar_image` | `TEXT NOT NULL DEFAULT ''` | Latest cached Steam avatar URL |
| `rust_hours` | `BIGINT` | Cached Steam Rust playtime, nullable when unavailable/private |
| `total_games` | `INTEGER` | Cached total owned games on Steam, nullable when private |
| `top_games` | `JSONB NOT NULL DEFAULT '[]'::jsonb` | Top 5 owned Steam games by playtime |
| `game_banned` | `BOOLEAN NOT NULL DEFAULT FALSE` | Cached whether Steam reported any game bans |
| `game_banned_at` | `TIMESTAMPTZ` | Approximate game-ban timestamp derived from `DaysSinceLastBan`, null when no game ban exists |
| `game_ban_checked_at` | `TIMESTAMPTZ` | Last Steam game-ban refresh timestamp used for the 6-hour TTL |
| `updated_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | Cache refresh timestamp |

### `bm_profile_cache`

BattleMetrics-keyed volatile profile data, keyed by BM player ID. Lets BM-only `/player` lookups (no Steam linkage) reuse cached BM data instead of always hitting BattleMetrics, and lets background pollers update cached BM names by `bm_id` without requiring a Steam linkage.

| Column | Type | Notes |
|---|---|---|
| `bm_id` | `TEXT` | PK |
| `bm_name` | `TEXT NOT NULL DEFAULT ''` | Latest cached BattleMetrics display name |
| `first_seen_at` | `TIMESTAMPTZ` | Cached BattleMetrics player creation / first-seen time |
| `vitality_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours for tracked Vitality servers |
| `atlas_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours for tracked Atlas servers |
| `vital_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours for tracked Vital servers |
| `modded_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours for tracked Modded servers |
| `official_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours for tracked Official servers |
| `build_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours for tracked Build servers |
| `bm_hours` | `BIGINT NOT NULL DEFAULT 0` | Cached whole hours across all BattleMetrics-returned servers |
| `bm_servers_played` | `INTEGER NOT NULL DEFAULT 0` | Distinct BattleMetrics servers played |
| `top_servers` | `JSONB NOT NULL DEFAULT '[]'::jsonb` | Top 5 BattleMetrics servers by playtime |
| `updated_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | Cache refresh timestamp |

### `bm_name_history`

Raw BattleMetrics name identifier history keyed by BM player ID. Fresh BM profile fetches merge returned `{name, lastSeen}` entries into this table. Bot-facing `nameGaps` / analytics `gaps` are computed from these raw rows at response time instead of being cached as derived JSON.

| Column | Type | Notes |
|---|---|---|
| `bm_id` | `TEXT NOT NULL` | BattleMetrics player ID |
| `name` | `TEXT NOT NULL` | BattleMetrics name identifier |
| `last_seen_at` | `TIMESTAMPTZ NOT NULL` | Timestamp returned by BattleMetrics for that name identifier |
| `observed_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | Last time RustLens saw this entry in a BM profile response |

**PK**: `(bm_id, name, last_seen_at)`.

**Index**: `bm_name_history_bm_seen_idx ON (bm_id, last_seen_at DESC)`.

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
| `high_frequency` | `BOOLEAN NOT NULL DEFAULT FALSE` | Poller subset flag; high-frequency servers are selected for three poll ticks before one low-frequency tick. |
| `last_polled_at` | `TIMESTAMPTZ` | Nullable, last successful or failed poll attempt time |
| `last_poll_error` | `TEXT NOT NULL DEFAULT ''` | Most recent poll error, empty on success |
| `empty_player_poll_count` | `INTEGER NOT NULL DEFAULT 0` | Consecutive successful zero-player BattleMetrics polls while the server still has online `player_states`; the poller waits for 3 before marking everyone offline. |

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

`online_summary.go` joins `guild_steam_discord_links`, `steam_accounts`, `bm_profile_cache`, `player_states`, and bot-wide `tracked_servers` to produce the guild online summary used by the bot's `/online` command. The query only returns rows where the linked Steam account has a non-null `bm_id`, the matching `player_states` row is currently online, and the server is globally tracked by the bot. It intentionally does not filter by `guild_tracked_servers`.

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

### `hidden_discord_link_identities`

Read-time suppression flags for Discord identities whose Steam/Discord links should not be disclosed.

| Column | Type | Notes |
|---|---|---|
| `discord_id` | `TEXT` | PK, Discord snowflake to suppress from link lookups |
| `reason` | `TEXT NOT NULL DEFAULT ''` | Optional operator note |
| `created_by` | `TEXT NOT NULL DEFAULT ''` | Optional operator identifier |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |
| `updated_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

Global and guild link list queries omit hidden Discord IDs in both Discord→Steam and Steam→Discord directions. The underlying `steam_discord_links` and `guild_steam_discord_links` rows are not deleted so dedupe, future unhide, and internal integrity are preserved.

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

### `server_player_count_history`

Per-server historical store of raw BattleMetrics daily player-count history used by `/analyze server`. BattleMetrics only returns up to 90 days per request, so the backend appends/upserts daily points over time and can compute distributions from more than 90 days once enough data has accumulated.

| Column | Type | Notes |
|---|---|---|
| `server_bm_id` | `TEXT NOT NULL` | FK to `tracked_servers.bm_id` |
| `timestamp_unix` | `BIGINT NOT NULL` | Daily data-point timestamp from BattleMetrics |
| `min` | `INTEGER NOT NULL` | Daily minimum player count |
| `value` | `DOUBLE PRECISION NOT NULL` | Daily average/value player count |
| `max` | `INTEGER NOT NULL` | Daily maximum player count |
| `observed_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | Last time RustLens observed this exact daily point |

**PK**: `(server_bm_id, timestamp_unix)`.

**Indexes**:
- `server_player_count_history_server_time_idx ON (server_bm_id, timestamp_unix)`
- `server_player_count_history_observed_idx ON (observed_at)`

### `server_player_count_history_fetches`

Per-server refresh marker for `/analyze server`. If a server has already been fetched during the current UTC day, the handler serves stored history without calling BattleMetrics; otherwise it fetches the latest 90-day window and upserts those points into `server_player_count_history`.

| Column | Type | Notes |
|---|---|---|
| `server_bm_id` | `TEXT` | PK, FK to `tracked_servers.bm_id` |
| `fetched_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | Last BattleMetrics refresh attempt that updated the local history |

### `platform_sync_state`

Tracks the last time RustLens queried the external `platformsync.io` service for a given Steam or Discord ID. Used to enforce the 14-day cool-down on outbound platformsync calls.

| Column | Type | Notes |
|---|---|---|
| `platform` | `TEXT NOT NULL` | `steam` or `discord` (CHECK constraint) |
| `platform_id` | `TEXT NOT NULL` | Steam ID (17 digits) or Discord snowflake |
| `last_platform_sync_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | Last upstream call timestamp |

**PK**: `(platform, platform_id)`. Only the queried side is upserted, so a later sync of a discovered counterpart still reaches the upstream.

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

## Background services

`cmd/server/main.go` starts three long-running goroutines, each cancelled by the root `ctx` on SIGINT/SIGTERM:

- `battlemetrics.Poller` — refreshes `player_states` from BattleMetrics on a rolling interval.
- `wipereactscheduler.Poller` — enqueues due wipe-react schedule posts into `wipe_react_schedule_posts` for the bot to claim.
- `cleanup.Service` — periodic retention sweep (see below).

### `cleanup.Service`

Hourly background sweep that drops stale rows from volatile tables. Conservative by design: long-lived product data (user accounts, links, audit events, tracked servers, schedules, persisted settings) is **never** touched here.

Defaults (overridable via `cleanup.Config`):

| Sweep | Retention | Tables |
|---|---|---|
| Wipe reacts | 7 days past `scheduled_unix` | `wipe_reacts` |
| Track-analytics session cache | 48 hours since `fetched_at` | `track_analytics_sessions_cache` |
| Auth tokens | consumed, OR expired >24h ago | `email_sign_in_tokens`, `password_reset_tokens`, `email_verification_tokens` |
| Tick interval | 1 hour | — |

Wipe reacts are kept for a week after they pass so `/compare` and `/wipereact missing` can still operate on recent history. The analytics cache window matches the 24h freshness rule used by the analytics handler with a 24h buffer. Auth-token cleanup is safe because consumed tokens cannot be reused regardless of expiry, and the FK `ON DELETE CASCADE` from the parent tables already ensures we never orphan user records.

Failures of any single step are logged and do not abort the others; the service never crashes the server.

Cleanup SQL lives in `internal/store/cleanup.go`; the goroutine lives in `internal/cleanup/service.go`.

## OpenAPI

The OpenAPI spec is built programmatically in `internal/http/openapi.go` as a Go map literal. **When adding, modifying, or removing any HTTP endpoint, always update the OpenAPI document to match.** This includes adding path entries, request/response schemas, and keeping parameter descriptions accurate. The spec is served at `/api/docs/openapi.json` and powers the interactive docs at `/api/docs`.

## Conventions

- Parameterized queries only (`$1`, `$2`, ...) — no string interpolation
- `EnsureSchema()` uses `CREATE TABLE IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS` — idempotent
- Handlers validate all snowflake inputs via `snowflakePattern` regex before touching the DB
- Sentinel errors (e.g. `ErrWipeReactNotFound`) mapped to HTTP 404 in handlers
- Reaction mutations use `SELECT ... FOR UPDATE` row locking to prevent races
- `cmd/server/main.go` imports `github.com/joho/godotenv/autoload`, so local backend runs automatically read `apps/backend/.env` before config loading.
- The BattleMetrics poller queries `GET https://api.battlemetrics.com/servers/{server_id}?include=player` on a rolling interval (unauthenticated, through the proxy pool), selecting three `tracked_servers.high_frequency=true` servers followed by one low-frequency server when both subsets exist, and updates `player_states` inside a single store transaction per server. If BattleMetrics returns a successful zero-player response while that server still has online rows, the poller skips the mass-offline update until the third consecutive empty poll; any non-empty poll resets the counter and syncs normally.
- Steam profile resolution lives in `internal/steamapi/`. The bot uses `POST /api/internal/bot/setup/steam/resolve` to turn a raw 17-digit Steam ID or a supported `steamcommunity.com/profiles/...` / `steamcommunity.com/id/...` URL into a canonical Steam ID. Vanity URLs are resolved live through `STEAM_API_KEY`, are not cached separately, and always go direct without using the backend proxy pool.
- Steam ↔ BattleMetrics matching is delegated to an external service in `internal/bmmatch/` configured via `BM_MATCH_URL` + `BM_MATCH_TOKEN`. It exposes `GET /steam-to-bm/{steam_id}` and `GET /bm-to-steam/{bm_id}`; both return a single-string-value JSON object, are sent **directly** (never proxied), and authenticate with a static Bearer token. When unconfigured, backfill resolvers return `ErrSteamAccountNotFound` instead of failing.
- Platform-identity sync is delegated to `internal/platformsync/`, a thin client for the external `https://link.platformsync.io/api.php` service (URL is hard-coded; only `PLATFORMSYNC_TOKEN` is configurable). Requests are sent **directly** (never proxied). The handler at `POST /api/internal/bot/setup/platform-sync/{platform}/{id}` enforces a 14-day cool-down via `platform_sync_state` and bulk-upserts every discovered `(steam_id, discord_id)` pair (including `previous_*_ids`) into `steam_discord_links` as hard links with `source='platformsync'`. Only the queried side is recorded in `platform_sync_state` so future syncs of discovered counterparts still hit the upstream.
- `PUT /api/internal/bot/setup/discord/{discordID}/link-visibility-hidden` and `DELETE /api/internal/bot/setup/discord/{discordID}/link-visibility-hidden` manage read-time hiding for Discord link identities.
- Bot-internal account lookup routes under `/api/internal/bot/setup/steam/` backfill missing `steam_accounts` rows by combining `bmmatch` for the Steam↔BM ID translation with the proxied, unauthenticated BattleMetrics `/players/{id}` for the rest of the profile (BM name, created-at, servers, hours).
- Bot-internal `GET /api/internal/bot/setup/steam-accounts/count` returns the current `steam_accounts` row count for bot presence updates.
- Steam game-ban scans use `GET /api/internal/bot/setup/guilds/{guildID}/game-bans`, which batches guild-linked Steam IDs through `ISteamUser/GetPlayerBans`, refreshes stale cache rows after 6 hours, ignores VAC-only bans, and returns only game bans newer than 90 days.
- Outbound BattleMetrics HTTP requests prefer enabled `outbound_proxies` rows managed by owner-guild-only `/manage proxy add|edit|list|remove`; DB proxy URLs are encrypted at rest with `PROXY_ENCRYPTION_KEY`, are live-reloaded after create/update/delete, and request logs only include credential-free `scheme://host:port` endpoints. The outbound provider tracks per-proxy request results over a 10-minute window and auto-disables a DB proxy when at least 50% of 10 or more recent requests fail. If no DB proxies exist, legacy `PROXY_ENABLED=true` + comma-separated `PROXY_ENDPOINTS` is still used as a fallback. Steam API requests are always sent directly.
- The bot-internal steam profile route lazily refreshes `steam_account_cache` after a 24-hour TTL. Fresh cache rows skip Steam/BattleMetrics API calls; stale rows are returned without changing `updated_at` if refresh fails. Steam requests are sent directly. The response includes optional current/last tracked-server status from `player_states` joined to `tracked_servers`.
- The bot-internal steam analytics route shares the same `steam_account_cache.updated_at` TTL and refresh path as the bot-internal steam profile route. Fresh cache rows skip Steam/BattleMetrics API calls for both routes; stale cache rows are returned if refresh fails. Name-gap data is shared between the profile risk calculation and analytics gap display. If Steam summary reports `communityvisibilitystate = 1`, RustLens skips `GetOwnedGames` and leaves game totals/playtime private.

## Commands

```bash
cd apps/backend && go test ./...          # Run all tests
cd apps/backend && go vet ./...           # Lint
cd apps/backend && go build ./cmd/server  # Build
```
