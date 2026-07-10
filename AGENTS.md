# AGENTS.md

This file provides guidance to AI coding agents working with this repository.
Symlinked as `CLAUDE.md` so it is automatically picked up by Claude Code, Codex, Gemini, GitHub Copilot, and similar tools.

> **Keep this file up to date.** After any significant change — new API routes, renamed files, added
> dependencies, schema migrations, architectural decisions — update the relevant sections below.
> Stale instructions lead to wasted effort and broken assumptions. When in doubt, re-read the
> codebase and correct anything that has drifted.

## Version Control

- Prefer small, frequent commits when changes are ready and the user has asked for commits, so individual changes are easier to revert.

## Repository Overview

RustLens is a monorepo with three apps. The Go backend (`apps/backend`) that used
to serve `/api/*` has been **deleted** (Go-removal Phase E): the backend is now
a fleet of five Rust services plus a one-shot schema-migration tool, all living
under `apps/rust/`.

| App | Stack | Description |
|---|---|---|
| `apps/frontend` | Next.js 16 (React 19, TypeScript, App Router, Tailwind v4, shadcn/ui) | Marketing site plus browser-authenticated user dashboard at `/dashboard` and internal ops dashboard at `/admin` |
| `apps/bot` | Discord.js (TypeScript, ESM) | Sharded worker runtime |
| `apps/rust` | Rust (axum 0.8, sqlx 0.8, tokio) | Cargo workspace, **the backend**: five axum microservices (`identity` :8090, `players` :8091, `servers` :8092, `guilds` :8093, `coordinator` :8094), a `migrate` one-shot schema tool, and shared crates (see [Backend (Rust fleet)](#backend-rust-fleet)) |

Orchestrated with **pnpm 10 workspaces** + **Turborepo** for the frontend and bot packages. The **Rust workspace (`apps/rust`) is outside the pnpm graph**: it is built/tested with `cargo` directly (see Common Commands) but it **is** wired into the root `dev`/`lint:all`/`test:all`/`build:all` scripts, `docker compose` (`infra/docker-compose.yml` builds and runs the full five-service fleet plus `migrate` by default), Traefik (`infra/traefik/dynamic/*.yml`, one file per service), and CI/publish. The Rust fleet is authoritative for all `/api/*` traffic; there is no Go fallback anymore.

## Common Commands

All commands run from the repo root unless noted.

```bash
pnpm run doctor      # Verify toolchain (docker, node, pnpm, cargo, git)
pnpm run setup       # pnpm install (no Go step; Rust toolchain is installed separately, see platform setup scripts)
pnpm dev             # docker compose up -d (Postgres, Redis, Traefik, and the full 5-service Rust fleet + migrate) + hot-reload frontend and bot
pnpm dev:app         # Same as `pnpm dev` but frontend only (no bot)
pnpm infra:up        # docker compose -f infra/docker-compose.yml up -d (same fleet as `pnpm dev`, without the frontend/bot hot-reload)
pnpm infra:down      # Stop infra containers
```

> `pnpm run` is required for `setup` and `doctor` because the unprefixed forms collide with pnpm built-in commands.

### Per-service dev

```bash
pnpm dev:frontend    # Next.js dev server for the landing app
pnpm dev:bot         # tsx watch for bot
```

There is no `dev:backend` script anymore. `pnpm dev` / `pnpm infra:up` bring up
the Rust fleet via `docker compose`, but the Rust services themselves are
**not** hot-reloaded: `scripts/dev/start-dev.mjs` only builds/starts their
Docker images. When iterating on a Rust service, run `cargo build`/`cargo run
--bin <service>` directly in `apps/rust` (or rebuild the compose image with
`docker compose -f infra/docker-compose.yml up -d --build <service>`).

### Lint / Test / Build

`pnpm lint` / `pnpm test` / `pnpm build` cover the pnpm workspace (frontend + bot). The `:all` variants additionally run the Rust workspace's equivalent step.

```bash
pnpm lint:all        # pnpm lint (frontend + bot) + cargo clippy --all-targets (apps/rust)
pnpm test:all        # pnpm test (frontend + bot) + cargo test (apps/rust)
pnpm build:all       # pnpm build (frontend + bot) + cargo build --release (apps/rust)
```

### Rust workspace (`apps/rust`)

```bash
cd apps/rust && cargo build                  # Build all crates + all 5 service binaries + migrate
cd apps/rust && cargo clippy --all-targets   # Lint (must be warning-clean)
cd apps/rust && cargo test                   # Run all crate tests (set RUSTLENS_TEST_PG_URL for the DB-integration tests)
cd apps/rust && cargo run --bin migrate      # Apply crates/rustlens-db/migrations/ against POSTGRES_URL
cd apps/rust && cargo run --bin identity     # Run a single service locally (identity/players/servers/guilds/coordinator)
```

### Running a single app's tests

```bash
# TypeScript (vitest)
pnpm --filter @rustlens/bot test

# Rust (per-crate or per-service)
cd apps/rust && cargo test -p rustlens-db
cd apps/rust && cargo test -p players
```

### Lint details

- **Bot**: `tsc --noEmit` (type-check only, no ESLint).
- **Rust**: `cargo clippy --all-targets -- -D warnings` (kept warning-clean).

## Architecture

### Routing (Traefik)

Local dev runs Traefik on `http://localhost`, configured entirely via file
routers in `infra/traefik/dynamic/*.yml` (one file per service, all
`entryPoints: [web]`, `Host(\`localhost\`)`):

- **`base.yml`**: `Path(/api/healthz)` (priority 100) routes to `identity:8090`
  since every service exposes an identical `/api/healthz` and no per-domain
  file claims the bare path; the catch-all `Host(localhost)` router (priority
  1) sends everything else to the frontend (`host.docker.internal:3000`, the
  Next.js dev server running outside Docker). There is no `/api` catch-all
  anymore, Go's single backend router is gone.
- **`identity.yml`** (priority 200 -> `identity:8090`): `/api/auth/discord*`,
  `/api/events*`, `/api/app/me` (+ `/preferences`), `/api/app/auth/logout`,
  `/api/app/me/discord-role-connection`, `/api/internal/bot/identity/*`, and
  the admin surfaces it owns: `/api/app/admin/{permissions,roles,users,
  administrators,audit,actions,api-keys}*`.
- **`players.yml`** (priority 200 -> `players:8091`): `/api/internal/bot/players/*`,
  `/api/app/players/*`.
- **`servers.yml`** (priority 200 -> `servers:8092`): `/api/internal/bot/servers/*`,
  `/api/app/admin/{bm-servers,proxies,servers}/*`.
- **`guilds.yml`** (priority 200 -> `guilds:8093`): `/api/internal/bot/guilds/*`,
  `/api/app/admin/{guilds,user-installs}/*`.
- **`coordinator.yml`** (priority 200 -> `coordinator:8094`): `/api/internal/bot/coordinator/*`,
  `/api/discord/webhook-events`.

Bot-internal routes use one domain prefix per service:
`/api/internal/bot/{identity,players,servers,guilds,coordinator}/...`, with a
single Traefik `PathPrefix` rule per service; there's one shared bot-JWT
audience (`rustlens-backend`), not a per-service audience. The `/api/app/admin/*`
surface is split by data ownership across identity/servers/guilds as listed
above; `/api/app/me*`, `/api/auth/discord*`, and `/api/events*` all belong to
identity.

Production routing is handled outside this repository, but mirrors the same
five dynamic files (deploying them is part of the S1 prod-cutover sign-off in
`docs/rust-go-removal-plan.md`).

### Frontend (Next.js)

```
apps/frontend/
  app/
    layout.tsx                          # App Router root layout + metadata
    page.tsx                            # Public landing page
    not-found.tsx                       # App Router 404 route that reuses the landing shell
    login/page.tsx                      # Sign-in entry: redirects to /api/auth/discord (OAuth start lives on the backend); renders an error/notice page only when redirected back with ?error= or ?notice=
    logout/route.ts                     # CSRF-protected POST that clears the session cookie
    admin/page.tsx                      # Internal ops dashboard guarded by app-user internal_admin role
    admin/guilds/page.tsx               # Admin Discord/Guilds list (paginated bot_guilds with name+ID+owner search)
    admin/guilds/[guildId]/page.tsx     # Admin guild detail drilldown (settings, members, wipe reacts, tracking, links, usage)
    admin/roles/page.tsx                # Admin RBAC management — create/edit/delete custom roles, view system roles read-only (admins.grant only)
    admin/bm-servers/page.tsx           # Admin tracked BattleMetrics servers management (bm_servers.read; edits require bm_servers.edit)
    dashboard/page.tsx                  # Authenticated user dashboard root
    dashboard/guilds/[guildId]/page.tsx # Per-guild authenticated dashboard view
    dashboard/_components/discord-role-connection-card.tsx  # Dashboard card showing Discord community role link state
    dashboard/_components/discord-role-connection-actions.ts # Connect/disconnect server actions
    dashboard/_components/notice-toast.tsx                  # Dashboard-scoped Sonner notice/error toast component
    search/page.tsx                     # Player search results page (?q=&includeOffline=); admin-only offline toggle
    player/[steamId]/page.tsx           # Public player profile page (mirror of bot /player embed, charts deferred)
    privacy/page.tsx                    # Public privacy-policy page
    terms-of-service/page.tsx           # Public terms-of-service page
    refund-policy/page.tsx              # Public refund-policy page
    globals.css                         # Tailwind v4 theme tokens and global styling
  components/
    app-shell.tsx              # Authenticated shell wrapping shadcn SidebarProvider/SidebarInset
    app-sidebar.tsx            # Route-aware RustLens sidebar navigation for dashboard/admin
    dashboard-app-sidebar.tsx  # Dashboard-specific sidebar variant
    app-header.tsx             # Authenticated shell header with breadcrumbs and session-aware user menu
    app-breadcrumbs.tsx        # Breadcrumb derivation for the authenticated header
    site-header.tsx            # Public-page top navigation
    header.tsx                 # Sticky top navigation with docs / add-bot CTAs
    hero.tsx                   # Landing hero
    feature-section.tsx        # Landing feature strip
    faqs-section.tsx           # Landing FAQ accordion
    logos-section.tsx / logo-cloud.tsx  # Integration logo strip
    pricing-section.tsx        # Landing pricing block
    footer.tsx                 # Footer with FaDiscord (react-icons) social link
    auth-page.tsx              # "Sign in with Discord" panel (single OAuth button, no email/password UI)
    navbar-player-search.tsx   # Discord-style player search popover injected into the dashboard/admin header
    player-hover-card.tsx      # Hover wrapper around PlayerProfileCard with per-session in-memory cache
    player-profile-card.tsx    # Shared profile card body used by /player and the hover preview
    auth-divider.tsx           # Divider used inside the auth panel
    not-found.tsx              # 404 panel reused by app/not-found.tsx
    legal-page.tsx / legal-toc.tsx       # Shared long-form legal page shell + TOC used by /privacy, /terms-of-service, /refund-policy
    chart-area-interactive.tsx, data-table.tsx, section-cards.tsx, latest-change.tsx, frequency-toggle.tsx, dashboard-skeleton.tsx  # Dashboard widgets
    nav-main.tsx, nav-secondary.tsx, nav-documents.tsx, nav-group.tsx, nav-links.tsx, nav-user.tsx, sidebar-nav-user.tsx, mobile-nav.tsx, desktop-nav.tsx, custom-sidebar-trigger.tsx  # Navigation primitives
    session-provider.tsx       # React context for live current-user snapshot (roles, permissions)
    session-events.tsx         # Mounts the EventSource hook; dispatches SSE events to Sonner + router
    session-refresh-trigger.tsx # Sliding-TTL refresher (mount/focus/online/hourly)
    logo.tsx                   # Brand logo component
    ui/                        # shadcn/ui primitives added as source
  lib/
    rustlens-api.ts      # Server-side client for /api/app aggregate, admin, and (still-exposed) password-auth backend routes
    session.ts           # Signed Next.js `rustlens_session` cookie helpers (14-day TTL)
    session-errors.ts    # SessionRevokedError + notice-slug mapping for /login?notice=
    session-refresh.ts   # `refreshSession` server action: re-mints cookie when TTL < 7d
    session-actions.ts   # `refreshCurrentUser` + `clearSessionAction` server actions
    events-client.ts     # Browser EventSource lifecycle helper for /api/events/stream
    csrf.ts              # Double-submit CSRF token helper used by the Discord OAuth start link and /logout POST
    rate-limit.ts        # Redis-backed rate limiter (used by auth-related routes)
    discord-cdn.ts       # Discord avatar/CDN URL helpers
    app-origin.ts        # APP_ORIGIN resolution
    relative-time.ts     # Time formatting utility
    legal-content.ts     # Shared privacy/terms/refund copy and legal contact placeholders
    site-content.ts      # Landing-page content model used by the page, FAQ section, and tests
  Dockerfile             # Frontend image for GHCR publish workflow
```

Key patterns:

- The frontend has three surfaces: public marketing/legal routes, authenticated user routes under `/dashboard`, and internal ops routes under `/admin`.
- **Login is Discord OAuth only.** `/login` redirects unauthenticated visitors to `/api/auth/discord` (handled by the identity service, not Next.js). The signed `rustlens_session` cookie is set by identity on successful callback; `APP_SESSION_SECRET` is required and shared with it. The Go backend used to also expose password-auth endpoints (`/api/app/auth/register`, `/login`, `/password/reset/*`, `/email/verify/*`), and `lib/rustlens-api.ts` still has client helpers for them, but no frontend page ever invoked those flows and the Rust identity service does not implement them: there is no email/password form, password reset UI, verification UI, or backing route.
- Auth-related browser actions (the Discord OAuth start link and `/logout` POST) use a double-submit CSRF token via `lib/csrf.ts`. Redis-backed rate limiting (`lib/rate-limit.ts`) is wired in for auth-related handlers.
- The frontend calls **app-facing backend routes** under `/api/app/*` with a shared internal app secret (`APP_API_INTERNAL_SECRET`) from server-side code only. Authenticated `/api/app/*` routes also receive the signed `rustlens_session` token via a server-only header so the backend derives the acting user from the validated session instead of trusting a caller-supplied user ID. The browser never calls these routes directly and the frontend does **not** call the bot JWT routes from the browser.
- The authenticated shell wraps shadcn `SidebarProvider`/`SidebarInset` primitives (no `@efferd/*` dependency).
- Next.js runs as a **hybrid Node server** in dev/prod rather than a static export.
- **Real-time session events**: `<SessionProvider>` + `<SessionEvents>` mount inside `app/dashboard/layout.tsx` and `app/admin/layout.tsx`. The client opens a persistent `EventSource` to `/api/events/stream`; the backend pushes `roles.changed`, `session.revoked`, `session.expiring`, and 25-second `ping` events. Role/permission changes refetch `/api/app/me` and call `router.refresh()` without re-login; force-logout / suspension / deletion redirects to `/login?notice=<reason>` and the `<NoticeToast>` client component fires a Sonner toast on next render. `<SessionRefreshTrigger>` re-mints the cookie via the `refreshSession` server action when remaining TTL drops below 7 days (triggered on mount, focus, online, and hourly).

### Backend (Rust fleet)

`apps/rust/` is a Cargo workspace (resolver 2, edition 2021) that **is** the
RustLens backend: five independently-deployed axum 0.8 + sqlx 0.8 services
sharing one Postgres and one Redis, plus a one-shot `migrate` tool. Stack:
axum, sqlx (runtime queries, no compile-time DB checks), tokio, reqwest,
redis, aes-gcm. Every service exposes `GET /api/healthz`.

```
apps/rust/
  Cargo.toml                     # Workspace manifest (11 members + shared dep versions)
  Dockerfile                     # Single parametrized image for all 6 binaries (ARG SERVICE selects the bin)
  crates/
    rustlens-session/            # rustlens_session cookie mint/verify (cross-language contract with the frontend)
    rustlens-config/             # Env-based per-service config structs: IdentityConfig, PlayersConfig, ServersConfig, GuildsConfig, CoordinatorConfig
    rustlens-db/                 # sqlx pool + ~30 query modules + owns the schema (crates/rustlens-db/migrations/)
    rustlens-events/             # In-process Hub + Redis pub/sub bridge (channel rustlens:user-events) for SSE fan-out
    rustlens-crypto/             # AES-256-GCM token encryption, byte-compatible with the old Go proxycrypto
    rustlens-botjwt/             # Bot service JWT (HS256) validation for /api/internal/bot/*
    rustlens-httpauth/           # Shared /api/app/* auth gate (app-secret header + session header + permission checks)
    rustlens-outbound/           # Outbound HTTP proxy provider: rotation, 10-minute health window, auto-disable at >=50% errors over >=10 requests
    rustlens-battlemetrics/      # Unauthenticated BattleMetrics JSON:API client, always routed through rustlens-outbound
    rustlens-steam/              # Steam<->BattleMetrics identity stack: steamapi, steammatch resolver chain, bmsearch, bmmatch, platformsync clients (always direct, never proxied)
    rustlens-otel/                # Shared tracing/OTLP init, called first thing in every binary's main()
  services/
    identity/                    # PORT 8090 - auth, sessions, admin dashboard, RBAC, Discord linked roles
    players/                     # PORT 8091 - Steam/BM identity resolution, player search+profile, game bans, analytics
    servers/                     # PORT 8092 - tracked servers, BM poller, presence, proxies, server analytics
    guilds/                      # PORT 8093 - guild settings, wipe reacts + schedules, track groups, bot/user-install sync
    coordinator/                 # PORT 8094 - shard coordination, owner stats, rate-limit, Discord webhook, cleanup sweep
  tools/
    migrate/                     # One-shot schema migrator binary `migrate` (crate rustlens-migrate); see Schema below
```

Every service's `main()` calls `rustlens_otel::init(...)` first, then connects
Postgres, then (identity only) seeds RBAC, then builds its axum router and
serves. None of the five services run schema migrations themselves.

**identity** (`apps/rust/services/identity/src/`) owns auth, sessions, and the
admin dashboard: `GET/POST /api/auth/discord` (+ `/callback`, PKCE S256, scope
`identify guilds`), `GET /api/app/me` (+ `/preferences`, `/connections`,
`/guilds`, `/guilds/installed`, `/guilds/{guildID}/links`), `POST
/api/app/auth/logout`, the admin read routes (`GET /api/app/admin/{permissions,
roles,users,administrators,audit,api-keys}` and drilldowns), the admin action
dispatcher (`POST /api/app/admin/actions/{type}`), Discord linked-roles OAuth
(`GET /api/auth/discord/role-connection` + `/callback`, `DELETE
/api/app/me/discord-role-connection`), SSE (`GET /api/events/stream`), and
bot-internal routes under `/api/internal/bot/identity/*` (`command-usage`,
`bans/{discordID}`, `discord/{discordID}/is-website-admin`, `.../has-pro-role`,
`users/ensure`). Background work: at boot, `rustlens_db::rbac::
seed_permissions_and_roles` idempotently upserts the 27-permission catalog and
7 system roles (`superadmin`, `moderator`, `infra_admin`, `guild_ops`,
`user_support`, `user_admin`, `user_auditor`) into `admin_permissions` /
`admin_role_permissions`; without it a fresh deployment has an empty RBAC
catalog. If linked-roles are configured (`PROXY_ENCRYPTION_KEY` +
`DISCORD_BOT_TOKEN` set), identity also registers the Discord metadata schema
once at boot and spawns a 6-hour advisory-locked reconciler that re-pushes
`is_pro`/`pro_since` metadata for drift.

**players** (`apps/rust/services/players/src/`) owns Steam/BattleMetrics
identity resolution and player-facing data: app-facing `GET
/api/app/players/search` and `/{identifier}/profile`, and a large bot-internal
surface under `/api/internal/bot/players/*` covering Steam/BM lookup and
backfill, profile + analytics caching, friends, game-ban scans
(`guilds/{guildID}/game-bans`), track-group analytics
(`guilds/{guildID}/track-groups/{groupID}/analytics`, which players owns
because it shares the offline-analytics engine and outbound/BM client, even
though the group metadata lives in guilds), platform-sync, and global +
guild-scoped Steam<->Discord link CRUD (`steam/links`, `discord/{discordID}/
links`, `guilds/{guildID}/links`, nickname targets). Steam<->BM resolution
uses the HYBRID strategy: a Steam-first lookup still resolving past
`BM_SEARCH_INLINE_TIMEOUT_MS` (default 4s) returns Steam-only data immediately
while a detached background task finishes the resolve within a 90s budget so a
repeat lookup is complete. No periodic background task runs here (the outbound
proxy auto-disable callback is reactive, not scheduled).

**servers** (`apps/rust/services/servers/src/`) owns tracked-server state and
BattleMetrics polling: `GET/POST/PUT/DELETE /api/internal/bot/servers/
tracked-servers*`, `.../player-count-history`, per-guild tracked-server
selection, `.../online-summary`, `.../tracked-server-log-events`, player-state
reads (`.../player-states/*`), owner + admin proxy CRUD (`.../owner/proxies*`,
`/api/app/admin/proxies*`), and admin BM-server CRUD + bulk actions
(`/api/app/admin/bm-servers*`, `/api/app/admin/servers`). Background work:
`poller::run` is a rolling BattleMetrics poller (interval from
`BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS`, default 30s) that polls 2
high-frequency + 1 standard due server per cycle and syncs `player_states`;
`server_analytics::run_refresher` is the daily player-count-history refresher
(mirrors the old 00:01 UTC job over a rolling 90-day window). Both are
Redis-lock coordinated so only one pod runs them at a time.

**guilds** (`apps/rust/services/guilds/src/`) owns per-guild configuration:
`GET/PUT /api/internal/bot/guilds/{guildID}/settings`, user-guild and
bot-guild sync (`user-guilds*`, `bot-guilds/*`, `bot-user-installs/seen`),
wipe-react CRUD + reactions, wipe-react schedule CRUD + the schedule-post
queue (`claim`/`complete`/`release`), track-group CRUD + members + online-state
+ notification claim, and the admin guild list/detail + user-installs routes
(`/api/app/admin/guilds*`, `/api/app/admin/user-installs`). Background work:
`scheduler::run` is the wipe-react schedule poller (interval
`WIPE_SCHEDULER_INTERVAL_SECONDS`, default 60s) that computes each schedule's
next due occurrence and enqueues `wipe_react_schedule_posts` rows for the bot
to claim and post; Redis-lock coordinated.

**coordinator** (`apps/rust/services/coordinator/src/`) owns bot-shard
coordination and cross-cutting infra: `POST /api/internal/bot/coordinator/
shards/{register,heartbeat,release}`, `GET .../shards/assignments`, `GET
/api/internal/bot/coordinator/stats`, `POST .../rate-limit/consume`, and `POST
/api/discord/webhook-events` (Ed25519-signature-verified in the handler, not
bot-JWT). Background work: `cleanup::run` is an hourly retention sweep
(interval `CLEANUP_INTERVAL_SECONDS`, default 3600s) that deletes stale rows
from `wipe_reacts` (7d), `track_analytics_sessions_cache` (48h), and
`command_usage` (90d); runs once immediately at boot then on interval,
Redis-lock coordinated across replicas.

**Auth model**: the signed **`rustlens_session` cookie** is a cross-language
contract minted/verified byte-for-byte identically by `crates/rustlens-session`,
the frontend (`apps/frontend/lib/session.ts`), and (historically) the deleted
Go backend: `base64url(json) "." base64url(HMAC_SHA256(base64url(json)))`,
url-safe no-pad, key `APP_SESSION_SECRET`, times in **ms**, `epoch` =
revocation counter. Revocation is DB-epoch based (`app_users.session_epoch`,
plus `suspended_at`/`deleted_at`), which is why it works across all five
services via the shared Postgres. `/api/app/*` routes are gated by the
internal **`X-RustLens-App-Secret`** header (`APP_API_INTERNAL_SECRET`,
checked by `rustlens-httpauth`) and read the acting session from the
**`X-RustLens-Session`** header the Next.js server forwards; the browser never
calls them directly. OAuth login/callback, the role-connection flow, and
`/api/events/stream` authenticate via the cookie itself (top-level browser
navigation / EventSource). `/api/internal/bot/*` routes are gated by a shared
bot-service JWT (`rustlens-botjwt`, HS256, `BOT_SERVICE_JWT_SECRET`, one
audience `rustlens-backend` across all five services, no per-service
audiences).

Conventions: parameterized queries only; no service ever runs a schema
migration (see Schema below); keep `cargo clippy --all-targets` warning-clean;
response JSON must match the frontend TS types exactly.

A few cross-cutting behaviors worth knowing:

- The admin `GET /api/app/admin/guilds` route (guilds service) reads paginated rows from `bot_guilds` LEFT JOIN `guild_settings` LEFT JOIN `guild_tracked_servers` (count); `GET /api/app/admin/guilds/{guildID}` returns a read-only detail aggregate (metadata, settings, tracked servers, wipe reacts + schedules, member snapshots, guild Steam links, track groups, command usage) with page-addressed sub-lists.
- The admin `GET /api/app/admin/permissions` route (identity) returns the static permission catalog for the `/admin/roles` checkbox grid; role mutations go through the action dispatcher (`roles.create/update/delete/permissions.set`), all requiring `admins.grant` and a `reason`, and rejecting `is_system=true` roles.
- Admin BattleMetrics-server routes (servers service: `/api/app/admin/bm-servers*`) require `bm_servers.read`/`bm_servers.edit`, support search/sort/pagination, and a bulk endpoint (`set_high_freq`/`set_standard`/`delete`, capped at 200 IDs).
- **`/api/events/stream`** (identity) is cookie-authenticated SSE, backed by `rustlens-events`' in-process `Hub` (capped connections per user) plus its Redis bridge for multi-pod fanout. Admin actions publish `roles.changed`, `session.revoked`, and `session.expiring` events; session validation rejects suspended/deleted users in addition to the `session_epoch` check.
- `command_usage` writes (bot fire-and-forget via `POST /api/internal/bot/identity/command-usage`) and wipe-react schedule timing (guilds service enqueues due posts centrally; the bot shard that owns a guild claims and sends them) both moved onto their owning Rust service unchanged in behavior from the old Go implementation.

**Discord linked-roles configuration**

Linked Roles requires two configurations in the Discord Developer Portal under your application:

1. **OAuth2 -> Redirects**: add `{APP_ORIGIN}/api/auth/discord/role-connection/callback`.
2. **App -> Linked Roles Verification URL**: set to the same URL. This is what makes the "Link your account" button appear in your community Discord under the linked-role verification prompt.
3. `DISCORD_BOT_TOKEN` env var must be set on the identity service (share with the bot's `DISCORD_TOKEN`) - identity uses it to PUT the metadata schema at startup.

**App users & browser auth**

`app_users` stores first-class browser-facing RustLens users with roles such as `user` and `internal_admin`. Discord identity (snowflake, username, global name, avatar hash) lives directly on this table; sign-in is Discord OAuth only via `/api/auth/discord`.

`app_audit_events` stores browser-auth and internal-admin audit events.

`command_usage` stores every Discord bot interaction (chat command, button, modal submit, string-select) a user runs, with `app_user_id` (FK to `app_users(id) ON DELETE CASCADE`), `discord_id`, optional `guild_id` / `channel_id`, `interaction_type`, `command`, optional `subcommand` / `target`, raw `options` JSONB, `outcome` (`success|error|banned|denied`), optional `error_code`, `latency_ms`, optional `correlation_id` (per-invocation token; <=64 chars `[A-Za-z0-9_-]`), optional `parent_usage_id` (self-FK `ON DELETE SET NULL`), `created_at`. Written fire-and-forget by the bot via `POST /api/internal/bot/identity/command-usage`. The bot mints a `correlationId` per slash-command invocation and embeds it in any modal/button/select customId it builds; when those follow-ups arrive, the bot sends both its own row's `correlationId` and the originating `parentCorrelationId`, which identity resolves (same `app_user_id` only) into `parent_usage_id` at insert time. Purged nightly to a 90-day rolling window by the coordinator service's cleanup sweep. Surfaced on `/admin/users/{id}` Usage tab via `GET /api/app/admin/users/{id}/command-usage` (filterable by `q`, `outcome`, `interactionType`, `command`, `guildId`, `windowDays`); the list response filters out rows with `parent_usage_id IS NOT NULL` so each command flow is a single top-level row whose `childCount` indicates how many follow-ups it owns. Fan-out commands (`/announce`, `/summon`, `/nickname`, `/admin setsteam|removesteam`, `/manage ban`, `/setsteam`, `/transfer`, `/create invite`, `/track add|remove`) additionally write rows into `command_usage_targets` (FK CASCADE off `command_usage`, snapshotted `display_name` + `avatar_hash` at capture time, `relation` of `recipient|scope|subject`, capped at 500 per record). The admin list response embeds up to 100 targets per row inline and exposes the rest via `GET /api/app/admin/users/{id}/command-usage/{usageId}/targets`. Follow-up rows linked to a parent are exposed via `GET /api/app/admin/users/{id}/command-usage/{usageId}/children`. Filter autocomplete is served by `GET /api/app/admin/users/{id}/command-usage/filter-options`.

**Steam & Discord linking**

`steam_accounts` stores known Steam players and their optional BattleMetrics player ID (`bm_id`). The `bm_id` is populated when a hard link is created via BattleMetrics scrape. Volatile profile display/cache data lives in `steam_account_cache`, keyed by `steam_id`, with `bm_name`, `avatar_image`, first-seen metadata, total/category BattleMetrics hours, Steam Rust hours, game-ban cache fields (`game_banned`, `game_banned_at`, `game_ban_checked_at`), and an `updated_at` timestamp used for the 24-hour lazy profile refresh TTL.

`steam_discord_links` maps Steam accounts to Discord users with a `type` and `source`:
- **soft** — user self-reported via bot command or frontend. No verification that the Steam account actually belongs to them. Source examples: `bot_command`, `frontend`.
- **hard** — discovered from a trusted source (e.g., BattleMetrics API) where the mapping is authoritatively verified. Most likely valid. Source examples: `battlemetrics`.

A single (steam_id, discord_id) pair can have both a soft and hard link simultaneously. The relationship is many-to-many: one Steam account can link to multiple Discord users and vice versa.

Bot-internal endpoints under `/api/internal/bot/players/steam/` and `/api/internal/bot/players/discord/` (players service) provide CRUD for both tables. Admins can only delete soft links.

`hidden_discord_link_identities` stores Discord IDs whose Steam/Discord link associations should not be disclosed by link lookup endpoints. It is a read-time suppression flag: `steam_discord_links` and `guild_steam_discord_links` rows remain stored for internal integrity, future unhide, and dedupe, while global and guild link list reads omit hidden identities in both Steam→Discord and Discord→Steam directions.

`guild_steam_discord_links` maps Steam accounts to Discord users within a specific guild. Unlike the global `steam_discord_links` table, these are guild-scoped — a guild admin decides which steam-discord associations are relevant to their server. The composite PK is `(guild_id, discord_id, steam_id)`. These rows are deleted when the Discord user leaves that guild.

Bot-internal endpoints under `/api/internal/bot/players/guilds/{guildID}/` (players service) provide CRUD for guild-scoped links: create, list by discord ID, list by steam ID, and delete.

`user_guilds` records Discord users and their current guild membership state with composite PK `(discord_id, guild_id)`, `is_active`, `first_seen_at`, `last_seen_at`, and nullable `left_at`. Bot-internal endpoints under `/api/internal/bot/guilds/user-guilds` and `/api/internal/bot/guilds/discord/{discordID}/guilds` (guilds service) support marking a user active/seen in a guild, marking that user inactive/left for a guild, and listing all guild state rows for a Discord user. The bot populates this table for non-bot users via `guildMemberAdd` and `guildMemberRemove` events, and removes any guild-scoped steam links for that user when they leave.

### Bot (TypeScript)

```
apps/bot/src/
  index.ts              # Entrypoint: config -> register -> poll assignments -> manage shards
  config.ts             # Env config loader
  backend-client.ts     # HTTP client for backend shard API (register, heartbeat, release, assignments)
  guild-member-cache.ts # Shared in-memory expiring guild member snapshot cache for commands that need broad guild membership scans; refreshes only on demand after expiry
  shards.ts             # Shard reconciliation logic
  shard-runtime.ts      # discord.js ShardingManager wrapper
  shard-worker.ts       # Individual shard worker process + interaction handling (requires Guilds + GuildMembers + GuildVoiceStates intents) and refreshes bot presence to "tracking {users} users" from the steam_accounts row count
  admin-command.ts      # /admin setsteam|removesteam registration + guild-link admin flow
  settings-command.ts   # /settings embed + select-menu/modal flow for guild settings
  bind-command.ts       # /bind fullkit/turret/rocket/c4 modal registration + submit handling
  setsteam-command.ts   # /setsteam {steam_id} — user self-service guild soft-linking for Steam IDs
  player-command.ts     # /player {identifier} — public player profile lookup by Steam ID, supported steamcommunity.com profile URL, or BattleMetrics ID
  analyze-command.ts    # /analyze offline {identifier} and /analyze server {server} {timeframe} analytics commands
  checkgamebans-command.ts # /checkgamebans — guild admin scan of linked Steam IDs for recent Steam game bans using 6h cache TTL; method=deep requires PRO/website admin and expands through strong global links, replied ephemerally
  wipereact-command.ts  # /wipereact create/delete/notreacted/react + /wipereact schedule create/delete/edit flow, persistence sync, button handling, edit modal, and logs embeds
  summon-command.ts     # /summon {scope} — move all online voice members of a role into the invoker's voice channel (admin-only)
  announce-command.ts   # /announce {scope} {message} — DM an embed announcement to all members of a role except the invoker (admin-only)
  online-command.ts     # /online — public guild command that lists linked users currently online on tracked BattleMetrics servers
  nickname-command.ts   # /nickname — admin command that syncs guild nicknames to "{discord nickname} - {BattleMetrics name}" from guild links + steam_accounts
  compare-command.ts    # /compare {server?} {wipereact?} — admin comparison of all active guild voice channels against a tracked server, the most recent passed / specified wipe react, or both against each other when both options are supplied
  user-guilds.ts        # Shared bot-side sync for user_guilds active/inactive membership state on guildMemberAdd/guildMemberRemove
  view-command.ts       # /view user {discord_user|identifier} and /view all — view guild-linked steam/discord accounts
  server-command.ts     # /manage server add|edit|remove|list — owner-only tracked BattleMetrics server management in the control guild with add/edit modals, a guild-settings visibility toggle, and autocomplete-backed selection
  ban-command.ts        # /manage ban {user} — owner-only restriction command in the control guild; modal with 6 feature category checkboxes that toggles `user_bans.categories` on the backend
  ban-categories.ts     # Canonical 6 ban categories plus maps from (commandName, subcommand) and interaction customId prefixes to a category
  transfer-command.ts   # /transfer — guild-admin-only per-guild command that ingests Hexaytron /summary embed messages (by message ID) and bulk-creates global soft Steam/Discord links + guild-scoped links from them for users who are currently in the guild
  search-command.ts     # /search {string} {server} {include_offline?} — PRO-or-website-admin guild command that searches online `player_states` joined with `steam_account_cache.bm_name` on a global tracked server (autocompleted), optionally including players offline for less than 24 hours, and posts one /player profile embed per match; over 20 results returns an ephemeral "scope too wide" error
  track-command.ts      # /track create {group_name} {server} and /track delete {track_name} — admin-only guild command that creates/deletes tracking channels under the configured Tracked users category, sends a pinned online/offline embed, and exposes Add/Remove/Notifications/Analytics buttons. /track add {player} and /track remove {player} infer the track from the current channel, are usable by members who can view that track channel, and autocomplete against the inferred track server/current track members. A poller refreshes the pinned embed, renames the channel to "{slug}-{online_count}", deletes stale track rows when their channel is gone, and (when notifications are enabled) posts join/leave embeds. The Analytics button is PRO-or-website-admin gated and posts a non-ephemeral dark chart image backed by the backend track-group analytics route, with a weekday/hour heatmap and hourly bar chart using Europe/London minute buckets.
  create-command.ts     # /create invite {role} {required_role?} — guild-admin command that posts a public clan-invite request embed with a button; clicking the button validates the user's guild Steam link (and optional required role) and posts a `/clan invite {steamId}` message that pings the configured role + requester, with an admin-only Accept button that deletes the message and DMs the requester a "Clan Invite Pending" embed
  ban-check.ts          # In-memory TTL cache of per-user ban categories plus `enforceBan` / `enforceBanForAutocomplete` helpers used by the interaction dispatcher to short-circuit banned users with a red #d31239 "Error — You have been banned" embed
  website-admin.ts      # In-memory TTL cache + helper that checks the backend `pro` role first, then whether a Discord user is a website admin (any row in `admin_user_roles`); used by `/search`, `/track` analytics, and the `/player` links button / discord-user lookup
  manage-command.ts     # /manage — owner-only management surface in the control guild (server, proxy, ban, etc.) gated by `BOT_OWNER_DISCORD_IDS`
  owner-command.ts      # Shared owner-command scaffolding consumed by /manage
  owner-guard.ts        # Runtime owner-Discord-ID guard for owner-only commands
  dm-limit.ts           # Caps `/announce` and `/create invite` DMs in large guilds (`DM_GUILD_MEMBER_LIMIT`)
  bot-guilds.ts         # Bot-side sync of `bot_guilds` table on join/leave/reconcile
  guild-settings.ts     # Shared guild-settings fetch/cache helper
  shard-stats.ts        # Shard-side stats reporting helper
  tracked-server-logs.ts # Polls `/tracked-server-log-events` and posts join/leave embeds to the configured log channel
  track-analytics-chart.ts # PNG renderer for the weekday/hour heatmap + hourly bar chart on /track Analytics and /analyze offline
  server-analytics-chart.ts # PNG renderer for /analyze server player-count trend and weekday distribution charts
  next-best-offline.ts  # Pure logic for picking the lowest-avg-online 2-hour window from analytics data
  bm-input.ts / steam-input.ts # Identifier parsers (Steam ID, profile URLs, BM ID)
  embed-paginator.ts    # ◀️▶️ pagination fallback when an embed exceeds Discord limits
  proxy-aware-parallelism.ts # Concurrency helper that scales with proxy count when PROXY_ENABLED
  shared.ts             # Shared bot helpers
```

Flow: worker registers with backend -> polls `/assignments` -> reconciles local discord.js shards -> heartbeats leases -> releases on shutdown. Auth via self-signed JWT using `BOT_SERVICE_JWT_SECRET`.
Command flow: shard `0` registers `/settings`, `/setsteam`, `/player`, `/checkgamebans`, `/wipereact ...`, `/bind ...`, `/create ...`, `/summon`, `/announce`, `/online`, `/nickname`, `/compare`, `/view`, `/manage`, `/track`, `/transfer`, `/search`, `/analyze offline`, `/analyze server`, and `/admin ...` at startup. `/settings` is a guild-administrator-only command that renders an ephemeral settings embed with a select menu for configuring wipe-react logs, linking logs, tracked-server logs, the tracked-users category (parent for `/track` group channels), the admin role, and the guild's selected tracked servers. Guild tracked servers are chosen from the owner-managed global `/manage server list` entries that have `Include in guild settings?` enabled and are persisted separately from guild settings. Turning that toggle off later removes the server from every guild's selected tracked-server list. `/setsteam {steam_id}` is a public guild command that accepts either a 17-digit Steam ID or a supported `steamcommunity.com/profiles/...` or `steamcommunity.com/id/...` URL, resolves vanity URLs through the backend with `STEAM_API_KEY`, then creates a soft link in both the global `steam_discord_links` table and the guild-scoped `guild_steam_discord_links` table; if the user already has a guild Steam link, RustLens replies with an error embed instructing them to contact an administrator. When Linking Logs is configured, successful `/setsteam` calls emit an embed containing the Steam ID, optional BattleMetrics ID, and Discord user mention. `/player {identifier?} {discord_user?}` is a **global** command (usable in any guild or DM) where `identifier` accepts a 17-digit Steam ID, a supported `steamcommunity.com/profiles/...` or `steamcommunity.com/id/...` URL, or a BattleMetrics ID and looks up the matching `steam_accounts` row directly (no guild-link check), then renders the same player profile card as `/view user` using the backend steam-profile endpoint. The optional `discord_user` option is **PRO-or-website-admin only** and renders a `Links - {discord_name}` embed instead, listing the Discord user's strong (`hard`) and soft Steam links from `steam_discord_links`; soft entries that also appear as strong are dropped, and missing `bm_id`s are filled in via the same BattleMetrics fallback `/player` uses. Before rendering the embed, the bot calls the backend platform-sync route for that Discord ID, which triggers a 14-day-stale `platformsync.io` lookup that upserts any discovered Steam IDs (including previous IDs) as hard links. If the `steam_accounts` lookup misses for a Steam ID or BattleMetrics ID, the backend resolves Steam<->BM through the `steammatch` resolver chain (bmsearch.io primary, bmmatch fallback), persists the recovered Steam/BM link, and warms the profile cache before replying. For a cold Steam-first lookup that bmsearch is still resolving, the backend returns Steam-only data now and backfills the `bm_id` in the background (so a repeat lookup shows full data); when no resolver can resolve it the resolver returns `ErrSteamAccountNotFound`. If the stored `bm_id` refresh returns BattleMetrics `404 Unknown player`, `/player` now replies with an `Error` embed whose description is `BM profile private` instead of rendering the normal player card. The 🔗 links button on the `/player` and `/search` profile embeds is **PRO-or-website-admin only** and renders a `Links - {bm_name}` embed listing Discord users linked to the Steam ID, including strong and soft links. Before rendering the embed, the bot calls the backend platform-sync route for that Steam ID: the backend triggers an external `platformsync.io` lookup if the last sync for that Steam ID is older than 14 days, upserts any discovered Discord IDs (including previous IDs) as hard links, and the resulting embed reflects the freshly-upserted links. `/view user` accepts the same Steam inputs for its `identifier` field in addition to BattleMetrics IDs and Discord identifiers, resolving vanity URLs live through the backend without separate caching. `/checkgamebans` allows either Discord Administrator or the configured admin role, scans all guild-linked Steam IDs through the backend Steam ban endpoint, reuses cached results newer than 6 hours, ignores VAC-only bans, and posts a timeframe-filtered `Gamebans` embed defaulting to 90 days with optional 30d, 180d, and all-time choices. `/view` remains guild-scoped; `/player` is intentionally not. The card uses `steam_accounts.bm_id`, reads cached profile display/hour data from the backend steam-profile endpoint, sums total BattleMetrics playtime from all servers returned by BattleMetrics, sums category buckets from globally tracked `/manage server` entries, fetches Steam-owned Rust playtime directly, and includes a Risk Factor field composed of three factors (Steam→BM hour gap, servers played, BM hours) each computed against a separate formula. The card includes an analytics button (📈) that posts an ephemeral embed via the backend analytics endpoint showing top owned games, top played servers, and multi-month activity gaps from BattleMetrics name-change history, plus a links button (🔗) placeholder. `/view user` uses the same backend profile card flow, while `/view all` and `/view nosteam` keep their existing guild-admin-only summary behavior. `/admin setsteam {user} {steam_id}` and `/admin removesteam {user}` are protected by either Discord Administrator or the configured admin role and manage guild-scoped Steam links for other users; they never delete existing rows from the global `steam_discord_links` table, and `/admin setsteam` ensures a global soft link exists before replacing the guild link. `/online` is a public guild command that renders one or more non-ephemeral RustLens embeds grouping currently online linked users by tracked BattleMetrics server selected for that guild; if the embed limits are reached, follow-up embeds use the title `Online summary cont.`. `/nickname` allows either Discord Administrator or the configured admin role, reads one guild-scoped Steam link per Discord user from `/api/internal/bot/players/guilds/{guildID}/nickname-targets`, and attempts to set each member nickname to `{discord nickname} - {bm_name}` while counting missing `bm_id` / `bm_name` entries and Discord edit failures as failed. Commands that need full-guild member scans must use `apps/bot/src/guild-member-cache.ts` instead of calling `guild.members.fetch()` directly; the cache is in-memory, shared within the bot process, only refreshes on demand when expired, and deduplicates concurrent refreshes per guild to reduce `Request Guild Members` rate limits. `/compare {server?} {wipereact?}` is administrator-only, autocompletes tracked servers from global `tracked_servers`, autocompletes past guild wipe reacts by title/date, defaults to the most recently passed wipe react when no options are given, compares a wipe react against all active guild voice channels or the full guild member list when nobody is in voice, compares a server against all active guild voice channels, and compares the selected wipe react against the selected server when both options are provided. All comparisons ignore members or guild links that do not resolve through `guild_steam_discord_links -> steam_accounts.bm_id` where BattleMetrics player resolution is required. `/transfer` only creates guild-scoped links for users who are currently in the guild, skipping missing members from the imported embed data. Guild-scoped links are also removed automatically when a user leaves the guild. `/manage ban {user}` is owner-guild-only, registered only in `BOT_OWNER_GUILD_ID` and gated at runtime by `BOT_OWNER_DISCORD_IDS`. It opens a modal with six feature-category checkboxes (Setup And Configuration, Steam Linking And Identity, Player And Account Lookup, Activity Presence And Checks, Guild Coordination And Utility, Other) pre-filled with the user's current restrictions; submitting PUTs the selection to `/api/internal/bot/identity/bans/{discordID}` (empty selection deletes the ban row). The shard worker's `interactionCreate` dispatcher maps each incoming `(commandName, subcommand)`, and component `customId` prefixes for modals, buttons, and string selects, to a ban category via `apps/bot/src/ban-categories.ts` and calls `enforceBan` (30s TTL per-shard cache) from `apps/bot/src/ban-check.ts`. Banned users receive an ephemeral red `#d31239` embed titled `Error` with description `You have been banned`; the `/manage ban` command itself is always exempt so owners can never lock themselves out. `/manage server add`, `/manage server edit`, `/manage server remove`, and `/manage server list` are owner-guild-only commands registered only in `BOT_OWNER_GUILD_ID`; runtime access is restricted to the comma-separated Discord ID list in `BOT_OWNER_DISCORD_IDS`. `/manage server add` and `/manage server edit` open the same modal with server name, BattleMetrics ID, an `Include in guild settings?` toggle, and category checkboxes for `Vitality`, `Atlas`, `Vital`, `Modded`, `Official`, and `Build`. `/manage server edit` and `/manage server remove` use autocomplete-backed tracked-server selection instead of manual BM ID entry, and `/manage server list` shows the global tracked server registry with stored categories and poll status. `/summon {scope}`, `/announce {scope} {message}`, `/wipereact`, `/bind massinvite`, and `/create invite` allow either Discord Administrator or the configured admin role. `/bind` is `GuildInstall`-only (not user-installable) so subcommands like `/bind massinvite` cannot be invoked outside guilds where the bot is installed. `/wipereact` uses in-command autocomplete for selecting active wipe reacts on `/wipereact delete`, `/wipereact missing {scope} {wipereact}`, `/wipereact react add {user} {type} {wipereact}`, and `/wipereact react remove {user} {wipereact}`. Wipe reacts now require Wipe React Logs to be configured in `/settings`; successful wipe-react reaction changes continue to emit the existing log embeds to that configured channel. `/wipereact schedule create` opens a modal to create a recurring wipe-react schedule (max 3 per guild) with day selection (Mon-Sun + Force Wipe), time, timezone, repeat interval, and post-before timing; `/wipereact schedule delete` and `/wipereact schedule edit` manage existing schedules via autocomplete. Wipe reacts are persisted in Postgres through backend bot-only endpoints, including the message/channel IDs, config, and accepted/declined/late user lists, and shard workers rehydrate active wipe-react expiry timers on startup. `/wipereact missing` posts an embed titled `Missing reactants` listing members of the selected role who have not accepted, declined, or marked late; this requires the bot to have the Discord Guild Members privileged intent enabled. `/track {group_name} {server}` allows either Discord Administrator or the configured admin role; team name is capped at 12 characters, server is autocompleted from the global tracked-servers list, and the command creates a text channel under the configured Tracked users category, persists a `track_groups` row, sends a pinned online/offline embed with Add/Remove/Notifications/Analytics buttons, and runs a poller (every ~30s) that refreshes the embed, renames the channel to `{slug}-{online_count}`, and (when `send_updates` is enabled via the notifications modal) posts join/leave embeds in the channel. The Add modal accepts comma- or period-separated Steam IDs and BattleMetrics IDs, resolves Steam→BM via `steam_accounts` with the same lazy fallback as `/player`, and replies with a `Tracking Status` embed listing successful/unsuccessful counts and unresolved IDs. The 📈 Analytics button is restricted to PRO users or website admins and posts a non-ephemeral weekday analytics embed: each weekday lists the best-time-for-an-offline window (lowest 2-hour avg-online block) and the most-online hours; the embed pulls data from the backend track-group analytics route (200 BattleMetrics sessions per member per server, cached for 24h in `track_analytics_sessions_cache`, dropping days under 30 minutes of play), and falls back to gray ◀️▶️ pagination buttons that anyone can use when the rendered embed exceeds Discord limits.

`/analyze offline {identifier}` is a public global command registered by shard `0`; it accepts Steam IDs, supported Steam profile URLs, or BattleMetrics IDs and posts the same offline timing chart style as `/track` analytics without website-admin gating.

`/analyze server {server} {timeframe}` is a public global command registered by shard `0`; `server` autocompletes from bot-global tracked servers, `timeframe` supports `7d`, `14d`, `30d`, `60d`, and `90d`, and the chart renders BattleMetrics daily max/value/min player-count history with weekday distribution computed from the backend's cached 90-day window.

## Schema

The schema is owned entirely by `crates/rustlens-db/migrations/` and applied
by the `migrate` binary (`apps/rust/tools/migrate`) **before** any service
starts; none of the five services runs a migration itself. There is
currently one migration, `0001_baseline.sql` (the distilled end-state schema,
generated from the old Go `EnsureSchema()` output and massaged to be fully
idempotent, not a replay of its migration history); future schema changes are
added as new numbered files (`0002_*.sql`, etc.) and `0001` is never edited
after landing (sqlx tracks migration checksums). In `docker compose`, `migrate`
runs as a one-shot service (`restart: "no"`, gated on the Postgres
healthcheck) and every Rust service `depends_on: migrate:
service_completed_successfully`. In CI, the `rust` job runs `cargo run --bin
migrate` against a Postgres service container before `cargo test`. There is no
`EnsureSchema()`-equivalent auto-migration inside any service anymore.

## Infrastructure

Local infra is defined in `infra/docker-compose.yml`. Running `pnpm dev` / `pnpm infra:up` starts all of it (there is no compose profile flag needed; `up -d` brings up the whole fleet by default):

- **Postgres 18** on `:5432` (user/pass/db: `rustlens`)
- **Redis 8** on `:6379`
- **`migrate`**: one-shot (`restart: "no"`), builds `apps/rust/Dockerfile` with `SERVICE=migrate`, gated on the Postgres healthcheck; every service below depends on it completing successfully
- **`identity`** on `:8090`, **`players`** on `:8091`, **`servers`** on `:8092`, **`guilds`** on `:8093`, **`coordinator`** on `:8094`: all five build the same `apps/rust/Dockerfile` parametrized by a `SERVICE` build arg, read shared secrets from `infra/rustlens.env` (gitignored; template `infra/rustlens.env.example`), and connect to the compose Postgres/Redis directly (not through Traefik)
- **Traefik v3** on `:80` (HTTP) and `:8081` (dashboard), file-provider routed from `infra/traefik/dynamic/*.yml` (see Routing above)

The frontend and bot are **not** in compose; they run on the host via `pnpm dev:frontend`/`pnpm dev:bot` (or together via `pnpm dev`), and Traefik reaches the frontend at `host.docker.internal:3000`.

## Environment Variables

**Rust fleet** (`crates/rustlens-config` is the source of truth: five separate
per-service config structs, `IdentityConfig`/`PlayersConfig`/`ServersConfig`/
`GuildsConfig`/`CoordinatorConfig`, each with its own `from_env()`; no shared
struct). Local dev copies `infra/rustlens.env.example` to `infra/rustlens.env`
and fills it in; `PORT`, `POSTGRES_URL`, and `REDIS_URL` are set per-service by
`docker-compose.yml` itself and are **not** in that file.

| Variable | Default | Used by |
|---|---|---|
| `PORT` | `8090`/`8091`/`8092`/`8093`/`8094` | all (set by compose, not the env file) |
| `POSTGRES_URL` | `postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable` | all |
| `REDIS_URL` | `redis://localhost:6379/0` | all |
| `BOT_SERVICE_JWT_SECRET` | required | all (bot-JWT validation) |
| `BOT_JWT_ISSUER` | `rustlens-bot` | all |
| `BOT_JWT_AUDIENCE` | `rustlens-backend` | all |
| `APP_SESSION_SECRET` | required | identity, players, servers, guilds (not coordinator) |
| `APP_API_INTERNAL_SECRET` | required | identity, players, servers, guilds (not coordinator) |
| `DISCORD_CLIENT_ID` / `DISCORD_CLIENT_SECRET` | required | identity (OAuth) |
| `APP_ORIGIN` | `http://localhost` | identity |
| `APP_BASE_URL` | `http://localhost:3000` | identity |
| `PROXY_ENCRYPTION_KEY` | `` (optional) | identity (linked-roles token encryption), servers + players (proxy credential encryption) |
| `DISCORD_BOT_TOKEN` | `` (optional, enables linked-roles) | identity |
| `DISCORD_APP_PUBLIC_KEY` | `` (empty disables the webhook route) | coordinator (Ed25519 verification of `/api/discord/webhook-events`) |
| `STEAM_API_KEY` | `` | players |
| `BM_SEARCH_ENABLED` | `true` (primary Steam<->BM resolver: bmsearch.io public no-auth API) | players |
| `BM_SEARCH_URL` | `https://bmsearch.io/api` | players |
| `BM_SEARCH_INLINE_TIMEOUT_MS` | `4000` (inline wait before a cold lookup is backfilled in the background) | players |
| `BM_MATCH_URL` / `BM_MATCH_TOKEN` | `` (optional legacy bmmatch fallback, tried after bmsearch; disabled when unset) | players |
| `PLATFORMSYNC_TOKEN` | `` | players |
| `BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS` | `30` | servers |
| `PROXY_ENABLED` | `false` | servers, players |
| `PROXY_ENDPOINTS` | `` (CSV; legacy fallback when no DB-managed proxies exist) | servers, players |
| `WIPE_SCHEDULER_INTERVAL_SECONDS` | `60` | guilds |
| `TRUST_PROXY_HEADERS` | `false` | identity, players, servers, guilds |
| `TRUST_PROXY_IP_HEADER` | `CF-Connecting-IP` | identity, players, servers, guilds |
| `BOT_TARGET_SHARDS` | `4` | coordinator |
| `BOT_HEARTBEAT_TTL_SECONDS` | `45` | coordinator |
| `CLEANUP_INTERVAL_SECONDS` | `3600` | coordinator |
| `CLEANUP_WIPE_REACT_RETENTION_SECONDS` | `604800` (7d) | coordinator |
| `CLEANUP_ANALYTICS_CACHE_RETENTION_SECONDS` | `172800` (48h) | coordinator |
| `CLEANUP_COMMAND_USAGE_RETENTION_SECONDS` | `7776000` (90d) | coordinator |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | unset | all (see below) |

**Client IP (`TRUST_PROXY_HEADERS` / `TRUST_PROXY_IP_HEADER`)**: the four
app-facing services (identity/players/servers/guilds) resolve the real client
IP for `api_keys.last_used_ip` and `audit_events.ip_address` via
`rustlens_httpauth::clientip`, a faithful port of the Go `internal/clientip`
package. The services don't wire axum `ConnectInfo`, so the only trusted source
is an edge-injected header, and trusting it is opt-in: with `TRUST_PROXY_HEADERS`
off (the default) both columns stay NULL exactly as before. When enabled, the
service reads `TRUST_PROXY_IP_HEADER` (default `CF-Connecting-IP`, matching Go),
takes the first comma-separated token, and records it only if it parses as an
IP. Same env-var convention as the frontend rate limiter. The config lives on
each service's config struct and is installed once at boot via
`rustlens_httpauth::clientip::init`.

The BattleMetrics public API client is unauthenticated; there is no `BATTLEMETRICS_API_KEY` env var. Steam<->BM resolution is delegated to the `steammatch` resolver chain: the **bmsearch.io** public no-auth API (`BM_SEARCH_*`) as the primary source, with the legacy external `bmmatch` service (`BM_MATCH_URL` + `BM_MATCH_TOKEN`) as an optional fallback.

**Observability (`rustlens-otel`, every binary including `migrate`)**: with
`OTEL_EXPORTER_OTLP_ENDPOINT` unset (the local-dev default), every service
just logs to stdout via `tracing_subscriber`: no exporters, no network calls.
Setting it additionally installs an OTLP gRPC/tonic trace exporter plus a
30-second-interval cumulative-temporality metrics pipeline; a broken/
unreachable collector at boot falls back to stdout-only instead of crashing.
Also respected: `OTEL_SERVICE_NAME` (override), `OTEL_SERVICE_NAMESPACE`
(default `rustlens`), `DEPLOYMENT_ENV` (default `local`), `HOSTNAME` (for
`service.instance.id`), and `OTEL_TRACES_SAMPLER_ARG` (trace sample ratio,
default `0.1`). Logs always stay on stdout regardless (no OTLP log export);
Pyroscope profiling is not yet implemented.

**Bot** (`apps/bot/src/config.ts`, `apps/bot/.env.example`):

| Variable | Default |
|---|---|
| `DISCORD_TOKEN` | required for gateway login |
| `BOT_SERVICE_JWT_SECRET` | required (must match the Rust fleet's) |
| `BOT_JWT_ISSUER` | `rustlens-bot` |
| `BOT_JWT_AUDIENCE` | `rustlens-backend` |
| `BACKEND_URL` | `http://localhost:8080` |
| `BOT_WORKER_ID` | `bot-{pid}` |
| `BOT_RECONCILE_INTERVAL_MS` | `15000` |
| `BOT_HEARTBEAT_INTERVAL_MS` | `8000` |
| `BOT_OWNER_GUILD_ID` / `BOT_OWNER_DISCORD_IDS` | required for owner-only `/manage` |
| `PROXY_ENABLED` / `PROXY_ENDPOINTS` | `false` / `` (controls bot-side proxied request concurrency) |

> `BACKEND_URL`'s checked-in default (`http://localhost:8080`) predates the Go
> removal and no longer has anything listening on it. `apps/bot/src/backend-client.ts`
> prefixes every call with this single base URL and relies on domain-prefixed
> paths (`/api/internal/bot/{identity,players,servers,guilds,coordinator}/...`)
> to reach the right service, which only works if `BACKEND_URL` points at
> Traefik. Set it to `http://localhost` (or your Traefik origin) in your local
> `.env` until the checked-in default is updated.

**Frontend** has no checked-in `.env.example`. It expects `APP_SESSION_SECRET` and `APP_API_INTERNAL_SECRET` (shared with the Rust fleet's identity service), `RUSTLENS_API_URL` (server-side base URL for `/api/app/*` calls, checked-in default `http://127.0.0.1:8080`, same stale-port caveat as the bot's `BACKEND_URL` above; point it at Traefik), and `BACKEND_ORIGIN` (used only by `next.config.ts`'s `/api/*` rewrite proxy, default `http://localhost`, i.e. Traefik). `APP_ORIGIN` (or legacy `NEXT_PUBLIC_APP_URL`) must be the canonical external origin used for Discord OAuth callbacks. `DISCORD_CLIENT_ID` (server-side, shared with identity) is read by the dashboard to render the "Install Bot" sidebar link; if unset, the link is hidden. Auth-handler rate limiting uses `REDIS_URL` (or `RATE_LIMIT_REDIS_URL`) via `lib/rate-limit.ts`. `TRUST_PROXY_HEADERS` defaults to `false` and should only be enabled when the deployment edge strips and re-injects a dedicated canonical client-IP header configured via `TRUST_PROXY_IP_HEADER` for the rate limiter. `NEXT_SERVER_ACTIONS_ENCRYPTION_KEY` **must be set** in any multi-replica deployment to a stable 32-byte base64 value (`openssl rand -base64 32`) shared by every frontend pod, or rolling deploys produce "Failed to find Server Action" errors when a client built against pod A calls the action on pod B.

`/wipereact missing` requires the Discord Guild Members privileged intent because the shard worker requests `GatewayIntentBits.GuildMembers` (alongside `Guilds` and `GuildVoiceStates`).

## CI/CD

- **`.github/workflows/ci.yml`**: on PR and push to `main`, two jobs, both `runs-on: self-hosted`:
  - **`workspace`**: `pnpm install --frozen-lockfile`, then `pnpm lint`, `pnpm test`, `pnpm build` (frontend + bot).
  - **`rust`** (`working-directory: apps/rust`): spins up a `postgres:16-alpine` service container, installs the stable toolchain + clippy, then `cargo build --all-targets`, `cargo clippy --all-targets -- -D warnings`, `cargo run --bin migrate` (applies the schema to the service Postgres), and `cargo test` (with `RUSTLENS_TEST_PG_URL` pointed at the same DB so the DB-integration tests run). No Go steps exist anywhere in this workflow.
- **`.github/workflows/publish.yml`**: on push to `main` and `workflow_dispatch`, one `runs-on: self-hosted` job, matrix-driven over 8 images pushed to GHCR (`ghcr.io/<owner>/rustlens-<service>:main` + `:sha-<sha>`, lowercase owner): `frontend` (`apps/frontend/Dockerfile`), `bot` (`apps/bot/Dockerfile`), and `identity`/`players`/`servers`/`guilds`/`coordinator`/`migrate` (all six sharing the single parametrized `apps/rust/Dockerfile`, `SERVICE=<name>` build arg).
- **`.github/workflows/runner-diagnostics.yml`**: manual self-hosted runner validation workflow that checks checkout, workspace paths, and action cache layout to catch runner-hook regressions early.
- **`.github/dependabot.yml`**: weekly updates for `npm` (`/`), `docker` (`/apps/bot`, `/apps/frontend`, `/apps/rust`), `docker-compose` (`/infra`), `cargo` (`/apps/rust`), and `github-actions` (`/`). No `gomod` entry.

## Skills

Agent skills live in `.agents/skills/` (symlinked to `.claude/skills/` for Claude Code). Each skill is a directory with a `SKILL.md` file. When creating or editing skills, always write to `.agents/skills/` — the symlink ensures all agent tooling picks them up.

Current skills:

| Skill | Description |
|---|---|
| `battlemetrics-api` | BattleMetrics REST API reference (endpoints, auth, pagination, JSON:API shapes) |

