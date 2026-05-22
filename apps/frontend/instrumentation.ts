import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-http";
import { AggregationTemporality, PeriodicExportingMetricReader } from "@opentelemetry/sdk-metrics";
import { registerOTel } from "@vercel/otel";

export function register() {
    if (process.env.OBSERVABILITY_ENABLED !== "true") {
        return;
    }
    const endpoint = process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
    if (!endpoint) {
        throw new Error("OBSERVABILITY_ENABLED=true but OTEL_EXPORTER_OTLP_ENDPOINT is empty");
    }

    // @vercel/otel only configures the trace exporter by default. Wire an
    // explicit OTLP/HTTP metric reader so SSR duration, route hit counters,
    // and backend fetch latency reach Mimir.
    // Pin every instrument kind to cumulative temporality. The SDK's default
    // selector returns delta for some kinds; Mimir's OTLP endpoint then
    // mis-handles counter-reset semantics and inflates rate()/increase()
    // dramatically.
    const metricReader = new PeriodicExportingMetricReader({
        exporter: new OTLPMetricExporter({
            url: `${endpoint.replace(/\/$/, "")}/v1/metrics`,
            temporalityPreference: AggregationTemporality.CUMULATIVE,
        }),
        exportIntervalMillis: 30_000,
    });

    // service.instance.id MUST be unique per process for Mimir to keep each
    // pod's cumulative counters as separate series; without it pod restarts
    // look like counter resets and rate() inflates dramatically. Use the
    // HOSTNAME env var (set to the pod name by K8s) instead of node:os or
    // process.pid; instrumentation.ts can be evaluated in the edge runtime.
    const serviceInstanceId =
        process.env.HOSTNAME ??
        process.env.VERCEL_DEPLOYMENT_ID ??
        process.env.OTEL_SERVICE_NAME ??
        "rustlens-frontend";

    registerOTel({
        serviceName: process.env.OTEL_SERVICE_NAME ?? "rustlens-frontend",
        attributes: {
            "service.namespace": process.env.OTEL_SERVICE_NAMESPACE ?? "rustlens",
            "service.instance.id": serviceInstanceId,
            "deployment.environment": process.env.DEPLOYMENT_ENV ?? "local",
            team: "rustlens",
        },
        metricReaders: [metricReader],
    });
}
