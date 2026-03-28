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

  it("posts the configured log channel id", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({ guildId: "123", logChannelId: "456" }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.setGuildLogChannel("worker-a", "123", "456");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/guilds/123/settings/log-channel");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(JSON.stringify({ logChannelId: "456" }));
  });
});
