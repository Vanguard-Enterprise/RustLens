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
| `apps/frontend` | Next.js 14 (React 18, TypeScript) | Web UI |
| `apps/bot` | Discord.js (TypeScript, ESM) | Sharded worker runtime |

Orchestrated with **pnpm 10 workspaces** + **Turborepo**. The Go backend is outside the pnpm workspace graph but integrated via the root `dev:all` script and Makefile targets.

## Common Commands

All commands run from the repo root unless noted.

```bash
make doctor          # Verify toolchain (Node, pnpm, Go, Docker, etc.)
make setup           # pnpm install + go mod tidy + install Air
make dev             # Start infra (Postgres, Redis, Traefik) + hot-reload all three apps
make infra-up        # Start only infra containers
make infra-down      # Stop infra containers
```

### Per-service dev

```bash
make dev-backend     # Air hot-reload for Go backend
make dev-frontend    # Next.js dev server on :3000
make dev-bot         # tsx watch for bot
```

### Lint / Test / Build

```bash
make lint            # pnpm lint (TS type-check) + go vet
make test            # pnpm test (vitest) + go test
make build           # pnpm build (Next.js + tsc) + go build
```

### Running a single app's tests

```bash
# Go (supports ./... and package-level patterns)
cd apps/backend && go test ./internal/shards/...

# TypeScript (vitest)
pnpm --filter @rustlens/frontend test
pnpm --filter @rustlens/bot test
```

### Lint details

- **Frontend & Bot**: `tsc --noEmit` (type-check only, no ESLint).
- **Backend**: `go vet ./...`

## Architecture

### Routing (Traefik)

Local dev runs Traefik on `http://localhost`:

- `/api/*` -> backend on `:8080`
- Everything else -> frontend on `:3000`

Production routing is handled outside this repository.

### Backend (Go)

```
apps/backend/
  cmd/server/main.go        # Entrypoint: config -> stores -> service -> router -> server
  internal/
    config/config.go         # Env-based config with defaults (PORT, POSTGRES_URL, REDIS_URL, etc.)
    http/
      router.go              # chi routes: /api/docs, /api/healthz, /api/internal/bot/shards/*, /api/internal/bot/setup/* incl. guild settings, steam/discord linking, player state lookups, guild online summaries, and nickname targets
      handlers.go            # Handler implementations incl. guild-settings, tracked BattleMetrics servers, player states, player profile aggregation, guild online summaries, nickname-target lookups, and wipe-react persistence/history endpoints
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
      wipe_reacts.go         # Persisted wipe react CRUD + reaction mutation helpers
      redis.go               # go-redis client for heartbeats and shard leases
      steam.go               # Steam account & Discord link CRUD (upsert, lookup, profile metadata refresh, bulk hard links, soft link delete, guild nickname target joins)
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
- Bot steam profile route (`/api/internal/bot/setup/steam/{steamID}/profile`) is JWT-protected and lazily refreshes `steam_account_cache` after a 24-hour TTL. Fresh cache entries skip Steam/BattleMetrics API calls; stale cache entries are returned if refresh fails unless BattleMetrics responds with `404 Unknown player`, in which case the route returns `404 battlemetrics profile private`. The route caches BattleMetrics name, Steam avatar, first-seen timestamp, total BattleMetrics hours across all returned servers, tracked-server category buckets, Steam Rust playtime, and the count of distinct BattleMetrics servers played (`bm_servers_played`).
- Bot steam account lookup routes (`/api/internal/bot/setup/steam/{steamID}` and `/api/internal/bot/setup/steam/by-bm/{bmID}`) now backfill missing `steam_accounts` rows through direct, authorized BattleMetrics requests with `BATTLEMETRICS_API_KEY` and immediately warm `steam_account_cache` from the returned profile data. These fallback requests are sent directly and are never proxied.
- Bot steam analytics route (`/api/internal/bot/setup/steam/{steamID}/analytics`) is JWT-protected and uses the same 24-hour `steam_account_cache.updated_at` TTL and refresh path as the steam profile route. Profile and analytics share the same cached Steam/BattleMetrics data, including name-gap data used by both the player risk factor and analytics gap display. If Steam summary reports `communityvisibilitystate = 1`, RustLens skips `GetOwnedGames` and leaves game totals/playtime private. The response includes top 5 owned Steam games by playtime with `totalGames` (null if the Steam profile is private), top 5 BattleMetrics servers by playtime with `totalServers`, and `gaps` between consecutive name identifier periods longer than ~1 month (each as `fromUnix`/`toUnix`).
- Bot guild game-ban route (`/api/internal/bot/setup/guilds/{guildID}/game-bans`) is JWT-protected and scans all guild-linked Steam IDs through Steam `ISteamUser/GetPlayerBans`, caching `game_banned`, `game_banned_at`, and `game_ban_checked_at` in `steam_account_cache` with a 6-hour TTL. The response only includes linked users with game bans newer than 90 days; VAC bans without game bans are ignored.
- Bot guild online summary route (`/api/internal/bot/setup/guilds/{guildID}/online-summary`) is JWT-protected and returns guild-linked users currently online on tracked BattleMetrics servers.
- Bot tracked-server log route (`/api/internal/bot/setup/guilds/{guildID}/tracked-server-log-events`) is JWT-protected and returns the current `player_states` snapshot joined against `guild_tracked_servers` + `guild_steam_discord_links` so only rows relevant to that guild's linked players on that guild's selected servers come through. The bot shard polls this every ~20s, diffs the current snapshot against its prior in-memory snapshot for that guild, and posts join/leave embeds to `tracked_server_log_channel_id` when a player's `is_online` state changes.
- Bot guild nickname target route (`/api/internal/bot/setup/guilds/{guildID}/nickname-targets`) is JWT-protected and returns one guild-linked Steam mapping per Discord user joined with `steam_accounts.bm_id` and `steam_account_cache.bm_name` for nickname sync.
- Bot wipe-react routes (`/api/internal/bot/setup/guilds/*/wipe-reacts*`) are JWT-protected and persist wipe react metadata plus accepted/declined/late user lists in Postgres.
- Bot wipe-react schedule routes (`/api/internal/bot/setup/guilds/*/wipe-react-schedules*`) are JWT-protected and persist recurring wipe-react schedule configs (max 3 per guild) with CRUD operations.
- The backend now owns wipe-react schedule timing. It enqueues due schedule posts centrally, and the bot shard that owns a guild claims queued posts from backend and sends the Discord message.
- OpenAPI spec is served at `/api/docs/openapi.json`; interactive docs at `/api/docs`.
- The backend runs a rolling BattleMetrics poller that queries one tracked server every `BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS`, updates `tracked_servers.last_polled_at` / `last_poll_error`, and syncs per-server player presence into `player_states`.

**Steam & Discord linking**

`steam_accounts` stores known Steam players and their optional BattleMetrics player ID (`bm_id`). The `bm_id` is populated when a hard link is created via BattleMetrics scrape. Volatile profile display/cache data lives in `steam_account_cache`, keyed by `steam_id`, with `bm_name`, `avatar_image`, first-seen metadata, total/category BattleMetrics hours, Steam Rust hours, game-ban cache fields (`game_banned`, `game_banned_at`, `game_ban_checked_at`), and an `updated_at` timestamp used for the 24-hour lazy profile refresh TTL.

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
  player-command.ts     # /player {identifier} — public player profile lookup by Steam ID, supported steamcommunity.com profile URL, or BattleMetrics ID
  checkgamebans-command.ts # /checkgamebans — guild admin scan of linked Steam IDs for recent Steam game bans using 6h cache TTL
  wipereact-command.ts  # /wipereact create/delete/notreacted/react + /wipereact schedule create/delete/edit flow, persistence sync, button handling, edit modal, and logs embeds
  summon-command.ts     # /summon {scope} — move all online voice members of a role into the invoker's voice channel (admin-only)
  announce-command.ts   # /announce {scope} {message} — DM an embed announcement to all members of a role except the invoker (admin-only)
  online-command.ts     # /online — public guild command that lists linked users currently online on tracked BattleMetrics servers
  nickname-command.ts   # /nickname — admin command that syncs guild nicknames to "{discord nickname} - {BattleMetrics name}" from guild links + steam_accounts
  compare-command.ts    # /compare {server?} {wipereact?} — admin comparison of the invoker's current voice channel against a tracked server, the most recent passed / specified wipe react, or both against each other when both options are supplied
  user-guilds.ts        # Shared bot-side sync for user_guilds active/inactive membership state on guildMemberAdd/guildMemberRemove
  view-command.ts       # /view user {discord_user|identifier} and /view all — view guild-linked steam/discord accounts
  server-command.ts     # /server add|edit|remove|list — owner-only tracked BattleMetrics server management in the control guild with add/edit modals, a guild-settings visibility toggle, and autocomplete-backed selection
```

Flow: worker registers with backend -> polls `/assignments` -> reconciles local discord.js shards -> heartbeats leases -> releases on shutdown. Auth via self-signed JWT using `BOT_SERVICE_JWT_SECRET`.
Command flow: shard `0` registers `/settings`, `/setsteam`, `/player`, `/checkgamebans`, `/wipereact ...`, `/bind ...`, `/summon`, `/announce`, `/online`, `/nickname`, `/compare`, `/view`, `/server`, and `/admin ...` at startup. `/settings` is a guild-administrator-only command that renders an ephemeral settings embed with a select menu for configuring wipe-react logs, linking logs, tracked-server logs, alerts, the admin role, and the guild's selected tracked servers. Guild tracked servers are chosen from the owner-managed global `/server` list entries that have `Include in guild settings?` enabled and are persisted separately from guild settings. Turning that toggle off later removes the server from every guild's selected tracked-server list. `/setsteam {steam_id}` is a public guild command that accepts either a 17-digit Steam ID or a supported `steamcommunity.com/profiles/...` or `steamcommunity.com/id/...` URL, resolves vanity URLs through the backend with `STEAM_API_KEY`, then creates a soft link in both the global `steam_discord_links` table and the guild-scoped `guild_steam_discord_links` table; if the user already has a guild Steam link, RustLens replies with an error embed instructing them to contact an administrator. When Linking Logs is configured, successful `/setsteam` calls emit an embed containing the Steam ID, optional BattleMetrics ID, and Discord user mention. `/player {identifier}` is a **global** public command (usable in any guild or DM) that accepts a 17-digit Steam ID, a supported `steamcommunity.com/profiles/...` or `steamcommunity.com/id/...` URL, or a BattleMetrics ID and looks up the matching `steam_accounts` row directly — no guild-link check — then renders the same player profile card as `/view user` using the backend steam-profile endpoint. If the `steam_accounts` lookup misses for a Steam ID or BattleMetrics ID, the backend now performs a direct, authorized BattleMetrics lookup, persists the recovered Steam/BM link, and warms the profile cache before replying. If the stored `bm_id` refresh returns BattleMetrics `404 Unknown player`, `/player` now replies with an `Error` embed whose description is `BM profile private` instead of rendering the normal player card. `/view user` accepts the same Steam inputs for its `identifier` field in addition to BattleMetrics IDs and Discord identifiers, resolving vanity URLs live through the backend without separate caching. `/checkgamebans` allows either Discord Administrator or the configured admin role, scans all guild-linked Steam IDs through the backend Steam ban endpoint, reuses cached results newer than 6 hours, ignores VAC-only bans, and posts a `Gamebans < 90 days` embed showing only game bans newer than 90 days. `/view` remains guild-scoped; `/player` is intentionally not. The card uses `steam_accounts.bm_id`, reads cached profile display/hour data from the backend steam-profile endpoint, sums total BattleMetrics playtime from all servers returned by BattleMetrics, sums category buckets from globally tracked `/server` entries, fetches Steam-owned Rust playtime directly, and includes a Risk Factor field composed of three factors (Steam→BM hour gap, servers played, BM hours) each computed against a separate formula. The card includes an analytics button (📈) that posts an ephemeral embed via the backend analytics endpoint showing top owned games, top played servers, and multi-month activity gaps from BattleMetrics name-change history, plus a links button (🔗) placeholder. `/view user` uses the same backend profile card flow, while `/view all` and `/view nosteam` keep their existing guild-admin-only summary behavior. `/admin setsteam {user} {steam_id}` and `/admin removesteam {user}` are protected by either Discord Administrator or the configured admin role and manage guild-scoped Steam links for other users; they never delete existing rows from the global `steam_discord_links` table, and `/admin setsteam` ensures a global soft link exists before replacing the guild link. `/online` is a public guild command that renders one or more non-ephemeral RustLens embeds grouping currently online linked users by tracked BattleMetrics server selected for that guild; if the embed limits are reached, follow-up embeds use the title `Online summary cont.`. `/nickname` allows either Discord Administrator or the configured admin role, reads one guild-scoped Steam link per Discord user from `/api/internal/bot/setup/guilds/{guildID}/nickname-targets`, and attempts to set each member nickname to `{discord nickname} - {bm_name}` while counting missing `bm_id` / `bm_name` entries and Discord edit failures as failed. Commands that need full-guild member scans must use `apps/bot/src/guild-member-cache.ts` instead of calling `guild.members.fetch()` directly; the cache is in-memory, shared within the bot process, only refreshes on demand when expired, and deduplicates concurrent refreshes per guild to reduce `Request Guild Members` rate limits. `/compare {server?} {wipereact?}` is administrator-only, autocompletes tracked servers from global `tracked_servers`, autocompletes past guild wipe reacts by title/date, defaults to the most recently passed wipe react when no options are given, compares a wipe react against the invoker's current voice channel or the full guild member list when the invoker is not in voice, compares a server against the invoker's current voice channel, and compares the selected wipe react against the selected server when both options are provided. All comparisons ignore members or guild links that do not resolve through `guild_steam_discord_links -> steam_accounts.bm_id` where BattleMetrics player resolution is required. `/server add`, `/server edit`, `/server remove`, and `/server list` are owner-guild-only commands registered only in `BOT_OWNER_GUILD_ID`; runtime access is restricted to the comma-separated Discord ID list in `BOT_OWNER_DISCORD_IDS`. `/server add` and `/server edit` open the same modal with server name, BattleMetrics ID, an `Include in guild settings?` toggle, and category checkboxes for `Vitality`, `Atlas`, `Vital`, `Modded`, `Official`, and `Build`. `/server edit` and `/server remove` use autocomplete-backed tracked-server selection instead of manual BM ID entry, and `/server list` shows the global tracked server registry with stored categories and poll status. `/summon {scope}`, `/announce {scope} {message}`, `/wipereact`, and `/bind massinvite` allow either Discord Administrator or the configured admin role. `/wipereact` uses in-command autocomplete for selecting active wipe reacts on `/wipereact delete`, `/wipereact missing {scope} {wipereact}`, `/wipereact react add {user} {type} {wipereact}`, and `/wipereact react remove {user} {wipereact}`. Wipe reacts now require Wipe React Logs to be configured in `/settings`; successful wipe-react reaction changes continue to emit the existing log embeds to that configured channel. `/wipereact schedule create` opens a modal to create a recurring wipe-react schedule (max 3 per guild) with day selection (Mon-Sun + Force Wipe), time, timezone, repeat interval, and post-before timing; `/wipereact schedule delete` and `/wipereact schedule edit` manage existing schedules via autocomplete. Wipe reacts are persisted in Postgres through backend bot-only endpoints, including the message/channel IDs, config, and accepted/declined/late user lists, and shard workers rehydrate active wipe-react expiry timers on startup. `/wipereact missing` posts an embed titled `Missing reactants` listing members of the selected role who have not accepted, declined, or marked late; this requires the bot to have the Discord Guild Members privileged intent enabled.

### Frontend (Next.js)

Minimal at this stage. Source in `apps/frontend/src/`.

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
| `BOT_TARGET_SHARDS` | `4` |
| `BOT_HEARTBEAT_TTL_SECONDS` | `45` |
| `STEAM_API_KEY` | `` |
| `BATTLEMETRICS_API_KEY` | `` |
| `BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS` | `10` |

The bot requires a real `DISCORD_TOKEN` for gateway login. Owner-only `/server` command registration also requires `BOT_OWNER_GUILD_ID` and `BOT_OWNER_DISCORD_IDS`. The Go backend autoloads `apps/backend/.env` in local runs, so values like `STEAM_API_KEY` and `BATTLEMETRICS_API_KEY` can be provided there without separate shell export. `/wipereact notreacted` requires the Discord Guild Members privileged intent to be enabled for the application because the shard worker now requests `GatewayIntentBits.GuildMembers`.

## CI/CD

- **`.github/workflows/ci.yml`** — On PR and push to `main`: lint/test/build for JS workspace and vet/test/build for Go backend. Runs on `ubuntu-latest` as a temporary rollback while the self-hosted runner fleet is remediated.
- **`.github/workflows/publish.yml`** — On `main`: builds and pushes `backend`, `frontend`, `bot` Docker images to GHCR using lowercase owner tags. Runs on `ubuntu-latest` while the self-hosted runner fleet is remediated.
- **`.github/workflows/runner-diagnostics.yml`** — Manual self-hosted runner validation workflow that checks checkout, workspace paths, and action cache layout to catch runner-hook regressions early.

## Skills

Agent skills live in `.agents/skills/` (symlinked to `.claude/skills/` for Claude Code). Each skill is a directory with a `SKILL.md` file. When creating or editing skills, always write to `.agents/skills/` — the symlink ensures all agent tooling picks them up.

Current skills:

| Skill | Description |
|---|---|
| `battlemetrics-api` | BattleMetrics REST API reference (endpoints, auth, pagination, JSON:API shapes) |

