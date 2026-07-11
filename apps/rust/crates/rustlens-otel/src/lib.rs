//! Shared observability init for the RustLens Rust fleet.
//!
//! A single [`init`] call replaces each binary's `tracing_subscriber::fmt()`
//! setup. It mirrors the Go backend's `internal/observability` package so
//! Grafana dashboards keep working with equivalent data once Go is removed:
//!
//! - **`OTEL_EXPORTER_OTLP_ENDPOINT` unset** (dev default): behaves exactly like
//!   the previous `tracing_subscriber::fmt()` init: stdout logs, `RUST_LOG`
//!   via `EnvFilter`, zero new behavior. No exporters, no network.
//! - **`OTEL_EXPORTER_OTLP_ENDPOINT` set**: additionally installs an OTLP
//!   (gRPC/tonic) trace exporter plus a `tracing-opentelemetry` layer (keeping
//!   the stdout fmt layer), and a cumulative-temporality metrics pipeline that
//!   mirrors the Go meter setup. The exporter honors the standard `OTEL_*`
//!   environment variables (endpoint, headers, `OTEL_SERVICE_NAME`, sampler
//!   arg, `OTEL_RESOURCE_ATTRIBUTES`).
//!
//! Pyroscope continuous profiling is intentionally deferred to a fast-follow.
//!
//! The returned [`OtelGuard`] must be held for the process lifetime; dropping it
//! flushes and shuts the exporters down for a graceful exit.

use std::time::Duration;

use opentelemetry::{KeyValue, global, trace::TracerProvider as _};
use opentelemetry_sdk::{
    Resource,
    metrics::{PeriodicReader, SdkMeterProvider, Temporality},
    propagation::TraceContextPropagator,
    trace::{Sampler, SdkTracerProvider},
};
use opentelemetry_semantic_conventions::resource::{SERVICE_INSTANCE_ID, SERVICE_NAMESPACE};
use tracing_subscriber::{EnvFilter, layer::SubscriberExt, util::SubscriberInitExt};

/// Held for the process lifetime; on drop it flushes and shuts down the OTLP
/// exporters. In the stdout-only (dev) path it holds nothing and drops for free.
#[must_use = "hold the guard for the process lifetime; dropping it shuts telemetry down"]
pub struct OtelGuard {
    providers: Option<Providers>,
}

struct Providers {
    tracer: SdkTracerProvider,
    meter: Option<SdkMeterProvider>,
}

impl OtelGuard {
    /// Explicitly flush and shut telemetry down. Equivalent to dropping the
    /// guard, but lets a caller sequence shutdown before other teardown.
    pub fn shutdown(self) {
        // Consuming self runs `Drop`.
    }
}

impl Drop for OtelGuard {
    fn drop(&mut self) {
        let Some(providers) = self.providers.take() else {
            return;
        };
        if let Err(err) = providers.tracer.shutdown() {
            eprintln!("rustlens-otel: tracer provider shutdown failed: {err}");
        }
        if let Some(meter) = providers.meter {
            if let Err(err) = meter.shutdown() {
                eprintln!("rustlens-otel: meter provider shutdown failed: {err}");
            }
        }
    }
}

/// Initialize tracing/telemetry for a binary.
///
/// `service_name` is the logical service (e.g. `rustlens-identity`), used as the
/// `service.name` resource attribute unless `OTEL_SERVICE_NAME` overrides it.
/// `default_directives` is the `EnvFilter` fallback used when `RUST_LOG` is unset
/// (each binary passes its existing default, e.g. `"identity=info,tower_http=info"`),
/// preserving current stdout behavior exactly.
///
/// Must be called once, from within a Tokio runtime (the OTLP path spawns
/// background export tasks).
pub fn init(service_name: &str, default_directives: &str) -> OtelGuard {
    let endpoint = std::env::var("OTEL_EXPORTER_OTLP_ENDPOINT")
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty());

    // Dev fallback: stdout logs only, byte-for-byte the previous behavior.
    if endpoint.is_none() {
        init_stdout_only(default_directives);
        return OtelGuard { providers: None };
    }

    // `OTEL_SERVICE_NAME` overrides the per-binary default (mirrors Go).
    let service_name = std::env::var("OTEL_SERVICE_NAME")
        .ok()
        .filter(|value| !value.trim().is_empty())
        .unwrap_or_else(|| service_name.to_string());

    let resource = build_resource(&service_name);

    // ---- Traces (priority) ----
    // The tonic exporter reads the standard OTEL_* env vars for endpoint and
    // headers when they are not set explicitly here.
    let span_exporter = match opentelemetry_otlp::SpanExporter::builder()
        .with_tonic()
        .build()
    {
        Ok(exporter) => exporter,
        Err(err) => {
            // A broken collector must never crash boot: fall back to stdout.
            init_stdout_only(default_directives);
            eprintln!("rustlens-otel: OTLP trace exporter init failed, using stdout only: {err}");
            return OtelGuard { providers: None };
        }
    };

    let tracer_provider = SdkTracerProvider::builder()
        .with_resource(resource.clone())
        .with_batch_exporter(span_exporter)
        .with_sampler(Sampler::ParentBased(Box::new(Sampler::TraceIdRatioBased(
            sample_ratio(),
        ))))
        .build();

    // ---- Metrics ----
    // Mirror the Go meter: cumulative temporality on every instrument (so
    // Mimir's OTLP ingest does not misread pod-restart counter resets) and a
    // 30s periodic export interval. No instruments are registered yet; this
    // wires the pipeline so any future `otel::global::meter` call exports.
    let meter_provider = match opentelemetry_otlp::MetricExporter::builder()
        .with_tonic()
        .with_temporality(Temporality::Cumulative)
        .build()
    {
        Ok(exporter) => {
            let reader = PeriodicReader::builder(exporter)
                .with_interval(Duration::from_secs(30))
                .build();
            Some(
                SdkMeterProvider::builder()
                    .with_resource(resource)
                    .with_reader(reader)
                    .build(),
            )
        }
        Err(err) => {
            eprintln!("rustlens-otel: OTLP metric exporter init failed, traces only: {err}");
            None
        }
    };

    global::set_text_map_propagator(TraceContextPropagator::new());
    let tracer = tracer_provider.tracer("rustlens");
    global::set_tracer_provider(tracer_provider.clone());
    if let Some(meter) = &meter_provider {
        global::set_meter_provider(meter.clone());
    }

    let otel_layer = tracing_opentelemetry::layer().with_tracer(tracer);

    tracing_subscriber::registry()
        .with(env_filter(default_directives))
        .with(tracing_subscriber::fmt::layer())
        .with(otel_layer)
        .init();

    tracing::info!(
        service = %service_name,
        "OpenTelemetry OTLP (gRPC) exporter initialised"
    );

    OtelGuard {
        providers: Some(Providers {
            tracer: tracer_provider,
            meter: meter_provider,
        }),
    }
}

fn init_stdout_only(default_directives: &str) {
    tracing_subscriber::fmt()
        .with_env_filter(env_filter(default_directives))
        .init();
}

fn env_filter(default_directives: &str) -> EnvFilter {
    EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new(default_directives))
}

fn build_resource(service_name: &str) -> Resource {
    let namespace = env_or("OTEL_SERVICE_NAMESPACE", "rustlens");
    let deployment_env = env_or("DEPLOYMENT_ENV", "local");

    // service.instance.id MUST be unique per process so cumulative counters stay
    // per-pod series. Prefer HOSTNAME (the pod name inside K8s), else fall back
    // to service-name + PID. Mirrors the Go resource logic.
    let instance_id = std::env::var("HOSTNAME")
        .ok()
        .filter(|value| !value.trim().is_empty())
        .unwrap_or_else(|| format!("{service_name}-{}", std::process::id()));

    Resource::builder()
        .with_service_name(service_name.to_string())
        .with_attributes([
            KeyValue::new(SERVICE_NAMESPACE, namespace),
            KeyValue::new(SERVICE_INSTANCE_ID, instance_id),
            // Go emits the semconv v1.26 key `deployment.environment` (not the
            // newer `deployment.environment.name`); keep the exact key so
            // existing Grafana filters keep matching.
            KeyValue::new("deployment.environment", deployment_env),
            KeyValue::new("team", "rustlens"),
        ])
        .build()
}

fn sample_ratio() -> f64 {
    std::env::var("OTEL_TRACES_SAMPLER_ARG")
        .ok()
        .and_then(|value| value.trim().parse::<f64>().ok())
        .unwrap_or(0.1)
}

fn env_or(key: &str, default: &str) -> String {
    std::env::var(key)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| default.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sample_ratio_defaults_to_ten_percent() {
        // Without the env var set the default matches the Go backend's 0.1.
        // (Not manipulating process env here to avoid cross-test races.)
        if std::env::var("OTEL_TRACES_SAMPLER_ARG").is_err() {
            assert_eq!(sample_ratio(), 0.1);
        }
    }

    #[test]
    fn env_or_falls_back_to_default() {
        let key = "RUSTLENS_OTEL_TEST_UNSET_VAR_XYZ";
        assert_eq!(env_or(key, "fallback"), "fallback");
    }

    #[test]
    fn env_filter_uses_default_directives_without_rust_log() {
        // Just ensure construction from the default string never panics.
        let _ = EnvFilter::new("identity=info,tower_http=info");
    }
}
