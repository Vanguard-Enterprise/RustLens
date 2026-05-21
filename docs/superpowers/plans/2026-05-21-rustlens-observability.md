# RustLens Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Instrument RustLens (backend, bot, frontend) with OpenTelemetry pushing OTLP to the cluster `alloy-receiver`, plus provision Grafana dashboards and PrometheusRule alerts via the existing `v-deployments` GitOps pattern.

**Architecture:** All three apps emit OTLP (metrics + traces + logs) push to `alloy-receiver.alloy-receiver.svc.cluster.local:4317`, which already fans to Mimir/Tempo/Loki with the right tenant headers. Pyroscope profiles for backend + bot ride the receiver's `:4040` HTTP ingest. No `ServiceMonitor`s for app metrics — OTLP push is the single source of truth. Availability alerts derive from kube-state-metrics' deployment-replica gauges. Everything is gated by a single `OBSERVABILITY_ENABLED` env so the RustLens PR ships dark and the deployment ConfigMap commit flips it on.

**Tech Stack:**
- Go: `go.opentelemetry.io/otel/*`, `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`, `go.opentelemetry.io/contrib/bridges/otelslog`, `github.com/exaring/otelpgx`, `github.com/redis/go-redis/extra/redisotel/v9`, `github.com/grafana/pyroscope-go`.
- Node: `@opentelemetry/sdk-node`, `@opentelemetry/auto-instrumentations-node`, `@opentelemetry/exporter-trace-otlp-grpc`, `@opentelemetry/exporter-metrics-otlp-grpc`, `@opentelemetry/exporter-logs-otlp-grpc`, `@opentelemetry/instrumentation-pino`, `pino`, `@grafana/pyroscope-nodejs`.
- Next.js: `@vercel/otel`, `@opentelemetry/api`, `pino`, `@opentelemetry/instrumentation-pino`.
- Kubernetes: Kustomize, Prometheus Operator CRDs (`PrometheusRule`), Grafana sidecar dashboards.

**Spec:** `docs/superpowers/specs/2026-05-21-rustlens-observability-design.md`

**Worktree note:** Branch `feature/observability` already checked out in the main `B:\.dev\RustLens` working tree. v-deployments tasks operate in a separate repo at `B:\.dev\Vanguard\v-deployments` on the `main` branch.

---

## Phase 1 — Backend observability foundation

### Task 1: Add OTel + Pyroscope Go dependencies

**Files:**
- Modify: `apps/backend/go.mod`
- Modify: `apps/backend/go.sum`

- [ ] **Step 1: Add dependencies**

Run from `apps/backend`:

```bash
go get \
  go.opentelemetry.io/otel@v1.32.0 \
  go.opentelemetry.io/otel/sdk@v1.32.0 \
  go.opentelemetry.io/otel/sdk/metric@v1.32.0 \
  go.opentelemetry.io/otel/sdk/log@v0.8.0 \
  go.opentelemetry.io/otel/log@v0.8.0 \
  go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.32.0 \
  go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc@v1.32.0 \
  go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc@v0.8.0 \
  go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@v0.57.0 \
  go.opentelemetry.io/contrib/bridges/otelslog@v0.8.0 \
  github.com/exaring/otelpgx@v0.9.0 \
  github.com/redis/go-redis/extra/redisotel/v9@v9.7.0 \
  github.com/grafana/pyroscope-go@v1.2.0
go mod tidy
```

- [ ] **Step 2: Verify build still passes**

```bash
go build ./...
```

Expected: exit 0, no output.

- [ ] **Step 3: Commit**

```bash
git add apps/backend/go.mod apps/backend/go.sum
git commit -m "feat(backend): add opentelemetry and pyroscope dependencies"
```

---

### Task 2: Extend backend config with observability fields

**Files:**
- Modify: `apps/backend/internal/config/config.go`

- [ ] **Step 1: Add fields to the Config struct**

In `apps/backend/internal/config/config.go`, add to the `Config` struct (alongside the existing fields):

```go
// Observability.
ObservabilityEnabled bool
OTLPEndpoint         string
PyroscopeAddress     string
ServiceName          string
ServiceNamespace     string
DeploymentEnv        string
TraceSampleRatio     float64
```

- [ ] **Step 2: Populate the fields in Load()**

In the `return Config{...}` literal inside `Load()`, add:

```go
ObservabilityEnabled: getEnvBool("OBSERVABILITY_ENABLED", false),
OTLPEndpoint:         strings.TrimSpace(getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "")),
PyroscopeAddress:     strings.TrimSpace(getEnv("PYROSCOPE_SERVER_ADDRESS", "")),
ServiceName:          getEnv("OTEL_SERVICE_NAME", "rustlens-backend"),
ServiceNamespace:     getEnv("OTEL_SERVICE_NAMESPACE", "rustlens"),
DeploymentEnv:        getEnv("DEPLOYMENT_ENV", "local"),
TraceSampleRatio:     getEnvFloat("OTEL_TRACES_SAMPLER_ARG", 0.1),
```

- [ ] **Step 3: Add the getEnvFloat helper**

Append to the file (next to the other `getEnv*` helpers):

```go
func getEnvFloat(key string, fallback float64) float64 {
    if v := os.Getenv(key); v != "" {
        if f, err := strconv.ParseFloat(v, 64); err == nil {
            return f
        }
    }
    return fallback
}
```

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

Expected: exit 0.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/config/config.go
git commit -m "feat(backend): add observability fields to config"
```

---

### Task 3: Create observability package — SDK init + no-op fallback

**Files:**
- Create: `apps/backend/internal/observability/observability.go`
- Create: `apps/backend/internal/observability/observability_test.go`

- [ ] **Step 1: Write the failing test**

`apps/backend/internal/observability/observability_test.go`:

```go
package observability

import (
    "context"
    "testing"

    "rustlens/backend/internal/config"
)

func TestInitDisabledReturnsNoopShutdown(t *testing.T) {
    cfg := config.Config{ObservabilityEnabled: false}
    shutdown, err := Init(context.Background(), cfg)
    if err != nil {
        t.Fatalf("init: %v", err)
    }
    if shutdown == nil {
        t.Fatal("shutdown was nil")
    }
    if err := shutdown(context.Background()); err != nil {
        t.Fatalf("shutdown: %v", err)
    }
}

func TestInitEnabledWithMissingEndpointFails(t *testing.T) {
    cfg := config.Config{ObservabilityEnabled: true, OTLPEndpoint: ""}
    if _, err := Init(context.Background(), cfg); err == nil {
        t.Fatal("expected error when enabled without endpoint")
    }
}
```

- [ ] **Step 2: Run test, expect compile failure**

```bash
go test ./internal/observability/...
```

Expected: compile error — `Init` undefined.

- [ ] **Step 3: Implement observability.go**

`apps/backend/internal/observability/observability.go`:

```go
// Package observability initializes OpenTelemetry (traces, metrics, logs) and
// Pyroscope profiling for the backend. When ObservabilityEnabled is false the
// global providers stay as the SDK's built-in no-ops so call sites can use
// otel.Tracer / otel.Meter unconditionally.
package observability

import (
    "context"
    "errors"
    "fmt"
    "net/url"
    "time"

    "github.com/grafana/pyroscope-go"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
    "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    sdklog "go.opentelemetry.io/otel/sdk/log"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

    "rustlens/backend/internal/config"
)

// Shutdown gracefully drains pending telemetry; safe to call multiple times.
type Shutdown func(context.Context) error

// Init wires up OTel + Pyroscope from cfg. Returns a no-op Shutdown when
// observability is disabled.
func Init(ctx context.Context, cfg config.Config) (Shutdown, error) {
    if !cfg.ObservabilityEnabled {
        return noopShutdown, nil
    }
    if cfg.OTLPEndpoint == "" {
        return nil, errors.New("observability enabled but OTEL_EXPORTER_OTLP_ENDPOINT is empty")
    }

    res, err := newResource(ctx, cfg)
    if err != nil {
        return nil, fmt.Errorf("resource: %w", err)
    }

    endpoint, insecure, err := parseOTLPEndpoint(cfg.OTLPEndpoint)
    if err != nil {
        return nil, fmt.Errorf("otlp endpoint: %w", err)
    }

    traceShutdown, err := initTracer(ctx, res, endpoint, insecure, cfg.TraceSampleRatio)
    if err != nil {
        return nil, fmt.Errorf("tracer: %w", err)
    }
    metricShutdown, err := initMeter(ctx, res, endpoint, insecure)
    if err != nil {
        _ = traceShutdown(ctx)
        return nil, fmt.Errorf("meter: %w", err)
    }
    logShutdown, err := initLogger(ctx, res, endpoint, insecure)
    if err != nil {
        _ = traceShutdown(ctx)
        _ = metricShutdown(ctx)
        return nil, fmt.Errorf("logger: %w", err)
    }

    profilerShutdown, err := initProfiler(cfg)
    if err != nil {
        _ = traceShutdown(ctx)
        _ = metricShutdown(ctx)
        _ = logShutdown(ctx)
        return nil, fmt.Errorf("profiler: %w", err)
    }

    return composeShutdown(traceShutdown, metricShutdown, logShutdown, profilerShutdown), nil
}

func noopShutdown(context.Context) error { return nil }

func newResource(ctx context.Context, cfg config.Config) (*resource.Resource, error) {
    return resource.New(ctx,
        resource.WithFromEnv(), // picks up OTEL_RESOURCE_ATTRIBUTES
        resource.WithProcess(),
        resource.WithHost(),
        resource.WithAttributes(
            semconv.ServiceName(cfg.ServiceName),
            semconv.ServiceNamespace(cfg.ServiceNamespace),
            semconv.DeploymentEnvironment(cfg.DeploymentEnv),
            attribute.String("team", "rustlens"),
        ),
    )
}

func parseOTLPEndpoint(raw string) (string, bool, error) {
    u, err := url.Parse(raw)
    if err != nil {
        return "", false, err
    }
    if u.Scheme == "" {
        // bare host:port
        return raw, true, nil
    }
    return u.Host, u.Scheme != "https", nil
}

func initTracer(ctx context.Context, res *resource.Resource, endpoint string, insecure bool, ratio float64) (Shutdown, error) {
    opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint)}
    if insecure {
        opts = append(opts, otlptracegrpc.WithInsecure())
    }
    exp, err := otlptracegrpc.New(ctx, opts...)
    if err != nil {
        return nil, err
    }
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithResource(res),
        sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(5*time.Second)),
        sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
    )
    otel.SetTracerProvider(tp)
    return tp.Shutdown, nil
}

func initMeter(ctx context.Context, res *resource.Resource, endpoint string, insecure bool) (Shutdown, error) {
    opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(endpoint)}
    if insecure {
        opts = append(opts, otlpmetricgrpc.WithInsecure())
    }
    exp, err := otlpmetricgrpc.New(ctx, opts...)
    if err != nil {
        return nil, err
    }
    mp := sdkmetric.NewMeterProvider(
        sdkmetric.WithResource(res),
        sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(30*time.Second))),
    )
    otel.SetMeterProvider(mp)
    return mp.Shutdown, nil
}

func initLogger(ctx context.Context, res *resource.Resource, endpoint string, insecure bool) (Shutdown, error) {
    opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(endpoint)}
    if insecure {
        opts = append(opts, otlploggrpc.WithInsecure())
    }
    exp, err := otlploggrpc.New(ctx, opts...)
    if err != nil {
        return nil, err
    }
    lp := sdklog.NewLoggerProvider(
        sdklog.WithResource(res),
        sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)),
    )
    // The global logger provider is set so the slog bridge picks it up.
    SetGlobalLoggerProvider(lp)
    return lp.Shutdown, nil
}

func initProfiler(cfg config.Config) (Shutdown, error) {
    if cfg.PyroscopeAddress == "" {
        return noopShutdown, nil
    }
    p, err := pyroscope.Start(pyroscope.Config{
        ApplicationName: cfg.ServiceName,
        ServerAddress:   cfg.PyroscopeAddress,
        Tags: map[string]string{
            "environment": cfg.DeploymentEnv,
            "namespace":   cfg.ServiceNamespace,
        },
        ProfileTypes: []pyroscope.ProfileType{
            pyroscope.ProfileCPU,
            pyroscope.ProfileAllocObjects,
            pyroscope.ProfileAllocSpace,
            pyroscope.ProfileInuseObjects,
            pyroscope.ProfileInuseSpace,
            pyroscope.ProfileGoroutines,
        },
    })
    if err != nil {
        return nil, err
    }
    return func(context.Context) error { return p.Stop() }, nil
}

func composeShutdown(fns ...Shutdown) Shutdown {
    return func(ctx context.Context) error {
        var firstErr error
        for _, fn := range fns {
            if fn == nil {
                continue
            }
            if err := fn(ctx); err != nil && firstErr == nil {
                firstErr = err
            }
        }
        return firstErr
    }
}
```

- [ ] **Step 4: Add the slog bridge holder (separate file for clarity)**

`apps/backend/internal/observability/logger.go`:

```go
package observability

import (
    "context"
    "log/slog"
    "sync/atomic"

    "go.opentelemetry.io/contrib/bridges/otelslog"
    sdklog "go.opentelemetry.io/otel/sdk/log"
)

var globalLoggerProvider atomic.Pointer[sdklog.LoggerProvider]

// SetGlobalLoggerProvider stores the provider for Logger() to read.
func SetGlobalLoggerProvider(lp *sdklog.LoggerProvider) {
    globalLoggerProvider.Store(lp)
}

// Logger returns a slog.Logger that writes to both stdout (handled by the
// caller's default slog setup) and Loki via OTLP. If observability is disabled
// or Init has not run, callers receive slog.Default().
//
// The returned logger automatically attaches trace_id / span_id when a span is
// active in ctx.
func Logger(ctx context.Context) *slog.Logger {
    lp := globalLoggerProvider.Load()
    if lp == nil {
        return slog.Default()
    }
    handler := otelslog.NewHandler("rustlens-backend", otelslog.WithLoggerProvider(lp))
    return slog.New(handler).With(slog.String("trace_ctx", "active"))
}
```

- [ ] **Step 5: Run tests, expect pass**

```bash
go test ./internal/observability/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/observability/
git commit -m "feat(backend): add observability package with otel sdk init"
```

---

### Task 4: Wire observability.Init into main.go

**Files:**
- Modify: `apps/backend/cmd/server/main.go`

- [ ] **Step 1: Add import**

Add to the import block of `cmd/server/main.go`:

```go
"rustlens/backend/internal/observability"
```

- [ ] **Step 2: Init observability immediately after config.Load**

After `cfg := config.Load()` and `defer cancel()`, add:

```go
shutdownObs, err := observability.Init(ctx, cfg)
if err != nil {
    log.Fatalf("observability init failed: %v", err)
}
defer func() {
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := shutdownObs(shutdownCtx); err != nil {
        log.Printf("[backend] observability shutdown error: %v", err)
    }
}()
```

- [ ] **Step 3: Verify build**

```bash
go build ./...
```

Expected: exit 0.

- [ ] **Step 4: Smoke run**

```bash
OBSERVABILITY_ENABLED=false go run ./cmd/server &
sleep 2
kill %1
```

Expected: server starts, no observability noise in logs.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/cmd/server/main.go
git commit -m "feat(backend): wire observability init into main"
```

---

### Task 5: Default slog handler to JSON stdout

**Files:**
- Modify: `apps/backend/cmd/server/main.go`

- [ ] **Step 1: Set default slog handler**

Above the `cfg := config.Load()` line in `main()`, add:

```go
slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
    Level:     slog.LevelInfo,
    AddSource: false,
})))
```

Add `log/slog` to the imports.

- [ ] **Step 2: Verify build**

```bash
go build ./...
```

Expected: exit 0.

- [ ] **Step 3: Smoke check log format**

```bash
go run ./cmd/server 2>&1 | head -3
# kill with ^C
```

Expected: first line is JSON like `{"time":"...","level":"INFO",...}` rather than the default `2024/05/21 ...` format.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/cmd/server/main.go
git commit -m "refactor(backend): default slog handler to json stdout"
```

---

## Phase 2 — Backend instrumentation wiring

### Task 6: Instrument the chi HTTP router

**Files:**
- Modify: `apps/backend/internal/http/router.go`
- Create: `apps/backend/internal/http/observability.go`
- Create: `apps/backend/internal/http/observability_test.go`

- [ ] **Step 1: Write the failing integration test**

`apps/backend/internal/http/observability_test.go`:

```go
package httpapi

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/go-chi/chi/v5"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"
    "go.opentelemetry.io/otel"
)

func TestOtelHTTPMiddlewareEmitsRouteSpan(t *testing.T) {
    rec := tracetest.NewSpanRecorder()
    tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
    otel.SetTracerProvider(tp)
    t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

    r := chi.NewRouter()
    r.Use(WithOTel("rustlens-backend"))
    r.Get("/api/healthz", func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusOK)
    })

    req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
    rr := httptest.NewRecorder()
    r.ServeHTTP(rr, req)

    spans := rec.Ended()
    if len(spans) != 1 {
        t.Fatalf("want 1 span, got %d", len(spans))
    }
    found := false
    for _, attr := range spans[0].Attributes() {
        if string(attr.Key) == "http.route" && attr.Value.AsString() == "/api/healthz" {
            found = true
        }
    }
    if !found {
        t.Fatalf("missing http.route attribute, got %v", spans[0].Attributes())
    }
}
```

- [ ] **Step 2: Run, expect compile fail**

```bash
go test ./internal/http/... -run TestOtelHTTPMiddleware
```

Expected: undefined `WithOTel`.

- [ ] **Step 3: Implement the middleware helper**

`apps/backend/internal/http/observability.go`:

```go
package httpapi

import (
    "net/http"

    "github.com/go-chi/chi/v5"
    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
    "go.opentelemetry.io/otel/attribute"
)

// WithOTel wraps the router so every request produces an HTTP server span and
// the http.server.* metric family, with http.route set from chi's resolved
// route pattern (rather than the raw URL, which would explode cardinality).
//
// The middleware runs after chi has resolved the route pattern by deferring
// the otelhttp wrapping to the downstream handler.
func WithOTel(serviceName string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            next.ServeHTTP(w, r)
            if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
                if labeler, ok := otelhttp.LabelerFromContext(r.Context()); ok {
                    labeler.Add(attribute.String("http.route", rctx.RoutePattern()))
                }
            }
        })
        return otelhttp.NewHandler(
            wrapped,
            "",
            otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
                if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
                    return r.Method + " " + rctx.RoutePattern()
                }
                return r.Method + " " + r.URL.Path
            }),
        )
    }
}
```

- [ ] **Step 4: Register the middleware in NewRouter**

In `apps/backend/internal/http/router.go`, find the line where `chi.NewRouter()` is created and add `r.Use(WithOTel("rustlens-backend"))` as the first middleware — before any existing `r.Use(...)` lines.

- [ ] **Step 5: Run the test**

```bash
go test ./internal/http/... -run TestOtelHTTPMiddleware
```

Expected: PASS. If `http.route` is not set, the route attribute is being read before chi resolves it; revisit the wrapping order.

- [ ] **Step 6: Run the full test suite for the package**

```bash
go test ./internal/http/...
```

Expected: PASS (no existing tests should regress).

- [ ] **Step 7: Commit**

```bash
git add apps/backend/internal/http/observability.go apps/backend/internal/http/observability_test.go apps/backend/internal/http/router.go
git commit -m "feat(backend): instrument chi router with otelhttp"
```

---

### Task 7: Instrument outbound HTTP

**Files:**
- Modify: `apps/backend/internal/outboundhttp/provider.go` (or whichever file creates the `http.Transport`)

- [ ] **Step 1: Find where the Transport is built**

```bash
grep -rn "http.Transport\|NewProvider" apps/backend/internal/outboundhttp/
```

- [ ] **Step 2: Wrap with otelhttp.NewTransport**

Where the existing `http.Transport` is assigned to the client, wrap it:

```go
import (
    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
    "go.opentelemetry.io/otel/attribute"
)

// ... existing transport build ...
transport := &http.Transport{ /* existing config */ }
client := &http.Client{
    Transport: otelhttp.NewTransport(transport,
        otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
            return r.Method + " " + r.URL.Host
        }),
    ),
    Timeout: timeout,
}
```

- [ ] **Step 3: Add target tagging helper**

In the same file or a sibling `tracing.go`, expose:

```go
import (
    "context"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/trace"
)

// WithTarget annotates the active span with the logical outbound target.
// Call this from each caller (battlemetrics, steam, etc.) before issuing
// the request.
func WithTarget(ctx context.Context, target string) {
    if span := trace.SpanFromContext(ctx); span != nil {
        span.SetAttributes(attribute.String("rustlens.outbound.target", target))
    }
}
```

- [ ] **Step 4: Tag call sites**

For each of `internal/battlemetrics/client.go`, `internal/steamapi/client.go`, `internal/bmmatch/client.go`, `internal/platformsync/client.go`, immediately before the `client.Do(req)` (or equivalent) call:

```go
outboundhttp.WithTarget(ctx, "battlemetrics") // adjust target name per package
```

- [ ] **Step 5: Verify build**

```bash
go build ./...
```

Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/outboundhttp/ apps/backend/internal/battlemetrics/client.go \
        apps/backend/internal/steamapi/client.go apps/backend/internal/bmmatch/client.go \
        apps/backend/internal/platformsync/client.go
git commit -m "feat(backend): trace outbound http with target tagging"
```

---

### Task 8: Instrument pgx and emit pool stats

**Files:**
- Modify: `apps/backend/internal/store/postgres.go`

- [ ] **Step 1: Update NewPostgresStore**

Replace the body of `NewPostgresStore`:

```go
import (
    "github.com/exaring/otelpgx"
    "github.com/jackc/pgx/v5/pgxpool"
)

func NewPostgresStore(ctx context.Context, connectionString string) (*PostgresStore, error) {
    cfg, err := pgxpool.ParseConfig(connectionString)
    if err != nil {
        return nil, err
    }
    cfg.ConnConfig.Tracer = otelpgx.NewTracer(
        otelpgx.WithIncludeQueryParameters(false),
    )

    pool, err := pgxpool.NewWithConfig(ctx, cfg)
    if err != nil {
        return nil, err
    }
    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        return nil, err
    }
    if err := otelpgx.RecordStats(pool); err != nil {
        pool.Close()
        return nil, fmt.Errorf("otelpgx record stats: %w", err)
    }
    return &PostgresStore{pool: pool}, nil
}
```

Add `"fmt"` to imports if not present.

- [ ] **Step 2: Verify build**

```bash
go build ./...
```

Expected: exit 0.

- [ ] **Step 3: Run any existing postgres tests**

```bash
go test ./internal/store/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/store/postgres.go
git commit -m "feat(backend): instrument pgx pool with otelpgx"
```

---

### Task 9: Instrument Redis client

**Files:**
- Modify: `apps/backend/internal/store/redis.go`

- [ ] **Step 1: Wrap the client after construction**

In `NewRedisStore`, after `client := redis.NewClient(options)`:

```go
import (
    "github.com/redis/go-redis/extra/redisotel/v9"
)

if err := redisotel.InstrumentTracing(client); err != nil {
    return nil, fmt.Errorf("redis tracing: %w", err)
}
if err := redisotel.InstrumentMetrics(client); err != nil {
    return nil, fmt.Errorf("redis metrics: %w", err)
}
```

Add `"fmt"` to imports if not present.

- [ ] **Step 2: Verify build**

```bash
go build ./...
```

Expected: exit 0.

- [ ] **Step 3: Run store tests**

```bash
go test ./internal/store/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/store/redis.go
git commit -m "feat(backend): instrument redis client with redisotel"
```

---

### Task 10: Replace key log.Printf sites with slog through Logger helper

**Files:**
- Modify: `apps/backend/cmd/server/main.go`
- Modify: select 5–10 high-traffic `log.Printf` sites flagged by `grep -rn "log.Printf" apps/backend/internal/ | head -20`

The goal of this task is _not_ a full migration — it is to prove the slog→OTel path works end-to-end on the loudest sites. A bulk migration belongs in a follow-up cleanup PR.

- [ ] **Step 1: Pick targets**

```bash
grep -rn "log.Printf" apps/backend/internal/ | head -20
```

Pick 5–10 log lines that fire during normal request handling (HTTP handlers, BattleMetrics poller, outbound proxy auto-disable).

- [ ] **Step 2: Convert each**

For each target site:
- Add `"log/slog"` import if missing.
- Add `"rustlens/backend/internal/observability"` import if a request `ctx` is in scope.
- Replace `log.Printf("[backend] auto-disable proxy %q failed: %v", redactedEndpoint, err)` with:
  ```go
  observability.Logger(ctx).Error("auto disable proxy failed",
      slog.String("endpoint", redactedEndpoint),
      slog.Any("error", err))
  ```
- For sites with no `ctx` in scope (init code in `main.go`), use `slog.Error(...)` directly.

- [ ] **Step 3: Verify build + tests**

```bash
go build ./... && go test ./...
```

Expected: exit 0, all tests pass.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/cmd/server/main.go apps/backend/internal/
git commit -m "refactor(backend): convert high-traffic log sites to slog"
```

---

## Phase 3 — Backend domain metrics

### Task 11: Define shard service metrics

**Files:**
- Create: `apps/backend/internal/shards/metrics.go`
- Modify: `apps/backend/internal/shards/service.go` (and any helpers that mutate shard state)

- [ ] **Step 1: Create metrics.go**

```go
package shards

import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
)

var (
    meter            = otel.Meter("rustlens.shards")
    heartbeatAgeHist metric.Float64Histogram
    rebalanceCounter metric.Int64Counter
)

func init() {
    var err error
    if heartbeatAgeHist, err = meter.Float64Histogram(
        "rustlens.shard.heartbeat_age_seconds",
        metric.WithDescription("Age of the most recent shard heartbeat"),
        metric.WithUnit("s"),
    ); err != nil {
        panic(err)
    }
    if rebalanceCounter, err = meter.Int64Counter(
        "rustlens.shard.rebalance_total",
        metric.WithDescription("Number of shard rebalances performed"),
    ); err != nil {
        panic(err)
    }
}
```

- [ ] **Step 2: Record metrics from the service**

In `service.go`, find the rebalance code path and add at the end of a successful rebalance:

```go
rebalanceCounter.Add(ctx, 1)
```

For the heartbeat recording function:

```go
heartbeatAgeHist.Record(ctx, ageSeconds)
```

For the assigned-count gauge, use an `Int64ObservableGauge` driven by an existing snapshot method on the service. In `service.go`, ensure there is a method `(s *Service) AssignedCounts(ctx context.Context) (map[string]int64, error)` returning a map of `workerID → assigned shard count` (add it if missing — read from Redis the same way the existing rebalance code does). Then in `metrics.go` add an `init`-time observable callback:

```go
import (
    "context"
    "log/slog"
)

func RegisterObservableGauges(s *Service) {
    if _, err := meter.Int64ObservableGauge(
        "rustlens.shard.assigned_count",
        metric.WithDescription("Currently assigned shards per worker"),
        metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
            counts, err := s.AssignedCounts(ctx)
            if err != nil {
                slog.Warn("shard assigned-count callback failed", slog.Any("error", err))
                return nil
            }
            for worker, n := range counts {
                o.Observe(n, metric.WithAttributes(attribute.String("worker_id", worker)))
            }
            return nil
        }),
    ); err != nil {
        panic(err)
    }
}
```

In `cmd/server/main.go`, after `shardService := shards.NewService(...)`, call `shards.RegisterObservableGauges(shardService)` so the callback is wired against a live service. Remove the `shardAssignedGauge` `UpDownCounter` declared in the first version of `metrics.go` since the observable gauge supersedes it.

- [ ] **Step 3: Verify build + tests**

```bash
go build ./... && go test ./internal/shards/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/shards/
git commit -m "feat(backend): emit shard service metrics"
```

---

### Task 12: Define data-pipeline metrics (battlemetrics, events, fraud, ratelimit, outbound, wipe scheduler)

**Files:**
- Create: `apps/backend/internal/battlemetrics/metrics.go`
- Create: `apps/backend/internal/events/metrics.go`
- Create: `apps/backend/internal/fraud/metrics.go`
- Create: `apps/backend/internal/ratelimit/metrics.go`
- Create: `apps/backend/internal/outboundhttp/metrics.go`
- Create: `apps/backend/internal/wipereactscheduler/metrics.go`
- Modify: the corresponding source files in each package to call the metric APIs at the right places.

Follow the same pattern as Task 11 for each. Concrete instruments to register (one per file):

- `battlemetrics/metrics.go`: `pollDurationHist` (`rustlens.battlemetrics.poll_duration_seconds`, histogram, unit `s`), `pollErrorCounter` (`rustlens.battlemetrics.poll_error_total`, counter, attribute `reason`), `serversPolledGauge` (`rustlens.battlemetrics.servers_polled`, Int64ObservableGauge populated from a periodic callback).
- `events/metrics.go`: `publishedCounter` (`rustlens.events.published_total`, counter, attribute `channel`), `subscribersGauge` (`rustlens.events.subscribers`, Int64ObservableGauge).
- `fraud/metrics.go`: `signalCounter` (`rustlens.fraud.signal_total`, counter, attribute `signal_type`).
- `ratelimit/metrics.go`: `rejectionCounter` (`rustlens.ratelimit.rejection_total`, counter, attribute `bucket`).
- `outboundhttp/metrics.go`: `autoDisabledCounter` (`rustlens.outbound.auto_disabled_total`, counter, attribute `target`).
- `wipereactscheduler/metrics.go`: `runDurationHist` (`rustlens.wipe_scheduler.run_duration_seconds`, histogram, unit `s`), `runErrorCounter` (`rustlens.wipe_scheduler.run_error_total`, counter).

- [ ] **Step 1: Create each metrics.go**

For each file, follow the Task 11 template — `var meter = otel.Meter("rustlens.<package>")` plus an `init()` that builds each instrument and panics on error.

- [ ] **Step 2: Wire calls into the owning source files**

For each instrument, find the matching code path (`grep` for the function names below) and add the recording call:

- `battlemetrics.Poller.runOnce` (or whichever method drives polling) — measure duration with `time.Since` and record into `pollDurationHist`, increment `pollErrorCounter` with `attribute.String("reason", classify(err))` on failure.
- `events.Hub.Publish` — `publishedCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("channel", channel)))`.
- Fraud signal emitter — `signalCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("signal_type", t)))`.
- Rate-limit middleware reject branch — `rejectionCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("bucket", bucket)))`.
- `outboundhttp.Provider`'s auto-disable callback (already wired in `cmd/server/main.go`) — `autoDisabledCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("target", classify(endpoint))))`.
- `wipereactscheduler` run loop — duration around each scheduled run; counter on error.

- [ ] **Step 3: Verify build + tests**

```bash
go build ./... && go test ./...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/
git commit -m "feat(backend): emit data pipeline metrics"
```

---

### Task 13: End-to-end backend OTLP smoke test

**Files:**
- Create: `apps/backend/internal/observability/integration_test.go`

- [ ] **Step 1: Add integration test against a fake OTLP receiver**

```go
package observability_test

import (
    "context"
    "net"
    "testing"
    "time"

    coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
    "google.golang.org/grpc"

    "rustlens/backend/internal/config"
    "rustlens/backend/internal/observability"

    "go.opentelemetry.io/otel"
)

type fakeTraceServer struct {
    coltracepb.UnimplementedTraceServiceServer
    got chan struct{}
}

func (f *fakeTraceServer) Export(_ context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
    select {
    case f.got <- struct{}{}:
    default:
    }
    return &coltracepb.ExportTraceServiceResponse{}, nil
}

func TestInitEmitsSpansToReceiver(t *testing.T) {
    lis, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        t.Fatal(err)
    }
    server := grpc.NewServer()
    fake := &fakeTraceServer{got: make(chan struct{}, 1)}
    coltracepb.RegisterTraceServiceServer(server, fake)
    go server.Serve(lis)
    t.Cleanup(server.Stop)

    cfg := config.Config{
        ObservabilityEnabled: true,
        OTLPEndpoint:         "http://" + lis.Addr().String(),
        ServiceName:          "rustlens-backend-test",
        ServiceNamespace:     "rustlens",
        DeploymentEnv:        "test",
        TraceSampleRatio:     1.0,
    }
    shutdown, err := observability.Init(context.Background(), cfg)
    if err != nil {
        t.Fatalf("init: %v", err)
    }
    t.Cleanup(func() { _ = shutdown(context.Background()) })

    ctx, span := otel.Tracer("smoke").Start(context.Background(), "smoke-span")
    span.End()
    _ = ctx

    select {
    case <-fake.got:
        // success
    case <-time.After(15 * time.Second):
        t.Fatal("no span received within timeout")
    }
}
```

- [ ] **Step 2: Run it**

```bash
go test ./internal/observability/... -run TestInitEmitsSpansToReceiver -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/observability/integration_test.go
git commit -m "test(backend): otlp end-to-end smoke against fake receiver"
```

---

## Phase 4 — Bot instrumentation

### Task 14: Add OTel + Pyroscope Node dependencies

**Files:**
- Modify: `apps/bot/package.json`
- Modify: `apps/bot/pnpm-lock.yaml`

- [ ] **Step 1: Install**

Run from `apps/bot`:

```bash
pnpm add \
  @opentelemetry/api \
  @opentelemetry/sdk-node \
  @opentelemetry/auto-instrumentations-node \
  @opentelemetry/exporter-trace-otlp-grpc \
  @opentelemetry/exporter-metrics-otlp-grpc \
  @opentelemetry/exporter-logs-otlp-grpc \
  @opentelemetry/instrumentation-pino \
  @opentelemetry/resources \
  @opentelemetry/semantic-conventions \
  pino \
  @grafana/pyroscope-nodejs
```

- [ ] **Step 2: Verify build still passes**

```bash
pnpm build
```

Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add apps/bot/package.json pnpm-lock.yaml
git commit -m "feat(bot): add opentelemetry and pyroscope dependencies"
```

---

### Task 15: Create observability.ts and wire as first import

**Files:**
- Create: `apps/bot/src/observability.ts`
- Modify: `apps/bot/src/index.ts`

- [ ] **Step 1: Write observability.ts**

```ts
import { diag, DiagConsoleLogger, DiagLogLevel } from "@opentelemetry/api";
import { NodeSDK } from "@opentelemetry/sdk-node";
import { getNodeAutoInstrumentations } from "@opentelemetry/auto-instrumentations-node";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-grpc";
import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-grpc";
import { OTLPLogExporter } from "@opentelemetry/exporter-logs-otlp-grpc";
import { PeriodicExportingMetricReader } from "@opentelemetry/sdk-metrics";
import { BatchLogRecordProcessor } from "@opentelemetry/sdk-logs";
import { Resource } from "@opentelemetry/resources";
import { ATTR_SERVICE_NAME, ATTR_SERVICE_NAMESPACE } from "@opentelemetry/semantic-conventions";
import Pyroscope from "@grafana/pyroscope-nodejs";

const enabled = process.env.OBSERVABILITY_ENABLED === "true";
const endpoint = process.env.OTEL_EXPORTER_OTLP_ENDPOINT;

let sdk: NodeSDK | undefined;

export async function startObservability(): Promise<() => Promise<void>> {
    if (!enabled) {
        return async () => undefined;
    }
    if (!endpoint) {
        throw new Error("OBSERVABILITY_ENABLED=true but OTEL_EXPORTER_OTLP_ENDPOINT is empty");
    }

    diag.setLogger(new DiagConsoleLogger(), DiagLogLevel.WARN);

    const resource = new Resource({
        [ATTR_SERVICE_NAME]: process.env.OTEL_SERVICE_NAME ?? "rustlens-bot",
        [ATTR_SERVICE_NAMESPACE]: process.env.OTEL_SERVICE_NAMESPACE ?? "rustlens",
        "deployment.environment": process.env.DEPLOYMENT_ENV ?? "local",
        team: "rustlens",
    });

    sdk = new NodeSDK({
        resource,
        traceExporter: new OTLPTraceExporter({ url: endpoint }),
        metricReader: new PeriodicExportingMetricReader({
            exporter: new OTLPMetricExporter({ url: endpoint }),
            exportIntervalMillis: 30_000,
        }),
        logRecordProcessor: new BatchLogRecordProcessor(new OTLPLogExporter({ url: endpoint })),
        instrumentations: getNodeAutoInstrumentations({
            "@opentelemetry/instrumentation-fs": { enabled: false },
        }),
    });

    sdk.start();

    if (process.env.PYROSCOPE_SERVER_ADDRESS) {
        Pyroscope.init({
            appName: process.env.OTEL_SERVICE_NAME ?? "rustlens-bot",
            serverAddress: process.env.PYROSCOPE_SERVER_ADDRESS,
            tags: {
                environment: process.env.DEPLOYMENT_ENV ?? "local",
                namespace: process.env.OTEL_SERVICE_NAMESPACE ?? "rustlens",
            },
        });
        Pyroscope.start();
    }

    return async () => {
        if (sdk) {
            await sdk.shutdown();
        }
        if (process.env.PYROSCOPE_SERVER_ADDRESS) {
            await Pyroscope.stop();
        }
    };
}
```

- [ ] **Step 2: Make observability the very first import in src/index.ts**

The current `src/index.ts` begins with `import { BackendClient, ... } from "./backend-client.js";`. Add **above all other imports**:

```ts
import { startObservability } from "./observability.js";
```

And inside `async function main()`, immediately after `const config = loadConfig();`:

```ts
const stopObservability = await startObservability();
process.once("SIGTERM", () => void stopObservability());
process.once("SIGINT", () => void stopObservability());
```

- [ ] **Step 3: Verify build**

```bash
pnpm build
```

Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add apps/bot/src/observability.ts apps/bot/src/index.ts
git commit -m "feat(bot): wire opentelemetry sdk and pyroscope"
```

---

### Task 16: Migrate bot console.* to pino with OTel correlation

**Files:**
- Create: `apps/bot/src/logger.ts`
- Modify: `apps/bot/src/index.ts` and 5–10 high-traffic call sites

- [ ] **Step 1: Create logger.ts**

```ts
import pino from "pino";

export const logger = pino({
    level: process.env.LOG_LEVEL ?? "info",
    formatters: {
        level: (label) => ({ level: label }),
    },
    base: undefined,
});
```

- [ ] **Step 2: Replace targets**

```bash
grep -rn "console\\.\\(log\\|error\\|warn\\|info\\)" apps/bot/src | head -20
```

Pick the noisy ones. For each:
```ts
import { logger } from "./logger.js";
// before: console.log("[bot] shard ready", shardId);
logger.info({ shardId }, "shard ready");
```

- [ ] **Step 3: Verify build + tests**

```bash
pnpm build && pnpm test
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/bot/src/
git commit -m "refactor(bot): convert key log sites to pino"
```

---

### Task 17: Define bot domain metrics

**Files:**
- Create: `apps/bot/src/metrics.ts`
- Modify: command dispatcher + shard runtime + gateway event handler

- [ ] **Step 1: Create metrics.ts**

```ts
import { metrics } from "@opentelemetry/api";

const meter = metrics.getMeter("rustlens.bot");

export const gatewayEventCounter = meter.createCounter("rustlens.bot.gateway_event_total", {
    description: "Discord gateway events processed",
});
export const commandDurationHist = meter.createHistogram("rustlens.bot.command_duration_seconds", {
    description: "Slash command handler duration",
    unit: "s",
});
export const commandFailureCounter = meter.createCounter("rustlens.bot.command_failure_total", {
    description: "Slash command failures",
});
export const shardStateGauge = meter.createUpDownCounter("rustlens.bot.shard_state", {
    description: "Discord shard state, 1 ready / 0 disconnected",
});
export const interactionFailureCounter = meter.createCounter("rustlens.bot.interaction_failure_total", {
    description: "Discord interaction failures",
});
```

- [ ] **Step 2: Record from call sites**

For the command dispatcher (find with `grep -rn "interactionCreate\|onInteraction" apps/bot/src`):

```ts
import { commandDurationHist, commandFailureCounter, interactionFailureCounter } from "./metrics.js";

const start = process.hrtime.bigint();
try {
    await handler(interaction);
} catch (err) {
    commandFailureCounter.add(1, { command: interaction.commandName, reason: classify(err) });
    throw err;
} finally {
    const elapsed = Number(process.hrtime.bigint() - start) / 1e9;
    commandDurationHist.record(elapsed, { command: interaction.commandName });
}
```

For shard state changes (`shardReady`, `shardDisconnect` events on the client):

```ts
client.on("shardReady", (id) => shardStateGauge.add(+1, { shard_id: String(id) }));
client.on("shardDisconnect", (_, id) => shardStateGauge.add(-1, { shard_id: String(id) }));
```

For raw gateway events (where the bot's existing gateway listener lives):

```ts
gatewayEventCounter.add(1, { event: eventName });
```

For unhandled interaction failures (the catch block of the top-level interaction handler):

```ts
interactionFailureCounter.add(1, { interaction_type: interaction.type });
```

- [ ] **Step 3: Verify build + tests**

```bash
pnpm build && pnpm test
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/bot/src/
git commit -m "feat(bot): emit gateway, command, and shard metrics"
```

---

### Task 18: Bot observability smoke test

**Files:**
- Create: `apps/bot/src/observability.test.ts`

- [ ] **Step 1: Write the test**

```ts
import { describe, expect, it, vi, beforeEach } from "vitest";

describe("observability", () => {
    beforeEach(() => {
        vi.resetModules();
        delete process.env.OBSERVABILITY_ENABLED;
        delete process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
    });

    it("returns a no-op shutdown when disabled", async () => {
        const { startObservability } = await import("./observability.js");
        const stop = await startObservability();
        await expect(stop()).resolves.toBeUndefined();
    });

    it("throws when enabled without an endpoint", async () => {
        process.env.OBSERVABILITY_ENABLED = "true";
        const { startObservability } = await import("./observability.js");
        await expect(startObservability()).rejects.toThrow(/OTEL_EXPORTER_OTLP_ENDPOINT/);
    });
});
```

- [ ] **Step 2: Run it**

```bash
pnpm test --run observability
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/bot/src/observability.test.ts
git commit -m "test(bot): observability init guards"
```

---

## Phase 5 — Frontend instrumentation

### Task 19: Add Next.js OTel deps

**Files:**
- Modify: `apps/frontend/package.json`
- Modify: `pnpm-lock.yaml`

- [ ] **Step 1: Install**

Run from `apps/frontend`:

```bash
pnpm add \
  @vercel/otel \
  @opentelemetry/api \
  @opentelemetry/instrumentation-pino \
  @opentelemetry/resources \
  @opentelemetry/semantic-conventions \
  pino
```

- [ ] **Step 2: Verify build**

```bash
pnpm build
```

Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/package.json pnpm-lock.yaml
git commit -m "feat(frontend): add opentelemetry and pino dependencies"
```

---

### Task 20: Create instrumentation.ts + metrics.ts + logger.ts

**Files:**
- Create: `apps/frontend/instrumentation.ts`
- Create: `apps/frontend/lib/observability/metrics.ts`
- Create: `apps/frontend/lib/observability/logger.ts`

- [ ] **Step 1: instrumentation.ts**

```ts
import { registerOTel } from "@vercel/otel";

export function register() {
    if (process.env.OBSERVABILITY_ENABLED !== "true") {
        return;
    }
    if (!process.env.OTEL_EXPORTER_OTLP_ENDPOINT) {
        throw new Error("OBSERVABILITY_ENABLED=true but OTEL_EXPORTER_OTLP_ENDPOINT is empty");
    }
    registerOTel({
        serviceName: process.env.OTEL_SERVICE_NAME ?? "rustlens-frontend",
        attributes: {
            "service.namespace": process.env.OTEL_SERVICE_NAMESPACE ?? "rustlens",
            "deployment.environment": process.env.DEPLOYMENT_ENV ?? "local",
            team: "rustlens",
        },
    });
}
```

- [ ] **Step 2: metrics.ts**

```ts
import { metrics } from "@opentelemetry/api";

const meter = metrics.getMeter("rustlens.frontend");

export const ssrDurationHist = meter.createHistogram("rustlens.frontend.ssr_duration_seconds", {
    description: "Server-side render duration",
    unit: "s",
});
export const routeHitCounter = meter.createCounter("rustlens.frontend.route_hit_total", {
    description: "Route hits (server-side)",
});
export const backendFetchHist = meter.createHistogram("rustlens.frontend.backend_fetch_duration_seconds", {
    description: "Server-side backend fetch duration",
    unit: "s",
});
```

- [ ] **Step 3: logger.ts**

```ts
import pino from "pino";

export const logger = pino({
    level: process.env.LOG_LEVEL ?? "info",
    formatters: {
        level: (label) => ({ level: label }),
    },
    base: undefined,
});
```

- [ ] **Step 4: Wire metrics into a route handler as a proof point**

Pick one Route Handler (e.g., `apps/frontend/app/api/healthz/route.ts` — create if not present) and emit a `routeHitCounter.add(1, { route: "/api/healthz" })` plus `backendFetchHist.record(...)` if it proxies. Defer broader instrumentation to a follow-up. The point is to prove the metric path works.

- [ ] **Step 5: Verify build**

```bash
pnpm build
```

Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add apps/frontend/instrumentation.ts apps/frontend/lib/observability/ apps/frontend/app/
git commit -m "feat(frontend): wire @vercel/otel and define base metrics"
```

---

### Task 21: Frontend observability smoke test

**Files:**
- Create: `apps/frontend/lib/observability/instrumentation.test.ts`

- [ ] **Step 1: Write the test**

```ts
import { describe, expect, it, vi, beforeEach } from "vitest";

vi.mock("@vercel/otel", () => ({
    registerOTel: vi.fn(),
}));

describe("instrumentation.register", () => {
    beforeEach(() => {
        vi.resetModules();
        delete process.env.OBSERVABILITY_ENABLED;
        delete process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
    });

    it("no-ops when disabled", async () => {
        const otel = await import("@vercel/otel");
        const { register } = await import("../../instrumentation");
        register();
        expect(otel.registerOTel).not.toHaveBeenCalled();
    });

    it("throws when enabled without endpoint", async () => {
        process.env.OBSERVABILITY_ENABLED = "true";
        const { register } = await import("../../instrumentation");
        expect(() => register()).toThrow(/OTEL_EXPORTER_OTLP_ENDPOINT/);
    });

    it("registers when enabled with endpoint", async () => {
        process.env.OBSERVABILITY_ENABLED = "true";
        process.env.OTEL_EXPORTER_OTLP_ENDPOINT = "http://localhost:4317";
        const otel = await import("@vercel/otel");
        const { register } = await import("../../instrumentation");
        register();
        expect(otel.registerOTel).toHaveBeenCalledTimes(1);
    });
});
```

- [ ] **Step 2: Run it**

```bash
pnpm test --run instrumentation
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/lib/observability/instrumentation.test.ts
git commit -m "test(frontend): instrumentation register guards"
```

---

## Phase 6 — Push the RustLens PR

### Task 22: Push branch and open the PR

- [ ] **Step 1: Verify clean working tree and tests**

```bash
git status
go test ./apps/backend/...
(cd apps/bot && pnpm test)
(cd apps/frontend && pnpm test)
```

Expected: clean tree, all green.

- [ ] **Step 2: Push and open PR**

```bash
git push -u origin feature/observability
gh pr create --title "feat(observability): instrument backend, bot, and frontend with OpenTelemetry" --body "$(cat <<'EOF'
## Summary
- Backend (Go): OTel SDK init (traces/metrics/logs), chi router instrumentation, otelpgx, redisotel, otelhttp on outbound, Pyroscope, slog-to-OTel bridge, domain metrics for shards / battlemetrics / events / fraud / ratelimit / outboundhttp / wipescheduler.
- Bot (Node): `@opentelemetry/sdk-node` with auto-instrumentations, pino, Pyroscope, gateway / command / shard metrics.
- Frontend (Next.js): `@vercel/otel`, pino, base SSR / route / backend-fetch metrics.
- All three apps are dark by default (`OBSERVABILITY_ENABLED=false`). The v-deployments ConfigMap commit flips them on.

## Test plan
- [ ] `go test ./...` in apps/backend
- [ ] `pnpm test` in apps/bot
- [ ] `pnpm test` in apps/frontend
- [ ] After merge: v-deployments Commit A enables observability; verify in Grafana Explore that traces/metrics/logs flow for all three `service.name` values.
EOF
)"
```

- [ ] **Step 3: Wait for the PR to be merged before proceeding**

Once merged, the RustLens images that ArgoCD pulls will contain the dark-shipped instrumentation. Then proceed to Phase 7.

---

## Phase 7 — v-deployments rollout (direct commits to `main`)

These tasks operate in `B:\.dev\Vanguard\v-deployments`. Each commit is independently revertable.

### Task 23: Commit A — enable OTel env vars and pod labels

**Files (all in `B:\.dev\Vanguard\v-deployments`):**
- Modify: `applications/rustlens/overlays/backend/configmap.yaml`
- Modify: `applications/rustlens/overlays/bot/configmap.yaml`
- Modify: `applications/rustlens/overlays/frontend/configmap.yaml`
- Modify: `applications/rustlens/overlays/backend/deployment.yaml`
- Modify: `applications/rustlens/overlays/bot/deployment.yaml`
- Modify: `applications/rustlens/overlays/frontend/deployment.yaml`

- [ ] **Step 1: Add OTel env vars to each ConfigMap**

For each `configmap.yaml`, append to `data:` (substituting `<svc>` per file):

```yaml
OBSERVABILITY_ENABLED: "true"
OTEL_SERVICE_NAME: rustlens-<svc>
OTEL_SERVICE_NAMESPACE: rustlens
OTEL_RESOURCE_ATTRIBUTES: "deployment.environment=use1,team=rustlens"
OTEL_EXPORTER_OTLP_ENDPOINT: "http://alloy-receiver.alloy-receiver.svc.cluster.local:4317"
OTEL_EXPORTER_OTLP_PROTOCOL: grpc
OTEL_TRACES_SAMPLER: parentbased_traceidratio
OTEL_TRACES_SAMPLER_ARG: "0.1"
PYROSCOPE_SERVER_ADDRESS: "http://alloy-receiver.alloy-receiver.svc.cluster.local:4040"
DEPLOYMENT_ENV: use1
```

(Frontend may omit `PYROSCOPE_SERVER_ADDRESS` since it does not push profiles in v1.)

- [ ] **Step 2: Add pod labels to each Deployment**

In each `deployment.yaml`, under `spec.template.metadata.labels`, add alongside the existing `app: rustlens-<svc>`:

```yaml
app.kubernetes.io/name: rustlens-<svc>
app.kubernetes.io/part-of: rustlens
team: rustlens
```

- [ ] **Step 3: Validate kustomize**

```bash
cd B:\.dev\Vanguard\v-deployments
kustomize build applications/rustlens/overlays/backend > /dev/null
kustomize build applications/rustlens/overlays/bot > /dev/null
kustomize build applications/rustlens/overlays/frontend > /dev/null
```

Expected: exit 0 on each.

- [ ] **Step 4: Commit and push**

```bash
git add applications/rustlens/overlays/
git commit -m "chore(rustlens): enable observability env vars and pod labels"
git push origin main
```

- [ ] **Step 5: Wait for ArgoCD sync, then verify**

In Grafana:
- Explore → Tempo → query `{ resource.service.name = "rustlens-backend" }` → traces appear within 1–2 min.
- Explore → Loki → `{service_name="rustlens-backend"}` → logs appear.
- Explore → Mimir → `http_server_request_duration_seconds_count{service_name="rustlens-backend"}` → series appear.
- Explore → Pyroscope → application `rustlens-backend` → flamegraph appears.
- Repeat for `rustlens-bot` and `rustlens-frontend` (no Pyroscope for frontend).

If any signal is missing, **flip `OBSERVABILITY_ENABLED=false`** in a one-line revert commit, debug from the application logs (`kubectl -n rustlens-<svc> logs -l app=rustlens-<svc>`), and reapply.

---

### Task 24: Commit B — provision RustLens dashboard folder

**Files (all in `B:\.dev\Vanguard\v-deployments`):**
- Create: `applications/grafana/overlays/use1/vanguard/dashboards/files/rustlens/rustlens-overview.json`
- Create: `applications/grafana/overlays/use1/vanguard/dashboards/files/rustlens/rustlens-backend.json`
- Create: `applications/grafana/overlays/use1/vanguard/dashboards/files/rustlens/rustlens-bot.json`
- Create: `applications/grafana/overlays/use1/vanguard/dashboards/files/rustlens/rustlens-frontend.json`
- Create: `applications/grafana/overlays/use1/vanguard/dashboards/files/rustlens/rustlens-data-pipeline.json`
- Modify: `applications/grafana/overlays/use1/vanguard/dashboards/kustomization.yaml`

- [ ] **Step 1: Build the dashboards in Grafana UI, then export**

In Grafana (`grafana.vngenterprise.com`):
1. New dashboard → add panels per the spec (RED + USE + logs panel + trace link panel for each service).
2. Datasource UIDs to reference: `mimir`, `loki`, `tempo`, `pyroscope` (already provisioned in `grafana/values.yaml`).
3. For each panel that drills down by service, parameterize with a `service` variable defaulting to the service name.
4. Save dashboard.
5. Dashboard settings → JSON Model → copy to clipboard.
6. Paste into the corresponding `rustlens-<name>.json` file. **Important:** Strip the `id` and `uid` fields so Grafana assigns fresh ones on provision, and ensure the `datasource` blocks use `{ "type": "...", "uid": "mimir" }` form (not numeric IDs).

Build all five dashboards listed above, one at a time. Each should match the panel set described in the spec under "Dashboards".

- [ ] **Step 2: Add configMapGenerator entries**

In `applications/grafana/overlays/use1/vanguard/dashboards/kustomization.yaml`, append five entries following the existing pattern (one per dashboard):

```yaml
  - name: grafana-dashboard-rustlens-overview
    files:
      - files/rustlens/rustlens-overview.json
    options:
      annotations:
        grafana_dashboard_folder: RustLens
  - name: grafana-dashboard-rustlens-backend
    files:
      - files/rustlens/rustlens-backend.json
    options:
      annotations:
        grafana_dashboard_folder: RustLens
  - name: grafana-dashboard-rustlens-bot
    files:
      - files/rustlens/rustlens-bot.json
    options:
      annotations:
        grafana_dashboard_folder: RustLens
  - name: grafana-dashboard-rustlens-frontend
    files:
      - files/rustlens/rustlens-frontend.json
    options:
      annotations:
        grafana_dashboard_folder: RustLens
  - name: grafana-dashboard-rustlens-data-pipeline
    files:
      - files/rustlens/rustlens-data-pipeline.json
    options:
      annotations:
        grafana_dashboard_folder: RustLens
```

- [ ] **Step 3: Validate kustomize**

```bash
kustomize build applications/grafana/overlays/use1/vanguard > /dev/null
```

Expected: exit 0.

- [ ] **Step 4: Commit and push**

```bash
git add applications/grafana/overlays/use1/vanguard/dashboards/
git commit -m "feat(grafana): provision rustlens dashboard folder"
git push origin main
```

- [ ] **Step 5: Verify in Grafana after sync**

Open the new "RustLens" folder. Open each dashboard. Every panel should render with non-empty data. If a panel is empty, check the metric/log name against the actual emitted series in Mimir/Loki Explore, then update the JSON and re-commit.

---

### Task 25: Commit C — provision baseline alerts

**Files (all in `B:\.dev\Vanguard\v-deployments`):**
- Create: `applications/rustlens/overlays/backend/prometheusrule.yaml`
- Create: `applications/rustlens/overlays/bot/prometheusrule.yaml`
- Create: `applications/rustlens/overlays/frontend/prometheusrule.yaml`
- Modify: `applications/rustlens/overlays/backend/kustomization.yaml`
- Modify: `applications/rustlens/overlays/bot/kustomization.yaml`
- Modify: `applications/rustlens/overlays/frontend/kustomization.yaml`

- [ ] **Step 1: Write `applications/rustlens/overlays/backend/prometheusrule.yaml`**

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: rustlens-backend
  namespace: rustlens-backend
  labels:
    alloy_rules: pods
    team: rustlens
    service: rustlens-backend
spec:
  groups:
    - name: rustlens-backend.availability
      rules:
        - alert: RustLensBackendDown
          expr: kube_deployment_status_replicas_available{namespace="rustlens-backend",deployment="rustlens-backend"} == 0
          for: 5m
          labels:
            severity: critical
            team: rustlens
            service: rustlens-backend
          annotations:
            summary: "rustlens-backend has zero ready replicas"
        - alert: RustLensBackendDegraded
          expr: kube_deployment_status_replicas_available{namespace="rustlens-backend",deployment="rustlens-backend"} < kube_deployment_spec_replicas{namespace="rustlens-backend",deployment="rustlens-backend"}
          for: 15m
          labels:
            severity: warning
            team: rustlens
            service: rustlens-backend
          annotations:
            summary: "rustlens-backend has fewer ready replicas than desired"
        - alert: RustLensBackendCrashLooping
          expr: rate(kube_pod_container_status_restarts_total{namespace="rustlens-backend"}[15m]) > 0
          for: 15m
          labels:
            severity: warning
            team: rustlens
            service: rustlens-backend
          annotations:
            summary: "rustlens-backend pod is restarting repeatedly"

    - name: rustlens-backend.http
      rules:
        - alert: RustLensBackendHighErrorRate
          expr: |
            sum(rate(http_server_request_duration_seconds_count{service_name="rustlens-backend",http_response_status_code=~"5.."}[5m]))
            /
            sum(rate(http_server_request_duration_seconds_count{service_name="rustlens-backend"}[5m])) > 0.05
          for: 10m
          labels:
            severity: warning
            team: rustlens
            service: rustlens-backend
          annotations:
            summary: "rustlens-backend 5xx rate above 5% for 10m"
        - alert: RustLensBackendHighLatencyP99
          expr: |
            histogram_quantile(0.99,
              sum by (le) (rate(http_server_request_duration_seconds_bucket{service_name="rustlens-backend"}[5m]))
            ) > 1
          for: 10m
          labels:
            severity: warning
            team: rustlens
            service: rustlens-backend
          annotations:
            summary: "rustlens-backend p99 request latency > 1s for 10m"

    - name: rustlens-backend.domain
      rules:
        - alert: RustLensBackendDBPoolExhausted
          expr: |
            (sum(db_client_connections_usage{state="used",service_name="rustlens-backend"})
             / sum(db_client_connections_max{service_name="rustlens-backend"})) > 0.9
          for: 10m
          labels:
            severity: warning
            team: rustlens
            service: rustlens-backend
          annotations:
            summary: "rustlens-backend pg pool > 90% utilization for 10m"
        - alert: RustLensBackendBattleMetricsErrors
          expr: sum(rate(rustlens_battlemetrics_poll_error_total[10m])) > 0.5
          for: 15m
          labels:
            severity: warning
            team: rustlens
            service: rustlens-backend
          annotations:
            summary: "rustlens-backend battlemetrics poll error rate elevated"
        - alert: RustLensBackendShardAssignmentStalled
          expr: max(rustlens_shard_heartbeat_age_seconds) > 120
          for: 5m
          labels:
            severity: critical
            team: rustlens
            service: rustlens-backend
          annotations:
            summary: "rustlens-backend has a shard with stale heartbeat (>2m)"
```

- [ ] **Step 2: Write `applications/rustlens/overlays/bot/prometheusrule.yaml`**

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: rustlens-bot
  namespace: rustlens-bot
  labels:
    alloy_rules: pods
    team: rustlens
    service: rustlens-bot
spec:
  groups:
    - name: rustlens-bot.availability
      rules:
        - alert: RustLensBotDown
          expr: kube_deployment_status_replicas_available{namespace="rustlens-bot",deployment="rustlens-bot"} == 0
          for: 5m
          labels:
            severity: critical
            team: rustlens
            service: rustlens-bot
          annotations:
            summary: "rustlens-bot has zero ready replicas"
        - alert: RustLensBotDegraded
          expr: kube_deployment_status_replicas_available{namespace="rustlens-bot",deployment="rustlens-bot"} < kube_deployment_spec_replicas{namespace="rustlens-bot",deployment="rustlens-bot"}
          for: 15m
          labels:
            severity: warning
            team: rustlens
            service: rustlens-bot
          annotations:
            summary: "rustlens-bot has fewer ready replicas than desired"
        - alert: RustLensBotCrashLooping
          expr: rate(kube_pod_container_status_restarts_total{namespace="rustlens-bot"}[15m]) > 0
          for: 15m
          labels:
            severity: warning
            team: rustlens
            service: rustlens-bot
          annotations:
            summary: "rustlens-bot pod is restarting repeatedly"

    - name: rustlens-bot.domain
      rules:
        - alert: RustLensBotShardDisconnected
          expr: min(rustlens_bot_shard_state) == 0
          for: 5m
          labels:
            severity: critical
            team: rustlens
            service: rustlens-bot
          annotations:
            summary: "at least one rustlens-bot shard is disconnected"
        - alert: RustLensBotCommandFailureRate
          expr: |
            sum(rate(rustlens_bot_command_failure_total[10m]))
            /
            sum(rate(rustlens_bot_command_duration_seconds_count[10m])) > 0.1
          for: 15m
          labels:
            severity: warning
            team: rustlens
            service: rustlens-bot
          annotations:
            summary: "rustlens-bot command failure rate > 10%"
```

- [ ] **Step 3: Write `applications/rustlens/overlays/frontend/prometheusrule.yaml`**

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: rustlens-frontend
  namespace: rustlens-frontend
  labels:
    alloy_rules: pods
    team: rustlens
    service: rustlens-frontend
spec:
  groups:
    - name: rustlens-frontend.availability
      rules:
        - alert: RustLensFrontendDown
          expr: kube_deployment_status_replicas_available{namespace="rustlens-frontend",deployment="rustlens-frontend"} == 0
          for: 5m
          labels:
            severity: critical
            team: rustlens
            service: rustlens-frontend
          annotations:
            summary: "rustlens-frontend has zero ready replicas"
        - alert: RustLensFrontendDegraded
          expr: kube_deployment_status_replicas_available{namespace="rustlens-frontend",deployment="rustlens-frontend"} < kube_deployment_spec_replicas{namespace="rustlens-frontend",deployment="rustlens-frontend"}
          for: 15m
          labels:
            severity: warning
            team: rustlens
            service: rustlens-frontend
          annotations:
            summary: "rustlens-frontend has fewer ready replicas than desired"
        - alert: RustLensFrontendCrashLooping
          expr: rate(kube_pod_container_status_restarts_total{namespace="rustlens-frontend"}[15m]) > 0
          for: 15m
          labels:
            severity: warning
            team: rustlens
            service: rustlens-frontend
          annotations:
            summary: "rustlens-frontend pod is restarting repeatedly"
```

- [ ] **Step 4: Wire each into its overlay's kustomization.yaml**

For each of `applications/rustlens/overlays/{backend,bot,frontend}/kustomization.yaml`, add `prometheusrule.yaml` to the `resources:` list.

- [ ] **Step 5: Validate kustomize**

```bash
kustomize build applications/rustlens/overlays/backend > /dev/null
kustomize build applications/rustlens/overlays/bot > /dev/null
kustomize build applications/rustlens/overlays/frontend > /dev/null
```

Expected: exit 0 on each.

- [ ] **Step 6: Commit and push**

```bash
git add applications/rustlens/overlays/
git commit -m "feat(rustlens): provision baseline prometheus alert rules"
git push origin main
```

- [ ] **Step 7: Synthetic alert test**

```bash
kubectl scale deploy/rustlens-backend -n rustlens-backend --replicas=0
# wait 5-7 minutes
```

Expected: the `discord-rustlens` Discord channel receives a `RustLensBackendDown` alert. Then:

```bash
kubectl scale deploy/rustlens-backend -n rustlens-backend --replicas=2
```

Expected: the alert resolves within a minute or two.

---

## Done

After Task 25, the full LGTM(P) signal flow for RustLens is live:
- Metrics → Mimir, queryable in Grafana, charted in the RustLens dashboard folder.
- Traces → Tempo, correlated with metrics + logs via the existing datasource wiring.
- Logs → Loki, with `trace_id` / `span_id` attached for instrumented request flows.
- Profiles → Pyroscope for backend + bot.
- Alerts → Discord rustlens channel via the existing notification policy on the `team: rustlens` label.

Follow-ups intentionally deferred:
- Browser RUM (Grafana Faro).
- SLO + burn-rate alerts after a week of baseline data.
- Tail-based sampling at alloy-receiver if 10 % head sampling proves insufficient.
- Bulk migration of remaining `log.Printf` / `console.*` call sites.
