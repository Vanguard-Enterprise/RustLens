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
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
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
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(endpoint),
		// Force cumulative temporality on every instrument kind. Without this,
		// the SDK's default selector returns delta for UpDownCounters/observable
		// counters, which Mimir's OTLP /otlp endpoint converts back to
		// cumulative in a way that inflates rate()/increase() by orders of
		// magnitude on counter resets — every export looks like a reset.
		otlpmetricgrpc.WithTemporalitySelector(func(sdkmetric.InstrumentKind) metricdata.Temporality {
			return metricdata.CumulativeTemporality
		}),
	}
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
	// Set the global logger provider via the wrapper so that the
	// providerInitialised flag is also set and Logger(ctx) returns the
	// OTel bridge instead of slog.Default().
	SetGlobalLoggerProvider(lp)
	return lp.Shutdown, nil
}

func initProfiler(cfg config.Config) (Shutdown, error) {
	if cfg.PyroscopeAddress == "" {
		return noopShutdown, nil
	}
	// pyroscope-go requires a non-empty ApplicationName; if missing it errors
	// "new session: service name is required" and the whole observability
	// init fails, crashlooping the pod. Pass ServiceName here and DO NOT also
	// set service_name in Tags (that's what caused the duplicate-label
	// rejection #124 was responding to).
	p, err := pyroscope.Start(pyroscope.Config{
		ApplicationName: cfg.ServiceName,
		ServerAddress:   cfg.PyroscopeAddress,
		Tags: map[string]string{
			"environment": cfg.DeploymentEnv,
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
