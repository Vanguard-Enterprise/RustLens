# RustLens — Go → Rust Multiservice Migration Roadmap

> Historical: the migration completed on `rework/rust-backend`; see `rust-go-removal-plan.md`.

Status: **APPROVED — executing.** Plan for replacing the Go monolith
(`apps/backend`) with a set of Rust services. Decisions resolved (§6); **Phase 0
(finish + cut over `identity`) is in progress.** Builds on the completed identity
service (`docs/rust-identity-completeness-spec.md`).

Ground rules carried from the identity migration:
- **One shared Postgres** for the whole migration. The Go backend owns
  `EnsureSchema()` until the very end; no per-service migrations. Each table has
  **exactly one writer service**; other services may read cross-domain tables
  directly during the transition (physical DB-per-service split is a post-
  migration stretch goal — cross-domain FKs like `command_usage → app_users` and
  `guild_steam_discord_links → steam_accounts` make it a large separate lift).
- **Dual-run + path-by-path cutover.** A new Rust route runs alongside the Go
  handler; Traefik is flipped per path once verified; the Go handler is then
  deleted. Nothing is a big-bang switch.
- **Cross-language contracts stay byte-for-byte.** `rustlens_session` cookie,
  bot JWT, AES-GCM tokens, and all JSON response shapes match the Go/TS today.
- **Parked password-auth endpoints are NOT ported.** (`/api/app/auth/register`,
  `/login`, `/password/reset/*`, `/email/verify/*` — no frontend uses them.)
  They are dropped at identity cutover.

---

## 1. Target topology — 5 services + shared crates

Recommended: **5 long-running Rust services.** (Could be 4 if `coordinator`
folds into `guilds`; could be 6 if we later add an edge gateway — but Traefik
path routing removes the need for a gateway, and a fleet coordinator is cleanly
separable, so 5 is the target.)

| # | Service | Domain | Owns (sole writer) | Serves | Background workers | Status |
|---|---|---|---|---|---|---|
| 1 | **identity** | Auth, app-users, admin/RBAC, moderation, telemetry read | `app_users`, `admin_*`, `user_entitlements`, `discord_role_connections`, `audit_events`(+legacy `app_audit_events`), `command_usage`(+`_targets`), `user_bans`, `user_notification_preferences` | `/api/auth/discord*`, `/api/events/stream`, `/api/app/me*` (auth/preferences), `/api/app/admin/*` (RBAC, users, audit, command-usage), bot-internal `command-usage` ingest + `bans` + `is-website-admin` + `has-pro-role` | role-connection reconciler (done); `command_usage` nightly purge | **built (auth core); needs finishing + cutover** |
| 2 | **players** | Player identity: Steam↔BM resolution, profiles, analytics, all Steam↔Discord linking | `steam_accounts`, `steam_account_cache`, `bm_profile_cache`, `bm_name_history`, `steam_discord_links`, `guild_steam_discord_links`, `hidden_discord_link_identities`, `platform_sync_state` | app `/api/app/players/*`; bot-internal steam profile/analytics/resolve, steam↔BM lookup + link CRUD, game-bans, platform-sync, link-visibility | — | not started |
| 3 | **servers** | Tracked BM servers, live presence, server analytics, outbound proxy infra | `tracked_servers`, `guild_tracked_servers`, `player_states`, `server_player_count_history`(+`_fetches`), `outbound_proxies`, `track_analytics_sessions_cache` | bot-internal tracked-server CRUD, player-states/search, online-summary, tracked-server-log-events, player-count-history, track/offline analytics; admin `bm-servers` + `proxies` CRUD | **BattleMetrics rolling poller**, daily server-analytics refresher, outbound proxy provider (rotation/health) | not started |
| 4 | **guilds** | Per-guild Discord bot features + guild ops | `guild_settings`, `bot_guilds`, `user_guilds`, `bot_user_installs`, `track_groups`(+`_members`), `wipe_reacts`, `wipe_react_schedules`(+`_posts`) | bot-internal guild settings, track-groups, wipe-reacts (+schedules), user-guilds, bot-guilds, nickname-targets, guild game-bans, online summary; admin `guilds` list/detail | **wipe-react scheduler** (enqueue due posts) | not started |
| 5 | **coordinator** | Bot shard fleet coordination | — (Redis leases/heartbeats; no owned Postgres tables) | bot-internal `/api/internal/bot/shards/*` (register, assignments, heartbeat, release) | lease reaper | not started |

Notes on gray-area ownership:
- `guild_steam_discord_links` and `guild_tracked_servers` are guild-scoped but
  are *linking*/*server-selection* data → owned by **players** / **servers**
  respectively; **guilds** reads them.
- `track_analytics_sessions_cache` is BM-session data → **servers**; **guilds**
  (track analytics) triggers refresh via a servers API call.
- `command_usage`/`user_bans` are written by the bot but are account-level +
  admin-surfaced → **identity**. Ingest endpoints are bot-internal (JWT) but the
  data is identity-domain.
- `is-website-admin` / `has-pro-role` read `app_users`/`user_entitlements` →
  **identity** exposes them to the bot.

`cleanup` (the Go retention sweep) does **not** become a service — each owning
service runs its own retention task (wipe_reacts → guilds, sessions cache →
servers, command_usage → identity).

---

## 2. Shared crates (libraries, not services)

Existing: `rustlens-session`, `rustlens-config`, `rustlens-db`,
`rustlens-events`, `rustlens-crypto`.

New (built in Phase 1, before the domain services depend on them):
- **`rustlens-httpauth`** — the `/api/app/*` gate (app-secret + session-header +
  permission) and cookie-session extractor, factored out of `identity` so every
  app-facing service shares it.
- **`rustlens-botjwt`** — verify/mint the bot service JWT (`BOT_SERVICE_JWT_SECRET`,
  issuer/audience) for `/api/internal/bot/*`.
- **`rustlens-battlemetrics`** — the unauthenticated BM client (servers, players,
  sessions, player-count-history), routed through the outbound provider.
- **`rustlens-steam`** — Steam Web API client + the `steammatch` resolver chain
  (bmsearch primary, bmmatch fallback) + platformsync client.
- **`rustlens-outbound`** — proxy provider: rotation, per-proxy health window,
  auto-disable; reads `outbound_proxies` (owned by **servers**).
- **`rustlens-openapi`** (optional) — per-service spec fragments + a docs UI, if
  we keep `/api/docs`.

---

## 3. Bot-internal routing decision

The bot-internal API is flat today (`/api/internal/bot/setup/...`), mixing
domains, so Traefik can't route it to per-domain services as-is. Chosen approach:
**reorganize bot-internal paths under domain prefixes** as each domain migrates —
e.g. `/api/internal/bot/players/*`, `/servers/*`, `/guilds/*`, `/shards/*` — so
Traefik routes by prefix and the bot's TS client targets the right service. The
Go backend keeps serving the old flat paths until each is cut over (bot updated
in lockstep). This avoids a bot-gateway shim.

---

## 4. Phases

Each phase: build behind dual-run → verify (unit + smoke vs throwaway PG/Redis,
plus parity check against the live Go response) → add a Dockerfile + CI cargo job
→ flip Traefik per path → delete the migrated Go handlers → update the bot TS
client. No schema changes until Phase 6.

### Phase 0 — Finish & cut over `identity`
Goal: prove the deploy/routing/dual-run pattern on the service that already
exists.
- Port the remaining identity-domain routes: `/me/preferences` (GET/PATCH),
  `/api/app/auth/logout`, admin user-detail (`/users/{id}` + `/audit` +
  `/command-usage*`), `audit/{id}`, `audit/event-types`; bot-internal
  `command-usage` ingest, `bans`, `is-website-admin`, `has-pro-role`.
- Add the deferred hardening: admin **API-key auth** path + dispatcher
  **rate-limiting + idempotency**.
- Infra: identity Dockerfile, CI cargo job, Traefik routes for the identity
  paths → `:8090`. **Drop** the parked password-auth handlers at cutover.
- Exit: identity serves 100% of its domain in prod; the Go auth/admin handlers
  for those paths are deleted.

### Phase 1 — Shared foundation crates
Goal: de-risk every later service by building the shared clients/middleware once.
- `rustlens-httpauth`, `rustlens-botjwt`, `rustlens-battlemetrics`,
  `rustlens-steam`, `rustlens-outbound` (see §2), each with unit tests and a
  golden-parity test against the Go client where a wire contract exists (JWT,
  BM/Steam request shapes).
- Exit: crates published in-workspace, ready to consume.

### Phase 2 — `players` service
Goal: the most self-contained high-value domain (player identity + linking).
- App `/api/app/players/{search,profile}`; bot-internal steam profile/analytics/
  resolve, steam↔BM lookup + backfill, link CRUD (soft/hard, guild-scoped),
  game-bans, platform-sync, link-visibility. Uses `rustlens-steam` +
  `rustlens-battlemetrics`.
- Reproduce the HYBRID cold-lookup behavior (inline timeout + detached backfill,
  singleflight dedup) and the 24h cache TTLs exactly.
- Exit: players routes cut over; the Go steam/link/platform-sync handlers deleted.

### Phase 3 — `servers` service
Goal: tracked servers + live presence + the first background poller in Rust.
- Bot-internal tracked-server CRUD, player-states/search, online-summary,
  tracked-server-log-events, player-count-history, analytics; admin `bm-servers`
  + `proxies` CRUD.
- **BattleMetrics rolling poller** as an advisory-locked singleton (same
  `pg_try_advisory_lock` pattern as the role-connection reconciler), the daily
  server-analytics refresher, and the `rustlens-outbound` proxy provider with
  live reload on `outbound_proxies` changes.
- Exit: server/presence/proxy routes + the poller cut over; Go poller + handlers
  deleted.

### Phase 4 — `guilds` service
Goal: the Discord-feature-heavy domain; depends on players (links) + servers
(presence/selection) via cross-service reads/calls.
- Bot-internal guild settings, track-groups (+analytics, calling servers for the
  session cache), wipe-reacts (+schedules), user-guilds, bot-guilds, nickname-
  targets, guild game-bans, online summary; admin `guilds` list/detail.
- **Wipe-react scheduler** background worker (enqueue due posts; the bot still
  claims + sends).
- Exit: guild routes + scheduler cut over; Go handlers deleted.

### Phase 5 — `coordinator` service + Go decommission
- Move shard register/assignments/heartbeat/lease to Rust (Redis-backed).
- Flip `/api/internal/bot/shards/*`; delete the Go shard service.
- With every route + background worker in Rust, **retire `apps/backend`**
  entirely (including OpenAPI-in-Go and the never-ported password-auth code).
- Exit: the Go backend is gone; Traefik routes only to Rust services.

### Phase 6 (stretch, optional) — physical data separation
- Only after full route migration: split the shared Postgres into per-service
  schemas/DBs, replacing cross-domain direct reads with API calls or read
  replicas / an events feed. Large lift due to cross-domain FKs; not required to
  "replace the Go backend," so explicitly optional.

---

## 5. Cross-cutting workstreams (run alongside all phases)

- **Observability**: `/healthz` + structured logs on every service; add metrics
  before cutting over background workers.
- **Bot TS client**: update `apps/bot/src/backend-client.ts` per phase to target
  the new domain base path/URL; keep old paths working until cutover.
- **CI**: one cargo `build + clippy --all-targets + test` job covering the whole
  `apps/rust` workspace, plus per-service Docker builds in the publish workflow.
- **Local dev**: extend `pnpm dev` / `infra/docker-compose.yml` to run the Rust
  services once we're ready to exercise them locally (deferred until Phase 0
  cutover).
- **Parity harness**: a repeatable "same request → Go vs Rust response diff"
  check per route, run before each Traefik flip.

---

## 6. Decisions (RESOLVED — 2026-07-01)

1. **Service count → 5** (identity, players, servers, guilds, coordinator).
   `coordinator` stays its own small service (not folded into guilds); no edge
   gateway — Traefik path-routing covers it.
2. **Bot-internal path reorg → yes.** Rename `/api/internal/bot/setup/*` into
   domain prefixes (`/players/*`, `/servers/*`, `/guilds/*`, `/shards/*`) as each
   domain migrates, so Traefik routes by prefix; the bot's TS client is updated
   in lockstep. No gateway shim.
3. **DB separation → out of scope for the migration.** Stop at "all routes +
   workers in Rust, shared Postgres, one writer per table." Phase 6 (physical
   split) stays an explicitly optional future stretch.
4. **Shard leases → keep Redis** (as Go does) in `coordinator`.
