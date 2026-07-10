# RustLens Rust Migration — Phase 1 Foundation Crates Spec

> Historical: the migration completed on `rework/rust-backend`; see `rust-go-removal-plan.md`.

Status: **✅ COMPLETE** — all four crates built, tested, and merged to
`rework/rust-backend`.
Author: migration work, 2026-07-01
Branch base: `rework/rust-backend`

## Completion summary

| Crate | PR | Status |
|---|---|---|
| `rustlens-httpauth` | #185 | ✅ merged — factored the `/api/app/*` gate out of identity; identity re-runs its auth smokes 10/10 |
| `rustlens-outbound` | #186 | ✅ merged — proxy provider (rotation, health window, auto-disable); 10 unit + DB-gated integ test |
| `rustlens-battlemetrics` | #187 | ✅ merged — unauth JSON:API client via `OutboundSender`; 12 tests (mock sender) |
| `rustlens-steam` | #188 | ✅ merged — steamapi + steammatch Chain + bmsearch hybrid + bmmatch + platformsync; 26 tests (wiremock) |

The four design decisions (D1–D4) were taken as recommended. `Cargo.toml`
workspace now has 10 crates + `services/identity`; the whole workspace is
`cargo clippy --all-targets` clean and `cargo test` green. Next: roadmap Phase 2
(`players` service), which consumes `rustlens-steam` + `rustlens-battlemetrics`
and adds the hybrid inline-timeout + singleflight backfill + `steam_accounts`
persist orchestration (spec D4).

Phase 1 of the Go→Rust migration (roadmap §4) builds the shared library crates
every later domain service (`players`, `servers`, `guilds`) depends on. This spec
captures, per crate, the exact Go source to mirror, the request/response
contracts, config/env, the Rust crate boundary + dependencies, and how each will
be verified. It was assembled from a fan-out read of the Go backend; file:line
refs point at the source of truth.

## Ground rules (carried from Phase 0)

- **Same Postgres, no schema changes, no migrations.** Go owns `EnsureSchema()`.
  Rust reads/writes existing tables only.
- **Cross-language contracts stay byte-for-byte:** `rustlens_session` cookie, bot
  JWT, AES-GCM tokens (`rustlens-crypto`), and every JSON response shape.
- `cargo clippy --all-targets` warning-clean; parameterized queries only.
- One PR per crate onto `rework/rust-backend`, merged then branch-pruned.

## Build order & dependency graph

```
rustlens-httpauth   (axum + rustlens-session + rustlens-db)          ← do first (identity reuse)
rustlens-outbound   (reqwest + rustlens-crypto + rustlens-db)        ← BM depends on it
   └── rustlens-battlemetrics (reqwest + serde + rustlens-outbound)  ← consumes outbound
rustlens-steam      (reqwest + serde; independent)                   ← standalone
```

`rustlens-battlemetrics` and `rustlens-steam` have **no consuming service until
Phase 2** (`players`/`servers`), so their verification is unit + parity tests
(fixtures / recorded JSON), not end-to-end smokes. `rustlens-httpauth` is proven
by re-running the identity smokes after the refactor.

Existing crates reused: `rustlens-session`, `rustlens-config`, `rustlens-db`,
`rustlens-events`, `rustlens-crypto`, `rustlens-botjwt`.

---

## Crate 1 — `rustlens-httpauth` (HIGHEST VALUE, do first)

Factor the `/api/app/*` gate + cookie-session extractor **out of**
`apps/rust/services/identity/src/appauth.rs` into a reusable crate, then refactor
identity to depend on it. This is a **pure refactor** — the Rust code already
exists and is a faithful port of the Go middleware; the goal is reuse across
future app-facing services, not behavior change.

### 1.1 Go source of truth (parity check)

| Go file | Purpose |
|---|---|
| `internal/appauth/middleware.go` | `RequireAppSecret`, `RequireSession`, `SessionFromContext`, cookie helpers |
| `internal/appauth/session.go` | `ParseSession`, `ValidateSession`, `ValidateSessionFromCookie`, epoch revocation |
| `internal/http/router.go:88-117` | `requireAdminPerm` (permission gate) |
| `internal/http/events_handlers.go` | `classifySessionError` (cookie/SSE error codes) |
| `internal/appauth/api_key_middleware.go` | API-key auth — **deferred, not ported** (see 1.5) |

### 1.2 Public API to expose

Mirror the existing `appauth.rs` functions, but **decoupled from identity's
`AppState`** so any service can call them. Take the primitives (`&PgPool`,
`&str` secrets) instead of `&AppState`:

```rust
pub const APP_SECRET_HEADER: &str = "x-rustlens-app-secret";
pub const SESSION_HEADER: &str    = "x-rustlens-session";

pub fn require_app_secret(headers: &HeaderMap, expected: &str) -> Result<(), Box<Response>>;
pub async fn require_session(headers: &HeaderMap, pool: &PgPool, session_secret: &str)
    -> Result<SessionUser, Box<Response>>;
pub async fn validate_session_cookie(headers: &HeaderMap, pool: &PgPool, session_secret: &str)
    -> Result<SessionUser, (StatusCode, &'static str)>;
pub async fn require_permission(pool: &PgPool, user_id: &str, perm: &str) -> Result<(), Box<Response>>;
pub async fn require_admin(headers: &HeaderMap, pool: &PgPool, app_secret: &str, session_secret: &str, perm: &str)
    -> Result<SessionUser, Box<Response>>;
```

Identity's thin wrappers (`appauth.rs`) then just forward from `AppState`:
`require_session(headers, &st.pool, &st.config.app_session_secret)`, etc. A cookie
read helper is needed — either move `cookies::read_cookie` in, or have the crate
take the raw cookie header. **Decision D1 (below).**

### 1.3 Error-code parity checklist (must match Go exactly)

| Path | Failure | Code | HTTP |
|---|---|---|---|
| app-secret | missing/mismatch | `app_secret_required` | 401 |
| session (header) | missing header | `session_required` | 401 |
| session (header) | epoch/suspended/deleted/missing-user | `session_revoked` | 401 |
| session (header) | bad sig / malformed / **expired** | `invalid_session` | 401 |
| session (**cookie**/SSE) | missing cookie | `session_required` | 401 |
| session (cookie/SSE) | **expired** | `session_expired` | 401 |
| session (cookie/SSE) | revoked | `session_revoked` | 401 |
| session (cookie/SSE) | other | `invalid_session` | 401 |
| permission | denied | `forbidden` | 403 |
| permission | check error | `permission_check_failed` | 500 |

**Key nuance confirmed in Go:** the **header** path collapses expired→`invalid_session`;
the **cookie** path distinguishes `session_expired`. The current Rust already does
this — preserve it. Constant-time secret comparison (Go uses `subtle.ConstantTimeCompare`;
Rust uses a hand-rolled `ct_eq` — keep or switch to the `subtle` crate).

### 1.4 Revocation check

`ValidateSession` reads `SELECT session_epoch, suspended_at, deleted_at FROM
app_users WHERE id=$1`; missing user OR `epoch < current` OR suspended OR deleted
⇒ revoked. Already implemented as `rustlens_db::identity::revocation_check` — the
crate reuses it (dependency on `rustlens-db`).

### 1.5 Known gaps vs Go (intentional, matches identity's deferrals)

- **API-key auth** (`Authorization: Bearer rl_admin_*`): not ported. Go bypasses
  the app-secret + session gate when an API-key context is present, and
  `requireAdminPerm` resolves the actor from either the API-key or session
  context. Identity already deferred this (spec §8.2). The crate stays
  session+app-secret only; API-key support is a future add that slots in as an
  alternate actor resolver. Documented, not blocking.
- **Context attachment:** Go attaches `*SessionUser` to the request context;
  Rust returns it from `require_session` and the handler threads it. No change.
- **`permission_denied` vs `forbidden`:** the Go *action dispatcher* returns
  `permission_denied`; the *route middleware* returns `forbidden`. The crate's
  `require_permission` mirrors the **middleware** (`forbidden`); identity's
  dispatcher keeps its own `permission_denied` string. No conflict.

### 1.6 Verification

- `cargo build` / `cargo clippy --all-targets` / `cargo test` from `apps/rust`.
- Refactor identity to depend on the crate; **re-run the identity smokes**:
  header-auth `/me` (401 no-secret → 401 no-session → 200), admin gating
  (401/401/403 + 200 with perm), SSE cookie auth (401 no-cookie, `text/event-stream`).
  Behavior must be byte-identical to pre-refactor.
- Unit tests move with the code (the existing appauth unit coverage, if any).

---

## Crate 2 — `rustlens-outbound`

Port `apps/backend/internal/outboundhttp/`: the proxy provider (round-robin
rotation, per-proxy 10-min health window, auto-disable), reading `outbound_proxies`
(AES-GCM via `rustlens-crypto`) with a legacy `PROXY_ENDPOINTS` fallback. Exposes
the HTTP-send abstraction BM requests use.

### 2.1 Go source of truth

| Go file | Purpose |
|---|---|
| `internal/outboundhttp/provider.go` | Provider struct, rotation, health window, auto-disable, `Prepare()`, `Replace()`, `RedactProxyEndpoint()` |
| `internal/outboundhttp/metrics.go` | otel counter `rustlens.outbound.auto_disabled_total` (skip/stub) |
| `internal/outboundhttp/tracing.go` | `WithTarget(ctx, name)` span tag (skip/stub) |
| `internal/store/outbound_proxies.go` | `outbound_proxies` CRUD (encrypted) |
| `internal/proxycrypto/crypto.go` | AES-GCM — already mirrored by `rustlens-crypto` |
| `cmd/server/main.go:242-266` | `initialProxyEndpoints` (DB-preferred, CSV fallback) + auto-disable callback |
| `internal/http/owner_proxies_handler.go` | `validateProxyEndpoint`, `reloadOutboundProvider` |

### 2.2 Rotation algorithm (exact)

- Round-robin via an atomic `u64` counter: `idx = next.fetch_add(1); proxies[idx % len]`.
- State guarded by an `RwLock` (reads on the hot `prepare` path; writes on record/replace).
- `prepare()` returns `(client, endpoint, proxied: bool)`; when disabled or empty
  → `(direct_client, "", false)`.

### 2.3 Health window (exact numbers — must match)

Constants: `PROXY_HEALTH_WINDOW = 10 min`, `PROXY_HEALTH_MIN_REQUESTS = 10`.

Per redacted endpoint, keep a `Vec<(Instant, success: bool)>`. On each recorded
result: append, drop entries older than 10 min (`at.Before(cutoff)`), count
failures over the kept window. **Auto-disable when**
`kept.len() >= 10 && failures*2 >= kept.len() && !already_disabled`. On disable:
remove the proxy from the active list, mark disabled, fire the callback async
(spawn), record the metric.

**Success classification** (per request):
`err.is_none() && response.is_some() && status < 500 && status != 429`.
Anything else (transport error, ≥500, or 429) = failure.

Re-enable is **only** via DB (`SetOutboundProxyEnabled(id,true)` /
`ClearOutboundProxyAutoDisabled(id)` clears `auto_disabled_at`) followed by a
reload — never automatic.

### 2.4 The HTTP-send abstraction — **key Rust design decision (D2)**

Go returns an `*http.Client` whose transport is a stats-recording `RoundTripper`
wrapping a proxy transport; the caller does `client.Do(req)` and the transport
feeds `recordProxyResult` automatically. `reqwest` has no equally clean
transport-injection point, so two options:

- **D2a (recommended): a `send` method on the provider.** Expose
  `async fn send(&self, req: reqwest::Request) -> reqwest::Result<Response>` that
  internally: picks a proxy (or direct) → clones the pre-built `reqwest::Client`
  for that proxy → sends → classifies the result → calls `record_result`. BM's
  client depends on a small trait:
  ```rust
  #[async_trait] pub trait OutboundSender: Send + Sync {
      async fn send(&self, req: reqwest::Request) -> reqwest::Result<reqwest::Response>;
      fn proxy_count(&self) -> usize;
  }
  ```
  Clean, keeps health-recording inside the crate, mirrors the Go boundary
  (`requestPreparer` → BM client). Recommended.
- **D2b: `reqwest-middleware`.** Add the `reqwest-middleware` dep and implement a
  `Middleware` that records results. More faithful to the RoundTripper shape but
  adds a dependency and is heavier. Not recommended unless we want per-request
  middleware elsewhere.

Either way, one pre-built `reqwest::Client` per proxy (with `.proxy(Proxy::all(url))`)
plus one direct client, all with the provider timeout (default 20s for the BM
fallback; Go uses 20s when constructed nil). Proxy URL carries credentials; only
the redacted `scheme://host:port` is logged / used as the health-map key.

### 2.5 Proxy sourcing & live reload

- `initial_endpoints(pool, cipher, cfg)`: `ListEnabledOutboundProxies` → if any,
  **decrypt each** (`rustlens-crypto`, requires key) and use them; if none, fall
  back to `PROXY_ENDPOINTS` CSV **only when `PROXY_ENABLED`**; else empty/direct.
- `replace(enabled, endpoints)`: validate/build all clients first (fail without
  mutating), then atomically swap `enabled` + `proxies`, **clearing** the health
  map, disabled set, and resetting the counter (matches Go `Replace`).
- Auto-disable callback: `DisableOutboundProxyByRedactedEndpoint` → reload from DB
  → `replace`. The crate exposes the provider + a reload hook; the *servers*
  service (Phase 3) owns `outbound_proxies` writes and wires create/update/delete
  → reload. Phase 1 just ships the provider + reload primitive.

### 2.6 `outbound_proxies` table (read-only in Phase 1)

`id BIGSERIAL PK, redacted_endpoint TEXT UNIQUE, endpoint_ciphertext BYTEA,
endpoint_nonce BYTEA, key_id TEXT DEFAULT 'default', enabled BOOL DEFAULT TRUE,
created_at, updated_at, auto_disabled_at TIMESTAMPTZ NULL`. Phase 1 only needs
`ListEnabledOutboundProxies` (+ decrypt) and `DisableOutboundProxyByRedactedEndpoint`
for the callback; full CRUD lands with the `servers` service. Add these to
`rustlens-db` (new `outbound_proxies` module) or keep them in the crate —
**Decision D3** (recommend `rustlens-db`, consistent with existing query-module layout).

### 2.7 Env

| Var | Type | Default |
|---|---|---|
| `PROXY_ENABLED` | bool | `false` |
| `PROXY_ENDPOINTS` | CSV | `""` (legacy fallback) |
| `PROXY_ENCRYPTION_KEY` | base64 32-byte | `""` (required for DB proxies) |

### 2.8 Direct-vs-proxied boundary

**Proxied:** BattleMetrics only. **Always direct:** Steam, bmsearch, bmmatch,
platformsync, Discord. The crate is only wired into BM; the `rustlens-steam`
clients build their own direct `reqwest::Client`.

### 2.9 Verification

- Unit: round-robin sequence across N proxies; health window (feed 10 results,
  5 failures → disabled; 9 results not enough; results older than 10 min dropped);
  success classification (500/429 = failure; 200/404 = success); `replace` clears
  state; redaction strips credentials.
- Parity: AES-GCM round-trip already covered by `rustlens-crypto`; assert the
  disable threshold matches Go's `failures*2 >= len` exactly.
- No DB smoke required for the algorithm; a small DB-gated test can cover
  `ListEnabledOutboundProxies` decrypt if we add the query here.

---

## Crate 3 — `rustlens-battlemetrics`

Port `apps/backend/internal/battlemetrics/` (`client.go`, `sessions.go`,
`server_history.go`): the **unauthenticated** BM JSON:API client, routed through
`rustlens-outbound`.

### 3.1 Base URL & headers

- Base: `https://api.battlemetrics.com`.
- Every request: `Accept: application/json`, **no Authorization** (server-history
  explicitly deletes any auth header). No User-Agent set.
- Sends through `OutboundSender` (proxied). Numeric-ID validation `^\d+$` before
  every call. Caller supplies context/timeout (poller uses 15s).

### 3.2 Endpoints & methods (exact)

| Method | Request | Notes |
|---|---|---|
| `fetch_server_players(server_id)` | `GET /servers/{id}?include=player` | returns `Vec<Player{bm_id,name}>` from `included[type=player]` |
| `fetch_player_profile(player_id)` | `GET /players/{id}?include=server,identifier` | 404 → `ErrPlayerNotFound` sentinel |
| `fetch_player_sessions(player_bm_id, server_bm_id, page_size, max_pages)` | `GET /players/{id}/relationships/sessions?filter[servers]={srv}&page[size]={n}` | 404 → **empty vec, no error**; cursor paginate via `links.next` |
| `fetch_player_sessions_all_servers(player_bm_id, page_size, max_pages)` | same, **no** `filter[servers]` | |
| `fetch_server_player_count_history(server_id, start, stop)` | `GET /servers/{id}/player-count-history?start={rfc3339}&stop={rfc3339}` | validate `stop>start`; 404 → `ErrPlayerNotFound`; other non-2xx includes body (≤2048) |

Defaults: `page_size<=0 → 100`, `max_pages<=0 → 2`. The "up to 200 (2×100)"
behavior is the client loop (`for page in 0..max_pages while next != ""`), driven
by caller constants (`trackAnalyticsPageSize=100`, `trackAnalyticsMaxPages=2`).

### 3.3 Response structs to mirror (serde)

- **Server players:** `included[]{ type, id, attributes{ id, name } }` → filter
  `type=="player"`, id fallback to `attributes.id`, skip empty.
- **Player profile:** `data{ id, attributes{ name, createdAt: Option<DateTime> } }`
  + `included[]` where each item carries `type` (`"server"`|`"identifier"`),
  `attributes{ name, type, identifier, lastSeen?, details{ rust_type } }`,
  `relationships.game.data.id`, `meta{ timePlayed:i64, firstSeen?, lastSeen? }`.
  Parse → `PlayerProfile{ bm_id, steam_id, bm_name, created_at, servers[], name_identifiers[] }`
  where `PlayerServer{ server_id, server_name, time_played, first_seen_at,
  last_seen_at, game, rust_type }` and `PlayerNameIdentifier{ name, last_seen }`
  (name lastSeen falls back to `meta.lastSeen`). SteamID = first `identifier`
  with `type=="steamID"`.
- **Sessions:** `data[]{ type, attributes{ start: Option<DateTime>, stop:
  Option<DateTime> } }` + `links{ next }` → filter `type=="session"` →
  `Vec<PlayerSession{ start, stop }>`.
- **Player-count history:** `data[]{ type, attributes{ timestamp: DateTime,
  min:i32, value:f64, max:i32 } }` → filter `type=="dataPoint"` &&
  non-zero timestamp → `ServerPlayerCountPoint{ timestamp(utc), min, value:f64, max }`.
  **Note `value` is `f64`, not int.**

### 3.4 Error handling

Sentinel `ErrPlayerNotFound` (used by profile 404 and history 404). Sessions 404
→ empty. Other non-2xx → `battlemetrics returned status {n}` (history includes
body). JSON decode / request errors propagate. These are the raw client's
concern; playtime summing / category buckets / name-history persistence live in
the **consumer** (players/servers services), not here.

### 3.5 Verification

- Parse recorded/sample JSON:API fixtures for all five endpoints; assert struct
  fields, nullable handling, and `type` filtering match Go.
- Assert built request URLs/params/headers **exactly** (a mock `OutboundSender`
  captures the `reqwest::Request`: path, query order, `Accept`, no-auth).
- Sentinel behavior: 404 profile → `ErrPlayerNotFound`; 404 sessions → empty;
  `stop<=start` → range error.
- The `battlemetrics-api` skill is available for endpoint cross-reference, but the
  Go behavior is the contract.

---

## Crate 4 — `rustlens-steam`

Port the Steam↔BM identity stack: `internal/steamapi/` (Steam Web API),
`internal/steammatch/` (Resolver trait + Chain + Translating), `internal/bmsearch/`
(primary resolver, async RPC + hybrid), `internal/bmmatch/` (legacy fallback),
`internal/platformsync/` (identity discovery). **All direct (never proxied).**

### 4.1 `steamapi` — Steam Web API client

Base `https://api.steampowered.com`, direct `reqwest::Client` (10s timeout),
`STEAM_API_KEY` (empty allowed → `ErrSteamAPIKeyRequired` when a method needs it).
SteamID validation `^\d{17}$`.

| Method | Endpoint | Query |
|---|---|---|
| `get_player_summaries(id)` | `/ISteamUser/GetPlayerSummaries/v2/` | `key`, `steamids` → `PlayerSummary{ steamid, personaname, avatarfull, communityvisibilitystate:i32 }` |
| `get_owned_games(id)` | `/IPlayerService/GetOwnedGames/v1/` | `key, steamid, include_appinfo=true, include_played_free_games=true, format=json` → `OwnedGame{ appid:i32, name, playtime_forever:i64 (min) }` |
| `get_player_bans(ids)` | `/ISteamUser/GetPlayerBans/v1/` | `key, steamids` (CSV) → `PlayerBan{ SteamId(PascalCase), CommunityBanned, VACBanned, NumberOfVACBans, DaysSinceLastBan, NumberOfGameBans, EconomyBan }` |
| `get_friend_list(id)` | `/ISteamUser/GetFriendList/v1/` | `key, steamid, relationship=friend`; 401 → `ErrFriendListPrivate` |
| `resolve_vanity_url(vanity)` | `/ISteamUser/ResolveVanityURL/v1/` | `key, vanityurl` → resolved id |

`communityvisibilitystate == 1` (private) is the short-circuit the consumer uses
to skip `get_owned_games` (the flag is returned; the skip is caller logic).

### 4.2 `steammatch` — Resolver trait + Chain

```rust
#[async_trait] pub trait Resolver: Send + Sync {
    async fn steam_to_bm(&self, steam_id: &str) -> Result<String, MatchError>;
    async fn bm_to_steam(&self, bm_id: &str) -> Result<String, MatchError>;
}
pub enum MatchError { NotFound, NotConfigured, Pending, Other(anyhow::Error) }
```

Sentinels: `ErrNotFound`, `ErrNotConfigured`, `ErrPending`.
`FALLBACK_RESOLVER_TIMEOUT = 4s`.

**Chain fallthrough (exact, `steammatch.go:74-130`):** iterate resolvers; track
`pending` + `configured` flags.
- success (`ok && val != ""`) → return immediately (a fallback resolving after a
  prior `ErrPending` **wins** over pending).
- `ErrNotConfigured` → skip, don't mark configured.
- `ErrPending` → set `pending=true, configured=true`, continue. **If a subsequent
  resolver in the chain runs after a pending, give it a fresh bounded budget**
  (`timeout(without_cancel(ctx), 4s)`) so it isn't skipped with an instant
  deadline-exceeded.
- `ErrNotFound` / `ok-but-empty` → mark configured, continue.
- hard error → mark configured, log (unless it's deadline/cancel — those are
  silent), continue.
- After loop: `pending → ErrPending`; `!configured → ErrNotConfigured`; else
  `ErrNotFound`.

`Translating` adapter maps an inner resolver's own sentinels
(`bmmatch::ErrNotFound`/`ErrNotConfigured`) onto the `steammatch` sentinels.

Wire-up: `Chain[ bmsearch (if BM_SEARCH_ENABLED), Translating{bmmatch} ]`.

### 4.3 `bmsearch` — primary resolver (bmsearch.io, public no-auth)

Base `https://bmsearch.io/api` (`BM_SEARCH_URL`, trailing `/` trimmed), direct,
`POLL_INTERVAL = 700ms`, inline wait from `BM_SEARCH_INLINE_TIMEOUT_MS` (4000).

- Cache Steam→BM: `GET /profiles?steam_id=eq.{id}&select=battlemetrics_id&limit=1`
- Cache BM→Steam: `GET /profiles?battlemetrics_id=eq.{id}&select=steam_id&limit=1`
- Cache by profile: `GET /profiles?id=eq.{pid}&select=battlemetrics_id&limit=1`
- Enqueue: `POST /rpc/lookup {p_steam_input, p_force:false}` → `{queue_id?, profile_id?}`
- Poll: `GET /rpc/lookup_status?p_queue_id={id}` → `{status, profile_id?, error?}`

`battlemetrics_id`/`queue_id`/`profile_id` come back as JSON numbers → parse as
i64 → stringify (normalize bigint). Terminal-failure statuses (case-insensitive):
`failed`, `no_match`, `not_found`, `error`.

**Hybrid `steam_to_bm` (`bmsearch.go:82-108` + `pollLookup` 218-269):**
1. cache read → hit returns immediately.
2. miss → `enqueueLookup`; if it returns a `profile_id` immediately, try
   `profileBmIDByID`.
3. `poll_lookup(queue_id)`: bound by the caller's ctx deadline (fall back to
   `defaultWait` if the ctx has none); tick every 700ms; on `done` resolve
   `profile_id`→bm_id; on terminal-failure or non-empty `error` → `ErrNotFound`;
   on **ctx deadline before done → `ErrPending`** (the signal for the caller to
   backfill). `bm_to_steam` is **cache-only** (the lookup RPC takes Steam input
   only).

### 4.4 `bmmatch` — legacy fallback (optional)

Base `BM_MATCH_URL` (empty disables), `Authorization: Bearer BM_MATCH_TOKEN`, 10s
timeout, direct. `GET /steam-to-bm/{id}` and `GET /bm-to-steam/{id}` → single-entry
JSON map, first non-empty value. 404 → `ErrNotFound`; not configured
(empty url/token) → `ErrNotConfigured`; other non-2xx → status error.

### 4.5 `platformsync` — identity discovery (optional)

Hard-coded `https://link.platformsync.io/api.php`, `PLATFORMSYNC_TOKEN`, 10s,
direct. `GET ?id={steam_or_discord}&token={token}` → `{ linked, steam_id,
discord_id, previous_steam_ids, previous_discord_ids }`. `previous_*` is `"NA"`
or CSV → split/trim/drop empty+`"NA"` → `Vec<String>` (nil when none). Not
configured (empty token) → `ErrNotConfigured`; `linked:false` → `Lookup` +
`ErrNotLinked`; upstream HTTP error → `ErrNotConfigured`.

### 4.6 Crate boundary — **key design decision (D4)**

The **hybrid inline-timeout + detached singleflight backfill + DB-persist**
(`internal/http/handlers.go`: `resolveSteamAccountBySteamID`,
`backfillSteamAccountBmID`, `resolveSteamAccountBySteamIDWithin`) is **handler-layer
orchestration** — it touches `steam_accounts`, the BM client, `singleflight`, and
DB upserts. Recommendation: **`rustlens-steam` provides the clients + the Chain
(which yields `ErrPending`) only.** The inline-timeout wrapper + singleflight +
persist orchestration belongs in the **`players` service (Phase 2)**, where the DB
writer + BM client are wired together. The roadmap line "reproduce the hybrid" is
satisfied at the crate level by the Chain's pending/fallback-budget logic and
bmsearch's `poll_lookup` `ErrPending`; the *backfill goroutine* is Phase 2. Flag
for confirmation.

### 4.7 Env

| Var | Type | Default |
|---|---|---|
| `STEAM_API_KEY` | string | `""` |
| `BM_SEARCH_ENABLED` | bool | `true` |
| `BM_SEARCH_URL` | string | `https://bmsearch.io/api` |
| `BM_SEARCH_INLINE_TIMEOUT_MS` | int(ms) | `4000` |
| `BM_MATCH_URL` / `BM_MATCH_TOKEN` | string | `""` (fallback disabled when unset) |
| `PLATFORMSYNC_TOKEN` | string | `""` |

### 4.8 Verification

- Parse recorded Steam / bmsearch / bmmatch / platformsync JSON fixtures; assert
  structs + query params (`key`/`steamids`/`select=eq.`/`p_steam_input`) match Go.
- Chain fallthrough unit tests with mock resolvers: not-configured skip, pending
  then fallback-success wins, all-notfound → NotFound, pending-only → Pending,
  hard-error fallthrough, the fresh-budget-after-pending path.
- bmsearch `poll_lookup`: mock a status server → done resolves, terminal-failure
  → NotFound, deadline-before-done → Pending.
- All request-shape assertions via a mock HTTP layer; no live external calls.

---

## Open decisions for review

- **D1 — httpauth cookie read.** Move `cookies::read_cookie` into the crate, or
  have `validate_session_cookie` take the raw `Cookie` header? *Recommend:* move a
  minimal cookie-read helper into the crate so it's self-contained.
- **D2 — outbound send abstraction.** D2a (a `send()` method + `OutboundSender`
  trait, health-recording inside the crate) vs D2b (`reqwest-middleware`).
  *Recommend D2a.*
- **D3 — where `outbound_proxies` queries live.** `rustlens-db` (consistent with
  existing modules) vs inside `rustlens-outbound`. *Recommend `rustlens-db`.*
- **D4 — steam hybrid boundary.** Crate ships clients + Chain only; the
  singleflight backfill + DB-persist orchestration lands in the `players` service
  (Phase 2). *Recommend as stated.*

## Verification summary (per crate)

| Crate | build/clippy/test | Extra |
|---|---|---|
| httpauth | ✅ | re-run identity smokes (header `/me`, admin gate, SSE) after refactor |
| outbound | ✅ | unit: rotation, health-window math, success class, replace/redact; parity vs Go threshold |
| battlemetrics | ✅ | fixture parse for 5 endpoints; request-shape assertions via mock sender; sentinels |
| steam | ✅ | fixture parse (steam/bmsearch/bmmatch/platformsync); Chain fallthrough + poll_lookup units |

## Git / workflow

- Branch off `rework/rust-backend`; one feature branch + PR per crate (httpauth →
  outbound → battlemetrics → steam), merged then branch-pruned.
- Commit source + manifests + `Cargo.lock`; never `target/`.
- Throwaway Docker Postgres on a spare port for any DB-touching test; tear down
  after. Mint test tokens with `node` using the shared secrets.
- Update the project memory file as each crate lands.
