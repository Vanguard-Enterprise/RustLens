import { metrics, type Counter, type Histogram, type UpDownCounter } from "@opentelemetry/api";

// Instruments must be created AFTER the OTel SDK starts and registers a
// real MeterProvider. Static module-scope creation produces no-op proxies
// that never delegate. Lazy init on first use guarantees the SDK is up.

let _gatewayEventCounter: Counter;
let _commandDurationHist: Histogram;
let _commandFailureCounter: Counter;
let _shardStateGauge: UpDownCounter;
let _interactionFailureCounter: Counter;

function meter() {
    return metrics.getMeter("rustlens.bot");
}

export function gatewayEventCounter() {
    return (_gatewayEventCounter ??= meter().createCounter("rustlens.bot.gateway_event_total", {
        description: "Discord gateway events processed",
    }));
}
export function commandDurationHist() {
    return (_commandDurationHist ??= meter().createHistogram("rustlens.bot.command_duration_seconds", {
        description: "Slash command handler duration",
        unit: "s",
    }));
}
export function commandFailureCounter() {
    return (_commandFailureCounter ??= meter().createCounter("rustlens.bot.command_failure_total", {
        description: "Slash command failures",
    }));
}
export function shardStateGauge() {
    return (_shardStateGauge ??= meter().createUpDownCounter("rustlens.bot.shard_state", {
        description: "Discord shard state, 1 ready / 0 disconnected",
    }));
}
export function interactionFailureCounter() {
    return (_interactionFailureCounter ??= meter().createCounter("rustlens.bot.interaction_failure_total", {
        description: "Discord interaction failures",
    }));
}
