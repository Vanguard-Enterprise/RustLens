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

  it("posts wipe react reaction updates", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        changed: true,
        wipeReact: {
          guildId: "123",
          channelId: "456",
          messageId: "789",
          creatorId: "111",
          name: "Friday",
          description: "Main group",
          timeZone: "Europe/London",
          scheduledUnix: 1774557951,
          askLateReason: true,
          askDeclinedReason: false,
          acceptedUserIds: ["222"],
          declinedUserIds: [],
          lateUserIds: [],
          embedTimestamp: "2026-03-02T20:44:18.917Z",
        },
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.setWipeReactReaction("worker-a", "123", "789", "222", "accepted");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/guilds/123/wipe-reacts/789/reactions");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(JSON.stringify({ userId: "222", type: "accepted" }));
  });
});
