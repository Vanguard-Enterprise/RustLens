# RustLens Monorepo

RustLens is a multi-application monorepo with three apps:

- `apps/rust`: the backend. A Cargo workspace of five axum microservices (`identity`, `players`, `servers`, `guilds`, `coordinator`) plus a `migrate` one-shot schema tool and shared crates. See `AGENTS.md` for the full service breakdown.
- `apps/frontend`: Next.js web UI.
- `apps/bot`: Discord.js sharded worker runtime.

The Go backend (`apps/backend`) that used to serve `/api/*` has been deleted; the Rust fleet above is now authoritative for all API traffic.

## Architecture

- Frontend handles user-facing web traffic: a public landing surface plus the authenticated `/dashboard` and internal `/admin` dashboards.
- The five Rust services together handle all API traffic under `/api/*`, split by domain ownership (see Traefik Routing below).
- Bot workers register with the coordinator service, fetch shard assignments, heartbeat leases, and release on shutdown.
- Postgres stores all persistent state; schema is owned by `crates/rustlens-db/migrations/` and applied by the `migrate` binary before any service starts (no in-service auto-migration).
- Redis stores active worker heartbeats, shard lease ownership, cross-pod SSE fan-out, and assorted per-service coordination locks.

## Traefik Routing

### Development (local Docker Compose)

Traefik routes from `http://localhost`, configured via `infra/traefik/dynamic/*.yml` (one file per service):

- `Path(/api/healthz)` -> `identity:8090`
- `identity.yml` -> `identity:8090`: `/api/auth/discord*`, `/api/events*`, `/api/app/me*`, `/api/app/auth/logout`, `/api/internal/bot/identity/*`, and its admin routes (`/api/app/admin/{permissions,roles,users,administrators,audit,actions,api-keys}*`)
- `players.yml` -> `players:8091`: `/api/internal/bot/players/*`, `/api/app/players/*`
- `servers.yml` -> `servers:8092`: `/api/internal/bot/servers/*`, `/api/app/admin/{bm-servers,proxies,servers}/*`
- `guilds.yml` -> `guilds:8093`: `/api/internal/bot/guilds/*`, `/api/app/admin/{guilds,user-installs}/*`
- `coordinator.yml` -> `coordinator:8094`: `/api/internal/bot/coordinator/*`, `/api/discord/webhook-events`
- everything else -> frontend on `host.docker.internal:3000`

There is no `/api` catch-all anymore; every `/api/*` path is claimed by one of the five service files above.

### Production

Production deployment manifests are not maintained in this repository, but production routing mirrors the same five Traefik dynamic files (deploying them is part of the still-open production cutover, see `docs/rust-go-removal-plan.md`).

## Prerequisites

### Windows

Run PowerShell as Administrator:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/setup/windows-dev.ps1
```

Installs Docker Desktop, Node.js LTS, the Rust toolchain (rustup), and Git.

### Ubuntu/Debian

```bash
bash scripts/setup/linux-dev.sh
```

Installs Docker, Node.js LTS, pnpm, the Rust toolchain (rustup), and Git.

## Developer Workflow

1. Install prerequisites with the platform setup script above.
2. Validate toolchain (checks docker, node, pnpm, cargo, git):

```bash
pnpm run doctor
```

3. Install dependencies:

```bash
pnpm run setup
```

> `pnpm run` is required for `setup` and `doctor` because the unprefixed forms collide with pnpm built-in commands.

4. Start local infra plus hot reload for the frontend and bot:

```bash
pnpm dev
```

This runs `docker compose up -d` (Postgres, Redis, Traefik, `migrate`, and all five Rust services) and then hot-reloads the frontend and bot on the host. The Rust services themselves are not hot-reloaded; rebuild them directly with `cargo` (see `AGENTS.md`) or `docker compose up -d --build <service>`.

### Useful Commands

- `pnpm infra:up`: start Postgres, Redis, Traefik, `migrate`, and the full Rust fleet.
- `pnpm infra:down`: stop infra containers.
- `pnpm dev:frontend`: start the frontend only.
- `pnpm dev:bot`: start the bot only.
- `pnpm lint`: workspace lint (frontend + bot). Use `pnpm lint:all` to also run `cargo clippy --all-targets` in `apps/rust`.
- `pnpm test`: workspace tests (frontend + bot). Use `pnpm test:all` to also run `cargo test` in `apps/rust`.
- `pnpm build`: workspace builds (frontend + bot). Use `pnpm build:all` to also run `cargo build --release` in `apps/rust`.

There is no `dev:backend` script; the backend runs via the Docker fleet started by `pnpm dev` / `pnpm infra:up`.

## Environment Files

- `apps/bot/.env.example`: copy to `.env` and fill in (`DISCORD_TOKEN` is required for gateway login).
- `infra/rustlens.env.example`: copy to `infra/rustlens.env` and fill in; this is the shared secrets file for all five Rust services (Postgres/Redis URLs and per-service `PORT` are set separately in `infra/docker-compose.yml`).
- The frontend has no checked-in `.env.example`; see the Environment Variables section in `AGENTS.md` for what it expects.

## Bot Sharding Model

1. Bot worker signs a JWT (`service=bot`) using `BOT_SERVICE_JWT_SECRET`.
2. Worker calls `/api/internal/bot/coordinator/shards/register`.
3. Worker periodically calls `/api/internal/bot/coordinator/shards/assignments` and reconciles local shard processes.
4. Worker heartbeats shard ownership via `/api/internal/bot/coordinator/shards/heartbeat`.
5. Worker releases leases via `/api/internal/bot/coordinator/shards/release` on shutdown.

The coordinator service computes assignments from active workers and the configured shard count (`BOT_TARGET_SHARDS`).

## CI/CD

- `.github/workflows/ci.yml` (both jobs `runs-on: self-hosted`):
  - `workspace`: lint/test/build for the pnpm workspace (frontend + bot).
  - `rust`: builds, clippy-lints, migrates a Postgres service container, and tests the full `apps/rust` workspace.
- `.github/workflows/publish.yml` (`runs-on: self-hosted`):
  - On `main`, builds and pushes 8 images to GHCR using lowercase owner tags: `frontend`, `bot`, and `identity`/`players`/`servers`/`guilds`/`coordinator`/`migrate` (the last six all built from the single parametrized `apps/rust/Dockerfile`).
- `.github/workflows/runner-diagnostics.yml`
  - Manual self-hosted runner validation workflow for checkout, workspace, and post-job path issues.
