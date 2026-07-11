import { describe, expect, it, vi, beforeEach } from "vitest";

describe("observability", () => {
    beforeEach(() => {
        vi.resetModules();
        delete process.env.OBSERVABILITY_ENABLED;
        delete process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
    });

    // The first dynamic import of ./observability.js pulls in the (heavy) OTel
    // SDK; under CI-runner load that cold import alone can exceed the default 5s
    // test timeout, which flaked this suite. Give these two a generous ceiling —
    // the assertions themselves are fast, so this only accommodates the cold load.
    it("returns a no-op shutdown when disabled", async () => {
        const { startObservability } = await import("./observability.js");
        const stop = await startObservability();
        await expect(stop()).resolves.toBeUndefined();
    }, 20_000);

    it("throws when enabled without an endpoint", async () => {
        process.env.OBSERVABILITY_ENABLED = "true";
        const { startObservability } = await import("./observability.js");
        await expect(startObservability()).rejects.toThrow(/OTEL_EXPORTER_OTLP_ENDPOINT/);
    }, 20_000);
});
