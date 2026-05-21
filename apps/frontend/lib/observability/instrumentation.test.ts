import { describe, expect, it, vi, beforeEach } from "vitest";

vi.mock("@vercel/otel", () => ({
    registerOTel: vi.fn(),
}));

describe("instrumentation.register", () => {
    beforeEach(() => {
        vi.resetModules();
        delete process.env.OBSERVABILITY_ENABLED;
        delete process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
    });

    it("no-ops when disabled", async () => {
        const otel = await import("@vercel/otel");
        const { register } = await import("../../instrumentation");
        register();
        expect(otel.registerOTel).not.toHaveBeenCalled();
    });

    it("throws when enabled without endpoint", async () => {
        process.env.OBSERVABILITY_ENABLED = "true";
        const { register } = await import("../../instrumentation");
        expect(() => register()).toThrow(/OTEL_EXPORTER_OTLP_ENDPOINT/);
    });

    it("registers when enabled with endpoint", async () => {
        process.env.OBSERVABILITY_ENABLED = "true";
        process.env.OTEL_EXPORTER_OTLP_ENDPOINT = "http://localhost:4317";
        const otel = await import("@vercel/otel");
        const { register } = await import("../../instrumentation");
        register();
        expect(otel.registerOTel).toHaveBeenCalledTimes(1);
    });
});
