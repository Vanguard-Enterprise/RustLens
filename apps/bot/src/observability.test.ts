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
