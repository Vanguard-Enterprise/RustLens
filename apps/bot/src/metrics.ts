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
