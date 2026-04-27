# Dashboard "Installed Guilds" — Design

**Date:** 2026-04-27
**Status:** Approved (pending implementation plan)

## Goal

Show every Discord guild that the signed-in user shares with the RustLens bot on `/dashboard`, with cached metadata (name, icon, banner, owner, member count) and the per-guild tracked-server count. Newly joined guilds — whether the user joins a bot-installed guild or the bot joins a guild the user is already in — must appear without the user having to log out and back in.

## Existing state

- **`user_guilds`** (also exposed as `UserGuildMembership` in code) — keyed by `(discord_id, guild_id)` with `is_active`, `first_seen_at`, `last_seen_at`, `left_at`. Populated only by the bot's `guildMemberAdd` / `guildMemberRemove` handlers in `apps/bot/src/user-guilds.ts`.
- **No "bot is installed" table.** `guild_settings` rows are created by the admin setup command; they are not authoritative for "the bot is currently a member of this guild". `guild_tracked_servers` only tells us which BattleMetrics servers a guild has linked.
- **No cached Discord guild metadata** (name, icon, banner, owner, approximate member count) anywhere.
- **OAuth scope is `identify` only** — backend never sees the user's guild list directly. All guild knowledge flows through the bot.
- **Bot intents:** `Guilds`, `GuildMembers`, `GuildVoiceStates`. The bot listens for member events but does **not** handle `guildCreate` / `guildDelete` / `guildUpdate`, and does not reconcile state on `clientReady`.
- **Internal bot endpoints** live under `/api/internal/bot/setup/...` and authenticate via the worker JWT issued during `BackendClient.register`. New endpoints in this design follow the same pattern.
- **Public app endpoints** live under `/api/app/...` (header `X-RustLens-App-Secret` + per-request `X-RustLens-Session`). The new dashboard endpoint follows the same pattern.
- **Frontend** has a stubbed `getMyInstalledGuilds()` in `apps/frontend/lib/rustlens-api.ts` that currently returns `[]`, and a server-rendered `<GuildList />` on `/dashboard` that already handles loading (Suspense) and empty states. `DashboardGuild` types already exist.

## Decisions

1. **Authority for "RustLens is installed":** a new `bot_guilds` table maintained by the bot. The bot is the only writer.
2. **Authority for "user is in guild":** the existing `user_guilds` table. The bot remains the only writer.
3. **Backfill strategy on bot join:** on every `guildCreate`, bulk-upsert all current human members into `user_guilds`. This is what makes the "bot joins a server I'm already in" case work without re-login.
4. **Idempotency of bulk-upsert across restarts:** track `members_synced_at` per guild on `bot_guilds`. On `clientReady`, only re-run the bulk-upsert when the timestamp is missing or older than `MEMBERS_RESYNC_INTERVAL` (default 7 days). Avoids hammering the DB on every bot restart in steady state, but still self-heals from any missed `guildMemberAdd` events.
5. **Existing-installation backfill on first deploy:** rely on the same `members_synced_at` mechanism — on the first `clientReady` after this ships, `members_synced_at IS NULL` for every guild, so each one runs the bulk-upsert exactly once. No separate migration script.
6. **Cached metadata storage:** store Discord asset **hashes** (not URLs) — matches `app_users.discord_avatar`. The frontend builds CDN URLs, including the `a_`-prefix → `.gif` rule for animated icons.
7. **Sharded reconcile:** each shard reconciles only the guilds in its own range using the standard formula `(guild_id >> 22) % shard_count == shard_id`. This prevents shard A from incorrectly marking shard B's guilds as left.
8. **Owner display:** stored as a denormalized snapshot (`owner_username`, `owner_global_name`, `owner_avatar_hash`). Refreshed on `guildUpdate` and during `clientReady` reconcile. Not joined to `app_users` — guild owners are not necessarily RustLens users.

## Schema

New table, added to the schema-init block in `apps/backend/internal/store/postgres.go` alongside existing `CREATE TABLE IF NOT EXISTS` statements:

```sql
CREATE TABLE IF NOT EXISTS bot_guilds (
    guild_id                 TEXT PRIMARY KEY,
    name                     TEXT NOT NULL,
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
```

No changes to `user_guilds`. The bulk-upsert path uses the existing schema and `ON CONFLICT (discord_id, guild_id)` semantics already implemented by `CreateUserGuildLink` / `MarkUserGuildLeft`.

## Backend routes

All bot-side routes live under `/api/internal/bot/setup/...` and use the existing JWT middleware. The dashboard route lives under `/api/app/...` and uses the existing app-secret + session middleware.

| Route | Method | Auth | Purpose |
|---|---|---|---|
| `/api/internal/bot/setup/bot-guilds/upsert` | POST | Worker JWT | Bot reports a guild it is in (on `guildCreate` and on `clientReady` reconcile per guild). Includes metadata; optionally includes `members[]` for bulk-upsert. |
| `/api/internal/bot/setup/bot-guilds/leave` | POST | Worker JWT | Bot reports it left a guild (`guildDelete`). Marks `is_installed=false`, sets `left_at`. |
| `/api/internal/bot/setup/bot-guilds/reconcile` | POST | Worker JWT | Bot reports the full set of guild IDs it currently sees, plus its `shardId` and `shardCount`. Backend marks any `bot_guilds` row in that shard's range and not in the set as left. |
| `/api/app/me/guilds/installed` | GET | App secret + session | Dashboard endpoint. Returns `{ items: DashboardGuild[] }` for the signed-in user. |

### Request shapes

`POST /bot-guilds/upsert`:

```json
{
  "guildId": "1234",
  "name": "My Guild",
  "iconHash": "a_abc..." ,
  "bannerHash": null,
  "ownerDiscordId": "5678",
  "ownerUsername": "name",
  "ownerGlobalName": "Display",
  "ownerAvatarHash": "...",
  "approximateMemberCount": 1234,
  "members": [{ "discordId": "...", "isBot": false }, ...] // optional
}
```

When `members` is present, the handler bulk-upserts non-bot members into `user_guilds` (active rows) in batches of 1000 inside a single transaction per batch, and updates `bot_guilds.members_synced_at` to `NOW()`. When absent (e.g., a `guildUpdate` metadata refresh), only `bot_guilds` is touched.

`POST /bot-guilds/leave`: `{ "guildId": "1234" }`.

`POST /bot-guilds/reconcile`:

```json
{ "shardId": 0, "shardCount": 4, "guildIds": ["1234", "5678"] }
```

The handler runs:

```sql
UPDATE bot_guilds
SET is_installed = FALSE, left_at = NOW()
WHERE is_installed = TRUE
  AND ((CAST(guild_id AS BIGINT) >> 22) % $shardCount) = $shardId
  AND guild_id <> ALL($guildIds);
```

(Snowflake IDs fit in `BIGINT`. The cast is required because we store guild IDs as `TEXT`.)

`GET /me/guilds/installed`:

```sql
SELECT bg.guild_id,
       bg.name,
       bg.icon_hash,
       bg.banner_hash,
       bg.owner_discord_id,
       bg.owner_username,
       bg.owner_global_name,
       bg.owner_avatar_hash,
       bg.approximate_member_count,
       (SELECT COUNT(*)
        FROM guild_tracked_servers gts
        WHERE gts.guild_id = bg.guild_id) AS tracked_server_count
FROM user_guilds ug
JOIN bot_guilds bg ON bg.guild_id = ug.guild_id
WHERE ug.discord_id = $1
  AND ug.is_active = TRUE
  AND bg.is_installed = TRUE
ORDER BY bg.name ASC, bg.guild_id ASC;
```

The response maps each row to `DashboardGuild` (defined in `apps/frontend/lib/rustlens-api.ts`). The handler resolves Discord ID from the session (consistent with how `GetCurrentAppUserGuilds` already works in `app_handlers.go`).

## Bot lifecycle

New module `apps/bot/src/bot-guilds.ts`. Wired from `apps/bot/src/shard-worker.ts` next to the existing `registerUserGuildSync(client)` call.

Handlers:

- **`guildCreate(guild)`** — Build the upsert payload (metadata + member list filtered to non-bot members, fetched via the existing `getGuildMembers` cache). Send `POST /bot-guilds/upsert` with the metadata immediately, then send the `members[]` payload in chunks of **1000** members per request, **fire-and-forget on the event loop** (the handler does not await the bulk-upsert) so we don't block the gateway. Each batch uses the same endpoint with `members` set to the chunk; the backend updates `members_synced_at` on the final batch (signalled by an explicit `membersSyncComplete: true` flag, or by sending a final empty-`members` request — picking the explicit flag for clarity).
- **`guildDelete(guild)`** — `POST /bot-guilds/leave`.
- **`guildUpdate(_, guild)`** — `POST /bot-guilds/upsert` with metadata only (no `members`). Refreshes name/icon/banner/owner/member-count cache.
- **`clientReady`** — Per shard, after the existing command-registration block:
  1. Build `guildIds = [...client.guilds.cache.keys()]`.
  2. `POST /bot-guilds/reconcile` with `{ shardId, shardCount, guildIds }`.
  3. For each guild, `POST /bot-guilds/upsert` with metadata. If `bot_guilds.members_synced_at` is NULL or older than `MEMBERS_RESYNC_INTERVAL_MS` (default 7 days), include `members[]` (chunked, fire-and-forget). The backend returns the current `members_synced_at` in the metadata-only response so the bot knows whether to follow up with a member sync.

The `members_synced_at` round-trip avoids the bot needing its own state — the source of truth lives in Postgres.

The `(guild_id >> 22) % shard_count == shard_id` check is enforced by the **backend** on `/reconcile` (per the SQL above), not by the bot. The bot just sends what its shard sees.

## Frontend

1. **Uncomment `backendFetch` in `getMyInstalledGuilds()`** in `apps/frontend/lib/rustlens-api.ts`. The function hits `/api/app/me/guilds/installed` and returns `payload.items`.
2. **New utility `apps/frontend/lib/discord-cdn.ts`** with the helpers below. Used by the guild card and any future Discord-asset rendering.

```ts
export function guildIconUrl(guildId: string, hash: string | null, size = 256): string | null {
  if (!hash) return null;
  const ext = hash.startsWith("a_") ? "gif" : "png";
  return `https://cdn.discordapp.com/icons/${guildId}/${hash}.${ext}?size=${size}`;
}

export function guildBannerUrl(guildId: string, hash: string | null, size = 1024): string | null {
  if (!hash) return null;
  const ext = hash.startsWith("a_") ? "gif" : "png";
  return `https://cdn.discordapp.com/banners/${guildId}/${hash}.${ext}?size=${size}`;
}

export function userAvatarUrl(userId: string, hash: string | null, size = 128): string | null {
  if (!hash) return null;
  const ext = hash.startsWith("a_") ? "gif" : "png";
  return `https://cdn.discordapp.com/avatars/${userId}/${hash}.${ext}?size=${size}`;
}
```

3. **Update the backend response shape and the frontend type to match.** The endpoint returns hashes, not URLs. Two options:
   - (a) Have the backend build URLs and return them, keeping `DashboardGuild` unchanged.
   - (b) Change `DashboardGuild` to carry hashes + IDs and let the frontend build URLs.

   Picking **(b)** — it keeps URL construction in one place (the new util), and lets us tweak `size` per consumer without changing the backend. Existing `DashboardGuild` becomes:

   ```ts
   export type DashboardGuildOwner = {
     id: string;
     username: string;
     globalName: string | null;
     avatarHash: string | null;
   };

   export type DashboardGuild = {
     id: string;
     name: string;
     iconHash: string | null;
     bannerHash: string | null;
     approximateMemberCount: number | null;
     trackedServerCount: number;
     owner: DashboardGuildOwner | null;
   };
   ```

   The `rustlensInstalled` field is dropped from the type — the endpoint only ever returns installed guilds, so it carries no information.

4. **Update `apps/frontend/app/dashboard/_components/guild-list.tsx`** to use `guildIconUrl` / `guildBannerUrl` / `userAvatarUrl`, the renamed fields, and `owner.globalName ?? owner.username` for the display name.

## Data flow summary

```
Bot guildCreate / clientReady / guildUpdate
        │
        ▼
POST /api/internal/bot/setup/bot-guilds/upsert  →  bot_guilds (UPSERT)
                                                  user_guilds (bulk UPSERT, batched)

Bot guildDelete
        │
        ▼
POST /api/internal/bot/setup/bot-guilds/leave    →  bot_guilds.is_installed = FALSE

Bot guildMemberAdd / guildMemberRemove (existing)
        │
        ▼
existing user-guilds endpoints                   →  user_guilds

Browser /dashboard
        │
        ▼
GET /api/app/me/guilds/installed                 →  JOIN user_guilds × bot_guilds
                                                    × (subquery) guild_tracked_servers
```

## Error handling

- **Bot → backend 5xx** during `guildCreate` upsert: log and continue. The next `clientReady` reconcile (on bot restart) re-attempts via the `members_synced_at`-driven path. Member-sync batches are independently idempotent.
- **Bot → backend 5xx** during `clientReady` reconcile: log and continue. The reconcile is the safety net; if it fails, the next bot startup retries.
- **Backend SQL failure** in `/me/guilds/installed`: existing pattern — log and return 500. Frontend `Suspense` boundary surfaces an error.
- **Stale `bot_guilds` after a guild is deleted by Discord (not a `guildDelete` event):** `clientReady` reconcile cleans this up on next startup. Acceptable.
- **Owner left the guild between `guildUpdate` events:** acceptable — fixed on the next refresh.

## Testing

- **Backend unit tests** (Go, table-driven): `bot_guilds` upsert + member-batch insert handler; `/me/guilds/installed` returns only joined+installed rows; reconcile correctly limits to its shard range; reconcile is a no-op when `guildIds` covers everything.
- **Bot unit tests** (vitest): `bot-guilds.ts` builds the right payloads from a fake `Guild`; resync path skips member sync when `members_synced_at` is fresh; chunking math.
- **Manual acceptance:** `dev` environment, sign in as a test user. Add the bot to a server the test user is already in. Refresh `/dashboard` (no logout). Guild appears within seconds of the bot finishing its bulk-upsert.

## Out of scope

- Expanding OAuth scope to `identify guilds`. The bot's data is sufficient now; revisit only if Discord changes member-sync semantics.
- Showing guilds where the bot is *not* installed (e.g. "invite the bot to this guild" CTAs).
- Realtime updates — the dashboard re-fetches on navigation. WebSocket push is unneeded for this use case.
- Animated banner rendering tuning beyond the `a_`-prefix rule.
