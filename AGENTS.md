# AGENTS.md

This file provides guidance to AI coding agents working with this repository.
Symlinked as `CLAUDE.md` so it is automatically picked up by Claude Code, Codex, Gemini, GitHub Copilot, and similar tools.

> **Keep this file up to date.** After any significant change — new API routes, renamed files, added
> dependencies, schema migrations, architectural decisions — update the relevant sections below.
> Stale instructions lead to wasted effort and broken assumptions. When in doubt, re-read the
> codebase and correct anything that has drifted.

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
    layout.tsx           # App Router root layout + metadata
    auth/                # Browser auth routes: explicit email/password sign-in, register, reset, verify, plus optional Discord OAuth start/callback
    admin/               # Internal RustLens ops dashboard routes guarded by app-user internal_admin role
    dashboard/           # Authenticated user dashboard routes built on the Efferd app shell
    logout/route.ts      # Session clear + redirect route
    not-found.tsx        # App Router 404 route that reuses the landing shell and RustLens-themed empty state
    page.tsx             # Public landing page only
    login/page.tsx       # Public login route using the RustLens-adapted auth block with email/password first and optional Discord entry
    sign-in/page.tsx     # Legacy alias that redirects to /login
    signin/page.tsx      # Legacy alias that redirects to /login
    signup/page.tsx      # Legacy alias that redirects to /login
    sign-up/page.tsx     # Legacy alias that redirects to /login
    globals.css          # Tailwind v4 theme tokens and global styling
  components/
    app-shell.tsx        # Efferd app-shell-4-based authenticated shell shared by /dashboard and /admin
    app-sidebar.tsx      # Route-aware RustLens sidebar navigation for dashboard/admin
    app-header.tsx       # Authenticated shell header with breadcrumbs and session-aware user menu
    auth-page.tsx        # RustLens sign-in panel adapted from the @efferd/auth-3 block with explicit sign-in vs create-account email flows and optional Discord action
    faqs-section.tsx     # FAQ accordion adapted from the @efferd/faqs-1 block with RustLens-specific questions
    feature-section.tsx  # Feature strip adapted from the @efferd/features-1 block using landingFeatures content
    header.tsx           # Sticky top navigation with docs / add-bot CTAs
    hero.tsx             # RustLens launch hero based on the @efferd/hero-3 block
    logos-section.tsx    # System/integration strip for the landing page
    not-found.tsx        # RustLens-themed 404 state adapted from the @efferd/not-found-2 block
    footer.tsx           # RustLens footer adapted from the @efferd/footer-1 block with repo/docs links
    ui/                  # shadcn/ui primitives added as source
  lib/
    rustlens-api.ts      # Server-side frontend client for app-auth and /api/app aggregate/admin routes
    session.ts           # Signed Next.js session cookie helpers for browser auth
    site-content.ts      # Landing-page content model used by the page, FAQ section, and tests
  Dockerfile             # Frontend image for GHCR publish workflow
```

Key patterns:

- The frontend now has three surfaces: public marketing/auth routes, authenticated user routes under `/dashboard`, and internal ops routes under `/admin`.
- Browser auth is **Next.js-owned** using a signed session cookie. `APP_SESSION_SECRET` is required; there is no insecure fallback. Email + password is the primary login, with explicit sign-in vs registration selection in the frontend, password reset and email verification flows, and lightweight in-memory rate limiting on the public auth route handlers. Discord OAuth is optional and used for account linking or convenience login when configured.
- The frontend calls **app-facing backend routes** under `/api/app/*` with a shared internal app secret from server-side code only. It does **not** call the bot JWT routes from the browser.
- The authenticated shell is based on `@efferd/app-shell-4` installed through the shadcn CLI and adapted to RustLens navigation/state.
- Next.js still runs as a **hybrid Node server** in dev/prod rather than a static export.

### Backend (Go)

```
apps/backend/
  cmd/server/main.go        # Entrypoint: config -> stores -> service -> router -> server
  internal/
    config/config.go         # Env-based config with defaults (PORT, POSTGRES_URL, REDIS_URL, etc.)
    http/
      router.go              # chi routes: /api/docs, /api/healthz, /api/internal/bot/shards/*, /api/internal/bot/setup/* incl. guild settings, steam/discord linking, player state lookups, guild online summaries, and nickname targets
      handlers.go            # Handler implementations incl. guild-settings, tracked BattleMetrics servers, player states, guild online summaries, nickname-target lookups, and wipe-react persistence/history endpoints
      app_handlers.go        # App-auth and /api/app aggregate/admin handlers for browser sessions and internal ops
      openapi.go             # Embedded OpenAPI spec + Scalar docs UI
    auth/jwt.go              # JWT signing/validation for bot service auth
    battlemetrics/
      client.go              # BattleMetrics HTTP client for tracked server polling
      poller.go              # Background rolling poller for tracked servers
    shards/
      service.go             # Shard coordination business logic
      allocator.go           # Shard assignment algorithm
      heartbeat.go           # Heartbeat/lease management
    store/
      postgres.go            # pgx pool + schema auto-migration (bot_worker_events, guild_settings, tracked_servers, player_states, wipe_reacts, user_guilds)
      app_users.go           # App-user, connected-account, aggregate, and admin listing store logic
      app_password_auth.go   # Password credential, reset-token, and email-verification store logic
      wipe_reacts.go         # Persisted wipe react CRUD + reaction mutation helpers
      redis.go               # go-redis client for heartbeats and shard leases
      steam.go               # Steam account & Discord link CRUD (upsert, lookup, bulk hard links, soft link delete, guild nickname target joins)
      tracked_servers.go     # Global tracked BattleMetrics server CRUD
      guild_tracked_servers.go # Guild-specific selection of owner-managed tracked servers
      player_states.go       # Per-server BattleMetrics player presence state sync and lookup
      online_summary.go      # Guild online summary join across guild links, guild tracked servers, steam accounts, and player states
```

Key patterns:

- Schema is auto-migrated at startup via `EnsureSchema()` (no migration tool).
- Bot routes (`/api/internal/bot/shards/*`) are JWT-protected via middleware.
- Bot guild settings routes (`/api/internal/bot/setup/guilds/*/settings`) are JWT-protected and store per-guild log channels, alerts, and the admin role.
- Bot guild tracked-server routes (`/api/internal/bot/setup/guilds/*/tracked-servers`) are JWT-protected and store each guild's selected subset of the owner-managed tracked server list.
- Bot tracked-server routes (`/api/internal/bot/setup/tracked-servers*`) are JWT-protected and persist the bot's global BattleMetrics server list in Postgres.
- Bot player-state routes (`/api/internal/bot/setup/player-states/*`) are JWT-protected and expose BattleMetrics presence state by server and by player.
- Bot guild online summary route (`/api/internal/bot/setup/guilds/{guildID}/online-summary`) is JWT-protected and returns guild-linked users currently online on tracked BattleMetrics servers.
- Bot guild nickname target route (`/api/internal/bot/setup/guilds/{guildID}/nickname-targets`) is JWT-protected and returns one guild-linked Steam mapping per Discord user joined with `steam_accounts.bm_id` and `bm_name` for nickname sync.
- Bot wipe-react routes (`/api/internal/bot/setup/guilds/*/wipe-reacts*`) are JWT-protected and persist wipe react metadata plus accepted/declined/late user lists in Postgres.
- Bot wipe-react schedule routes (`/api/internal/bot/setup/guilds/*/wipe-react-schedules*`) are JWT-protected and persist recurring wipe-react schedule configs (max 3 per guild) with CRUD operations.
- OpenAPI spec is served at `/api/docs/openapi.json`; interactive docs at `/api/docs`.
- App-facing routes under `/api/app/*` are protected by an internal shared-secret header used only by Next.js server-side code. Current-user and admin authorization resolve through `app_users` roles, not through bot JWT claims. The backend no longer exposes an email-status lookup route for browser auth.
- The backend runs a rolling BattleMetrics poller that queries one tracked server every `BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS`, authenticates with `Authorization: Bearer <BATTLEMETRICS_API_KEY>`, updates `tracked_servers.last_polled_at` / `last_poll_error`, and syncs per-server player presence into `player_states`.

**App users & browser auth**

`app_users` stores first-class browser-facing RustLens users with roles such as `user` and `internal_admin`.

`user_connected_accounts` stores external identities attached to an app user. Discord is optional here and is used for guild/game identity enrichment and convenience sign-in when enabled.

`user_emails` stores primary and secondary email identities for app users.

`user_password_credentials` stores bcrypt password hashes and password update timestamps. New and reset passwords require at least 12 characters. `password_reset_tokens` and `email_verification_tokens` store one-time password reset and email verification token hashes, not raw tokens.

`app_audit_events` stores browser-auth and internal-admin audit events. The frontend signs users into Next.js sessions after successful email/password auth, password reset, email verification, or optional Discord OAuth completion via `/api/app/auth/*`. Raw reset / verification tokens are not returned by default; explicit local debug previews require both backend `ALLOW_AUTH_DEBUG_TOKENS=true` and frontend `AUTH_DEBUG_TOKEN_PREVIEW=true`.

**Steam & Discord linking**

`steam_accounts` stores known Steam players, optionally with their BattleMetrics player ID (`bm_id`), latest known BattleMetrics display name (`bm_name`), and Steam avatar URL (`avatar_image`). The `bm_id` is populated when a hard link is created via BattleMetrics scrape.

`steam_discord_links` maps Steam accounts to Discord users with a `type` and `source`:
- **soft** — user self-reported via bot command or frontend. No verification that the Steam account actually belongs to them. Source examples: `bot_command`, `frontend`.
- **hard** — discovered from a trusted source (e.g., BattleMetrics API) where the mapping is authoritatively verified. Most likely valid. Source examples: `battlemetrics`.

A single (steam_id, discord_id) pair can have both a soft and hard link simultaneously. The relationship is many-to-many: one Steam account can link to multiple Discord users and vice versa.

Bot-internal endpoints under `/api/internal/bot/setup/steam/` and `/api/internal/bot/setup/discord/` provide CRUD for both tables. Admins can only delete soft links.

`guild_steam_discord_links` maps Steam accounts to Discord users within a specific guild. Unlike the global `steam_discord_links` table, these are guild-scoped — a guild admin decides which steam-discord associations are relevant to their server. The composite PK is `(guild_id, discord_id, steam_id)`.

Bot-internal endpoints under `/api/internal/bot/setup/guilds/{guildID}/` provide CRUD for guild-scoped links: create, list by discord ID, list by steam ID, and delete.

`user_guilds` records Discord users and their current guild membership state with composite PK `(discord_id, guild_id)`, `is_active`, `first_seen_at`, `last_seen_at`, and nullable `left_at`. Bot-internal endpoints under `/api/internal/bot/setup/user-guilds` and `/api/internal/bot/setup/discord/{discordID}/guilds` support marking a user active/seen in a guild, marking that user inactive/left for a guild, and listing all guild state rows for a Discord user. The bot now populates this table for non-bot users via `guildMemberAdd` and `guildMemberRemove` events.

### Bot (TypeScript)

```
apps/bot/src/
  index.ts              # Entrypoint: config -> register -> poll assignments -> manage shards
  config.ts             # Env config loader
  backend-client.ts     # HTTP client for backend shard API (register, heartbeat, release, assignments)
  guild-member-cache.ts # Shared in-memory expiring guild member snapshot cache for commands that need broad guild membership scans; refreshes only on demand after expiry
  shards.ts             # Shard reconciliation logic
  shard-runtime.ts      # discord.js ShardingManager wrapper
  shard-worker.ts       # Individual shard worker process + interaction handling (requires GuildMembers + GuildVoiceStates intents)
  admin-command.ts      # /admin setsteam|removesteam registration + guild-link admin flow
  settings-command.ts   # /settings embed + select-menu/modal flow for guild settings
  bind-command.ts       # /bind fullkit/turret/rocket/c4 modal registration + submit handling
  setsteam-command.ts   # /setsteam {steam_id} — user self-service guild soft-linking for Steam IDs
  wipereact-command.ts  # /wipereact create/delete/missing/react + /wipereact schedule create/delete/edit flow, persistence sync, button handling, edit modal, and logs embeds
  summon-command.ts     # /summon {scope} — move all online voice members of a role into the invoker's voice channel (admin-only)
  announce-command.ts   # /announce {scope} {message} — DM an embed announcement to all members of a role (admin-only)
  online-command.ts     # /online — public guild command that lists linked users currently online on tracked BattleMetrics servers
  nickname-command.ts   # /nickname — admin command that syncs guild nicknames to "{discord nickname} - {BattleMetrics name}" from guild links + steam_accounts
  compare-command.ts    # /compare {server?} {wipereact?} — admin comparison of the invoker's current voice channel against a tracked server or the most recent passed / specified wipe react
  user-guilds.ts        # Shared bot-side sync for user_guilds active/inactive membership state on guildMemberAdd/guildMemberRemove
  view-command.ts       # /view user {discord_user|identifier} and /view all — view guild-linked steam/discord accounts
  server-command.ts     # /server add|remove|list — owner-only tracked BattleMetrics server management in the control guild
```

Flow: worker registers with backend -> polls `/assignments` -> reconciles local discord.js shards -> heartbeats leases -> releases on shutdown. Auth via self-signed JWT using `BOT_SERVICE_JWT_SECRET`.
Command flow: shard `0` registers `/settings`, `/setsteam`, `/wipereact ...`, `/bind ...`, `/summon`, `/announce`, `/online`, `/nickname`, `/compare`, `/view`, `/server`, and `/admin ...` at startup. `/settings` is a guild-administrator-only command that renders an ephemeral settings embed with a select menu for configuring wipe-react logs, linking logs, tracked-server logs, alerts, the admin role, and the guild's selected tracked servers. Guild tracked servers are chosen from the owner-managed global `/server` list and are persisted separately from guild settings. `/setsteam {steam_id}` is a public guild command that accepts either a 17-digit Steam ID or a supported `steamcommunity.com/profiles/...` or `steamcommunity.com/id/...` URL, resolves vanity URLs through the backend with `STEAM_API_KEY`, then creates a soft link in both the global `steam_discord_links` table and the guild-scoped `guild_steam_discord_links` table; if the user already has a guild Steam link, RustLens replies with an error embed instructing them to contact an administrator. When Linking Logs is configured, successful `/setsteam` calls emit an embed containing the Steam ID, optional BattleMetrics ID, and Discord user mention. `/admin setsteam {user} {steam_id}` and `/admin removesteam {user}` are protected by either Discord Administrator or the configured admin role and manage guild-scoped Steam links for other users; they never delete existing rows from the global `steam_discord_links` table, and `/admin setsteam` ensures a global soft link exists before replacing the guild link. `/online` is a public guild command that renders one or more non-ephemeral RustLens embeds grouping currently online linked users by tracked BattleMetrics server selected for that guild; if the embed limits are reached, follow-up embeds use the title `Online summary cont.`. `/nickname` allows either Discord Administrator or the configured admin role, reads one guild-scoped Steam link per Discord user from `/api/internal/bot/setup/guilds/{guildID}/nickname-targets`, and attempts to set each member nickname to `{discord nickname} - {bm_name}` while counting missing `bm_id` / `bm_name` entries and Discord edit failures as failed. Commands that need full-guild member scans must use `apps/bot/src/guild-member-cache.ts` instead of calling `guild.members.fetch()` directly; the cache is in-memory, shared within the bot process, only refreshes on demand when expired, and deduplicates concurrent refreshes per guild to reduce `Request Guild Members` rate limits. `/compare {server?} {wipereact?}` is administrator-only, autocompletes tracked servers from global `tracked_servers`, autocompletes past guild wipe reacts by title/date, defaults to the most recently passed wipe react when no options are given, compares either the invoker's current voice channel or the full guild member list when the invoker is not in voice, prefers the specified/default wipe react over the server option when both are provided, and ignores members or guild links that do not resolve through `guild_steam_discord_links -> steam_accounts.bm_id`. `/server add {bm_id} {name}`, `/server remove {bm_id}`, and `/server list` are owner-only commands registered only in the control guild from `BOT_OWNER_GUILD_ID`; runtime access is restricted to the comma-separated Discord ID list in `BOT_OWNER_DISCORD_IDS`, and successful responses are public RustLens-themed embeds. `/summon {scope}`, `/announce {scope} {message}`, `/wipereact`, and `/bind massinvite` allow either Discord Administrator or the configured admin role. `/wipereact` uses in-command autocomplete for selecting active wipe reacts on `/wipereact delete`, `/wipereact missing {scope} {wipereact}`, `/wipereact react add {user} {type} {wipereact}`, and `/wipereact react remove {user} {wipereact}`. Wipe reacts now require Wipe React Logs to be configured in `/settings`; successful wipe-react reaction changes continue to emit the existing log embeds to that configured channel. `/wipereact schedule create` opens a modal to create a recurring wipe-react schedule (max 3 per guild) with day selection (Mon-Sun + Force Wipe), time, timezone, repeat interval, and post-before timing; `/wipereact schedule delete` and `/wipereact schedule edit` manage existing schedules via autocomplete. Wipe reacts are persisted in Postgres through backend bot-only endpoints, including the message/channel IDs, config, and accepted/declined/late user lists, and shard workers rehydrate active wipe-react expiry timers on startup. `/wipereact missing` posts an embed titled `Missing reactants` listing members of the selected role who have not accepted, declined, or marked late; this requires the bot to have the Discord Guild Members privileged intent enabled.

## Infrastructure

Local infra is defined in `infra/docker-compose.yml`:

- **Postgres 16** on `:5432` (user/pass/db: `rustlens`)
- **Redis 7** on `:6379`
- **Traefik v3** on `:80` (HTTP) and `:8081` (dashboard)

## Environment Variables

Each app has a `.env.example` — copy and fill before running. Key backend config with defaults:

| Variable | Default |
|---|---|
| `PORT` | `8080` |
| `POSTGRES_URL` | `postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable` |
| `REDIS_URL` | `redis://localhost:6379/0` |
| `BOT_SERVICE_JWT_SECRET` | `dev-bot-secret` |
| `APP_API_INTERNAL_SECRET` | required |
| `BOT_TARGET_SHARDS` | `4` |
| `BOT_HEARTBEAT_TTL_SECONDS` | `45` |
| `STEAM_API_KEY` | `` |
| `BATTLEMETRICS_API_KEY` | `` |
| `BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS` | `10` |
The bot requires a real `DISCORD_TOKEN` for gateway login. Owner-only `/server` command registration also requires `BOT_OWNER_GUILD_ID` and `BOT_OWNER_DISCORD_IDS`. The Go backend autoloads `apps/backend/.env` in local runs, so values like `STEAM_API_KEY`, `BATTLEMETRICS_API_KEY`, `APP_API_INTERNAL_SECRET`, and `APP_ADMIN_DISCORD_IDS` can be provided there without separate shell export. The frontend expects `APP_SESSION_SECRET`, `APP_API_INTERNAL_SECRET`, and usually `RUSTLENS_API_URL`; `NEXT_PUBLIC_APP_URL` is used for absolute callback/reset/verify links. `DISCORD_CLIENT_ID` and `DISCORD_CLIENT_SECRET` are optional and only needed when Discord login/linking is enabled. Local auth debug previews are disabled by default and only appear when backend `ALLOW_AUTH_DEBUG_TOKENS=true` and frontend `AUTH_DEBUG_TOKEN_PREVIEW=true`. `/wipereact missing` requires the Discord Guild Members privileged intent to be enabled for the application because the shard worker now requests `GatewayIntentBits.GuildMembers`.

## CI/CD

- **`.github/workflows/ci.yml`** — On PR and push to `main`: lint/test/build for the pnpm workspace (`apps/frontend` + `apps/bot`) and vet/test/build for Go backend. Runs on `ubuntu-latest` as a temporary rollback while the self-hosted runner fleet is remediated.
- **`.github/workflows/publish.yml`** — On `main`: builds and pushes `backend`, `frontend`, and `bot` Docker images to GHCR using lowercase owner tags. Runs on `ubuntu-latest` while the self-hosted runner fleet is remediated.
- **`.github/workflows/runner-diagnostics.yml`** — Manual self-hosted runner validation workflow that checks checkout, workspace paths, and action cache layout to catch runner-hook regressions early.

## Skills

Agent skills live in `.agents/skills/` (symlinked to `.claude/skills/` for Claude Code). Each skill is a directory with a `SKILL.md` file. When creating or editing skills, always write to `.agents/skills/` — the symlink ensures all agent tooling picks them up.

Current skills:

| Skill | Description |
|---|---|
| `battlemetrics-api` | BattleMetrics REST API reference (endpoints, auth, pagination, JSON:API shapes) |

