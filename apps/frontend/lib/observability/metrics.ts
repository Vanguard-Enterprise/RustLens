import { metrics, type Counter, type Histogram } from "@opentelemetry/api";

// Instruments must be created AFTER the OTel SDK starts (via register() in
// instrumentation.ts). Module-scope creation runs before the MeterProvider
// is registered and produces permanent no-op instruments. Lazy init on
// first use guarantees the SDK is up.

let _ssrDurationHist: Histogram;
let _routeHitCounter: Counter;
let _backendFetchHist: Histogram;

function meter() {
    return metrics.getMeter("rustlens.frontend");
}

export function ssrDurationHist() {
    return (_ssrDurationHist ??= meter().createHistogram("rustlens.frontend.ssr_duration_seconds", {
        description: "Server-side render duration",
        unit: "s",
    }));
}
export function routeHitCounter() {
    return (_routeHitCounter ??= meter().createCounter("rustlens.frontend.route_hit_total", {
        description: "Route hits (server-side)",
    }));
}
export function backendFetchHist() {
    return (_backendFetchHist ??= meter().createHistogram("rustlens.frontend.backend_fetch_duration_seconds", {
        description: "Server-side backend fetch duration",
        unit: "s",
    }));
}
