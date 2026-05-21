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
