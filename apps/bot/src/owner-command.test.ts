import { describe, expect, it } from "vitest";
import type { OwnerStatsResponse } from "./backend-client.js";
import { buildOwnerStatsEmbed, parseOwnerProxyInput } from "./owner-command.js";

function baseStats(overrides: Partial<OwnerStatsResponse> = {}): OwnerStatsResponse {
  return {
    guildCount: 0,
    memberCount: 0,
    configuredGuilds: 0,
    steamAccountCount: 0,
    hardLinkCount: 0,
    uniqueLinkedUsers: 0,
    trackedServerCount: 0,
    onlineTrackedPlayers: 0,
    activeWipeReacts: 0,
    targetShards: 0,
    workers: [],
    ...overrides,
  };
}

function fieldValue(embed: ReturnType<typeof buildOwnerStatsEmbed>, name: string) {
  const fields = embed.toJSON().fields ?? [];
  return fields.find((field) => field.name === name)?.value;
}

describe("buildOwnerStatsEmbed", () => {
  it("formats counts with thousands separators", () => {
    const embed = buildOwnerStatsEmbed(baseStats({
      guildCount: 2018,
      memberCount: 98_092,
      steamAccountCount: 22_833,
    }));

    expect(fieldValue(embed, "📊 Servers")).toBe("2,018");
    expect(fieldValue(embed, "👥 Total Members")).toBe("98,092");
    expect(fieldValue(embed, "🔗 Linked Steam IDs")).toBe("22,833");
  });

  it("summarises shards as ready/total with average ping when all shards are ready", () => {
    const embed = buildOwnerStatsEmbed(baseStats({
      targetShards: 4,
      workers: [
        {
          workerId: "worker-1",
          uptimeSec: 7200,
          rssBytes: 200 * 1024 * 1024,
          shards: [
            { shardId: 0, status: "ready", pingMs: 30, guilds: 500, members: 25_000 },
            { shardId: 1, status: "ready", pingMs: 50, guilds: 500, members: 25_000 },
          ],
        },
        {
          workerId: "worker-2",
          uptimeSec: 7000,
          rssBytes: 200 * 1024 * 1024,
          shards: [
            { shardId: 2, status: "ready", pingMs: 40, guilds: 500, members: 25_000 },
            { shardId: 3, status: "ready", pingMs: 40, guilds: 500, members: 25_000 },
          ],
        },
      ],
    }));

    const workersField = fieldValue(embed, "Workers") ?? "";
    expect(workersField).toContain("Shards: 4/4 ready");
    expect(workersField).toContain("⌀ ping 40ms");
    expect(workersField).not.toContain("⚠");
  });

  it("flags degraded shards in the workers field", () => {
    const embed = buildOwnerStatsEmbed(baseStats({
      targetShards: 4,
      workers: [
        {
          workerId: "worker-1",
          uptimeSec: 60,
          rssBytes: 100 * 1024 * 1024,
          shards: [
            { shardId: 0, status: "ready", pingMs: 30, guilds: 100, members: 1000 },
            { shardId: 1, status: "ready", pingMs: 30, guilds: 100, members: 1000 },
            { shardId: 2, status: "disconnected", pingMs: 0, guilds: 0, members: 0 },
            { shardId: 3, status: "idle", pingMs: 0, guilds: 0, members: 0 },
          ],
        },
      ],
    }));

    const workersField = fieldValue(embed, "Workers") ?? "";
    expect(workersField).toContain("Shards: 2/4 ready");
    expect(workersField).toContain("⚠");
    expect(workersField).toContain("shard 2 disconnected");
    expect(workersField).toContain("shard 3 idle");
  });

  it("formats uptime in days and memory in MB/GB", () => {
    const embed = buildOwnerStatsEmbed(baseStats({
      targetShards: 1,
      workers: [
        {
          workerId: "w",
          uptimeSec: 2 * 86_400 + 4 * 3600,
          rssBytes: 412 * 1024 * 1024,
          shards: [{ shardId: 0, status: "ready", pingMs: 38, guilds: 1, members: 1 }],
        },
      ],
    }));

    const workersField = fieldValue(embed, "Workers") ?? "";
    expect(workersField).toContain("Uptime: 2d 4h");
    expect(workersField).toContain("Memory: 412 MB");
  });

  it("handles zero workers gracefully", () => {
    const embed = buildOwnerStatsEmbed(baseStats({ targetShards: 4 }));
    const workersField = fieldValue(embed, "Workers") ?? "";
    expect(workersField).toContain("Shards: 0/4 reporting");
    expect(workersField).toContain("Uptime: —");
    expect(workersField).toContain("Memory: 0 B");
  });
});

describe("parseOwnerProxyInput", () => {
  it("parses comma and whitespace separated proxy URLs", () => {
    expect(parseOwnerProxyInput(
      "http://username:password@45.127.250.10:5619,http://username:password@82.22.249.124:6961\nhttp://username:password@199.180.9.165:6185",
    )).toEqual([
      "http://username:password@45.127.250.10:5619",
      "http://username:password@82.22.249.124:6961",
      "http://username:password@199.180.9.165:6185",
    ]);
  });

  it("deduplicates proxy URLs", () => {
    expect(parseOwnerProxyInput("http://user:pass@1.2.3.4:8080, http://user:pass@1.2.3.4:8080")).toEqual([
      "http://user:pass@1.2.3.4:8080",
    ]);
  });
});
