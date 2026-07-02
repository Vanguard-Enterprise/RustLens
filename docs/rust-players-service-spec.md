# RustLens Rust Migration — Phase 2: `players` Service Spec

Status: **DRAFT — awaiting review before implementation.**
Author: migration work, 2026-07-01
Branch base: `rework/rust-backend`

Phase 2 (roadmap §4) builds the **`players` service** — the player-identity
domain: Steam↔BM resolution, profiles/analytics, and all Steam↔Discord linking.
It's the largest domain (~23 routes) and the first *new* Rust service since
`identity`. It consumes the Phase 1 crates (`rustlens-steam`,
`rustlens-battlemetrics`, `rustlens-outbound`, `rustlens-httpauth`,
`rustlens-botjwt`, `rustlens-db`). **No background workers** (per the roadmap
topology).

This spec was assembled from a fan-out read of the Go backend; file:line refs
point at the source of truth. Where a JSON shape or SQL clause is a wire/DB
contract, it's quoted so the port is byte-for-byte.

## Ground rules (carried)

- **Same Postgres, no schema changes, no migrations.** Go owns `EnsureSchema()`.
- **Cross-language contracts stay byte-for-byte:** every JSON response shape must
  match what the bot TS client and frontend expect.
- `cargo clippy --all-targets` warning-clean; parameterized queries only.
- One PR per sub-phase onto `rework/rust-backend`, merged then branch-pruned.

---

## 1. Ownership & boundaries

**Owned (sole writer) by `players`:** `steam_accounts`, `steam_account_cache`,
`bm_profile_cache`, `bm_name_history`, `steam_discord_links`,
`guild_steam_discord_links`, `hidden_discord_link_identities`,
`platform_sync_state`.

**Read cross-domain (owned elsewhere, read directly during transition):**
- `player_states` ⨝ `tracked_servers` — profile online-status, name-search,
  category buckets. Owned by **servers** (Phase 3); players reads them directly.
- `track_analytics_sessions_cache` — offline-analytics session cache. Owned by
  **servers** (Phase 3); players **reads and upserts** it during Phase 2 (Go does
  too — shared write during the transition, documented).
- `app_users` / admin RBAC — the app-search name-mode admin gate reads the
  caller's permissions (via `rustlens-db::admin`). Owned by **identity**.
- `user_guilds` — deep game-bans reads `ListActiveUserGuildDiscordIDs`. Owned by
  **guilds** (Phase 4); players reads it directly. **The `user_guilds` *write*
  routes (`POST /user-guilds`, `/user-guilds/leave`, `GET /discord/{id}/guilds`)
  are NOT part of the players service** — they're guild-membership (guilds
  domain), and `identity` already writes `user_guilds` on OAuth login.

**Not ported (stays in the bot / Go):** the soft-vs-hard "Links embed" dedup and
the guild-leave link cascade are **application-orchestrated in the bot TS**
(`apps/bot/src/user-guilds.ts`, `player.ts`), not backend — the backend just
serves the raw list + delete routes. No change needed here.

---

## 2. Service skeleton

New binary `apps/rust/services/players` (mirrors `services/identity`):
`main.rs` (config → pool → clients → router → serve), `state.rs` (`AppState`),
`router.rs`, plus handler modules. Default `PORT` 8091 (identity is 8090).

**AppState holds:**
- `pool: PgPool`
- `config: PlayersConfig` (new `rustlens-config` struct or a local one — see §9)
- `steam: rustlens_steam::steamapi::Client` (direct)
- `resolver: Arc<rustlens_steam::Chain>` (bmsearch primary + bmmatch fallback)
- `bm: rustlens_battlemetrics::Client` (proxied via the outbound provider)
- `outbound: Arc<rustlens_outbound::Provider>` (BM only; built from
  `load_initial_endpoints`)
- `platform_sync: rustlens_steam::platformsync::Client` (direct)
- `redis: redis::aio::ConnectionManager` (app-route rate limiting)
- `backfill: <singleflight map>` (§5.3)

**Auth:**
- Bot-internal routes (`/api/internal/bot/setup/*`): `rustlens_botjwt` Bearer gate
  (mirror identity's `bot.rs` pattern).
- App routes (`/api/app/players/*`): `rustlens_httpauth::require_session` (header)
  behind the `X-RustLens-App-Secret` gate, exactly as identity's `/me`.

**External-client direct-vs-proxied boundary (critical):**
- **BattleMetrics → proxied** through `rustlens-outbound`.
- **Steam, bmsearch, bmmatch, platformsync → direct.** (Note: Go's steam client
  calls `provider.Prepare()`, but it's wired with the *direct* provider, so it's
  effectively direct — matching the `rustlens-steam` `steamapi` client, which is
  direct by construction.)

---

## 3. Store schema (verbatim; Go owns it — read/write only)

`steam_accounts` (`postgres.go:224-228`) — **only 3 columns**:
```sql
CREATE TABLE steam_accounts (
    steam_id TEXT PRIMARY KEY,
    bm_id TEXT UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

`steam_account_cache` (`postgres.go:230-241`) — **BM hours/name/first-seen were
migrated OUT to `bm_profile_cache`**:
```sql
CREATE TABLE steam_account_cache (
    steam_id TEXT PRIMARY KEY REFERENCES steam_accounts(steam_id) ON DELETE CASCADE,
    persona_name TEXT NOT NULL DEFAULT '',
    avatar_image TEXT NOT NULL DEFAULT '',
    rust_hours BIGINT,               -- steam Rust playtime (hours), null=private
    total_games INTEGER,             -- null=private
    top_games JSONB NOT NULL DEFAULT '[]',   -- [{appId,name,hours}]
    game_banned BOOLEAN NOT NULL DEFAULT FALSE,
    game_banned_at TIMESTAMPTZ,
    game_ban_checked_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);  -- index on (updated_at)
```

`bm_profile_cache` (`postgres.go:259-273`) — holds BM name/hours/first-seen:
```sql
CREATE TABLE bm_profile_cache (
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
    top_servers JSONB NOT NULL DEFAULT '[]',  -- [{bmId,name,hours}]
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);  -- index on (updated_at)
```

`bm_name_history` (`postgres.go:280-286`):
```sql
CREATE TABLE bm_name_history (
    bm_id TEXT NOT NULL, name TEXT NOT NULL, last_seen_at TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bm_id, name, last_seen_at)
);  -- index (bm_id, last_seen_at DESC); FK to steam_accounts dropped
```

`steam_discord_links` (`postgres.go:612-619`): `steam_id TEXT NOT NULL REFERENCES
steam_accounts(steam_id)`, `discord_id TEXT`, `type TEXT CHECK IN ('soft','hard')`,
`source TEXT DEFAULT ''`, `created_at`. PK `(steam_id, discord_id, type)`. Index
on `(discord_id)`.

`hidden_discord_link_identities` (`postgres.go:624-630`): `discord_id TEXT PK`,
`reason TEXT DEFAULT ''`, `created_by TEXT DEFAULT ''`, `created_at`, `updated_at`.

`guild_steam_discord_links` (`postgres.go:632-638`): `guild_id`, `discord_id`,
`steam_id`, `created_at`. PK `(guild_id, discord_id, steam_id)`. No FK. Indexes
`(guild_id, discord_id)` and `(guild_id, steam_id)`.

`platform_sync_state` (`postgres.go:646-651`): `platform TEXT CHECK IN
('discord','steam')`, `platform_id TEXT`, `last_platform_sync_at TIMESTAMPTZ
DEFAULT NOW()`. PK `(platform, platform_id)`.

Cross-domain reads: `player_states` (+ join `tracked_servers`), `tracked_servers`
(category flags), `track_analytics_sessions_cache`, `user_guilds`.

`rustlens-db` gets new modules (per spec D3 precedent): e.g. `players_steam`
(accounts + caches + name history), `players_links` (global + guild links +
hidden), `players_search` (name search), `platform_sync`, `game_bans`, and small
read helpers for `player_states`/`tracked_servers`/`track_analytics_sessions_cache`.

---

## 4. Route inventory

All bot-internal routes are under `/api/internal/bot/setup` (JWT-gated); app
routes under `/api/app` (session-gated). Grouped by sub-phase (§7).

### 4.1 Links (P2-B) — pure DB, no external clients
| Route | Method | Handler / SQL notes |
|---|---|---|
| `/steam/links` | POST | create **soft** link; ensure `steam_accounts` parent first; `409 soft link already exists` |
| `/steam/bulk-hard-links` | POST | ≤1000; per-link `upsertSteamAccountSQL` guard + `type='hard'` `ON CONFLICT DO NOTHING`; `{status:ok,count}` |
| `/steam/{steamID}/links` | GET | Steam→Discord list (hidden-suppressed) |
| `/discord/{discordID}/links` | GET | Discord→Steam list (hidden-suppressed) |
| `/steam/{steamID}/links/{discordID}/soft` | DELETE | soft-only (`type='soft'` in SQL); `404 soft link not found` |
| `/discord/{discordID}/setsteam-candidates` | GET | `/setsteam` picker (hidden-suppressed CTE) |
| `/discord/{discordID}/link-visibility-hidden` | PUT | upsert hidden row `{reason,createdBy}` |
| `/discord/{discordID}/link-visibility-hidden` | DELETE | delete hidden row |
| `/guilds/{guildID}/links` | POST | create guild link; `409 guild link already exists` |
| `/guilds/{guildID}/links` | GET | list all guild links (hidden-suppressed) |
| `/guilds/{guildID}/discord/{discordID}/links` | GET | by discord (hidden-suppressed) |
| `/guilds/{guildID}/steam/{steamID}/links` | GET | by steam (hidden-suppressed) |
| `/guilds/{guildID}/discord/{discordID}/steam/{steamID}` | DELETE | `404 guild link not found` |
| `/guilds/{guildID}/nickname-targets` | GET | one-row-per-discord derived view (NOT hidden-suppressed) |

**Suppression clause** (identical in the 5 list queries, keyed only on
`discord_id`, suppresses both directions, never deletes rows):
```sql
AND NOT EXISTS (SELECT 1 FROM hidden_discord_link_identities hidden
                WHERE hidden.discord_id = <alias>.discord_id)
```
Response maps: soft/hard link `{steamId,discordId,type,source,createdAt}`; guild
link `{guildId,discordId,steamId,createdAt}`; hidden `{discordId,reason,createdBy,
createdAt,updatedAt}`. Path validation: `steamID ^\d{17}$`, snowflake `^\d{17,20}$`.
`source` required non-empty on create. `upsertSteamAccountSQL` (`steam.go:206-219`)
null-coerces a `bm_id` already owned by another `steam_id` (avoids 23505 in bulk).

### 4.2 Resolve + backfill (P2-C) — the HYBRID (see §5)
| Route | Method | Notes |
|---|---|---|
| `/steam/{steamID}` | GET | `GetSteamAccountBySteamID`; on miss → hybrid resolve; also re-triggers resolve when the row exists but `bm_id` is empty; `404 steam account not found` |
| `/steam/by-bm/{bmID}` | GET | BM-profile-first then resolver fallback; **no** `ErrPending`/backfill on this side (bmsearch `BMToSteam` is cache-only) |

Response `steamAccountToMap` (`{steamId, bmId|null, bmName, avatarImage,
firstSeenAt|null, createdAt, updatedAt}`) — `bmName`/`avatarImage`/`firstSeenAt`
come from the joins in `GetSteamAccountBySteamID` (bm_profile_cache +
steam_account_cache); `updatedAt = COALESCE(sac.updated_at, sa.created_at)`.

### 4.3 Profile / analytics / offline-analytics (P2-C)
| Route | Method | Response struct |
|---|---|---|
| `/steam/{steamID}/profile` | GET | `steamProfileResponse` (§6.1); `404 steam account not found` / `404 steam account has no linked battlemetrics id` / `404 battlemetrics profile private` |
| `/steam/{steamID}/analytics` | GET | `steamAnalyticsResponse` (§6.2); `502 failed to fetch analytics` when private + no cache |
| `/players/{bmID}/offline-analytics` | GET | `trackAnalyticsResponse` (§6.3); `bmID ^\d{1,16}$` |

### 4.4 game-bans + platform-sync (P2-D)
| Route | Method | Notes |
|---|---|---|
| `/guilds/{guildID}/game-bans` | GET | `?method=light\|deep&timeframe=30d\|90d\|180d\|all&scopeDiscordId=…(repeatable)`; §6.4 |
| `/platform-sync/{platform}/{id}` | POST | platform `steam\|discord`; §6.5 |

### 4.5 App-facing (P2-E) — session-authed + rate-limited
| Route | Method | Notes |
|---|---|---|
| `/api/app/players/search` | GET | `?q=&includeOffline=`; identifier or name mode; name mode needs ≥1 admin perm; rate `players_search` 30/60s |
| `/api/app/players/{identifier}/profile` | GET | resolves identifier→Steam64, delegates to the §6.1 profile pipeline; rate `players_profile` 60/60s |

---

## 5. The HYBRID resolve+backfill (P2-C core — the trickiest part)

Ports `resolveSteamAccountBySteamID` / `backfillSteamAccountBmID` /
`resolveSteamAccountBySteamIDWithin` / `upsertResolvedSteamAccount` /
`refreshSteamOnlyCache` (`handlers.go:2280-2403`).

### 5.1 Inline attempt (bounded by `BM_SEARCH_INLINE_TIMEOUT_MS`, default 4000ms)
`resolveSteamAccountBySteamID(steamID)`: run `resolveSteamAccountBySteamIDWithin`
under a `tokio::time::timeout(inline_wait, …)`. On `MatchError::Pending` → spawn
the detached backfill and return `ErrSteamAccountNotFound` **now** (degraded).
Any other error propagates.

### 5.2 Core resolve+persist (`resolveSteamAccountBySteamIDWithin`) — error map
1. `resolver.steam_to_bm(steamID)`:
   - `Pending` → propagate `Pending` (caller backfills).
   - `NotFound`/`NotConfigured` → `ErrSteamAccountNotFound`.
   - other → propagate (500).
   - success but empty bm_id → `ErrSteamAccountNotFound`.
2. `bm.fetch_player_profile(bm_id)`:
   - `PlayerNotFound` → **bare-linkage path**: `UpsertSteamAccount(steamID,
     bm_id)` + `refreshSteamOnlyCache` → `UpsertSteamAccountCache` (no
     `bm_profile_cache` write); return the re-read row.
   - deadline/cancel → remap to **`Pending`** (shared inline budget exhausted →
     let the backfill finish; don't 500 a resolvable player).
   - other → propagate.
   - success → `upsertResolvedSteamAccount` (full persist).

### 5.3 Detached singleflight backfill
`backfillSteamAccountBmID(steamID)`: spawn a task (NOT tied to the request
cancellation) that, deduped per `steamID`, runs
`resolveSteamAccountBySteamIDWithin` under a **90s** `tokio::time::timeout`;
swallow `ErrSteamAccountNotFound`/`Pending`, log other errors. **Rust singleflight
equivalent:** a `Mutex<HashSet<String>>` (or `DashMap<String,()>`) in-flight guard
in `AppState`: insert-or-skip the `steamID`; the winner spawns the task and
removes the key on completion. Semantics to preserve: coalesce concurrent
same-id backfills to one; release the key after completion (no result caching);
budget independent of and outliving the request.

### 5.4 Full persist (`upsertResolvedSteamAccount`)
Guard empty steamID/bmID → `ErrSteamAccountNotFound`. Then, in order:
`UpsertSteamAccount(steamID, bm_id)` → `buildSteamCacheFromProfile` →
`UpsertSteamAccountCache` (7 cols) → `UpsertBmProfileCache` (13 cols) →
`upsertBmNameHistoryFromProfile` (best-effort, log on error) → re-read via
`GetSteamAccountBySteamID`.

### 5.5 `by-bm` direction (`resolveSteamAccountByBmID`)
BM-first: `bm.fetch_player_profile(bmID)`; on `PlayerNotFound` →
`resolver.bm_to_steam(bmID)` (bare linkage). On profile success with empty
`steam_id` → `resolver.bm_to_steam`. **No `Pending`/backfill** (bmsearch
`bm_to_steam` is cache-only). Then `upsertResolvedSteamAccount`.

---

## 6. Exact response contracts & algorithms

### 6.1 `steamProfileResponse` (`handlers.go:1830-1860`)
```jsonc
{
  "steamId": "…", "bmId": "…", "bmName": "…", "personaName": "…",
  "avatarImage": "…",
  "firstSeenUnix": 0 | null,
  "hours": { "vitality":0,"atlas":0,"vital":0,"modded":0,"official":0,"build":0,
             "bm":0, "rust": 0 | null },
  "serversPlayed": 0,
  "nameGaps": [ { "fromUnix":0, "toUnix":0 } ],   // never null; [] when none
  "status": { "isOnline":false, "serverBmId":"…", "serverName":"…",
              "onlineUnix":0|null, "offlineUnix":0|null }  // OMITTED (not null) when no player_states
}
```
- Requires resolved account with non-empty `bm_id` else `404 steam account has no
  linked battlemetrics id`.
- **24h cache gate:** read `steam_account_cache` + `bm_profile_cache`; skip all
  API calls only when **both** rows `< 24h` old. Else full refresh
  (`fetchFreshSteamCache`: 1 BM profile fetch + Steam summary + conditional owned
  games + `ListTrackedServers`).
- **Refresh failure:** BM `PlayerNotFound` → **`404 battlemetrics profile
  private`** (wins over stale); otherwise serve stale if both rows existed, else
  `500 failed to refresh steam profile`. Cache-write failure → stale-or-500.
- `hours.*` copied whole from `bm_profile_cache` (already `sec/3600`). Category
  buckets computed at refresh (`applyBmProfileToCache`): iterate Rust servers
  (skip `game != "" && != "rust"`); `modded`/`official` from `RustType`;
  `vitality`/`atlas`/`vital`/`build` from `tracked_servers` flags by server id;
  `bm` = total; `serversPlayed` = # Rust servers. `hours.rust` from Steam.
- `nameGaps` computed **live** from `bm_name_history` (§6.6). No `risk` field in
  the wire — the bot/frontend derive risk from the gaps.
- `status` from `ListPlayerStatesByPlayer(bm_id)` (join `tracked_servers`): pick
  latest-online else latest-offline; nil → omit field.

### 6.2 `steamAnalyticsResponse` (`handlers.go:2461-2488`)
```jsonc
{ "steamId":"…","bmId":"…","bmName":"…","avatarImage":"…",
  "totalGames": 0 | null,                       // null when Steam private
  "topGames": [ {"appId":0,"name":"…","hours":0} ],   // [] when private
  "totalServers": 0,
  "topServers": [ {"bmId":"…","name":"…","hours":0} ],
  "gaps": [ {"fromUnix":0,"toUnix":0} ] }
```
Same 24h gate/refresh as profile, but **no** `PlayerNotFound` special-case: private
BM + no cache → `502 failed to fetch analytics` (else stale). Top-5 games sorted
by `playtime_forever` desc (`hours = min/60`), `totalGames = len`; Rust app id
`252490`. `communityvisibilitystate==1` (or any GetOwnedGames error) → private →
`totalGames` null, `topGames` `[]`, `rust` null. `topServers`/`totalServers` from
`bm_profile_cache.top_servers`/`bm_servers_played`.

### 6.3 `trackAnalyticsResponse` — offline-analytics (`track_analytics_handler.go:39-60,114-141`)
```jsonc
{ "groupName":"Player {bmID}"|bmName, "serverName":"All BattleMetrics servers",
  "timeZone":"Europe/London", "members":1, "analyzed":0|1,
  "days":[ { "weekday":"Monday", "bestStart":unix, "bestEnd":unix,
             "hours":[ {"hourUnix":unix,"hour":0,"avg":0.0,"minutes":0.0} x24 ] } x7 ] }
```
Session cache key `(bmID, "__all__")` in `track_analytics_sessions_cache`, 24h TTL;
miss → `FetchPlayerSessionsAllServers(bmID, 100, 2)` (first 2 pages), drop
nil/`stop<=start`, upsert cache; on fetch error serve stale. Distribute sessions
across Europe/London hours, drop player-days `< 1h`, cap 3600s/hour, average per
weekday; `bestStart` = lowest 2-hour avg-online window (wraps midnight); day base
unix = `1767571200 (2026-01-05 Mon UTC) + weekday*86400`; Monday=0.

### 6.4 game-bans (`handlers.go:3515-3896`)
Response `{items:[{guildId,discordId,steamId,gameBannedAt(RFC3339),
gameBannedUnix,source}]}`, sorted `gameBannedAt` desc; `source` is
`direct_guild_link` | `linked_account`.
- Candidates: `ListGuildLinks(guildID)` (hidden-suppressed) → each a
  `direct_guild_link`. Optional repeatable `scopeDiscordId` filters links.
- **deep:** expand Discord set = scope, else `ListActiveUserGuildDiscordIDs(guildID)`
  ∪ distinct guild-link discord ids. Refresh stale (`platform_sync_state` missing
  or `≥14d`) via the platform-sync routine (§6.5), **but if >100 eligible →
  `422`**. Then `ListVisibleHardLinksByDiscordIDs` → `linked_account` candidates.
- Dedup by `guild:discord:steam` (first-wins keeps `direct_guild_link`); collapse
  to distinct Steam IDs.
- **Ban cache:** `steam_account_cache` `game_banned*`, **6h TTL**
  (`game_ban_checked_at`). Stale → `GetPlayerBans` (batch 100); `gameBanned =
  NumberOfGameBans > 0` (**VAC/community/economy ignored**); `gameBannedAt =
  now - DaysSinceLastBan`. `UpsertSteamAccountGameBanCache` writes only the 3 ban
  cols. Timeframe filter: `all`=Window 0 (keep all), else drop
  `now - gameBannedAt > window` (default 90d).

### 6.5 platform-sync (`platform_sync_handler.go`)
`POST /platform-sync/{platform}/{id}` (no body). Response `platformSyncResponse`
`{synced, reason?, lastSyncedAt, platform, platformId, linked, steamId?, discordId?,
previousSteamIds?, previousDiscordIds?, hardLinkCount}`.
- **Fresh check:** `platform_sync_state` row `< 14d` → `{synced:false,
  reason:"fresh", lastSyncedAt}`, no upstream call.
- Else `platform_sync.lookup(id)` (direct): `NotConfigured`→`503`;
  `NotLinked`→still record state, `{synced:true, linked:false}`; other→`502`;
  success → build hard links + `BulkUpsertHardLinks` + record state.
- **Hard links** (`buildPlatformSyncHardLinks`): cartesian product of valid
  steamIDs × discordIDs (queried id + all `previous_*` ids), each
  `{source:"platformsync", type:hard}`, `ON CONFLICT DO NOTHING`.
- **Only the queried `(platform,id)`** is recorded in `platform_sync_state`
  (`UpsertPlatformSyncState`, `last_platform_sync_at=NOW()`).
- Deep game-bans reuses this exact routine (`syncPlatformIdentity`).

### 6.6 name-gap compute (`handlers.go:2557-2610`)
On each fresh BM fetch: upsert `profile.name_identifiers` (type `name`, whitespace
collapsed, skip empty/nil lastSeen) into `bm_name_history`. At response time:
`ListBmNameHistory(bmID)` sorted `last_seen_at ASC`, emit gap
`{fromUnix:cur, toUnix:next}` for each consecutive pair where `next - cur >=
30*24h`. `[]` when empty. Gaps never stored — always computed live.

### 6.7 app search/profile (`players_handlers.go`)
- **search:** `q` trimmed, `≥2` chars else `400`. Rate-limit `players_search`
  30/60s (Redis fixed-window Lua INCR+PEXPIRE, **fail-open**, key
  `rustlens:players_search:{userId}`). `ClassifyQuery` (regexes: steam `^\d{17}$`,
  bm `^\d{6,16}$`, steam URL, bm URL, vanity). `isAdmin = len(PermissionsForUser)
  > 0` (live). name mode requires `isAdmin` else `403 name search requires admin`;
  runs `SearchByName` (`DISTINCT ON (steam_id)`, ILIKE bm_name/persona_name, online
  or `includeOffline` within 24h, `LIMIT 20`, online-first). identifier mode:
  `ResolveToSteamID` then `lookupSingleHit`. Response `{items:[playerSearchHit],
  mode:"name"|"identifier"}`; hit `{steamId, bmId|null, bmName|null, avatarUrl|null,
  currentServer:{bmId,name}|null, isOnline, lastSeenAt|null}`.
- **profile:** rate-limit `players_profile` 60/60s. `ResolveToSteamID` (miss →
  `404 player not found`), then run the §6.1 pipeline (identical JSON to the
  bot-internal profile route).
- **Identifier→Steam64** (`internal/players/classifier.go` + `service.go`):
  17-digit→as-is; `/profiles/{17}`→extract; `/id/{vanity}` or bare vanity →
  `steamapi::resolve_vanity_url` (needs `STEAM_API_KEY`); bm id → local
  `GetSteamAccountByBmID` else `resolver.bm_to_steam`.

---

## 7. Phasing (one PR each, onto `rework/rust-backend`)

- **P2-A — skeleton + store modules.** `services/players` binary (config, state,
  router, healthz, bot-JWT + app-secret/session gates), the `rustlens-db` player
  modules (schema §3 read/write helpers), and the identifier classifier
  (port `classifier.go` — pure logic, unit-testable). No routes yet beyond
  `/api/healthz`. Foundation for everything.
- **P2-B — links + visibility + guild links.** §4.1. Pure DB CRUD, no external
  clients. DB-gated integration tests for the suppression clause (both
  directions), soft-only delete, bulk-hard `upsertSteamAccountSQL` guard.
- **P2-C — resolve/backfill + profile/analytics/offline-analytics.** §4.2, §4.3,
  §5, §6.1-6.3, §6.6. The heavy core: BM + Steam clients, the 24h caches, the
  hybrid + singleflight. Tests: cache-gate/refresh/stale/404-private paths (mock
  clients), hybrid error-map + singleflight, name-gap compute, profile/analytics
  JSON fixtures.
- **P2-D — game-bans + platform-sync.** §4.4, §6.4, §6.5. Port platform-sync first
  (deep game-bans reuses it). Tests: light/deep candidate gathering + 6h ban
  cache + timeframe filter; platform-sync fresh-check + hard-link cartesian +
  state recording.
- **P2-E — app routes.** §4.5, §6.7. Session-auth via `rustlens-httpauth`, Redis
  rate limits, identifier classify, name-search admin gate. Delegates profile to
  P2-C. Tests: rate-limit fail-open, classify, admin gate, JSON shapes.
- **P2-F — infra cutover** (Dockerfile, CI cargo job already covers the workspace,
  publish matrix entry, compose service, Traefik dynamic route for the players
  paths → `:8091`, delete the migrated Go handlers, update the bot TS client to
  the new base path). **Outward-facing — confirm with the user first** (same as
  P0-F). The bot-internal path reorg (roadmap §3: `/api/internal/bot/players/*`)
  is decided here.

Each phase: `cargo build` + `cargo clippy --all-targets` clean + `cargo test`;
DB-touching tests against throwaway Postgres (spare port); parity check of JSON
vs the live Go response before any Traefik flip (P2-F).

---

## 8. Verification strategy

- **Unit:** identifier classifier; name-gap compute; category-bucket math;
  hybrid error-mapping (mock resolver + mock BM client); singleflight dedup;
  rate-limit fail-open.
- **JSON parity:** golden fixtures for `steamProfileResponse` /
  `steamAnalyticsResponse` / `trackAnalyticsResponse` / link maps / game-ban items
  / platform-sync response — assert field names/casing/null-vs-omitted match the
  Go structs in §6.
- **DB-gated integration** (throwaway PG, `RUSTLENS_TEST_PG_URL`): link CRUD +
  suppression, bulk-hard guard, game-ban candidate SQL, platform-sync state +
  hard-link upsert, resolve persist.
- **End-to-end smoke** (P2-C/E, throwaway PG + mock external servers via wiremock
  or a stub): profile 24h-gate → refresh → stale → 404-private; hybrid
  pending→backfill; app search identifier/name/rate-limit.
- **P2-F parity harness:** same request → Go vs Rust diff per route before flip.

---

## 9. Config / env (players service)

Reuses identity's shared vars + steam/BM/proxy vars:
`PORT` (8091), `POSTGRES_URL`, `REDIS_URL`, `APP_API_INTERNAL_SECRET`,
`APP_SESSION_SECRET`, `BOT_SERVICE_JWT_SECRET` (+ issuer/audience), `STEAM_API_KEY`,
`BM_SEARCH_ENABLED`/`BM_SEARCH_URL`/`BM_SEARCH_INLINE_TIMEOUT_MS`,
`BM_MATCH_URL`/`BM_MATCH_TOKEN`, `PLATFORMSYNC_TOKEN`,
`PROXY_ENABLED`/`PROXY_ENDPOINTS`/`PROXY_ENCRYPTION_KEY`. Add a `PlayersConfig`
to `rustlens-config` (or a local module) mirroring the Go defaults.

---

## 10. Open decisions for review

- **D-P1 — build all sub-phases (A–E) this pass, or stop after a subset?** The
  service is ~23 routes; A–E is a lot. *Recommend:* build A→E sequentially (one PR
  each), pausing only if a decision arises; hold **P2-F (infra cutover)** for
  explicit user sign-off (outward-facing).
- **D-P2 — store queries in `rustlens-db` vs in the service.** *Recommend
  `rustlens-db`* (consistent with D3 / existing modules; reusable by future
  services that read player data).
- **D-P3 — `user_guilds` write routes.** *Recommend excluded* from players (guilds
  domain; identity already writes `user_guilds`). Players only *reads* it for deep
  game-bans.
- **D-P4 — singleflight impl.** *Recommend* an in-`AppState`
  `Mutex<HashSet<String>>` insert-guard (no extra crate) over pulling in
  `async-singleflight`.
- **D-P5 — bot-internal path reorg (`/api/internal/bot/players/*`).** Defer the
  rename to **P2-F** (cutover), keeping the flat Go paths until the bot client is
  updated in lockstep (roadmap §3).
