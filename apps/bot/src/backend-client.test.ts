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

  it("patches guild settings", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        guildId: "123",
        wipeReactLogChannelId: "456",
        linkingLogChannelId: "",
        trackedServerLogChannelId: "",
        alertsChannelId: "",
        adminRoleId: "",
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.updateGuildSettings("worker-a", "123", { wipeReactLogChannelId: "456" });

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/guilds/123/settings");
    expect(init?.method).toBe("PUT");
    expect(init?.body).toBe(JSON.stringify({ wipeReactLogChannelId: "456" }));
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

  it("loads guild wipe react history", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        items: [{
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
        }],
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.listWipeReacts("worker-a", "123");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/guilds/123/wipe-reacts");
    expect(init?.method).toBe("GET");
  });

  it("posts tracked server creation", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({ bmId: "12345", name: "Vital EU Main" }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.createTrackedServer("worker-a", "12345", "Vital EU Main");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/tracked-servers");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(JSON.stringify({ bmId: "12345", name: "Vital EU Main" }));
  });

  it("loads tracked servers", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({ items: [{ bmId: "12345", name: "Vital EU Main" }] }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.listTrackedServers("worker-a");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/tracked-servers");
    expect(init?.method).toBe("GET");
  });

  it("replaces guild tracked servers", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        items: [
          { guildId: "123", bmId: "111", name: "Atlas EU 10x" },
          { guildId: "123", bmId: "222", name: "Vitality EU 10x" },
        ],
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.replaceGuildTrackedServers("worker-a", "123", ["111", "222"]);

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/guilds/123/tracked-servers");
    expect(init?.method).toBe("PUT");
    expect(init?.body).toBe(JSON.stringify({ bmIds: ["111", "222"] }));
  });

  it("loads guild online summary", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        items: [
          {
            guildId: "123",
            discordId: "456",
            steamId: "76561198000000000",
            playerBmId: "918543318",
            bmName: "turn it up",
            serverBmId: "16741517",
            serverName: "Atlas",
            onlineUnix: 1773019298,
          },
        ],
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.listGuildOnlineSummary("worker-a", "123");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/guilds/123/online-summary");
    expect(init?.method).toBe("GET");
  });

  it("posts guild steam-discord links", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        guildId: "123",
        discordId: "456",
        steamId: "76561198000000000",
        createdAt: "2026-04-01T10:00:00.000Z",
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.createGuildLink("worker-a", "123", "456", "76561198000000000");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/guilds/123/links");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(JSON.stringify({ discordId: "456", steamId: "76561198000000000" }));
  });

  it("deletes guild steam-discord links", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({ status: "deleted" }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.deleteGuildLink("worker-a", "123", "456", "76561198000000000");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/guilds/123/discord/456/steam/76561198000000000");
    expect(init?.method).toBe("DELETE");
  });

  it("posts steam id resolution requests", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({ steamId: "76561198000000000" }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.resolveSteamID("worker-a", "https://steamcommunity.com/id/rustlens");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/steam/resolve");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(JSON.stringify({ input: "https://steamcommunity.com/id/rustlens" }));
  });
});
