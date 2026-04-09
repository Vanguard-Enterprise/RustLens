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

Production uses the same split via Kubernetes Ingress (`deploy/k8s/ingress.yaml`).

### Backend (Go)

```
apps/backend/
  cmd/server/main.go        # Entrypoint: config -> stores -> service -> router -> server
  internal/
    config/config.go         # Env-based config with defaults (PORT, POSTGRES_URL, REDIS_URL, etc.)
    http/
      router.go              # chi routes: /api/docs, /api/healthz, /api/internal/bot/shards/*, /api/internal/bot/setup/* incl. steam/discord linking
      handlers.go            # Handler implementations incl. bot setup-state, guild-settings, and wipe-react persistence endpoints
      openapi.go             # Embedded OpenAPI spec + Scalar docs UI
    auth/jwt.go              # JWT signing/validation for bot service auth
    shards/
      service.go             # Shard coordination business logic
      allocator.go           # Shard assignment algorithm
      heartbeat.go           # Heartbeat/lease management
    store/
      postgres.go            # pgx pool + schema auto-migration (bot_worker_events, guild_setups, guild_settings, wipe_reacts)
      wipe_reacts.go         # Persisted wipe react CRUD + reaction mutation helpers
      redis.go               # go-redis client for heartbeats and shard leases
      steam.go               # Steam account & Discord link CRUD (upsert, lookup, bulk hard links, soft link delete)
```

Key patterns:

- Schema is auto-migrated at startup via `EnsureSchema()` (no migration tool).
- Bot routes (`/api/internal/bot/shards/*`) are JWT-protected via middleware.
- Bot setup-state routes (`/api/internal/bot/setup/guilds/*`) are JWT-protected and persist one-time guild setup state in Postgres, including selected roles and selected channels.
- Bot guild settings routes (`/api/internal/bot/setup/guilds/*/settings*`) are JWT-protected and currently store the logs channel override used by wipe reacts.
- Bot wipe-react routes (`/api/internal/bot/setup/guilds/*/wipe-reacts*`) are JWT-protected and persist wipe react metadata plus accepted/declined/late user lists in Postgres.
- Bot wipe-react schedule routes (`/api/internal/bot/setup/guilds/*/wipe-react-schedules*`) are JWT-protected and persist recurring wipe-react schedule configs (max 3 per guild) with CRUD operations.
- OpenAPI spec is served at `/api/docs/openapi.json`; interactive docs at `/api/docs`.

**Steam & Discord linking**

`steam_accounts` stores known Steam players, optionally with their BattleMetrics player ID (`bm_id`) and Steam avatar URL (`avatar_image`). The `bm_id` is populated when a hard link is created via BattleMetrics scrape.

`steam_discord_links` maps Steam accounts to Discord users with a `type` and `source`:
- **soft** — user self-reported via bot command or frontend. No verification that the Steam account actually belongs to them. Source examples: `bot_command`, `frontend`.
- **hard** — discovered from a trusted source (e.g., BattleMetrics API) where the mapping is authoritatively verified. Most likely valid. Source examples: `battlemetrics`.

A single (steam_id, discord_id) pair can have both a soft and hard link simultaneously. The relationship is many-to-many: one Steam account can link to multiple Discord users and vice versa.

Bot-internal endpoints under `/api/internal/bot/setup/steam/` and `/api/internal/bot/setup/discord/` provide CRUD for both tables. Admins can only delete soft links.

### Bot (TypeScript)

```
apps/bot/src/
  index.ts              # Entrypoint: config -> register -> poll assignments -> manage shards
  config.ts             # Env config loader
  backend-client.ts     # HTTP client for backend shard API (register, heartbeat, release, assignments)
  shards.ts             # Shard reconciliation logic
  shard-runtime.ts      # discord.js ShardingManager wrapper
  shard-worker.ts       # Individual shard worker process + interaction handling (requires GuildMembers intent for /wipereact missing)
  setup-command.ts      # /setup start command + setup modal handling
  bind-command.ts       # /bind fullkit/turret/rocket/c4 modal registration + submit handling
  wipereact-command.ts  # /wipereact create/delete/missing/react + /wipereact schedule create/delete/edit flow, persistence sync, button handling, edit modal, and logs embeds
```

Flow: worker registers with backend -> polls `/assignments` -> reconciles local discord.js shards -> heartbeats leases -> releases on shutdown. Auth via self-signed JWT using `BOT_SERVICE_JWT_SECRET`.
Command flow: shard `0` registers `/setup start`, `/setup logchannel`, `/wipereact ...`, `/bind ...`, and `/admin reset_setup` at startup. `/setup logchannel {channel}` persists a logs-channel override in backend guild settings for wipe reacts and logs. `/wipereact` is an administrator-only guild command and uses in-command autocomplete for selecting active wipe reacts on `/wipereact delete`, `/wipereact missing {scope} {wipereact}`, `/wipereact react add {user} {type} {wipereact}`, and `/wipereact react remove {user} {wipereact}`. `/wipereact schedule create` opens a modal to create a recurring wipe-react schedule (max 3 per guild) with day selection (Mon-Sun + Force Wipe), time, timezone, repeat interval, and post-before timing; `/wipereact schedule delete` and `/wipereact schedule edit` manage existing schedules via autocomplete. Wipe reacts are persisted in Postgres through backend bot-only endpoints, including the message/channel IDs, config, and accepted/declined/late user lists, and shard workers rehydrate active wipe-react expiry timers on startup. `/wipereact missing` posts an embed titled `Missing reactants` listing members of the selected role who have not accepted, declined, or marked late; this requires the bot to have the Discord Guild Members privileged intent enabled.

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

The bot requires a real `DISCORD_TOKEN` for gateway login. `/wipereact missing` also requires the Discord Guild Members privileged intent to be enabled for the application because the shard worker now requests `GatewayIntentBits.GuildMembers`.

## CI/CD

- **`.github/workflows/ci.yml`** — On PR and push to `main`: lint/test/build for JS workspace, vet/test/build for Go backend, and offline client-side Kubernetes manifest validation. Runs on `ubuntu-latest` as a temporary rollback while the self-hosted runner fleet is remediated.
- **`.github/workflows/publish.yml`** — On `main`: builds and pushes `backend`, `frontend`, `bot` Docker images to GHCR using lowercase owner tags. Runs on `ubuntu-latest` while the self-hosted runner fleet is remediated.
- **`.github/workflows/runner-diagnostics.yml`** — Manual self-hosted runner validation workflow that checks checkout, workspace paths, and action cache layout to catch runner-hook regressions early.

## Skills

Agent skills live in `.agents/skills/` (symlinked to `.claude/skills/` for Claude Code). Each skill is a directory with a `SKILL.md` file. When creating or editing skills, always write to `.agents/skills/` — the symlink ensures all agent tooling picks them up.

Current skills:

| Skill | Description |
|---|---|
| `battlemetrics-api` | BattleMetrics REST API reference (endpoints, auth, pagination, JSON:API shapes) |

## Kubernetes

Manifests in `deploy/k8s/`. Apply in order: namespace -> configmap -> secrets -> backend -> frontend -> bot -> hpa -> ingress.
