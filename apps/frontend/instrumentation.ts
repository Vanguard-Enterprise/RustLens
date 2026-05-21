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
