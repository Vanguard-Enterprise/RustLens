# Rust fleet cutover plan

How the five code-complete Rust services (`identity` 8090, `players` 8091,
`servers` 8092, `guilds` 8093, `coordinator` 8094 — all on `rework/rust-backend`)
go from "built + verified" to "serving production traffic," retiring the Go
backend. **Nothing here is a big-bang switch.**

## Model (proven on identity's P0-F)

Dual-run + **path-by-path Traefik flip**. A per-service
`infra/traefik/dynamic/<svc>.yml` at **priority 200** routes that service's owned
paths to it; everything else falls through to the Go backend (`base.yml`,
priority 100). Flip a path → verify in prod → delete the Go handler. Reversible
per-path (delete the dynamic file → the path falls back to Go). Repeat until the
Go backend serves nothing.

The identity cutover is the template to replicate ×4: `apps/rust/Dockerfile`, the
`ci.yml` rust job (already builds/clippy/tests the whole workspace — no change),
a `publish.yml` matrix entry, a `docker-compose` service, and a Traefik dynamic
file.

## Route ownership map

**App-facing** (`/api/app/*`, `/api/auth/*`, `/api/events/*`, `/api/discord/*`):
- **identity** — `/api/auth/discord*`, `/api/events/stream`, `/api/app/me*`, `/api/app/auth/logout`, `/api/app/admin/{permissions,roles,users*,administrators,audit*,actions}`
- **players** — `/api/app/players/*`
- **servers** — `/api/app/admin/bm-servers*`, `/api/app/admin/proxies*`
- **guilds** — `/api/app/admin/guilds*`
- **coordinator** — `/api/discord/webhook-events`

**Bot-internal** (`/api/internal/bot/*`) — the reorg targets (see below):
- **coordinator** — `/shards/*`, `/owner/stats`, `/setup/rate-limit/consume`
- **players** — steam profile/analytics/resolve, steam↔BM lookup + link CRUD, game-bans, platform-sync, nickname-targets, guild steam-discord links, track-group **analytics**
- **servers** — tracked-servers CRUD, player-states/search, online-summary, tracked-server-log-events, player-count-history, `/owner/proxies*`
- **guilds** — guild settings, wipe-reacts, wipe-react-schedules + schedule-posts, track-groups CRUD/members/online-state/notify-claim, user-guilds, bot-guilds, bot-user-installs

## The routing reorg (the hard part)

**Blocker:** nearly all bot-internal routes are flat under
`/api/internal/bot/setup/*`, and the `/setup/guilds/{guildID}/*` subtree is split
across **three** services (players: game-bans/links/nickname-targets/track-group
analytics; servers: tracked-servers/online-summary/log-events; guilds:
settings/wipe-reacts/schedules/track-groups). `/owner/*` is split too
(stats→coordinator, proxies→servers). Traefik cannot prefix-route this cleanly.

**Recommended (roadmap's choice): reorganize bot-internal paths under domain
prefixes** — `/api/internal/bot/{players,servers,guilds,shards,coordinator}/…`.
Touches three things in lockstep per domain: (1) the Rust service route
registrations, (2) the bot TS client (`apps/bot/src/backend-client.ts`), (3) the
Traefik prefix rule. Go keeps serving the old flat paths until each domain flips.

*Alternative (not recommended): keep flat paths + explicit `PathRegexp` rules per
sub-path. Avoids Rust/bot changes but yields large, fragile rules for the shared
`/guilds/{g}/*` subtree.*

App-facing `/api/app/*` splits cleanly by exact path / sub-prefix (no reorg
needed) — lower risk, they're Next-server-side calls.

## Background-worker singleton safety (verified)

During dual-run both Go and Rust run background workers. Lock coordination checked:
- **BM poller + server-analytics refresher** (servers): Rust reuses Go's exact
  Redis keys (`rustlens:battlemetrics:poller:{cadence,lock}`,
  `rustlens:server-analytics:refresh:lock`) → only one runs per cycle. ✅
- **role-connection reconciler** (identity): both use
  `pg_try_advisory_lock(7427198631011)` → ✅ one runs.
- **wipe-react scheduler** (guilds) + **cleanup** (coordinator): Go runs these
  **lock-free**; the Rust ones only lock among Rust replicas → they double-run
  alongside Go, but the work is idempotent (`UNIQUE(schedule_id,wipe_unix) DO
  NOTHING`; idempotent DELETEs) → harmless, just wasteful.
- **Recommendation:** as each domain flips, **disable the corresponding Go
  worker** (env flag / drop from `cmd/server/main.go`) so exactly one runs.

## Per-service cutover order (one at a time, reversible)

Lowest-risk first: **players → servers → guilds → coordinator**. Per service:
1. Deploy the image.
2. Add its Traefik dynamic file (priority 200) for owned paths.
3. Reorg its bot-internal paths (Rust routes + bot client) if applicable.
4. Verify the flipped paths in prod (compare responses to Go).
5. Disable the Go background workers for that domain.
6. Delete the migrated Go handlers/routes.

Rollback at any step = remove the dynamic file.

## Decommission Go

After all paths flip + handlers are deleted, the Go backend has only
`EnsureSchema()` + startup left. **Decision:** keep a stripped Go binary purely to
run migrations, **or** move schema ownership to a migration tool / one Rust
service. Then drop `backend` from compose + `base.yml`.

## Production caveat (out of this repo)

`base.yml` routes to `host.docker.internal:8080` — **local dev only**; per
`CLAUDE.md`, "production routing is handled outside this repository." The repo
changes below *enable* the cutover; the actual **prod Traefik flip + deploy +
Go-handler deletion** is the operator's infrastructure and is the outward-facing,
hard-to-reverse step.

## Work split

| Safe on `rework/rust-backend` (no prod impact, reversible) | Needs operator go / ops |
|---|---|
| Parametrize Dockerfile; `publish.yml` matrix; 4 compose services; the 4 Traefik dynamic files; the bot-internal path reorg (Rust routes + bot client); Go-worker disable flags | Deploy to prod; flip prod Traefik per path; delete Go handlers; decommission the Go backend |

## Open decisions

Both DECIDED 2026-07-08 (Frank). Go is being deleted from the repo entirely;
execution is broken down in `docs/rust-go-removal-plan.md`, which supersedes
the per-domain Go-handler-deletion steps above (Go is now deleted wholesale at
the end instead).

1. **Bot-internal routing: DECIDED, domain-prefix reorg.** One prefix per
   service (`/api/internal/bot/{identity,players,servers,guilds,coordinator}/...`),
   one Traefik `PathPrefix` rule each, single bot base URL and JWT audience
   preserved.
2. **Schema ownership after Go is gone: DECIDED, single one-shot `migrate`
   binary** (`apps/rust/tools/migrate`) using `sqlx::migrate!` with a distilled
   `0001_baseline.sql`; a migrations-only Go binary is off the table.

## Status of the plumbing (this repo)

- **Build/publish/run plumbing (done):** `apps/rust/Dockerfile` parametrized by
  `SERVICE`; `publish.yml` builds all five `rustlens-<svc>` images;
  `docker-compose` runs all five services against the compose Postgres/Redis. The
  whole fleet is buildable, publishable, and locally runnable — **but no traffic
  is routed to players/servers/guilds/coordinator yet** (only identity has a
  Traefik dynamic file).
- **Held for sign-off:** the Traefik dynamic files for the other four, the bot
  path reorg, and Go-handler deletion — i.e. the actual routing cutover.
