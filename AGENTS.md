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
      router.go              # chi routes: /api/docs, /api/healthz, /api/internal/bot/shards/*
      handlers.go            # Handler implementations incl. bot setup-state endpoints
      openapi.go             # Embedded OpenAPI spec + Scalar docs UI
    auth/jwt.go              # JWT signing/validation for bot service auth
    shards/
      service.go             # Shard coordination business logic
      allocator.go           # Shard assignment algorithm
      heartbeat.go           # Heartbeat/lease management
    store/
      postgres.go            # pgx pool + schema auto-migration (bot_worker_events table)
      redis.go               # go-redis client for heartbeats and shard leases
```

Key patterns:

- Schema is auto-migrated at startup via `EnsureSchema()` (no migration tool).
- Bot routes (`/api/internal/bot/shards/*`) are JWT-protected via middleware.
- Bot setup-state routes (`/api/internal/bot/setup/guilds/*`) are JWT-protected and persist one-time guild setup state in Postgres, including selected roles and selected channels.
- OpenAPI spec is served at `/api/docs/openapi.json`; interactive docs at `/api/docs`.

### Bot (TypeScript)

```
apps/bot/src/
  index.ts              # Entrypoint: config -> register -> poll assignments -> manage shards
  config.ts             # Env config loader
  backend-client.ts     # HTTP client for backend shard API (register, heartbeat, release, assignments)
  shards.ts             # Shard reconciliation logic
  shard-runtime.ts      # discord.js ShardingManager wrapper
  shard-worker.ts       # Individual shard worker process + interaction handling
  setup-command.ts      # /setup start command + setup modal handling
  bind-command.ts       # /bind fullkit/turret/rocket/c4 modal registration + submit handling
```

Flow: worker registers with backend -> polls `/assignments` -> reconciles local discord.js shards -> heartbeats leases -> releases on shutdown. Auth via self-signed JWT using `BOT_SERVICE_JWT_SECRET`.
Command flow: shard `0` registers `/setup start` and `/admin reset_setup` at startup. `/setup start` opens a modal with role and channel checkboxes (the `Bot` role is not shown in the modal — it is always created automatically), includes an `All` option for both groups, switches the deferred ephemeral response into a textual progress bar during setup, creates configured guild roles in a fixed order including a hoisted soft-blue `Bot` role between `Co Leader` and `member`, assigns that `Bot` role to the RustLens bot, creates the selected channel/category layout with permission overwrites, skips builder-only channels when the `Builder` role is not selected, adds `#wipe log` at the bottom of `Important` when announcements or wipe react channels are requested, supports an `Invite` modal option that creates `#press for invite`, creates an `afk` voice channel at the bottom of `Voice Channels`, tries to set `#join` as the guild system channel for join messages, and sets the guild AFK channel plus Discord's nearest supported AFK timeout of 15 minutes. Setup state is stored via backend bot-only endpoints. Channel creation only bootstraps from a guild that still has exactly one existing channel, and the command must be run in that channel because setup replaces it. `/admin reset_setup` clears that state and deletes the roles and channels created by setup, replying with a summary (silently catches reply failures when the command channel was deleted during reset).
`/bind fullkit` uses a two-step v2 modal flow because Discord modals only allow five top-level components. Step 1 collects weapon, Attachments, ammo, ammo amount, and `Amount of barricades`; step 2 collects medkits amount, meds amount, armor, Anything else, and `Bind` as the final field. The text-field character caps are ammo `3`, meds `2`, medkits `2`, barricades `2`, and `Bind` `1`. Submitting the second step posts a non-ephemeral embed with the generated bind command. The bind output uses `craft.add {id} {amount}` batching, keeps the order weapon -> meds -> medkits -> barricade -> attachments -> ammo -> armor, splits meds into commands of at most `10` and medkits into commands of at most `5`, crafts the meds required for medkits before the medkits themselves, then crafts the user-requested meds, and crafts barricades with item id `1373240771`. HV ammo uses item id `1712070256`. Armor always crafts `-194953424`, `1850456855`, `1751045826`, and `237239288`, except `Shorts` swaps out `237239288` for `-1695367501`; the armor selection adds `-2002277461` for `Road Sign`, `1110385766` for `Metal`, or `-1163532624` for `Jacket`, and `Boots` adds `-1549739227`.
`/bind turret` opens a single v2 modal with `Weapons`, `Ammo`, `Amount of Ammo`, `Anything else?`, and `Bind`. It always crafts `-2139580305` first, then the selected weapon (`Bolt Action Rifle`, `HMLMG`, or `Python`), then the selected ammo using the same ammo yields as fullkit, then optional `Muzzle Boost` and `Silencer`, and posts a non-ephemeral embed titled `Turret bind`.
`/bind rocket` opens a single v2 modal with `Amount of Rockets`, `Anything else?`, and `Bind`. It posts a non-ephemeral embed titled `Rocket bind` and crafts gunpowder first (`-265876753`) at `65` gunpowder craft commands per rocket, then explosives (`-592016202`) at `10` per rocket, then optional `Pipes` (`95950017`) at `2` per rocket, then the rockets themselves (`-742865266`) chunked into commands of at most `20`.
`/bind c4` opens a single v2 modal with `Amount of C4` and `Bind`. It posts a non-ephemeral embed titled `C4 bind` and crafts gunpowder first (`-265876753`) at `100` gunpowder craft commands per C4, then explosives (`-592016202`) at `20` per C4, then the C4 itself (`1248356124`) chunked into commands of at most `20`.

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

The bot requires a real `DISCORD_TOKEN` for gateway login.

## CI/CD

- **`.github/workflows/ci.yml`** — On PR and push to `main`: lint/test/build for JS workspace, vet/test/build for Go backend, dry-run validate K8s manifests. Runs on self-hosted runners.
- **`.github/workflows/publish.yml`** — On `main`: builds and pushes `backend`, `frontend`, `bot` Docker images to GHCR.

## Skills

Agent skills live in `.agents/skills/` (symlinked to `.claude/skills/` for Claude Code). Each skill is a directory with a `SKILL.md` file. When creating or editing skills, always write to `.agents/skills/` — the symlink ensures all agent tooling picks them up.

Current skills:

| Skill | Description |
|---|---|
| `battlemetrics-api` | BattleMetrics REST API reference (endpoints, auth, pagination, JSON:API shapes) |

## Kubernetes

Manifests in `deploy/k8s/`. Apply in order: namespace -> configmap -> secrets -> backend -> frontend -> bot -> hpa -> ingress.
