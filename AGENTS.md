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

RustLens is a monorepo with three services:

| App | Stack | Description |
|---|---|---|
| `apps/backend` | Go 1.24 (chi, pgx, go-redis) | API server and shard coordinator for bot workers |
| `apps/frontend` | Next.js 16 (React 19, TypeScript, App Router, Tailwind v4, shadcn/ui) | Marketing site plus browser-authenticated user dashboard at `/dashboard` and internal ops dashboard at `/admin` |
| `apps/bot` | Discord.js (TypeScript, ESM) | Sharded worker runtime |

Orchestrated with **pnpm 10 workspaces** + **Turborepo** for the frontend and bot packages. The Go backend is outside the pnpm workspace graph but integrated via the root `dev:all` script and Makefile targets.

## Common Commands

All commands run from the repo root unless noted.

```bash
make doctor          # Verify toolchain (Node, pnpm, Go, Docker, etc.)
make setup           # pnpm install + go mod tidy + install Air
make dev             # Start infra (Postgres, Redis, Traefik) + hot-reload backend, frontend, and bot
make infra-up        # Start only infra containers
make infra-down      # Stop infra containers
```

### Per-service dev

```bash
make dev-backend     # Air hot-reload for Go backend
make dev-frontend    # Next.js dev server for the landing app
make dev-bot         # tsx watch for bot
```

### Lint / Test / Build

```bash
make lint            # pnpm lint (frontend + bot) + go vet
make test            # pnpm test (frontend + bot) + go test
make build           # pnpm build (frontend + bot) + go build
```

### Running a single app's tests

```bash
# Go (supports ./... and package-level patterns)
cd apps/backend && go test ./internal/shards/...

# TypeScript (vitest)
pnpm --filter @rustlens/bot test
```

### Lint details

- **Bot**: `tsc --noEmit` (type-check only, no ESLint).
- **Backend**: `go vet ./...`

## Architecture

### Routing (Traefik)

Local dev runs Traefik on `http://localhost`:

- `/api/*` -> backend on `:8080`
- everything else -> frontend on `:3000`

Production routing is handled outside this repository.

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
    dashboard/page.tsx                  # Authenticated user dashboard root
    dashboard/guilds/[guildId]/page.tsx # Per-guild authenticated dashboard view
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
    auth-divider.tsx           # Divider used inside the auth panel
    not-found.tsx              # 404 panel reused by app/not-found.tsx
    legal-page.tsx / legal-toc.tsx       # Shared long-form legal page shell + TOC used by /privacy, /terms-of-service, /refund-policy
    chart-area-interactive.tsx, data-table.tsx, section-cards.tsx, latest-change.tsx, frequency-toggle.tsx, dashboard-skeleton.tsx  # Dashboard widgets
    nav-main.tsx, nav-secondary.tsx, nav-documents.tsx, nav-group.tsx, nav-links.tsx, nav-user.tsx, sidebar-nav-user.tsx, mobile-nav.tsx, desktop-nav.tsx, custom-sidebar-trigger.tsx  # Navigation primitives
    logo.tsx                   # Brand logo component
    ui/                        # shadcn/ui primitives added as source
  lib/
    rustlens-api.ts      # Server-side client for /api/app aggregate, admin, and (still-exposed) password-auth backend routes
    session.ts           # Signed Next.js `rustlens_session` cookie helpers (14-day TTL)
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
- **Login is Discord OAuth only.** `/login` redirects unauthenticated visitors to `/api/auth/discord` (handled by the Go backend, not Next.js). The signed `rustlens_session` cookie is set by the backend on successful callback; `APP_SESSION_SECRET` is required and shared with the backend. The backend still exposes password-auth endpoints (`/api/app/auth/register`, `/login`, `/password/reset/*`, `/email/verify/*`) and `lib/rustlens-api.ts` has client helpers for them, but **no frontend page invokes those flows today** — there is no email/password form, password reset UI, or verification UI.
- Auth-related browser actions (the Discord OAuth start link and `/logout` POST) use a double-submit CSRF token via `lib/csrf.ts`. Redis-backed rate limiting (`lib/rate-limit.ts`) is wired in for auth-related handlers.
- The frontend calls **app-facing backend routes** under `/api/app/*` with a shared internal app secret (`APP_API_INTERNAL_SECRET`) from server-side code only. Authenticated `/api/app/*` routes also receive the signed `rustlens_session` token via a server-only header so the backend derives the acting user from the validated session instead of trusting a caller-supplied user ID. The browser never calls these routes directly and the frontend does **not** call the bot JWT routes from the browser.
- The authenticated shell wraps shadcn `SidebarProvider`/`SidebarInset` primitives (no `@efferd/*` dependency).
- Next.js runs as a **hybrid Node server** in dev/prod rather than a static export.

### Backend (Go)

```
apps/backend/
  cmd/server/main.go         # Entrypoint: config -> stores -> service -> router -> server
  internal/
    config/config.go         # Env-based config with defaults
    http/
      router.go              # chi routes: /api/docs, /api/healthz, /api/internal/bot/shards/*, /api/internal/bot/owner/*, /api/internal/bot/setup/*, /api/auth/discord (+ /callback), /api/app/* (auth, me, admin)
      handlers.go            # Bot-internal handler implementations
      app_handlers.go        # /api/app aggregate, password-auth, and admin handlers
      openapi.go             # Embedded OpenAPI spec + Scalar docs UI
    auth/jwt.go              # JWT signing/validation for bot service auth + app session token
    appauth/                 # App-session auth: handler, repo, middleware, Discord-OAuth-driven session creation
    oauth/discord/           # Discord OAuth client + initiate/callback handlers backing /api/auth/discord
    admin/                   # Admin RBAC: roles, permissions, audit, action dispatcher
    battlemetrics/
      client.go              # BattleMetrics HTTP client (unauthenticated public API; routed through outbound proxy provider)
      poller.go              # Background rolling poller for tracked servers
    bmmatch/                 # External Steam<->BM resolution service client (BM_MATCH_URL + BM_MATCH_TOKEN, direct/never proxied)
    platformsync/            # platformsync.io client for cross-platform identity discovery
    outboundhttp/            # Outbound HTTP provider with proxy rotation, per-proxy health window, and auto-disable
    proxycrypto/             # AES-GCM encrypt/decrypt for outbound_proxies storage
    steamapi/                # Steam Web API client (profile, owned games, ban lookups)
    cleanup/                 # Background data-cleanup service
    wipereactscheduler/      # Background scheduler enqueuing due wipe-react schedule posts
    users/                   # App-user session helpers
    shards/
      service.go             # Shard coordination business logic
      allocator.go           # Shard assignment algorithm
      heartbeat.go           # Heartbeat/lease management
    store/
      postgres.go            # pgx pool + EnsureSchema (auto-migrates all tables listed under "Schema" below)
      app_users.go           # App-user, connected-account, aggregate, and admin listing store logic
      app_password_auth.go   # Password credential, reset-token, and email-verification store logic
      admin/audit             # Admin RBAC + audit-event store logic
      wipe_reacts.go          # Persisted wipe react CRUD + reaction mutation helpers
      wipe_react_schedules.go # Recurring wipe-react schedule CRUD
      wipe_react_schedule_posts.go # Backend-owned schedule post queue (claim/complete/release)
      redis.go               # go-redis client for heartbeats, leases, and notification claim keys
      steam.go                # Steam account & Discord link CRUD (upsert, lookup, profile metadata refresh, bulk hard links, soft link delete, guild nickname target joins)
      tracked_servers.go      # Global tracked BattleMetrics server CRUD
      outbound_proxies.go     # Encrypted outbound proxy CRUD for owner-managed BattleMetrics proxy rotation
      guild_tracked_servers.go # Guild-specific selection of owner-managed tracked servers
      player_states.go        # Per-server BattleMetrics player presence state sync and lookup
      online_summary.go       # Guild online summary join across guild links, global tracked servers, steam accounts, and player_states
      tracked_log_events.go   # Snapshot source for guild tracked-server log events
      track_groups.go         # /track-groups + members + online state + notification claims
      track_analytics.go      # Cached BM-session analytics for /track and offline analytics
      bot_guilds.go           # Bot guild metadata sync (upsert/leave/reconcile)
      platform_sync.go        # platform_sync_state CRUD for the 14-day platformsync TTL
      owner_stats.go          # Aggregated stats for /api/internal/bot/owner/stats
      cleanup.go              # Background-cleanup queries
      user_bans.go            # Owner-managed per-discord-user ban categories (used by the bot `/ban` command)
```

Key patterns:

- Schema is auto-migrated at startup via `EnsureSchema()` (no migration tool).
- Bot routes (`/api/internal/bot/shards/*`) are JWT-protected via middleware.
- Bot owner proxy routes (`/api/internal/bot/owner/proxies*`) are JWT-protected and back owner-guild-only `/manage proxy add|edit|list|remove`. Proxy URLs are encrypted at rest in `outbound_proxies` with AES-GCM using `PROXY_ENCRYPTION_KEY`; API responses and logs only expose credential-free `scheme://host:port` values. DB proxies are preferred over legacy `PROXY_ENDPOINTS`, and create/update/delete reloads the in-process outbound provider without a backend restart. The outbound provider tracks each proxy's request results over a 10-minute window and auto-disables DB proxies whose error rate reaches at least 50% after 10 or more requests.
- Bot guild settings routes (`/api/internal/bot/setup/guilds/*/settings`) are JWT-protected and store per-guild log channels, the tracked-users category (under which `/track` creates per-group channels), and the admin role.
- Bot track-group routes (`/api/internal/bot/setup/guilds/*/track-groups*`) are JWT-protected and persist `track_groups` (per-guild named tracking groups bound to a tracked server, with `channel_id`, `message_id`, `send_updates`, `online_count`) plus `track_group_members` (steam/bm IDs tracked by the group). The bot polls `/track-groups/{id}/online-state` to refresh the pinned group embed and channel name; online-state rows include optional guild-linked Discord user and avatar data for join/leave notification embeds. Before sending a join/leave notification, the bot claims `/track-groups/{id}/notifications/claim`, which uses a short Redis SETNX key per `(track group, steam ID, state, online timestamp)` so duplicate shard/process pollers do not post the same transition twice.
- Bot track-group analytics route (`/api/internal/bot/setup/guilds/{guildID}/track-groups/{groupID}/analytics`) is JWT-protected and computes per-weekday hourly minute averages for the group's members in `Europe/London`. It fetches up to 200 (2 pages x 100) BattleMetrics sessions per `(player_bm_id, server_bm_id)` using the proxied outbound provider with concurrency equal to the proxy count, caches the raw sessions in `track_analytics_sessions_cache` with a 24-hour TTL per `(player, server)` pair, drops player-days where that player accumulated under 60 minutes, and returns chart-ready 0-60 minute hour buckets plus the per-weekday best-offline window.
- Bot player offline analytics route (`/api/internal/bot/setup/players/{bmID}/offline-analytics`) is JWT-protected and reuses `track_analytics_sessions_cache` with an all-servers cache key. It fetches `GET /players/{bmID}/relationships/sessions?page[size]=100` for the first 2 pages without a server filter, then returns the same chart-ready weekday/hour offline window data as track analytics.
- Bot guild tracked-server routes (`/api/internal/bot/setup/guilds/*/tracked-servers`) are JWT-protected and store each guild's selected subset of the owner-managed tracked server list.
- Bot tracked-server routes (`/api/internal/bot/setup/tracked-servers*`) are JWT-protected and persist the bot's global BattleMetrics server list in Postgres.
- Bot player-state routes (`/api/internal/bot/setup/player-states/*`) are JWT-protected and expose BattleMetrics presence state by server and by player. The search route (`/player-states/servers/{serverID}/search?q=&limit=&includeOffline=`) does an `ILIKE '%q%'` match on `steam_account_cache.bm_name` joined with `steam_accounts` + `player_states` for one tracked server, returns online rows by default, optionally includes rows where `player_states.offline_since` is within the last 24 hours when `includeOffline=true`, is capped at `limit` (default 20, max 20), and is the source for the bot `/search` command. When `/search` fans out profile refreshes after the DB search returns, the bot now runs them with concurrency equal to the configured proxy count when `PROXY_ENABLED=true`, otherwise sequentially.
- Bot steam profile route (`/api/internal/bot/setup/steam/{steamID}/profile`) is JWT-protected and lazily refreshes `steam_account_cache` after a 24-hour TTL. Fresh cache entries skip Steam/BattleMetrics API calls; stale cache entries are returned if refresh fails unless BattleMetrics responds with `404 Unknown player`, in which case the route returns `404 battlemetrics profile private`. The route caches BattleMetrics name, Steam avatar, first-seen timestamp, total BattleMetrics hours across all returned servers, tracked-server category buckets, Steam Rust playtime, and the count of distinct BattleMetrics servers played (`bm_servers_played`). The response also includes optional current/last tracked-server status derived from `player_states` joined to `tracked_servers` for the profile embed.
- Bot steam account lookup routes (`/api/internal/bot/setup/steam/{steamID}` and `/api/internal/bot/setup/steam/by-bm/{bmID}`) backfill missing `steam_accounts` rows by resolving Steam<->BM through the external `bmmatch` service (`internal/bmmatch/`, configured via `BM_MATCH_URL` + `BM_MATCH_TOKEN`) and immediately warm `steam_account_cache` from the resolved profile data. These fallback requests are sent directly and are never proxied. When `bmmatch` is unconfigured, backfill resolvers return `ErrSteamAccountNotFound` rather than failing the parent request.
- Bot link lookup routes suppress Discord IDs present in `hidden_discord_link_identities`, hiding the user's Steam links when looking up by Discord ID and hiding that Discord account when looking up any associated Steam ID without deleting link rows.
- Bot steam-account count route (`/api/internal/bot/setup/steam-accounts/count`) is JWT-protected and returns the current `steam_accounts` row count so shards can keep the bot presence text in sync with tracked users.
- Bot steam analytics route (`/api/internal/bot/setup/steam/{steamID}/analytics`) is JWT-protected and uses the same 24-hour `steam_account_cache.updated_at` TTL and refresh path as the steam profile route. Profile and analytics share the same cached Steam/BattleMetrics data, including name-gap data used by both the player risk factor and analytics gap display. If Steam summary reports `communityvisibilitystate = 1`, RustLens skips `GetOwnedGames` and leaves game totals/playtime private. The response includes top 5 owned Steam games by playtime with `totalGames` (null if the Steam profile is private), top 5 BattleMetrics servers by playtime with `totalServers`, and `gaps` between consecutive name identifier periods longer than ~1 month (each as `fromUnix`/`toUnix`).
- Bot guild game-ban route (`/api/internal/bot/setup/guilds/{guildID}/game-bans`) is JWT-protected and scans all guild-linked Steam IDs through Steam `ISteamUser/GetPlayerBans`, caching `game_banned`, `game_banned_at`, and `game_ban_checked_at` in `steam_account_cache` with a 6-hour TTL. The response only includes linked users with game bans newer than 90 days; VAC bans without game bans are ignored.
- Bot guild online summary route (`/api/internal/bot/setup/guilds/{guildID}/online-summary`) is JWT-protected and returns guild-linked users currently online on bot-wide tracked BattleMetrics servers by reading current presence from `player_states`; it does not filter by per-guild selected tracked servers.
- Bot tracked-server log route (`/api/internal/bot/setup/guilds/{guildID}/tracked-server-log-events`) is JWT-protected and returns the current `player_states` snapshot joined against `guild_tracked_servers` + `guild_steam_discord_links` so only rows relevant to that guild's linked players on that guild's selected servers come through. The bot shard polls this every ~20s, diffs the current snapshot against its prior in-memory snapshot for that guild, and posts join/leave embeds to `tracked_server_log_channel_id` when a player's `is_online` state changes.
- Bot guild nickname target route (`/api/internal/bot/setup/guilds/{guildID}/nickname-targets`) is JWT-protected and returns one guild-linked Steam mapping per Discord user joined with `steam_accounts.bm_id` and `steam_account_cache.bm_name` for nickname sync.
- Bot wipe-react routes (`/api/internal/bot/setup/guilds/*/wipe-reacts*`) are JWT-protected and persist wipe react metadata plus accepted/declined/late user lists in Postgres.
- Bot wipe-react schedule routes (`/api/internal/bot/setup/guilds/*/wipe-react-schedules*`) are JWT-protected and persist recurring wipe-react schedule configs (max 3 per guild) with CRUD operations.
- The backend now owns wipe-react schedule timing. It enqueues due schedule posts centrally, and the bot shard that owns a guild claims queued posts from backend and sends the Discord message.
- Bot ban routes (`/api/internal/bot/setup/bans/{discordID}`) are JWT-protected and back the owner-only `/ban` command. `GET` returns `{discordId, categories[]}` (empty array when no row); `PUT` upserts, or deletes the row when `categories` is empty. Valid category values: `setup`, `linking`, `lookup`, `activity`, `coordination`, `other`.
- Bot website-admin lookup route (`/api/internal/bot/setup/discord/{discordID}/is-website-admin`) is JWT-protected and returns `{ "isAdmin": bool }`. A Discord user is a website admin when their `user_connected_accounts` row (`provider='discord'`) maps to an `app_users.id` that has at least one row in `admin_user_roles`. Used by `/search` and the `/player` links button / discord-user lookup.
- Bot platform-sync route (`POST /api/internal/bot/setup/platform-sync/{platform}/{id}` with `platform` of `steam` or `discord`) is JWT-protected and runs an external `platformsync.io` lookup if the last sync for that `(platform, id)` pair in `platform_sync_state` is older than 14 days. The upstream URL is hard-coded to `https://link.platformsync.io/api.php`; the bearer-style token comes from `PLATFORMSYNC_TOKEN`. Requests are sent **directly** (never proxied). Every `(steam_id, discord_id)` tuple discovered — including all `previous_*_ids` — is upserted as a hard link in `steam_discord_links` with `source='platformsync'`. Only the queried side is recorded in `platform_sync_state`, so a later sync of one of the discovered IDs still hits the upstream.
- OpenAPI spec is served at `/api/docs/openapi.json`; interactive docs at `/api/docs`.
- Discord OAuth lives at `/api/auth/discord` (initiate) and `/api/auth/discord/callback` on the **backend**, served by `internal/oauth/discord/`. The callback creates the signed `rustlens_session` cookie shared with Next.js.
- App-facing routes under `/api/app/*` are protected by an internal shared-secret header used only by Next.js server-side code. Session-authenticated `/api/app/*` routes additionally validate the signed frontend session token with `APP_SESSION_SECRET` and derive the actor from that session before resolving current-user/admin authorization through `app_users` roles. Admin RBAC routes are mounted under `/api/app/admin/*` and back the internal-admin dashboard (roles, users, guilds, servers, audit, action dispatcher, CSV export).
- Password-auth routes (`/api/app/auth/register|login|password/reset/*|email/verify/*`) exist on the backend, but no frontend page calls them today — the live login is Discord-only.
- The backend runs a rolling BattleMetrics poller that queries one tracked server every `BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS`, polling three `tracked_servers.high_frequency=true` servers followed by one non-high-frequency server when both subsets exist. It updates `tracked_servers.last_polled_at` / `last_poll_error` and syncs per-server player presence into `player_states`.

**App users & browser auth**

`app_users` stores first-class browser-facing RustLens users with roles such as `user` and `internal_admin`.

`user_connected_accounts` stores external identities attached to an app user. Discord is optional here and is used for guild/game identity enrichment and convenience sign-in when enabled.

`user_emails` stores primary and secondary email identities for app users.

`user_password_credentials` stores bcrypt password hashes and password update timestamps. New and reset passwords require at least 12 characters. `password_reset_tokens` and `email_verification_tokens` store one-time password reset and email verification token hashes, not raw tokens.

`app_audit_events` stores browser-auth and internal-admin audit events. The frontend signs users into Next.js sessions after successful email/password auth, password reset, or optional Discord OAuth completion via `/api/app/auth/*`. Email verification only marks the email verified and redirects back to the login flow. Raw reset / verification tokens are not returned by default; explicit local debug previews require both backend `ALLOW_AUTH_DEBUG_TOKENS=true` and frontend `AUTH_DEBUG_TOKEN_PREVIEW=true`.

**Steam & Discord linking**

`steam_accounts` stores known Steam players and their optional BattleMetrics player ID (`bm_id`). The `bm_id` is populated when a hard link is created via BattleMetrics scrape. Volatile profile display/cache data lives in `steam_account_cache`, keyed by `steam_id`, with `bm_name`, `avatar_image`, first-seen metadata, total/category BattleMetrics hours, Steam Rust hours, game-ban cache fields (`game_banned`, `game_banned_at`, `game_ban_checked_at`), and an `updated_at` timestamp used for the 24-hour lazy profile refresh TTL.

`steam_discord_links` maps Steam accounts to Discord users with a `type` and `source`:
- **soft** — user self-reported via bot command or frontend. No verification that the Steam account actually belongs to them. Source examples: `bot_command`, `frontend`.
- **hard** — discovered from a trusted source (e.g., BattleMetrics API) where the mapping is authoritatively verified. Most likely valid. Source examples: `battlemetrics`.

A single (steam_id, discord_id) pair can have both a soft and hard link simultaneously. The relationship is many-to-many: one Steam account can link to multiple Discord users and vice versa.

Bot-internal endpoints under `/api/internal/bot/setup/steam/` and `/api/internal/bot/setup/discord/` provide CRUD for both tables. Admins can only delete soft links.

`hidden_discord_link_identities` stores Discord IDs whose Steam/Discord link associations should not be disclosed by link lookup endpoints. It is a read-time suppression flag: `steam_discord_links` and `guild_steam_discord_links` rows remain stored for internal integrity, future unhide, and dedupe, while global and guild link list reads omit hidden identities in both Steam→Discord and Discord→Steam directions.

`guild_steam_discord_links` maps Steam accounts to Discord users within a specific guild. Unlike the global `steam_discord_links` table, these are guild-scoped — a guild admin decides which steam-discord associations are relevant to their server. The composite PK is `(guild_id, discord_id, steam_id)`. These rows are deleted when the Discord user leaves that guild.

Bot-internal endpoints under `/api/internal/bot/setup/guilds/{guildID}/` provide CRUD for guild-scoped links: create, list by discord ID, list by steam ID, and delete.

`user_guilds` records Discord users and their current guild membership state with composite PK `(discord_id, guild_id)`, `is_active`, `first_seen_at`, `last_seen_at`, and nullable `left_at`. Bot-internal endpoints under `/api/internal/bot/setup/user-guilds` and `/api/internal/bot/setup/discord/{discordID}/guilds` support marking a user active/seen in a guild, marking that user inactive/left for a guild, and listing all guild state rows for a Discord user. The bot now populates this table for non-bot users via `guildMemberAdd` and `guildMemberRemove` events, and removes any guild-scoped steam links for that user when they leave.

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
  analyze-command.ts    # /analyze offline {identifier} — public offline timing chart for one player using BattleMetrics sessions across all servers
  checkgamebans-command.ts # /checkgamebans — guild admin scan of linked Steam IDs for recent Steam game bans using 6h cache TTL, replied ephemerally
  wipereact-command.ts  # /wipereact create/delete/notreacted/react + /wipereact schedule create/delete/edit flow, persistence sync, button handling, edit modal, and logs embeds
  summon-command.ts     # /summon {scope} — move all online voice members of a role into the invoker's voice channel (admin-only)
  announce-command.ts   # /announce {scope} {message} — DM an embed announcement to all members of a role except the invoker (admin-only)
  online-command.ts     # /online — public guild command that lists linked users currently online on tracked BattleMetrics servers
  nickname-command.ts   # /nickname — admin command that syncs guild nicknames to "{discord nickname} - {BattleMetrics name}" from guild links + steam_accounts
  compare-command.ts    # /compare {server?} {wipereact?} — admin comparison of the invoker's current voice channel against a tracked server, the most recent passed / specified wipe react, or both against each other when both options are supplied
  user-guilds.ts        # Shared bot-side sync for user_guilds active/inactive membership state on guildMemberAdd/guildMemberRemove
  view-command.ts       # /view user {discord_user|identifier} and /view all — view guild-linked steam/discord accounts
  server-command.ts     # /server add|edit|remove|list — owner-only tracked BattleMetrics server management in the control guild with add/edit modals, a guild-settings visibility toggle, and autocomplete-backed selection
  ban-command.ts        # /ban {user} — owner-only restriction command in the control guild; modal with 6 feature category checkboxes that toggles `user_bans.categories` on the backend
  ban-categories.ts     # Canonical 6 ban categories plus maps from (commandName, subcommand) and interaction customId prefixes to a category
  transfer-command.ts   # /transfer — guild-admin-only per-guild command that ingests Hexaytron /summary embed messages (by message ID) and bulk-creates global soft Steam/Discord links + guild-scoped links from them for users who are currently in the guild
  search-command.ts     # /search {string} {server} {include_offline?} — website-admin-only guild command that searches online `player_states` joined with `steam_account_cache.bm_name` on a global tracked server (autocompleted), optionally including players offline for less than 24 hours, and posts one /player profile embed per match; over 20 results returns an ephemeral "scope too wide" error
  track-command.ts      # /track create {group_name} {server} and /track delete {track_name} — admin-only guild command that creates/deletes tracking channels under the configured Tracked users category, sends a pinned online/offline embed, and exposes Add/Remove/Notifications/Analytics buttons. /track add {player} and /track remove {player} infer the track from the current channel, are usable by members who can view that track channel, and autocomplete against the inferred track server/current track members. A poller refreshes the pinned embed, renames the channel to "{slug}-{online_count}", deletes stale track rows when their channel is gone, and (when notifications are enabled) posts join/leave embeds. The Analytics button is website-admin gated and posts a non-ephemeral dark chart image backed by the backend track-group analytics route, with a weekday/hour heatmap and hourly bar chart using Europe/London minute buckets.
  create-command.ts     # /create invite {role} {required_role?} — guild-admin command that posts a public clan-invite request embed with a button; clicking the button validates the user's guild Steam link (and optional required role) and posts a `/clan invite {steamId}` message that pings the configured role + requester, with an admin-only Accept button that deletes the message and DMs the requester a "Clan Invite Pending" embed
  ban-check.ts          # In-memory TTL cache of per-user ban categories plus `enforceBan` / `enforceBanForAutocomplete` helpers used by the interaction dispatcher to short-circuit banned users with a red #d31239 "Error — You have been banned" embed
  website-admin.ts      # In-memory TTL cache + helper that asks the backend whether a Discord user is a website admin (any row in `admin_user_roles`); used by `/search` and the `/player` links button / discord-user lookup
  manage-command.ts     # /manage — owner-only management surface in the control guild (proxies, etc.) gated by `BOT_OWNER_DISCORD_IDS`
  owner-command.ts      # Shared owner-command scaffolding consumed by /manage and /server
  owner-guard.ts        # Runtime owner-Discord-ID guard for owner-only commands
  dm-limit.ts           # Caps `/announce` and `/create invite` DMs in large guilds (`DM_GUILD_MEMBER_LIMIT`)
  bot-guilds.ts         # Bot-side sync of `bot_guilds` table on join/leave/reconcile
  guild-settings.ts     # Shared guild-settings fetch/cache helper
  shard-stats.ts        # Shard-side stats reporting helper
  tracked-server-logs.ts # Polls `/tracked-server-log-events` and posts join/leave embeds to the configured log channel
  track-analytics-chart.ts # PNG renderer for the weekday/hour heatmap + hourly bar chart on /track Analytics and /analyze offline
  next-best-offline.ts  # Pure logic for picking the lowest-avg-online 2-hour window from analytics data
  bm-input.ts / steam-input.ts # Identifier parsers (Steam ID, profile URLs, BM ID)
  embed-paginator.ts    # ◀️▶️ pagination fallback when an embed exceeds Discord limits
  proxy-aware-parallelism.ts # Concurrency helper that scales with proxy count when PROXY_ENABLED
  shared.ts             # Shared bot helpers
```

Flow: worker registers with backend -> polls `/assignments` -> reconciles local discord.js shards -> heartbeats leases -> releases on shutdown. Auth via self-signed JWT using `BOT_SERVICE_JWT_SECRET`.
Command flow: shard `0` registers `/settings`, `/setsteam`, `/player`, `/checkgamebans`, `/wipereact ...`, `/bind ...`, `/create ...`, `/summon`, `/announce`, `/online`, `/nickname`, `/compare`, `/view`, `/server`, `/ban`, `/manage`, `/track`, `/transfer`, `/search`, `/analyze offline`, and `/admin ...` at startup. `/settings` is a guild-administrator-only command that renders an ephemeral settings embed with a select menu for configuring wipe-react logs, linking logs, tracked-server logs, the tracked-users category (parent for `/track` group channels), the admin role, and the guild's selected tracked servers. Guild tracked servers are chosen from the owner-managed global `/server` list entries that have `Include in guild settings?` enabled and are persisted separately from guild settings. Turning that toggle off later removes the server from every guild's selected tracked-server list. `/setsteam {steam_id}` is a public guild command that accepts either a 17-digit Steam ID or a supported `steamcommunity.com/profiles/...` or `steamcommunity.com/id/...` URL, resolves vanity URLs through the backend with `STEAM_API_KEY`, then creates a soft link in both the global `steam_discord_links` table and the guild-scoped `guild_steam_discord_links` table; if the user already has a guild Steam link, RustLens replies with an error embed instructing them to contact an administrator. When Linking Logs is configured, successful `/setsteam` calls emit an embed containing the Steam ID, optional BattleMetrics ID, and Discord user mention. `/player {identifier?} {discord_user?}` is a **global** command (usable in any guild or DM) where `identifier` accepts a 17-digit Steam ID, a supported `steamcommunity.com/profiles/...` or `steamcommunity.com/id/...` URL, or a BattleMetrics ID and looks up the matching `steam_accounts` row directly — no guild-link check — then renders the same player profile card as `/view user` using the backend steam-profile endpoint. The optional `discord_user` option is **website-admin-only** and renders a `Links - {discord_name}` embed instead, listing the Discord user's strong (`hard`) and soft Steam links from `steam_discord_links`; soft entries that also appear as strong are dropped, and missing `bm_id`s are filled in via the same BattleMetrics fallback `/player` uses. Before rendering the embed, the bot calls the backend platform-sync route for that Discord ID, which triggers a 14-day-stale `platformsync.io` lookup that upserts any discovered Steam IDs (including previous IDs) as hard links. If the `steam_accounts` lookup misses for a Steam ID or BattleMetrics ID, the backend resolves Steam<->BM through the external `bmmatch` service, persists the recovered Steam/BM link, and warms the profile cache before replying (when `bmmatch` is unconfigured the resolver returns `ErrSteamAccountNotFound`). If the stored `bm_id` refresh returns BattleMetrics `404 Unknown player`, `/player` now replies with an `Error` embed whose description is `BM profile private` instead of rendering the normal player card. The 🔗 links button on the `/player` and `/search` profile embeds renders a `Links - {bm_name}` embed listing Discord users linked to the Steam ID; non-website-admins only see the strong-links section, website admins also see the soft-links section. Before rendering the embed, the bot calls the backend platform-sync route for that Steam ID — the backend triggers an external `platformsync.io` lookup if the last sync for that Steam ID is older than 14 days, upserts any discovered Discord IDs (including previous IDs) as hard links, and the resulting embed reflects the freshly-upserted links. `/view user` accepts the same Steam inputs for its `identifier` field in addition to BattleMetrics IDs and Discord identifiers, resolving vanity URLs live through the backend without separate caching. `/checkgamebans` allows either Discord Administrator or the configured admin role, scans all guild-linked Steam IDs through the backend Steam ban endpoint, reuses cached results newer than 6 hours, ignores VAC-only bans, and posts a `Gamebans < 90 days` embed showing only game bans newer than 90 days. `/view` remains guild-scoped; `/player` is intentionally not. The card uses `steam_accounts.bm_id`, reads cached profile display/hour data from the backend steam-profile endpoint, sums total BattleMetrics playtime from all servers returned by BattleMetrics, sums category buckets from globally tracked `/server` entries, fetches Steam-owned Rust playtime directly, and includes a Risk Factor field composed of three factors (Steam→BM hour gap, servers played, BM hours) each computed against a separate formula. The card includes an analytics button (📈) that posts an ephemeral embed via the backend analytics endpoint showing top owned games, top played servers, and multi-month activity gaps from BattleMetrics name-change history, plus a links button (🔗) placeholder. `/view user` uses the same backend profile card flow, while `/view all` and `/view nosteam` keep their existing guild-admin-only summary behavior. `/admin setsteam {user} {steam_id}` and `/admin removesteam {user}` are protected by either Discord Administrator or the configured admin role and manage guild-scoped Steam links for other users; they never delete existing rows from the global `steam_discord_links` table, and `/admin setsteam` ensures a global soft link exists before replacing the guild link. `/online` is a public guild command that renders one or more non-ephemeral RustLens embeds grouping currently online linked users by tracked BattleMetrics server selected for that guild; if the embed limits are reached, follow-up embeds use the title `Online summary cont.`. `/nickname` allows either Discord Administrator or the configured admin role, reads one guild-scoped Steam link per Discord user from `/api/internal/bot/setup/guilds/{guildID}/nickname-targets`, and attempts to set each member nickname to `{discord nickname} - {bm_name}` while counting missing `bm_id` / `bm_name` entries and Discord edit failures as failed. Commands that need full-guild member scans must use `apps/bot/src/guild-member-cache.ts` instead of calling `guild.members.fetch()` directly; the cache is in-memory, shared within the bot process, only refreshes on demand when expired, and deduplicates concurrent refreshes per guild to reduce `Request Guild Members` rate limits. `/compare {server?} {wipereact?}` is administrator-only, autocompletes tracked servers from global `tracked_servers`, autocompletes past guild wipe reacts by title/date, defaults to the most recently passed wipe react when no options are given, compares a wipe react against the invoker's current voice channel or the full guild member list when the invoker is not in voice, compares a server against the invoker's current voice channel, and compares the selected wipe react against the selected server when both options are provided. All comparisons ignore members or guild links that do not resolve through `guild_steam_discord_links -> steam_accounts.bm_id` where BattleMetrics player resolution is required. `/transfer` only creates guild-scoped links for users who are currently in the guild, skipping missing members from the imported embed data. Guild-scoped links are also removed automatically when a user leaves the guild. `/ban {user}` is owner-guild-only, registered only in `BOT_OWNER_GUILD_ID` and gated at runtime by `BOT_OWNER_DISCORD_IDS`. It opens a modal with six feature-category checkboxes (Setup And Configuration, Steam Linking And Identity, Player And Account Lookup, Activity Presence And Checks, Guild Coordination And Utility, Other) pre-filled with the user's current restrictions; submitting PUTs the selection to `/api/internal/bot/setup/bans/{discordID}` (empty selection deletes the ban row). The shard worker's `interactionCreate` dispatcher maps each incoming `(commandName, subcommand)` — and component `customId` prefixes for modals, buttons, and string selects — to a ban category via `apps/bot/src/ban-categories.ts` and calls `enforceBan` (30s TTL per-shard cache) from `apps/bot/src/ban-check.ts`. Banned users receive an ephemeral red `#d31239` embed titled `Error` with description `You have been banned`; the `/ban` command itself is always exempt so owners can never lock themselves out. `/server add`, `/server edit`, `/server remove`, and `/server list` are owner-guild-only commands registered only in `BOT_OWNER_GUILD_ID`; runtime access is restricted to the comma-separated Discord ID list in `BOT_OWNER_DISCORD_IDS`. `/server add` and `/server edit` open the same modal with server name, BattleMetrics ID, an `Include in guild settings?` toggle, and category checkboxes for `Vitality`, `Atlas`, `Vital`, `Modded`, `Official`, and `Build`. `/server edit` and `/server remove` use autocomplete-backed tracked-server selection instead of manual BM ID entry, and `/server list` shows the global tracked server registry with stored categories and poll status. `/summon {scope}`, `/announce {scope} {message}`, `/wipereact`, `/bind massinvite`, and `/create invite` allow either Discord Administrator or the configured admin role. `/bind` is `GuildInstall`-only (not user-installable) so subcommands like `/bind massinvite` cannot be invoked outside guilds where the bot is installed. `/wipereact` uses in-command autocomplete for selecting active wipe reacts on `/wipereact delete`, `/wipereact missing {scope} {wipereact}`, `/wipereact react add {user} {type} {wipereact}`, and `/wipereact react remove {user} {wipereact}`. Wipe reacts now require Wipe React Logs to be configured in `/settings`; successful wipe-react reaction changes continue to emit the existing log embeds to that configured channel. `/wipereact schedule create` opens a modal to create a recurring wipe-react schedule (max 3 per guild) with day selection (Mon-Sun + Force Wipe), time, timezone, repeat interval, and post-before timing; `/wipereact schedule delete` and `/wipereact schedule edit` manage existing schedules via autocomplete. Wipe reacts are persisted in Postgres through backend bot-only endpoints, including the message/channel IDs, config, and accepted/declined/late user lists, and shard workers rehydrate active wipe-react expiry timers on startup. `/wipereact missing` posts an embed titled `Missing reactants` listing members of the selected role who have not accepted, declined, or marked late; this requires the bot to have the Discord Guild Members privileged intent enabled. `/track {group_name} {server}` allows either Discord Administrator or the configured admin role; team name is capped at 12 characters, server is autocompleted from the global tracked-servers list, and the command creates a text channel under the configured Tracked users category, persists a `track_groups` row, sends a pinned online/offline embed with Add/Remove/Notifications/Analytics buttons, and runs a poller (every ~30s) that refreshes the embed, renames the channel to `{slug}-{online_count}`, and (when `send_updates` is enabled via the notifications modal) posts join/leave embeds in the channel. The Add modal accepts comma- or period-separated Steam IDs and BattleMetrics IDs, resolves Steam→BM via `steam_accounts` with the same lazy fallback as `/player`, and replies with a `Tracking Status` embed listing successful/unsuccessful counts and unresolved IDs. The 📈 Analytics button is restricted to website admins (any `admin_user_roles` row) and posts a non-ephemeral weekday analytics embed: each weekday lists the best-time-for-an-offline window (lowest 2-hour avg-online block) and the most-online hours; the embed pulls data from the backend track-group analytics route (200 BattleMetrics sessions per member per server, cached for 24h in `track_analytics_sessions_cache`, dropping days under 30 minutes of play), and falls back to gray ◀️▶️ pagination buttons that anyone can use when the rendered embed exceeds Discord limits.

`/analyze offline {identifier}` is a public global command registered by shard `0`; it accepts Steam IDs, supported Steam profile URLs, or BattleMetrics IDs and posts the same offline timing chart style as `/track` analytics without website-admin gating.

## Infrastructure

Local infra is defined in `infra/docker-compose.yml`:

- **Postgres 16** on `:5432` (user/pass/db: `rustlens`)
- **Redis 7** on `:6379`
- **Traefik v3** on `:80` (HTTP) and `:8081` (dashboard)

## Environment Variables

Each app has a `.env.example` — copy and fill before running.

**Backend** (`apps/backend/internal/config/config.go` is the source of truth):

| Variable | Default |
|---|---|
| `PORT` | `8080` |
| `POSTGRES_URL` | `postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable` |
| `REDIS_URL` | `redis://localhost:6379/0` |
| `BOT_SERVICE_JWT_SECRET` | required |
| `APP_API_INTERNAL_SECRET` | required |
| `APP_SESSION_SECRET` | required |
| `BOT_TARGET_SHARDS` | `4` |
| `BOT_HEARTBEAT_TTL_SECONDS` | `45` |
| `BOT_JWT_ISSUER` | `rustlens-bot` |
| `BOT_JWT_AUDIENCE` | `rustlens-backend` |
| `STEAM_API_KEY` | `` |
| `BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS` | `30` |
| `BM_MATCH_URL` / `BM_MATCH_TOKEN` | `` (Steam<->BM resolution disabled when unset) |
| `PLATFORMSYNC_TOKEN` | `` |
| `PROXY_ENABLED` | `false` |
| `PROXY_ENDPOINTS` | `` (CSV; legacy fallback when no DB-managed proxies exist) |
| `PROXY_ENCRYPTION_KEY` | `` (required for DB-managed proxy CRUD) |
| `APP_ORIGIN` | `http://localhost` |
| `APP_BASE_URL` | `http://localhost:3000` |
| `DISCORD_CLIENT_ID` / `DISCORD_CLIENT_SECRET` | `` (required to enable `/api/auth/discord`) |
| `APP_ADMIN_DISCORD_IDS` | `` (CSV; bootstraps internal-admin role) |
| `APP_EMAIL_TOKEN_TTL_MINUTES` | `20` |
| `ALLOW_AUTH_DEBUG_TOKENS` | `false` |

The BattleMetrics public API client is unauthenticated; there is no `BATTLEMETRICS_API_KEY` env var. Steam<->BM resolution is delegated to the external `bmmatch` service via `BM_MATCH_URL` + `BM_MATCH_TOKEN`.

**Bot** (`apps/bot/src/config.ts`):

| Variable | Default |
|---|---|
| `DISCORD_TOKEN` | required for gateway login |
| `BOT_SERVICE_JWT_SECRET` | required (must match backend) |
| `BOT_JWT_ISSUER` | `rustlens-bot` |
| `BOT_JWT_AUDIENCE` | `rustlens-backend` |
| `BACKEND_URL` | `http://localhost:8080` |
| `BOT_WORKER_ID` | `bot-{pid}` |
| `BOT_RECONCILE_INTERVAL_MS` | `15000` |
| `BOT_HEARTBEAT_INTERVAL_MS` | `8000` |
| `BOT_OWNER_GUILD_ID` / `BOT_OWNER_DISCORD_IDS` | required for owner-only `/server`, `/ban`, `/manage` |
| `PROXY_ENABLED` / `PROXY_ENDPOINTS` | `false` / `` (controls bot-side proxied request concurrency) |

**Frontend** expects `APP_SESSION_SECRET` and `APP_API_INTERNAL_SECRET` (shared with backend), and usually `RUSTLENS_API_URL`. `APP_ORIGIN` (or legacy `NEXT_PUBLIC_APP_URL`) must be the canonical external origin used for Discord OAuth callbacks. Auth-handler rate limiting uses `REDIS_URL` (or `RATE_LIMIT_REDIS_URL`) via `lib/rate-limit.ts`. `TRUST_PROXY_HEADERS` defaults to `false` and should only be enabled when the deployment edge strips and re-injects a dedicated canonical client-IP header configured via `TRUST_PROXY_IP_HEADER` for the rate limiter.

`/wipereact missing` requires the Discord Guild Members privileged intent because the shard worker requests `GatewayIntentBits.GuildMembers` (alongside `Guilds` and `GuildVoiceStates`).

## CI/CD

- **`.github/workflows/ci.yml`** — On PR and push to `main`: lint/test/build for the pnpm workspace (`apps/frontend` + `apps/bot`) and govulncheck/vet/test/`go test -race`/build for Go backend. Runs on `ubuntu-latest` as a temporary rollback while the self-hosted runner fleet is remediated.
- **`.github/workflows/publish.yml`** — On `main`: builds and pushes `backend`, `frontend`, and `bot` Docker images to GHCR using lowercase owner tags. Runs on `ubuntu-latest` while the self-hosted runner fleet is remediated.
- **`.github/workflows/runner-diagnostics.yml`** — Manual self-hosted runner validation workflow that checks checkout, workspace paths, and action cache layout to catch runner-hook regressions early.

## Skills

Agent skills live in `.agents/skills/` (symlinked to `.claude/skills/` for Claude Code). Each skill is a directory with a `SKILL.md` file. When creating or editing skills, always write to `.agents/skills/` — the symlink ensures all agent tooling picks them up.

Current skills:

| Skill | Description |
|---|---|
| `battlemetrics-api` | BattleMetrics REST API reference (endpoints, auth, pagination, JSON:API shapes) |

