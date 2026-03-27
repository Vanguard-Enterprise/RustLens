import { beforeEach, describe, expect, it, vi } from "vitest";
import { BackendClient } from "./backend-client.js";

describe("BackendClient", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("sends auth header and worker payload", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({ workerId: "worker-a", targetShards: 2, heartbeatTtlSeconds: 30 }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.register("worker-a");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [, init] = fetchSpy.mock.calls[0];
    expect(init?.method).toBe("POST");
    expect((init?.headers as Record<string, string>).authorization).toContain("Bearer ");
  });
});