# RustLens Monorepo

RustLens is a multi-application monorepo with three services:

- `apps/backend`: Go API and shard coordinator for bot workers.
- `apps/frontend`: Next.js web UI.
- `apps/bot`: Discord.js sharded worker runtime.

## Architecture

- Frontend handles user-facing web traffic.
- Backend handles all API traffic under `/api/*`.
- Bot workers register with backend, fetch shard assignments, heartbeat leases, and release on shutdown.
- Postgres stores worker event history.
- Redis stores active worker heartbeats and shard lease ownership.

## Traefik Routing

### Development (local Docker Compose)

Traefik routes from `http://localhost`:

- `PathPrefix(/api)` -> backend on `localhost:8080`
- everything else -> frontend on `localhost:3000`

### Production (Kubernetes + Traefik)

Kubernetes ingress in `deploy/k8s/ingress.yaml` routes:

- `/api/*` -> `backend` service
- `/` -> `frontend` service

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
make doctor
```

3. Install dependencies and backend tooling:

```bash
make setup
```

4. Start local infra + hot reload for all apps:

```bash
make dev
```

### Useful Commands

- `make infra-up`: Start Postgres, Redis, Traefik.
- `make infra-down`: Stop infra containers.
- `make dev-backend`: Start backend only.
- `make dev-frontend`: Start frontend only.
- `make dev-bot`: Start bot only.
- `make lint`: Run workspace lint + Go vet.
- `make test`: Run workspace tests + Go tests.
- `make build`: Run workspace builds + Go binary build.

## Environment Files

Copy examples and fill values:

- `apps/backend/.env.example`
- `apps/frontend/.env.example`
- `apps/bot/.env.example`

The bot requires a real `DISCORD_TOKEN` for gateway login.

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

## Kubernetes Deployment

Apply manifests in order:

```bash
kubectl apply -f deploy/k8s/namespace.yaml
kubectl apply -f deploy/k8s/configmap.yaml
kubectl apply -f deploy/k8s/secrets.example.yaml
kubectl apply -f deploy/k8s/backend.yaml
kubectl apply -f deploy/k8s/frontend.yaml
kubectl apply -f deploy/k8s/bot.yaml
kubectl apply -f deploy/k8s/hpa.yaml
kubectl apply -f deploy/k8s/ingress.yaml
```

Update `ghcr.io/OWNER/...` images and secret values before production use.
