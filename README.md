# RustLens Monorepo

RustLens is a multi-application monorepo with three services:

- `apps/backend`: Go API and shard coordinator for bot workers.
- `apps/frontend`: Next.js web UI.
- `apps/bot`: Discord.js sharded worker runtime.

## Architecture

- Frontend handles user-facing web traffic and currently ships as a public landing-first surface only.
- Backend handles all API traffic under `/api/*`.
- Bot workers register with backend, fetch shard assignments, heartbeat leases, and release on shutdown.
- Postgres stores worker event history.
- Redis stores active worker heartbeats and shard lease ownership.

## Traefik Routing

### Development (local Docker Compose)

Traefik routes from `http://localhost`:

- `PathPrefix(/api)` -> backend on `localhost:8080`
- everything else -> frontend on `localhost:3000`

### Production

Production deployment manifests are not maintained in this repository.

## Backend API Docs

The backend is self-documenting via OpenAPI:

- Interactive docs UI: `http://localhost/api/docs`
- OpenAPI JSON: `http://localhost/api/docs/openapi.json`

## Prerequisites

### Windows

Run PowerShell as Administrator:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/setup/windows-dev.ps1
```

### Ubuntu/Debian

```bash
bash scripts/setup/linux-dev.sh
```

## Developer Workflow

1. Install prerequisites with platform setup script.
2. Validate toolchain:

```bash
pnpm run doctor
```

3. Install dependencies and backend tooling:

```bash
pnpm run setup
```

> `pnpm run` is required for `setup` and `doctor` because the unprefixed forms collide with pnpm built-in commands.

4. Start local infra + hot reload for all apps:

```bash
pnpm dev
```

### Useful Commands

- `pnpm infra:up`: Start Postgres, Redis, Traefik.
- `pnpm infra:down`: Stop infra containers.
- `pnpm dev:backend`: Start backend only.
- `pnpm dev:frontend`: Start frontend only.
- `pnpm dev:bot`: Start bot only.
- `pnpm lint`: Workspace lint (frontend + bot). Use `pnpm lint:all` to also run `go vet`.
- `pnpm test`: Workspace tests (frontend + bot). Use `pnpm test:all` to also run `go test`.
- `pnpm build`: Workspace builds (frontend + bot). Use `pnpm build:all` to also build the Go binary.

## Environment Files

Copy examples and fill values:

- `apps/backend/.env.example`
- `apps/frontend/.env.example`
- `apps/bot/.env.example`

The bot requires a real `DISCORD_TOKEN` for gateway login.
The frontend currently only needs `PORT` when overriding the default Next.js port.

## Bot Sharding Model

1. Bot worker signs JWT (`service=bot`) using `BOT_SERVICE_JWT_SECRET`.
2. Worker calls `/api/internal/bot/shards/register`.
3. Worker periodically calls `/assignments` and reconciles local shard processes.
4. Worker heartbeats shard ownership via `/heartbeat`.
5. Worker releases leases via `/release` on shutdown.

Backend computes assignments from active workers and configured shard count (`BOT_TARGET_SHARDS`).

## CI/CD

- `.github/workflows/ci.yml`
  - Runs lint/test/build for JS workspace.
  - Runs vet/test/build for Go backend.
  - Runs on `ubuntu-latest` while the self-hosted fleet is being remediated.
- `.github/workflows/publish.yml`
  - On `main`, builds and pushes `backend`, `frontend`, and `bot` images to GHCR using lowercase owner tags.
  - Runs on `ubuntu-latest` while the self-hosted fleet is being remediated.
- `.github/workflows/runner-diagnostics.yml`
  - Manual self-hosted runner validation workflow for checkout, workspace, and post-job path issues.

