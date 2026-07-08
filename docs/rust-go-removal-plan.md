# Go removal master plan

How `rework/rust-backend` goes from its current state (five code-complete-ish Rust
services, Go still authoritative) to a **fully working Rust-only backend with Go
deleted from the repo**. Supersedes the per-domain Go-handler-deletion steps in
`docs/rust-cutover-plan.md`: since Go's end is confirmed, Go stays untouched as
the fallback until every domain is verified, then `apps/backend` is deleted
wholesale (one clean removal instead of five fiddly partial ones).

Decisions this plan builds on (both DECIDED 2026-07-08, recorded in
`docs/rust-cutover-plan.md`):

- **D1 routing**: domain-prefix reorg. Every bot-internal route moves under one
  prefix per service: `/api/internal/bot/{identity,players,servers,guilds,coordinator}/...`.
  One Traefik `PathPrefix` rule per service. Single bot base URL and single JWT
  audience (`rustlens-backend`) are preserved.
- **D2 schema**: a single one-shot `migrate` binary (`apps/rust/tools/migrate`)
  embedding ordered SQL via `sqlx::migrate!` from
  `crates/rustlens-db/migrations/`. Services never migrate. `0001_baseline.sql`
  is the distilled end-state schema, not a port of `EnsureSchema` history.

## Resolved: track-group analytics belongs to players

`GET .../track-groups/{groupID}/analytics` was ambiguous between players
(cutover plan) and guilds (roadmap). Verified against the Go implementation
(`apps/backend/internal/http/track_analytics_handler.go`): the handler is
track-group metadata + members lookup (three cheap DB reads) feeding the
analytics engine (BattleMetrics session fetch through the proxied outbound
provider, `track_analytics_sessions_cache` 24h TTL, Europe/London weekday/hour
compute). That entire engine already exists in
`apps/rust/services/players/src/track_analytics.rs` (it powers
offline-analytics), and players already has the outbound provider + BM client.
Guilds has neither. Cross-service DB reads are already the established pattern
(guilds' online-state route joins steam/player_states tables). **Players owns
it**; new path `/api/internal/bot/players/guilds/{guildID}/track-groups/{groupID}/analytics`.

## Phase A: route parity (close the Go-only gaps)

All items independent and parallelizable. Each follows the established
per-slice pattern: byte-for-byte response parity with the Go handler, queries
in `rustlens-db`, clippy-clean, live HTTP smoke vs throwaway PG/Redis.

| # | PR | Service | Contents (Go reference) | Executor |
|---|---|---|---|---|
| A1 | players BM/steam gap routes | players | `GET /players/{bmID}/bm-profile` + `/bm-analytics`, `GET /steam/{steamID}/friends`, `POST /steam/resolve`, `GET /steam-accounts/count` (`handlers.go:2667-3106+`, ~500 lines; friends uses steamapi `get_friend_list` already in rustlens-steam). Register under the OLD flat paths for now; the reorg (C1) renames. Note `steam/resolve` may be partially folded into `resolve.rs` already; confirm behavior parity, the route itself is missing. | sonnet |
| A2 | players track-group analytics | players | `GET /guilds/{g}/track-groups/{id}/analytics` (`track_analytics_handler.go`). Reuse `track_analytics.rs` compute + cache; add `get_track_group` / `list_track_analytics_members` / tracked-server-name reads via existing rustlens-db modules. | sonnet |
| A3 | identity me/* + users/ensure | identity | `POST /users/ensure` (`bot_users_handlers.go`, 54 lines), `GET /api/app/me/connections`, `/me/guilds`, `/me/guilds/installed`, `/me/guilds/{guildID}/links` (`app_handlers.go:140-345+`). | sonnet |
| A4 | identity admin API keys | identity | `GET /api/app/admin/api-keys` (`api_keys_handlers.go`, 102 lines) + dispatcher actions `api_keys.create/rotate/revoke` (`admin/actions/api_keys_*.go`) + API-key **auth** for admin routes (Go: `bridgeAPIKeyPerms` middleware) added to `rustlens-httpauth`. Bot-route API-key scope (`bot_route_scope.go`) is NOT ported by default (sign-off item S4). | opus |
| A5 | admin user-installs + servers | guilds + servers | `GET /api/app/admin/user-installs` (`admin_read_handlers.go:560`) into **guilds** (owns `bot_user_installs`); `GET /api/app/admin/servers` (`admin_read_handlers.go:621`, perm `bm_servers.read`) into **servers** (owns `tracked_servers`, already serves admin bm-servers). Two small additions, one PR. | sonnet |
| A6 | proxy auto-disable wiring | servers, players | `rustlens-outbound` already has the health window, threshold, and `set_auto_disable_callback` hook; the Go-parity gap is only the callback body (`cmd/server/main.go:129`): persist via `rustlens_db::outbound_proxies::disable_by_redacted_endpoint`, then reload the provider from DB. Wire it in both services that build a provider (servers, players). | sonnet |

**Gate A**: scripted route-parity audit. Diff the Go router registrations
(`internal/http/router.go`) against the union of the five Rust route tables;
the only allowed leftovers are `/api/docs*` (S3) and intentionally-dropped
API-key bot-route scope (S4). Plus workspace clippy/test green and per-PR
smokes.

## Phase B: the migrate binary (runs parallel to A)

| # | PR | Contents | Executor |
|---|---|---|---|
| B1 | migrate crate + baseline | New `apps/rust/tools/migrate` bin (~25 lines: connect, `rustlens_db::migrate(&pool)`, exit) + workspace member. Add `migrate` feature to the workspace sqlx dep. `rustlens_db::migrate()` wrapping `sqlx::migrate!("./migrations")`. **`0001_baseline.sql` generation**: run Go `EnsureSchema` against a throwaway Postgres, `pg_dump --schema-only --no-owner`, massage every statement to be fully idempotent (`CREATE TABLE/INDEX IF NOT EXISTS`, `CREATE EXTENSION IF NOT EXISTS pgcrypto` first, fold CHECK constraints, keep idempotent seeds like the `cached_counts` row, drop all legacy DROP/backfill/ALTER-TYPE history). Must be a clean no-op on a Go-migrated DB. Point the `RUSTLENS_TEST_PG_URL`-gated rustlens-db integration tests at `migrate()` instead of inline DDL. **0001 is never edited after landing** (sqlx checksum); future changes are `0002+`. | opus |
| B2 | migrate wiring | Compose: one-shot `migrate` service (SERVICE=migrate build arg, `restart: "no"`, gated on pg healthcheck); every Rust service gains `depends_on: migrate: service_completed_successfully`. CI: Postgres service container in the `rust` job, `cargo run --bin migrate`, run tests with `RUSTLENS_TEST_PG_URL` so the integration tests finally execute. Publish: `{ service: migrate, context: apps/rust }` matrix entry. **Schema-parity job** (dual-run guard, deleted with Go): Go `EnsureSchema` dump vs Rust `migrate` dump on two fresh DBs, assert empty diff. | sonnet |

**Gate B**: fresh `docker compose up` migrates then starts the fleet; parity
job green. Schema freeze during the dual-run window; any unavoidable change is
authored in both Go `EnsureSchema` and a new `000N` migration.

## Phase C: domain-prefix reorg (per service, after that service's Phase A items)

Per service: (1) Rust router dual-registers the new
`/api/internal/bot/<domain>/...` paths alongside the old flat ones (old ones
drop in Phase E); (2) new `infra/traefik/dynamic/<svc>.yml` at priority 200
with a single `PathPrefix(/api/internal/bot/<domain>)` rule plus that service's
app-facing paths from the cutover-plan ownership map; (3)
`apps/bot/src/backend-client.ts` methods flip to the new paths +
`backend-client.test.ts` fixture updates. The bot deploy is the cutover moment
per domain; rollback is reverting the bot (old paths still served by Rust and
Go).

Route-scheme rules (from the decision analysis): strip the
`setup/`/`owner/`/`shards` segment, root under the owning domain; guilds'
guild-scoped routes read `/guilds/{g}/...` with no doubled segment; players'
bm-scoped routes use a literal `bm/{bmID}` segment (`/players/bm/{bmID}/...`)
because axum/matchit panics when a bare param shares a position with literals;
coordinator gets `/coordinator/shards/*`, `/coordinator/stats`,
`/coordinator/rate-limit/consume`. Do not introduce per-service JWT audiences.

| # | PR | Notes | Executor |
|---|---|---|---|
| C1 | players reorg | Largest table, establishes the pattern, matchit `bm/` care. Blocked by A1+A2. | opus |
| C2 | servers reorg | Follows C1's template. `/owner/proxies*` becomes `/servers/owner/proxies*`. | sonnet |
| C3 | guilds reorg | Blocked by A5 (guilds half). | sonnet |
| C4 | coordinator reorg | Smallest table. | sonnet |
| C5 | identity reorg | Last: identity is already live on flat paths via `identity.yml`; this edits an in-service surface plus that file. Blocked by A3+A4. | sonnet |
| C6 | Go worker disable flags | Env flags in `cmd/server/main.go` to turn off the wipe-react scheduler and cleanup sweep (the two lock-free Go workers that double-run). BM poller / analytics refresher / role-connection reconciler already share locks with Rust and need no flag. One small Go PR, flipped per domain in prod. | sonnet |

**Gate C (per domain)**: local compose smoke proving the bot's calls for that
domain land on the Rust service via the new prefix; bot unit tests green;
prod verification after the bot deploy (held for Frank, see S1).

## Phase D: platform prep (parallel to A/B/C)

| # | PR | Contents | Executor |
|---|---|---|---|
| D1 | secrets relocation + frontend rewrite | New canonical env file `infra/rustlens.env` (+ checked-in `infra/rustlens.env.example` consolidating the shared vars from `apps/backend/.env.example`). Compose `env_file` entries repoint to it with `required: true` so a missing file fails loud (Rust configs already fail loud on missing required vars). Dev does a one-time `cp apps/backend/.env infra/rustlens.env`; Go keeps reading its own file until deleted. Same PR: `apps/frontend/next.config.ts` rewrite destination `http://localhost:8080` becomes env-driven (`BACKEND_ORIGIN`, default `http://localhost` = Traefik). | sonnet |
| D2 | rustlens-otel crate | New shared crate: `tracing-opentelemetry` + `opentelemetry-otlp` (gRPC) init behind `OTEL_EXPORTER_OTLP_ENDPOINT` (no-op when unset), service name per binary, wired into all five mains + migrate. Pyroscope profiling: optional fast-follow, not in this PR. | opus (crate) + sonnet (wiring) |

## Phase E: decommission sweep (after Gate C on all domains + S1 prod confirmation)

| # | PR | Contents | Executor |
|---|---|---|---|
| E1 | delete Go + infra refs | Delete `apps/backend/`. Drop the old flat-path registrations from all five Rust routers (dual-registration ends). `infra/traefik/dynamic/base.yml`: remove the `/api` catch-all router + backend service (frontend routing stays). Compose comments referencing `apps/backend/.env` cleaned. `package.json`: drop `dev:backend` and the Go halves of `setup`/`lint:all`/`test:all`/`build:all` (fold `:all` into the base scripts or keep them as pnpm+cargo). `scripts/dev/start-dev.mjs`: drop the backend command and port 8080; re-point `wait-for-backend.mjs` at the composed fleet health (or gate the bot on compose health directly). Setup scripts (`doctor.sh/.ps1`, `linux-dev.sh`, `windows-dev.ps1`): drop Go/Air installs and checks, add a `cargo` check. | sonnet |
| E2 | CI/CD + dependabot sweep | `ci.yml`: delete the `backend` job. `publish.yml`: remove the backend matrix entry. `dependabot.yml`: remove the `gomod` and backend-`docker` entries; add a `cargo` entry for `/apps/rust` (and `docker` for `apps/rust`). Delete the schema-parity job from B2 (its purpose ends with Go). | haiku |
| E3 | docs rewrite | `AGENTS.md`/`CLAUDE.md`: backend section rewritten around the Rust fleet (five services + migrate, ports, commands, env source of truth moves from `config.go` to `rustlens-config`), routing table, CI description, repo tree. `README.md` same. `.env.example` story updated to `infra/rustlens.env.example`. Mark the phase spec docs and this plan's predecessors as historical. | sonnet |

**Gate E**: `pnpm run doctor` passes without Go installed; fresh clone +
compose up serves everything through the five services; repo-wide grep for
`apps/backend`, `:8080`, `go test|go vet|go build` returns only historical
docs.

## Dependency graph

```
A1,A2 ─┐
A3,A4 ─┼─(independent, parallel)──► C1(A1,A2) ► C2 ► C3(A5) ► C4 ► C5(A3,A4)
A5,A6 ─┘                                │  (C-order = risk order; C6 anytime)
B1 ► B2 ──(parallel to A/C)─────────────┤
D1, D2 ───(parallel, anytime)───────────┤
                                        ▼
                    Gate: all domains verified + S1 prod confirmation
                                        ▼
                              E1 ► E2 ► E3
```

Roughly **19 PRs**: A 6, B 2, C 6, D 2 (+1 if otel wiring splits), E 3.
Critical path: A1/A2 → C1 → ... → C5 → prod verification → E1.

Executor distribution when running with subagents: **opus** for A4 (API-key
auth into the shared crate), B1 (baseline distillation), C1 (reorg
pattern-setter), D2 crate; **sonnet** for the well-referenced ports and
follow-the-template reorgs (most items); **haiku** for E2; **fable (main
loop)** holds orchestration, gate reviews, and the Gate A parity audit
judgment. Held for Frank: everything in S1 below.

## Sign-off items for Frank

- **S1 prod cutover (operator, outside this repo)**: deploy the fleet images +
  migrate init job, replicate the five Traefik dynamic files in prod, deploy
  the bot per domain, flip C6 worker flags, and finally confirm Rust-only so
  Phase E can land. Every in-repo item above is reversible; these are not.
- **S2 observability**: default recommendation is D2 **does not block repo
  work but lands before the first prod domain flip**, so the riskiest window
  keeps traces/metrics parity with Go's OTel + Pyroscope. Say the word to
  demote it to fast-follow.
- **S3 /api/docs**: default **drop**. The OpenAPI spec + Swagger UI is Go-only
  and nothing in the repo consumes it (zero references from frontend/bot). A
  utoipa-based reimplementation can be a later nice-to-have.
- **S4 API-key bot-route scope**: Go lets admin API keys call
  `/api/internal/bot/*` (`bot_route_scope.go`). Default: port API-key auth for
  **admin routes only** (A4) and drop the bot-route scope, unless some external
  consumer calls bot routes with an API key. Confirm none exists.
