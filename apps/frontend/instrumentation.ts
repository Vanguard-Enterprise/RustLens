import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-http";
import { PeriodicExportingMetricReader } from "@opentelemetry/sdk-metrics";
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
    const metricReader = new PeriodicExportingMetricReader({
        exporter: new OTLPMetricExporter({ url: `${endpoint.replace(/\/$/, "")}/v1/metrics` }),
        exportIntervalMillis: 30_000,
    });

    registerOTel({
        serviceName: process.env.OTEL_SERVICE_NAME ?? "rustlens-frontend",
        attributes: {
            "service.namespace": process.env.OTEL_SERVICE_NAMESPACE ?? "rustlens",
            "deployment.environment": process.env.DEPLOYMENT_ENV ?? "local",
            team: "rustlens",
        },
        metricReaders: [metricReader],
    });
}
