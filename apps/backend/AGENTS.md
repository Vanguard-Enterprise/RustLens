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

### `guild_setups`

One-time guild setup state (claim/pending/configured flow).

| Column | Type | Notes |
|---|---|---|
| `guild_id` | `TEXT` | PK |
| `status` | `TEXT NOT NULL` | `pending` or `configured` |
| `configured_by` | `TEXT NOT NULL DEFAULT ''` | User snowflake |
| `selected_roles` | `JSONB NOT NULL DEFAULT '[]'` | String array |
| `selected_channels` | `JSONB NOT NULL DEFAULT '[]'` | String array |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |
| `updated_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

### `guild_settings`

Per-guild bot settings (currently only log channel).

| Column | Type | Notes |
|---|---|---|
| `guild_id` | `TEXT` | PK |
| `log_channel_id` | `TEXT NOT NULL DEFAULT ''` | Channel snowflake |
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

### `steam_accounts`

Known Steam player accounts with optional BattleMetrics ID.

| Column | Type | Notes |
|---|---|---|
| `steam_id` | `TEXT` | PK (17-digit Steam ID) |
| `bm_id` | `TEXT UNIQUE` | Nullable, BattleMetrics player ID |
| `bm_name` | `TEXT NOT NULL DEFAULT ''` | Latest known BattleMetrics display name |
| `avatar_image` | `TEXT NOT NULL DEFAULT ''` | Steam avatar URL |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

### `tracked_servers`

Global BattleMetrics servers the bot should track.

| Column | Type | Notes |
|---|---|---|
| `bm_id` | `TEXT` | PK |
| `name` | `TEXT NOT NULL` | Display name |
| `last_polled_at` | `TIMESTAMPTZ` | Nullable, last successful or failed poll attempt time |
| `last_poll_error` | `TEXT NOT NULL DEFAULT ''` | Most recent poll error, empty on success |

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

`online_summary.go` joins `guild_steam_discord_links`, `steam_accounts`, `player_states`, and `tracked_servers` to produce the guild online summary used by the bot's `/online` command. The query only returns rows where the linked Steam account has a non-null `bm_id` and the matching player state is currently online.

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
- The BattleMetrics poller queries `GET https://api.battlemetrics.com/servers/{server_id}?include=player` on a rolling interval, authenticates with `Authorization: Bearer <BATTLEMETRICS_API_KEY>`, and updates `player_states` inside a single store transaction per server.
- Steam profile resolution lives in `internal/steamapi/`. The bot uses `POST /api/internal/bot/setup/steam/resolve` to turn a raw 17-digit Steam ID or a supported `steamcommunity.com/profiles/...` / `steamcommunity.com/id/...` URL into a canonical Steam ID. Vanity URLs are resolved live through `STEAM_API_KEY` and are not cached separately.

## Commands

```bash
cd apps/backend && go test ./...          # Run all tests
cd apps/backend && go vet ./...           # Lint
cd apps/backend && go build ./cmd/server  # Build
```
