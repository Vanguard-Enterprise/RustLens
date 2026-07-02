# RustLens Rust Migration — Phase 3: `servers` Service Spec

Status: **DRAFT — awaiting review before implementation.**
Author: migration work, 2026-07-02
Branch base: `rework/rust-backend`

Phase 3 (roadmap §4) builds the **`servers` service** — tracked BattleMetrics
servers, live player presence, server analytics, and the outbound-proxy infra.
It is the first Rust service with **background workers**: the BattleMetrics
rolling poller and the daily server-analytics refresher. It consumes the Phase 1
crates (`rustlens-battlemetrics`, `rustlens-outbound`, `rustlens-httpauth`,
`rustlens-botjwt`, `rustlens-crypto`, `rustlens-db`).

Assembled from a fan-out read of the Go backend; file:line refs point at the
source of truth. Where a JSON shape / SQL / algorithm is a contract, it's quoted
so the port is byte-for-byte.

## Ground rules (carried)

- Same Postgres, no schema changes, no migrations (Go owns `EnsureSchema`).
- Cross-language JSON contracts stay byte-for-byte.
- `cargo clippy --all-targets` warning-clean; parameterized queries only.
- One PR per slice onto `rework/rust-backend`, merged then branch-pruned.

---

## 1. Ownership & boundaries

**Owned (sole writer) by `servers`:** `tracked_servers`, `guild_tracked_servers`,
`player_states`, `server_player_count_history`(+`_fetches`), `outbound_proxies`,
`track_analytics_sessions_cache` (write during transition; players reads+upserts
it for offline-analytics — a shared write already accepted in Phase 2).

**Read/write cross-domain (owned elsewhere):**
- `bm_profile_cache` — the poller's `SyncPlayerStates` **writes** a name-only
  upsert (`updated_at='epoch'` + `IS DISTINCT FROM` guard) as a side effect.
  Owned by **players**; servers writes names during presence sync (matches Go).
- `steam_accounts` / `guild_steam_discord_links` — **read** by online-summary,
  log-events, and player-states search (joins). Owned by players.
- `app_users` / admin RBAC — read by the admin `bm-servers`/`proxies` permission
  checks. Owned by identity.

**Not in this service:**
- `/api/internal/bot/setup/steam-accounts/count` — reads a `cached_counts` TTL row
  over `steam_accounts`; belongs to the **players/accounts** domain (stays on Go
  or lands in players later). **D-S4.**
- Track-group analytics (`.../track-groups/{id}/analytics`) — guilds domain
  (Phase 4), calls servers for the session cache.

---

## 2. Service skeleton

New binary `apps/rust/services/servers` (mirrors `identity`/`players`): `main.rs`
(config → pool → clients → provider → **spawn workers** → router → serve),
`state.rs`, handler modules, worker modules. Default `PORT` 8092.

**AppState holds:** `pool`, `config: ServersConfig`, `bm:
rustlens_battlemetrics::Client` (proxied), `outbound: Arc<rustlens_outbound::Provider>`
(so admin/owner proxy mutations can call `replace()` to live-reload), `cipher:
Option<rustlens_crypto::Cipher>` (for proxy encrypt/decrypt), `redis:
Option<redis::aio::MultiplexedConnection>` (worker locks).

**Auth:** bot-internal routes → `rustlens_botjwt` Bearer gate (mirror identity's
`bot.rs`); admin routes → `rustlens_httpauth` app-secret + session + per-permission;
owner proxy routes → bot-JWT only (**no** permission gate — matches Go).

**External-client boundary:** BattleMetrics → proxied via `rustlens-outbound`.
No other external clients in this service.

---

## 3. Store schema (verbatim; Go owns it — read/write only)

`tracked_servers` (`postgres.go:427-440`):
```sql
CREATE TABLE tracked_servers (
    bm_id TEXT PRIMARY KEY, name TEXT NOT NULL,
    is_vitality BOOL NOT NULL DEFAULT FALSE, is_atlas BOOL NOT NULL DEFAULT FALSE,
    is_vital BOOL NOT NULL DEFAULT FALSE, is_modded BOOL NOT NULL DEFAULT FALSE,
    is_official BOOL NOT NULL DEFAULT FALSE, is_build BOOL NOT NULL DEFAULT FALSE,
    high_frequency BOOL NOT NULL DEFAULT FALSE, last_polled_at TIMESTAMPTZ,
    last_poll_error TEXT NOT NULL DEFAULT '',
    empty_player_poll_count INTEGER NOT NULL DEFAULT 0
);
```

`guild_tracked_servers` (`postgres.go:525-533`): `guild_id TEXT, server_bm_id TEXT
REFERENCES tracked_servers(bm_id) ON DELETE CASCADE, created_at`. PK `(guild_id,
server_bm_id)`. Index `(guild_id)`.

`player_states` (`postgres.go:506-520`): `server_bm_id TEXT REFERENCES
tracked_servers(bm_id) ON DELETE CASCADE, player_bm_id TEXT, is_online BOOL,
online_since TIMESTAMPTZ, offline_since TIMESTAMPTZ, updated_at`. PK `(server_bm_id,
player_bm_id)`. Indexes `(player_bm_id)` and `(server_bm_id, is_online,
online_since DESC, updated_at DESC)`.

`server_player_count_history` (`postgres.go:694-707`): `server_bm_id TEXT
REFERENCES tracked_servers(bm_id) ON DELETE CASCADE, timestamp_unix BIGINT, min
INTEGER, value DOUBLE PRECISION, max INTEGER, observed_at`. PK `(server_bm_id,
timestamp_unix)`.
`server_player_count_history_fetches`: `server_bm_id TEXT PK REFERENCES
tracked_servers(bm_id) ON DELETE CASCADE, fetched_at`.

`outbound_proxies` (`postgres.go:454-504`): `id BIGSERIAL PK, redacted_endpoint
TEXT (UNIQUE idx), endpoint_ciphertext BYTEA, endpoint_nonce BYTEA, key_id TEXT
DEFAULT 'default', enabled BOOL DEFAULT TRUE, created_at, updated_at,
auto_disabled_at TIMESTAMPTZ (migration-only, nullable)`. CHECK
`octet_length(ct)>0 AND octet_length(nonce)>0`.

`rustlens-db` gets new modules: `tracked_servers` (full struct+CRUD+rename-remap),
`player_states` (servers-side sync + reads + online-summary + log-events),
`server_analytics` (history cache); and extends `outbound_proxies` (Phase 1 added
`list_enabled` + `disable_by_redacted_endpoint`; add the rest). **D-S2.**

---

## 4. Slice S-A — tracked-server CRUD (bot + guild + admin bm-servers)

### 4.1 Bot-internal (JWT) — `tracked_servers*`
| Route | Method | Notes |
|---|---|---|
| `/tracked-servers` | GET | `{items:[trackedServer]}` |
| `/tracked-servers` | POST | create; name whitespace-collapsed; `409 tracked server already exists` |
| `/tracked-servers/{bmID}` | PUT | update incl. **bmId rename** (§4.3); `404`/`409` |
| `/tracked-servers/{bmID}` | DELETE | `{status:deleted}`; `404`; FK-cascades player_states/guild_tracked/history |
| `/guilds/{guildID}/tracked-servers` | GET | `{items:[{guildId,bmId,name}]}` |
| `/guilds/{guildID}/tracked-servers` | PUT | replace `{bmIds:[]}` (≤100, must exist, dedup) → `{items}` |
| `/guilds/{guildID}/tracked-servers/{bmID}` | POST | add; `{status:enabled}`; `404`/`409 already enabled` |
| `/guilds/{guildID}/tracked-servers/{bmID}` | DELETE | remove; `{status:disabled}`; `404 not enabled` |

`trackedServerToMap`: `{bmId,name,isVitality,isAtlas,isVital,isModded,isOfficial,
isBuild,highFrequency, lastPolledAt (RFC3339 or **null**), lastPollError}`.

### 4.2 Admin (app-secret+session+perm) — `/api/app/admin/bm-servers*`
| Route | Method | Perm |
|---|---|---|
| `/admin/bm-servers` | GET | `bm_servers.read` — `{items,total,page,pageSize}`; filters q/highFrequency/hasError/is_*, sortBy(name\|bmId\|highFrequency\|lastPolledAt\|pollStatus), sortOrder, page/pageSize(≤200) |
| `/admin/bm-servers` | POST | `bm_servers.edit` — validate bmId numeric ≤64, name ≤200; `409`; audit `bm_servers.create` |
| `/admin/bm-servers/{bmID}` | PUT | `bm_servers.edit` — rename; audit `bm_servers.update` |
| `/admin/bm-servers/bulk` | POST | `bm_servers.edit` — `{action(set_high_freq\|set_standard\|delete),bmIds[1..200],reason?}` → `{action,succeeded,failed,results:[{bmId,ok,error?}]}`; per-item audit |
| `/admin/bm-servers/{bmID}` | DELETE | `bm_servers.edit` — audit `bm_servers.delete` |

Admin item map: same fields but **omits `lastPolledAt` when null** (bot map emits
`null`); no `pollStatus` field (derived client-side; only a sort key). Perm check
inside handler via actor-from-session/api-key; `403 permission_denied`.

### 4.3 The rename-remap transaction (`UpdateTrackedServer`, `tracked_servers.go:252-434`)
Same-id → single `UPDATE ... RETURNING`. **Different id** → one tx:
1. `SELECT` old row (carry `last_polled_at`/`last_poll_error`); missing → NotFound.
2. `INSERT` new row (new bm_id, request fields, **carried** poll metadata); `23505`→AlreadyExists.
3-8. Repoint `guild_tracked_servers`, `player_states`, `track_groups`,
   `server_player_count_history`, `server_player_count_history_fetches`,
   **`track_analytics_sessions_cache`** (no FK → must be manual) — all `UPDATE ...
   SET server_bm_id=$new WHERE server_bm_id=$old`.
9. `DELETE` old row. 10. commit. Order matters (new row before children; old row last).

### 4.4 Store (rustlens-db::tracked_servers)
`TrackedServer{bm_id,name,is_*×6,high_frequency,last_polled_at:Option,last_poll_error}`.
`list`, `list_page(filters/sort/pagination + count)`, `create`, `update` (both
branches), `delete`, `get`, guild: `list_guild`, `replace_guild`, `add_guild`,
`remove_guild`. + the poll helpers for S-C: `list_to_poll(high_frequency, limit)`,
`record_poll_result`, `increment_empty_polls`, `reset_empty_polls`,
`set_high_frequency` (bulk). Exact SQL in the tracked-servers extraction.

---

## 5. Slice S-B — player-states reads + presence store

### 5.1 Routes (bot JWT)
| Route | Method | Notes |
|---|---|---|
| `/player-states/servers/{serverID}` | GET | online rows only; `{items:[playerStateMap]}` |
| `/player-states/servers/{serverID}/search` | GET | `?q=(req)&limit=(def 20, **cap 21**)&includeOffline=`; ILIKE `bm_profile_cache.bm_name`, INNER join `steam_accounts`; `{items:[{serverBmId,serverName,playerBmId,steamId,bmName,isOnline}]}` |
| `/player-states/servers/{serverID}/players/{playerID}` | GET | single map; `404 player state not found` |
| `/player-states/players/{playerID}` | GET | all servers; reuses `rustlens_db::players_steam::list_player_states_by_player` (already ported) |
| `/guilds/{guildID}/online-summary` | GET | bot-wide `tracked_servers`; ROW_NUMBER dedup 1/discord; `{items:[{guildId,discordId,steamId,playerBmId,bmName,serverBmId,serverName,onlineUnix}]}` |
| `/guilds/{guildID}/tracked-server-log-events` | GET | guild-scoped `guild_tracked_servers` + `guild_steam_discord_links`; incl. offline; `{items:[{serverBmId,serverName,playerBmId,steamId,discordId,bmName,avatarImage,isOnline,occurredUnix}]}` |

`playerStateMap`: `{serverBmId,serverName,playerBmId,isOnline,updatedAt,onlineSince
(or null),offlineSince (or null)}`. Exact SQL for all in the player-states extraction.

### 5.2 Presence store (rustlens-db::player_states)
- `sync_player_states(server_bm_id, players:[{player_bm_id,bm_name}], observed_at)`
  — the §6.2 diff (one tx, `FOR UPDATE`, never delete, `bm_profile_cache` name
  upsert). **Used by the poller (S-C).**
- `count_online(server_bm_id)`; the read queries above.

---

## 6. Slice S-C — the BattleMetrics rolling poller (background worker)

### 6.1 Loop + locking (**D-S1**)
Interval `BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS` (default 30). Go uses a
**Redis SetNX two-key scheme**, NOT a pg advisory lock:
- **cadence key** `rustlens:battlemetrics:poller:cadence`, `SetNX` TTL =
  `interval - max(interval/10, 250ms)`, never released (self-expires) — throttles
  to one cycle/interval across pods.
- **lock key** `rustlens:battlemetrics:poller:lock`, `SetNX` TTL =
  `max(75s, interval+30s)`, released at cycle end via a Lua CAS-del (`if get==owner
  then del`). Owner = `{host}:{pid}`.
Each cycle: reserve cadence (skip if taken) → acquire lock (skip if taken) → run →
release lock. **Recommend replicating the Redis scheme faithfully** (preserves the
cadence throttle + multi-pod safety); the alternative (pg advisory lock) loses the
throttle. The servers service adds a Redis `MultiplexedConnection` + a small
lock helper (`SET NX PX` + `EVAL` release).

### 6.2 Per-cycle selection (2 high + 1 low, backfill)
`list_to_poll(true, 3)` + `list_to_poll(false, 3)` (each ORDER BY `last_polled_at
NULLS FIRST, bm_id ASC LIMIT 3`). Take first 2 high + first 1 low; if <3, backfill
from remaining high (index 2+) then remaining low (index 1+). → both groups: 2H+1L;
high-only: 3H; low-only: 3L. Poll the ≤3 concurrently (one task each; proxy
rotation via the outbound provider). Per-request timeout **15s**.

### 6.3 pollServer
`fetch_server_players(bm_id)` (15s). On success:
- Build `[{player_bm_id, bm_name}]`. If **empty** and `count_online>0`:
  `increment_empty_polls` → if `< 3` **skip** sync (debounce); at 3 → sync (mass
  offline). Else sync normally.
- `sync_player_states(bm_id, inputs, observed_at)` (30s). `observed_at =
  cycle-start UTC`, reused for all timestamps.
- If synced (or non-empty): `reset_empty_polls`.
Always (success or error): `record_poll_result(bm_id, observed_at, err_or_"")`
(advances `last_polled_at` even on failure → round-robin).

### 6.4 `sync_player_states` diff (byte-exact, `player_states.go:182-293`)
One tx. `SELECT player_bm_id, is_online ... FOR UPDATE` into a map. For each fetched
player (skip empty id): mark `seen`; if `bm_name!=""` upsert `bm_profile_cache`
(`updated_at='epoch'`, `ON CONFLICT DO UPDATE SET bm_name WHERE bm_name IS DISTINCT
FROM`); then branch: **new** → INSERT `(TRUE, observed_at, NULL, observed_at)`;
**already online** → `UPDATE SET updated_at=observed_at` (preserve `online_since`);
**offline→online** → `UPDATE SET is_online=TRUE, online_since=observed_at,
offline_since=NULL, updated_at=observed_at`. Then for each existing online row NOT
seen → `UPDATE SET is_online=FALSE, offline_since=observed_at,
updated_at=observed_at`. Absent+already-offline skipped. Never delete. Commit.

---

## 7. Slice S-D — server-analytics (route + daily refresher)

### 7.1 player-count-history route (bot JWT)
`GET /tracked-servers/{bmID}/player-count-history?timeframe=7d|14d|30d|60d|90d`.
`bmID` `^\d{1,16}$`; timeframe→days map; `get_tracked_server` (bot-global) else
`404`. Cache: `get_server_player_count_history(bmID)` — fresh iff `fetched_at >=
today 00:00 UTC` → serve; else `fetch_server_player_count_history(bmID,
cacheStart, cacheStop)` (90d via `CompletedDayRange`) → `upsert` → re-read; on BM
error serve stale, else `PlayerNotFound→404` / other→`502`. Trend = points filtered
to the selected timeframe (inclusive unix bounds). Weekday distribution computed
**in Rust over ALL points** (not SQL): bucket by `mondayZero(UTC weekday)`, average
min/value/max, `roundTo2`, 7 fixed Monday→Sunday entries. Response
`serverAnalyticsResponse{serverBmId,serverName,timeframe,start,stop,points:[{timestampUnix,
timestamp(RFC3339),min,value(roundTo2),max}],weekdays:[{weekday,min,value,max}],
cachedAt,cacheWindow}`.

### 7.2 CompletedDayRange (`range.go`)
`days>=1`; `yesterday = today-midnight-UTC - 1 day`; `start = (yesterday-(days-1))
at 00:00:00 UTC` (inclusive); `stop = yesterday at 23:59:59 UTC` (secs=59, nanos=0,
inclusive). Reuse `rustlens_battlemetrics::fetch_server_player_count_history`
(already ported).

### 7.3 Daily refresher (background worker)
Schedule **00:01 UTC** (compute next via day math; `tokio` timer). Per run:
Redis lock `rustlens:server-analytics:refresh:lock` (`SetNX` TTL **23h**, Lua
release, owner `{host}:{pid}`). List `tracked_servers`; `CompletedDayRange(now,
90)`; per server `fetch_server_player_count_history` → convert → `upsert` (history
+ fetch record, one tx); **wait 30s between servers**. Tables + SQL in the
server-analytics extraction. No dedicated env var.

---

## 8. Slice S-E — admin bm-servers audit + proxies CRUD + provider reload

(bm-servers routes are in S-A; their **audit** wiring can land here or with S-A.)

### 8.1 Proxy routes
- **Owner (bot JWT only, no perm)** `/api/internal/bot/owner/proxies*`: GET list
  `{items:[{id,endpoint(redacted),enabled}]}`; POST create `{endpoint}` → `201`;
  PATCH `{enabled}`; DELETE. `503` when cipher absent; `409 proxy already exists`;
  `400 proxyID must be a positive integer`; reload-after-commit (`500 …but reload
  failed`).
- **Admin (app-secret+session+perm)** `/api/app/admin/proxies*`: GET list
  (`proxies.read`, page/pageSize/q/status(active|paused|broken)) →
  `{items:[{id,endpoint,enabled,status,createdAt,updatedAt,autoDisabledAt?}],
  total,page,pageSize}`; POST create / PATCH `{enabled}` / DELETE / POST bulk
  `{action(enable|disable|delete|clear_broken),ids[1..200],enabled?,reason?}` (all
  `proxies.edit`); audit `proxies.{create,update,delete,bulk.*}`. `proxies.rotate_credentials`
  is **reserved only** — no route.

### 8.2 Store (extend rustlens-db::outbound_proxies)
Add: `create(redacted, ct, nonce, key_id) -> Row|AlreadyExists`, `delete(id)`,
`set_enabled(id, enabled)` (clears `auto_disabled_at` when enabling), `clear_auto_disabled(id)`,
`list_all` (ORDER BY `(auto_disabled_at IS NOT NULL) DESC, enabled ASC, id ASC`),
`list_page(q, status, limit, offset)` + count. Have `list_enabled` +
`disable_by_redacted_endpoint` from Phase 1.

### 8.3 validate + redact + reload + crypto
`validate_proxy_endpoint` (scheme http/https, host+port mandatory, user+pass
mandatory) → `(canonical, redacted)`. `redact_proxy_endpoint` already in
`rustlens-outbound`. **Reload:** `decrypt enabled → provider.replace(true,
endpoints)`; when 0 enabled DB rows → `provider.replace(PROXY_ENABLED,
PROXY_ENDPOINTS)` (legacy fallback). Every create/update/delete/bulk reloads
after the DB write (bulk once after the batch). Crypto = `rustlens-crypto` (base64
32-byte key, 12-byte nonce, nil AAD — byte-compatible). `key_id` always `"default"`.
`proxyStatus`: `auto_disabled_at!=null→broken` > `enabled→active` > `paused`.

Note: players **also** builds its own provider from `outbound_proxies` at startup;
only the servers service live-reloads on mutation. Players picks up changes on
restart (acceptable during transition).

---

## 9. Phasing (one PR each onto `rework`)

- **S-A** — skeleton + `rustlens-db::tracked_servers` + bot tracked-server & guild
  routes. (Admin bm-servers can fold in here or into S-E with audit.)
- **S-B** — `rustlens-db::player_states` (sync + reads) + the 6 presence routes.
- **S-C** — the **poller** worker (Redis locks + selection + sync + debounce +
  poll-result). Depends on S-A + S-B.
- **S-D** — server-analytics: history route + `rustlens-db::server_analytics` +
  the **daily refresher** worker.
- **S-E** — admin bm-servers (audit) + owner/admin **proxies CRUD** + provider
  live-reload (extend `rustlens-db::outbound_proxies`).
- **S-F (held)** — infra cutover: Dockerfile, CI matrix, compose (incl. the two
  workers as one process), Traefik routing → `:8092`, `/api/internal/bot/servers/*`
  + `/players/*` bot-path reorg (bot client in lockstep), delete migrated Go
  handlers + the Go poller/refresher. **Outward-facing — confirm first.**

Each slice: build + clippy --all-targets clean + test; DB-gated integration tests
(throwaway PG) + live HTTP smokes; the workers get unit tests for the pure logic
(selection algorithm, sync diff, CompletedDayRange, weekday aggregation) + a
DB-gated worker smoke (seed → run one cycle → assert player_states/history).

---

## 10. Config / env (servers service)
`PORT` (8092), `POSTGRES_URL`, `REDIS_URL`, `APP_API_INTERNAL_SECRET`,
`APP_SESSION_SECRET`, `BOT_SERVICE_JWT_SECRET`(+issuer/audience),
`BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS` (30),
`PROXY_ENABLED`/`PROXY_ENDPOINTS`/`PROXY_ENCRYPTION_KEY`. Add a `ServersConfig` to
`rustlens-config`.

---

## 11. Open decisions for review

- **D-S1 — poller/refresher locking.** Replicate the Go **Redis SetNX** scheme
  (cadence+lock for the poller, single 23h lock for the refresher) vs a pg advisory
  lock. *Recommend: replicate Redis* (faithful; preserves the cadence throttle;
  multi-pod-safe). Adds a Redis conn + a `SET NX PX`/`EVAL`-release helper.
- **D-S2 — store queries in `rustlens-db`.** *Recommend yes* (D-P2 precedent).
- **D-S3 — worker scheduling.** `tokio` interval/timer + the Redis locks, mirroring
  Go's ticker. *Recommend.*
- **D-S4 — `steam-accounts/count` excluded** (players/accounts domain). *Recommend.*
- **D-S5 — `bm_profile_cache` name-write in `sync_player_states`.** Keep it (matches
  Go; players reads it). *Recommend.*
- **D-S6 — build cadence.** Same as Phase 2 — **build S-A→S-E, report once**;
  S-F held for sign-off. *Confirm.*
