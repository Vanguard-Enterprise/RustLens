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
        trackedUsersCategoryId: "",
        adminRoleId: "",
        alertRoleId: "",
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

  it("loads steam account count", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({ count: 42 }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.steamAccountCount("worker-a");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/steam-accounts/count");
    expect(init?.method).toBe("GET");
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
      json: async () => ({
        bmId: "12345",
        name: "Vital EU Main",
        isVitality: true,
        isAtlas: false,
        isVital: false,
        isModded: true,
        isOfficial: false,
        isBuild: false,
        highFrequency: true,
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.createTrackedServer("worker-a", {
      bmId: "12345",
      name: "Vital EU Main",
      isVitality: true,
      isAtlas: false,
      isVital: false,
      isModded: true,
      isOfficial: false,
      isBuild: false,
      highFrequency: true,
    });

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/tracked-servers");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(JSON.stringify({
      bmId: "12345",
      name: "Vital EU Main",
      isVitality: true,
      isAtlas: false,
      isVital: false,
      isModded: true,
      isOfficial: false,
      isBuild: false,
      highFrequency: true,
    }));
  });

  it("loads steam profile details", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        steamId: "76561198000000000",
        bmId: "918543318",
        bmName: "turn it up",
        avatarImage: "https://cdn.example/avatar.png",
        firstSeenUnix: 1766616480,
        hours: {
          vitality: 10,
          atlas: 20,
          vital: 30,
          modded: 40,
          official: 50,
          build: 60,
          bm: 210,
          rust: 7347,
        },
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.getSteamProfile("worker-a", "76561198000000000");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/steam/76561198000000000/profile");
    expect(init?.method).toBe("GET");
  });

  it("loads tracked servers", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        items: [{
          bmId: "12345",
          name: "Vital EU Main",
            isVitality: true,
          isAtlas: false,
          isVital: false,
          isModded: true,
          isOfficial: false,
          isBuild: false,
          highFrequency: true,
          lastPolledAt: null,
          lastPollError: "",
        }],
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.listTrackedServers("worker-a");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/tracked-servers");
    expect(init?.method).toBe("GET");
  });

  it("updates tracked servers", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        bmId: "98765",
        name: "Atlas EU Main",
        isVitality: false,
        isAtlas: true,
        isVital: false,
        isModded: true,
        isOfficial: false,
        isBuild: false,
        highFrequency: false,
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.updateTrackedServer("worker-a", "12345", {
      bmId: "98765",
      name: "Atlas EU Main",
      isVitality: false,
      isAtlas: true,
      isVital: false,
      isModded: true,
      isOfficial: false,
      isBuild: false,
      highFrequency: false,
    });

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/tracked-servers/12345");
    expect(init?.method).toBe("PUT");
    expect(init?.body).toBe(JSON.stringify({
      bmId: "98765",
      name: "Atlas EU Main",
      isVitality: false,
      isAtlas: true,
      isVital: false,
      isModded: true,
      isOfficial: false,
      isBuild: false,
      highFrequency: false,
    }));
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

  it("fetches setsteam candidates by discord id", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        items: [
          {
            steamId: "76561198000000000",
            type: "hard",
            displayName: "Sem",
            createdAt: "2026-04-01T10:00:00.000Z",
          },
        ],
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    const result = await client.listSetSteamCandidates("worker-a", "456");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/discord/456/setsteam-candidates");
    expect(init?.method).toBe("GET");
    expect(result.items[0].displayName).toBe("Sem");
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

  it("posts user guild links", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        discordId: "456",
        guildId: "123",
        isActive: true,
        firstSeenAt: "2026-04-01T10:00:00.000Z",
        lastSeenAt: "2026-04-01T10:00:00.000Z",
        leftAt: null,
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.createUserGuildLink("worker-a", "456", "123");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/user-guilds");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(JSON.stringify({ discordId: "456", guildId: "123" }));
  });

  it("marks user guild links inactive", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        discordId: "456",
        guildId: "123",
        isActive: false,
        firstSeenAt: "2026-04-01T10:00:00.000Z",
        lastSeenAt: "2026-04-02T10:00:00.000Z",
        leftAt: "2026-04-02T10:00:00.000Z",
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.markUserGuildLeft("worker-a", "456", "123");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/user-guilds/leave");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(JSON.stringify({ discordId: "456", guildId: "123" }));
  });

  it("loads user guild links by discord id", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        items: [{
          discordId: "456",
          guildId: "123",
          isActive: true,
          firstSeenAt: "2026-04-01T10:00:00.000Z",
          lastSeenAt: "2026-04-01T10:00:00.000Z",
          leftAt: null,
        }],
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.listUserGuildLinksByDiscord("worker-a", "456");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/discord/456/guilds");
    expect(init?.method).toBe("GET");
  });

  it("loads guild nickname targets", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({
        items: [{
          guildId: "123",
          discordId: "456",
          steamId: "76561198000000000",
          bmId: "918543318",
          bmName: "turn it up",
        }],
      }),
    } as Response;

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.listGuildNicknameTargets("worker-a", "123");

    expect(fetchSpy).toHaveBeenCalledOnce();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/guilds/123/nickname-targets");
    expect(init?.method).toBe("GET");
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

  it("upserts a bot guild with members and sync flag", async () => {
    const mockResponse = {
      ok: true,
      json: async () => ({ status: "ok", membersSyncedAt: "2026-04-27T12:00:00.000Z" }),
    } as Response;
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.upsertBotGuild("worker-a", {
      guildId: "1",
      name: "G",
      iconHash: null,
      bannerHash: null,
      ownerDiscordId: null,
      ownerUsername: null,
      ownerGlobalName: null,
      ownerAvatarHash: null,
      approximateMemberCount: null,
      members: ["10", "11"],
      membersSyncComplete: true,
    });

    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/bot-guilds/upsert");
    expect(init?.method).toBe("POST");
    const body = JSON.parse(init?.body as string);
    expect(body.guildId).toBe("1");
    expect(body.members).toEqual(["10", "11"]);
    expect(body.membersSyncComplete).toBe(true);
  });

  it("marks a bot guild left", async () => {
    const mockResponse = { ok: true, json: async () => ({ status: "ok" }) } as Response;
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.markBotGuildLeft("worker-a", "1");

    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/bot-guilds/leave");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({ guildId: "1" });
  });

  it("reconciles bot guilds with shard info", async () => {
    const mockResponse = { ok: true, json: async () => ({ status: "ok" }) } as Response;
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.reconcileBotGuilds("worker-a", { shardId: 1, shardCount: 4, guildIds: ["1", "2"] });

    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/internal/bot/setup/bot-guilds/reconcile");
    expect(JSON.parse(init?.body as string)).toEqual({ shardId: 1, shardCount: 4, guildIds: ["1", "2"] });
  });

  it("lists guild recent game bans with timeframe and scope", async () => {
    const mockResponse = { ok: true, json: async () => ({ items: [] }) } as Response;
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

    const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
    await client.listGuildRecentGameBans("worker-a", "1478118029018792076", "deep", "180d", ["10", "11"]);

    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe(
      "http://localhost:8080/api/internal/bot/setup/guilds/1478118029018792076/game-bans?method=deep&timeframe=180d&scopeDiscordId=10&scopeDiscordId=11",
    );
    expect(init?.method).toBe("GET");
  });
});
