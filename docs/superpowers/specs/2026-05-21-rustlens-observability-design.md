# RustLens Observability — Design

**Status:** Draft
**Date:** 2026-05-21
**Owner:** Frank Dobrovolny

## Goal

Wire RustLens (backend, bot, frontend) into the existing cluster LGTM+P stack so that every request, command, query, log line, and trace produced by the platform is observable, alertable, and profilable from Grafana. Dashboards and alerts ship as code via the same GitOps pattern used by every other application in `v-deployments`.

## Non-Goals

- Browser real-user monitoring (Grafana Faro). Server-side only this pass.
- SLO definitions and burn-rate alerts (need real production data first).
- Synthetic checks (blackbox exporter, Grafana Synthetic Monitoring).
- Cost-attribution dashboards.
- Tail-based trace sampling. Head-sampling at 10 % for v1.
- Per-tenant separation in Mimir/Loki/Tempo/Pyroscope. RustLens telemetry shares the existing `metrics` / `logs` / `traces` / `profiles` tenants alongside the rest of the platform — service identity comes from OTel resource attributes.

## Background — what already exists in the cluster

Loaded via `v-deployments` GitOps, no changes required for any of these:

- **Mimir** (`mimir.mimir.svc:8080`) — tenant `metrics`.
- **Loki** (`loki.loki.svc:3100`) — tenant `logs`.
- **Tempo** (`tempo.tempo.svc:3200` and OTLP gRPC `:4317`) — tenant `traces`.
- **Pyroscope** (`pyroscope.pyroscope.svc:4040`) — tenant `profiles`.
- **alloy-receiver** (`alloy-receiver.alloy-receiver.svc`) — OTLP gRPC `:4317`, OTLP HTTP `:4318`, Pyroscope HTTP `:4040`. Already fans signals out to the right backend with the correct `X-Scope-OrgID` header per signal type.
- **Grafana** — provisioned with all four datasources, trace-to-log and trace-to-metric correlation already wired, OIDC SSO via Zitadel, dashboard sidecar watching for ConfigMaps labeled `grafana_dashboard: "1"`, alert sidecar watching for `grafana_alert: "1"`.
- **Alert routing** — Grafana notification policy routes any alert with `team: rustlens` label to the `discord-rustlens` contact point.
- **PrometheusRule loader** — `alloy-metrics` picks up rules labeled `alloy_rules: pods`.
- **CNPG Postgres podmonitor** — `applications/cloudnative-postgres/overlays/rustlens/podmonitor.yaml` is already scraping `rustlens-db`. No change needed; the overview dashboard consumes it.

## Architecture

All three apps emit OTLP push to `alloy-receiver`. Pyroscope profiles ride the same receiver's HTTP ingest.

```
┌──────────────────┐   OTLP/gRPC :4317     ┌──────────────────┐
│ rustlens-backend │ ────────────────────▶ │                  │ ──▶ Mimir   (X-Scope-OrgID: metrics)
│   (Go + otel)    │                       │  alloy-receiver  │ ──▶ Tempo   (X-Scope-OrgID: traces)
└──────────────────┘                       │                  │ ──▶ Loki    (X-Scope-OrgID: logs)
┌──────────────────┐   OTLP/gRPC :4317     │                  │
│   rustlens-bot   │ ────────────────────▶ │                  │
│  (Node + otel)   │                       │                  │
└──────────────────┘                       │                  │
┌──────────────────┐   OTLP/gRPC :4317     │                  │
│ rustlens-frontend│ ────────────────────▶ │                  │
│  (Next.js + otel)│                       └──────────────────┘
└──────────────────┘
        │  Pyroscope HTTP :4040
        └──────────────▶ alloy-receiver ──▶ Pyroscope (X-Scope-OrgID: profiles)
```

No `ServiceMonitor` / `PodMonitor` for app metrics: OTLP push is the single source of truth, and adding a scrape path would double the cost and split the dashboards. Revisit only if push proves unreliable.

### Shared resource attributes

Set identically per service via env vars from each ConfigMap so they are not baked into images:

```
OTEL_SERVICE_NAME=rustlens-{backend,bot,frontend}
OTEL_SERVICE_NAMESPACE=rustlens
OTEL_RESOURCE_ATTRIBUTES=deployment.environment=use1,team=rustlens
OTEL_EXPORTER_OTLP_ENDPOINT=http://alloy-receiver.alloy-receiver.svc.cluster.local:4317
OTEL_EXPORTER_OTLP_PROTOCOL=grpc
OTEL_TRACES_SAMPLER=parentbased_traceidratio
OTEL_TRACES_SAMPLER_ARG=0.1
```

A single boolean kill-switch `OBSERVABILITY_ENABLED` gates the entire SDK init in all three apps. Default is `false` in each app's bundled defaults; the `v-deployments` ConfigMap flips it to `true`.

## Per-app instrumentation

### Backend — `apps/backend` (Go)

- **New package `internal/observability`** — owns SDK init (resource detector, OTLP gRPC exporters for traces / metrics / logs, batched span/log processors, periodic metric reader, graceful `Shutdown(ctx)`). Single `Init(ctx, cfg) (shutdown func(context.Context) error, err error)` entry point called from `cmd/server/main.go` immediately after `config.Load()`. When `cfg.ObservabilityEnabled` is false, returns a no-op shutdown and a `noop.NewTracerProvider()` / `noop.NewMeterProvider()` so call sites can register instruments unconditionally.
- **HTTP server** — wrap chi mux in `otelhttp.NewHandler` inside `internal/http/router.go` and set the route tag with `otelhttp.WithRouteTag` middleware so spans carry `http.route`. Produces `http.server.request.duration` histogram and `http.server.active_requests` gauge automatically.
- **Outbound HTTP** — wrap the `http.Transport` inside `outboundhttp.Provider` (and `directProvider`) with `otelhttp.NewTransport`. Every BattleMetrics, Steam, BMMatch, and proxy call becomes a client span with `http.client.*` metrics. Add a span attribute `rustlens.outbound.target` (`battlemetrics` / `steam` / `bmmatch` / `proxy`) so panels can break down by target without parsing URLs.
- **Postgres** — replace the `pgxpool.New` call in `internal/store/postgres.go` with a config that registers `otelpgx.NewTracer(otelpgx.WithIncludeQueryParameters(false))`. Gives per-query spans with the SQL text (parameters intentionally redacted). Add `otelpgx.RecordStats(pool)` to emit pool metrics (`db.client.connections.usage` etc.).
- **Redis** — `redisotel.InstrumentTracing(client)` + `redisotel.InstrumentMetrics(client)` in `internal/store/redis.go`.
- **Domain metrics** — defined where the behavior lives, registered against `otel.Meter("rustlens.<package>")`:
  - `internal/shards` — `rustlens.shard.assigned_count` (gauge), `rustlens.shard.heartbeat_age_seconds` (gauge), `rustlens.shard.rebalance_total` (counter).
  - `internal/battlemetrics` — `rustlens.battlemetrics.poll_duration_seconds` (histogram), `rustlens.battlemetrics.poll_error_total` (counter, attribute `reason`), `rustlens.battlemetrics.servers_polled` (gauge).
  - `internal/events` — `rustlens.events.published_total` (counter, attribute `channel`), `rustlens.events.subscribers` (gauge).
  - `internal/fraud` — `rustlens.fraud.signal_total` (counter, attribute `signal_type`).
  - `internal/ratelimit` — `rustlens.ratelimit.rejection_total` (counter, attribute `bucket`).
  - `internal/outboundhttp` — `rustlens.outbound.auto_disabled_total` (counter, attribute `target`).
  - `internal/wipereactscheduler` — `rustlens.wipe_scheduler.run_duration_seconds` (histogram), `rustlens.wipe_scheduler.run_error_total` (counter).
- **Logs** — replace `log.*` with `slog`. Default handler writes JSON to stdout (kept as fallback path so `alloy-logs` daemonset scrape still works). A second handler exports records via OTLP so they land in Loki with `trace_id` / `span_id` attached when emitted from an instrumented request. Use `slog.SetDefault(...)` in `main.go`; provide `internal/observability.Logger(ctx)` helper that returns a `slog.Logger` with the current span's IDs in its attributes.
- **Profiling** — `github.com/grafana/pyroscope-go` push agent started from `observability.Init` with:
  ```
  pyroscope.Start(pyroscope.Config{
      ApplicationName: "rustlens-backend",
      ServerAddress:   "http://alloy-receiver.alloy-receiver.svc.cluster.local:4040",
      Tags:            map[string]string{"environment": "use1", "namespace": "rustlens"},
      ProfileTypes:    []pyroscope.ProfileType{ /* cpu, alloc_objects, alloc_space, inuse_objects, inuse_space, goroutines, mutex, block */ },
  })
  ```
- **Testing** — smoke test under `internal/observability` that initializes against an in-memory exporter and asserts clean shutdown. Integration test for the HTTP server that hits `/api/healthz` against a stub OTLP receiver and asserts one span with `http.route=/api/healthz` plus a recorded `http.server.request.duration` data point.

### Bot — `apps/bot` (Node / TypeScript)

- **`src/observability.ts`** — `NodeSDK` from `@opentelemetry/sdk-node`, configured with `getNodeAutoInstrumentations` (excludes `fs` to avoid noise) plus explicit `@opentelemetry/instrumentation-http`, `@opentelemetry/instrumentation-pg`, `@opentelemetry/instrumentation-ioredis`, `@opentelemetry/instrumentation-pino`. OTLP gRPC trace + metric + log exporters reading endpoint from env. Must be imported as the very first statement of `src/index.ts` so auto-instrumentation patches modules before they are required.
- **Logs** — switch from `console.*` to `pino`. The pino instrumentation injects `trace_id` / `span_id` into every log record automatically; the OTel log exporter also pushes the same records to Loki.
- **Custom metrics**, registered against `meter('rustlens.bot')`:
  - `rustlens.bot.gateway_event_total` (counter, attribute `event`).
  - `rustlens.bot.command_duration_seconds` (histogram, attribute `command`).
  - `rustlens.bot.command_failure_total` (counter, attributes `command`, `reason`).
  - `rustlens.bot.shard_state` (gauge, attribute `shard_id`, value 0/1).
  - `rustlens.bot.interaction_failure_total` (counter, attribute `interaction_type`).
- **Profiling** — `@grafana/pyroscope-nodejs` with `serverAddress` pointing to alloy-receiver, `appName: 'rustlens-bot'`, same tags as backend.
- **Testing** — vitest smoke that initializes the SDK against the no-op OTel exporter and asserts metrics register. Command-dispatch test asserts a span is recorded per command execution.

### Frontend — `apps/frontend` (Next.js)

- **`instrumentation.ts`** at the app root, using `@vercel/otel`'s `registerOTel({ serviceName: 'rustlens-frontend' })`. This is the supported path on Next 14+ and handles both Node and Edge runtimes correctly. Server Components, Route Handlers, Server Actions, and middleware all auto-trace.
- **Custom metrics**, registered against `meter('rustlens.frontend')`:
  - `rustlens.frontend.ssr_duration_seconds` (histogram, attribute `route`).
  - `rustlens.frontend.route_hit_total` (counter, attribute `route`).
  - `rustlens.frontend.backend_fetch_duration_seconds` (histogram, attribute `endpoint`, `status_class`).
- **Logs** — `pino` with `@opentelemetry/instrumentation-pino`, same as bot. Replace existing `console.*` server-side calls.
- **No browser SDK and no profiling in this pass.**
- **Testing** — vitest smoke that imports `instrumentation.ts` against a stubbed OTel SDK and asserts `register` was called once.

## v-deployments changes

All paths relative to `Vanguard/v-deployments/`.

### Per-service overlays — `applications/rustlens/overlays/{backend,bot,frontend}/`

- `configmap.yaml` — add:
  ```
  OBSERVABILITY_ENABLED: "true"
  OTEL_SERVICE_NAME: rustlens-<svc>
  OTEL_SERVICE_NAMESPACE: rustlens
  OTEL_RESOURCE_ATTRIBUTES: "deployment.environment=use1,team=rustlens"
  OTEL_EXPORTER_OTLP_ENDPOINT: "http://alloy-receiver.alloy-receiver.svc.cluster.local:4317"
  OTEL_EXPORTER_OTLP_PROTOCOL: grpc
  OTEL_TRACES_SAMPLER: parentbased_traceidratio
  OTEL_TRACES_SAMPLER_ARG: "0.1"
  PYROSCOPE_SERVER_ADDRESS: "http://alloy-receiver.alloy-receiver.svc.cluster.local:4040"
  ```
- `deployment.yaml` — add pod labels (alongside existing `app: rustlens-<svc>`):
  ```
  app.kubernetes.io/name: rustlens-<svc>
  app.kubernetes.io/part-of: rustlens
  team: rustlens
  ```
  No port additions, no probe changes.

### New `prometheusrule.yaml` per service — `applications/rustlens/overlays/{backend,bot,frontend}/prometheusrule.yaml`

Each file owns its service's rules so blast radius is localized. All rules carry:
```
labels:
  team: rustlens
  service: rustlens-<svc>
  alloy_rules: pods
```

Cross-service baseline (instantiated once per service). Note: because we do not scrape the apps, the Prometheus `up` series does not exist for them — availability is derived from kube-state-metrics' deployment-replica gauges, which are already in Mimir.

- `RustLens<Svc>Down` — `kube_deployment_status_replicas_available{namespace="rustlens-<svc>", deployment="rustlens-<svc>"} == 0`, `for: 5m`, severity `critical`.
- `RustLens<Svc>Degraded` — `kube_deployment_status_replicas_available{namespace="rustlens-<svc>", deployment="rustlens-<svc>"} < kube_deployment_spec_replicas{namespace="rustlens-<svc>", deployment="rustlens-<svc>"}`, `for: 15m`, severity `warning`.
- `RustLens<Svc>HighErrorRate` —
  ```
  sum(rate(http_server_request_duration_seconds_count{
    service_name="rustlens-<svc>", http_response_status_code=~"5.."
  }[5m]))
  /
  sum(rate(http_server_request_duration_seconds_count{
    service_name="rustlens-<svc>"
  }[5m])) > 0.05
  ```
  `for: 10m`, severity `warning`.
- `RustLens<Svc>HighLatencyP99` — `histogram_quantile(0.99, sum by (le) (rate(http_server_request_duration_seconds_bucket{service_name="rustlens-<svc>"}[5m]))) > 1`, `for: 10m`, severity `warning`.
- `RustLens<Svc>CrashLooping` — `rate(kube_pod_container_status_restarts_total{namespace="rustlens-<svc>"}[15m]) > 0`, `for: 15m`, severity `warning`.

Backend-specific (in `backend/prometheusrule.yaml`):
- `RustLensBackendDBPoolExhausted` — `db_client_connections_usage{state="used", service_name="rustlens-backend"} / db_client_connections_max{service_name="rustlens-backend"} > 0.9`, `for: 10m`, severity `warning`.
- `RustLensBackendBattleMetricsErrors` — `sum(rate(rustlens_battlemetrics_poll_error_total[10m])) > 0.5`, `for: 15m`, severity `warning`.
- `RustLensBackendShardAssignmentStalled` — `max(rustlens_shard_heartbeat_age_seconds) > 120`, `for: 5m`, severity `critical`.

Bot-specific (in `bot/prometheusrule.yaml`):
- `RustLensBotShardDisconnected` — `min(rustlens_bot_shard_state) == 0`, `for: 5m`, severity `critical`.
- `RustLensBotCommandFailureRate` — `sum(rate(rustlens_bot_command_failure_total[10m])) / sum(rate(rustlens_bot_command_duration_seconds_count[10m])) > 0.1`, `for: 15m`, severity `warning`.

Frontend-specific (in `frontend/prometheusrule.yaml`): the four baseline rules only for v1.

Each overlay's `kustomization.yaml` gets `prometheusrule.yaml` added to its `resources:` list.

### Dashboards — `applications/grafana/overlays/use1/vanguard/dashboards/files/rustlens/`

New folder with five hand-authored Grafana JSON files (no Jsonnet — matches existing convention):
- `rustlens-overview.json` — cross-service request rate, error rate, p99 latency, pod count, restart count, log error rate, top traces by latency, DB row from CNPG metrics, "service health" stat panels.
- `rustlens-backend.json` — RED panels overall and per-route (`http.route` attribute), DB pool usage, Redis latency, outbound HTTP breakdown by `rustlens.outbound.target`, shard heartbeat panel, fraud signal counters, logs panel filtered to `service_name=rustlens-backend`, "exemplar trace" table linking to Tempo.
- `rustlens-bot.json` — gateway events per second, command latency by name, shard state matrix, interaction failures, logs panel.
- `rustlens-frontend.json` — SSR latency, route hits, server-side backend fetch breakdown, logs panel.
- `rustlens-data-pipeline.json` — BattleMetrics poller depth + error breakdown, events bridge throughput, fraud signal counters, wipe scheduler runs.

All panels use the `Mimir`, `Loki`, `Tempo` datasource UIDs that already exist in the cluster Grafana. Trace-to-log and trace-to-metric correlation is inherited from the existing datasource provisioning — no per-panel wiring required.

Add new `configMapGenerator` entries in `dashboards/kustomization.yaml`, each annotated:
```
options:
  annotations:
    grafana_dashboard_folder: RustLens
```

## Rollout

Two repos, executed in order.

### RustLens — single PR

One PR titled `feat(observability): instrument backend, bot, and frontend with OpenTelemetry`. Contents:
- New `internal/observability` package (Go).
- HTTP / outbound / pgx / redis instrumentation wiring.
- `slog` migration + logger helper.
- Pyroscope agent.
- All backend domain metrics.
- `src/observability.ts` + entry-point import order change (bot).
- All bot domain metrics + pino migration.
- `instrumentation.ts` + `@vercel/otel` setup (frontend).
- Frontend custom metrics + pino migration.
- Smoke + integration tests.
- Dependency additions to `go.mod` and the two `package.json` files.

The default value of `OBSERVABILITY_ENABLED` is `false` in every app's bundled config so merging this PR is a behaviour no-op in production until the `v-deployments` ConfigMap flips it.

### v-deployments — three direct commits to `main`, ArgoCD syncs each

Sequenced so we see signal before alerting on it. Manual verification gate between each.

1. **Commit A — `chore(rustlens): enable observability env vars and pod labels`.** Updates the three `configmap.yaml` files with the `OTEL_*` block + `OBSERVABILITY_ENABLED=true` + `PYROSCOPE_SERVER_ADDRESS`, and the three `deployment.yaml` files with the `app.kubernetes.io/part-of` + `team` labels. Pods restart.

   *Gate*: in Grafana → Explore → Tempo, filter `service.name=rustlens-backend` (and bot, frontend) and confirm spans appear. Same in Loki for logs, Mimir for `http_server_request_duration_seconds_count`, Pyroscope for `rustlens-backend`. If anything is wrong, flip `OBSERVABILITY_ENABLED=false` in a one-line revert commit and debug.

2. **Commit B — `feat(grafana): provision RustLens dashboard folder`.** Adds the five JSON files + the `configMapGenerator` entries.

   *Gate*: open the new RustLens folder in Grafana, confirm each dashboard loads and every panel returns data.

3. **Commit C — `feat(rustlens): provision baseline alert rules`.** Adds the three `prometheusrule.yaml` files and wires them into the kustomizations.

   *Synthetic test*: `kubectl scale deploy/rustlens-backend -n rustlens-backend --replicas=0`, wait 5–7 min, confirm `RustLensBackendDown` fires to the `discord-rustlens` Discord channel, then scale back to 2.

Each commit is independently revertable. Kill-switch for the whole feature is `OBSERVABILITY_ENABLED=false` in the ConfigMaps → pod restart → no OTLP traffic at all.

## Risks & mitigations

- **Cardinality explosion in `http.route` attribute** — auto-instrumentation can emit per-path-parameter routes if route tags are not set correctly. Mitigation: explicit `otelhttp.WithRouteTag` on every chi route group, and verify in Mimir's `cardinality_count` panel post-rollout. Add a cardinality cap rule in alloy-metrics if needed.
- **alloy-receiver becomes a single point of failure for app telemetry** — if it goes down, no signals flow. Acceptable for v1 since the same receiver already handles all platform traces and profiles; if it falls over, lots of things break at once and we'll know. Track as a follow-up to make alloy-receiver HA.
- **OTLP gRPC blocking startup** — if alloy-receiver is unreachable, the exporter's connection backoff can delay startup. Mitigation: use the async (`BatchSpanProcessor`) export path everywhere and never block on first export.
- **Profiling overhead** — Pyroscope CPU profiling typically adds < 2 % overhead but `mutex` / `block` profiles can be more. Start with `cpu`, `alloc_objects`, `alloc_space`, `inuse_objects`, `inuse_space`, `goroutines` only; add the contention profiles after we have baseline data.
- **Dependency bloat in `apps/bot` and `apps/frontend`** — OTel JS auto-instrumentations pull a lot of packages. Mitigation: explicit instrumentation list rather than `getNodeAutoInstrumentations({})` defaults, and a `pnpm dlx depcheck` sweep after the PR.
- **Trace sampling at 10 %** may under-sample rare-but-important request paths. Mitigation: revisit after a week of data; consider per-route sampler overrides or tail-based sampling in alloy-receiver as a follow-up.

## Open questions

None — all clarified during brainstorming.
