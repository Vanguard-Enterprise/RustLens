import type { Client } from "discord.js";

export type ShardStatSnapshot = {
  shardId: number;
  status: "ready" | "disconnected" | "idle";
  pingMs: number;
  guilds: number;
  members: number;
};

export type WorkerStatsMessage = {
  type: "shard-stats";
  shardId: number;
  uptimeSec: number;
  rssBytes: number;
  shard: ShardStatSnapshot;
};

const READY_STATUS = 0; // discord.js Status.Ready

function classifyStatus(rawStatus: number | undefined): ShardStatSnapshot["status"] {
  if (rawStatus === undefined) {
    return "idle";
  }
  if (rawStatus === READY_STATUS) {
    return "ready";
  }
  return "disconnected";
}

export function buildShardStatSnapshot(client: Client, shardId: number): ShardStatSnapshot {
  const wsShard = client.ws.shards.get(shardId);
  let members = 0;
  for (const guild of client.guilds.cache.values()) {
    members += guild.memberCount;
  }

  return {
    shardId,
    status: classifyStatus(wsShard?.status as number | undefined),
    pingMs: Math.max(0, Math.round(wsShard?.ping ?? 0)),
    guilds: client.guilds.cache.size,
    members,
  };
}

export function buildWorkerStatsMessage(client: Client, shardId: number): WorkerStatsMessage {
  return {
    type: "shard-stats",
    shardId,
    uptimeSec: Math.round(process.uptime()),
    rssBytes: process.memoryUsage().rss,
    shard: buildShardStatSnapshot(client, shardId),
  };
}

// startShardStatsPusher kicks off a periodic process.send to the parent
// orchestrator. No-op when the worker has no IPC channel (e.g. running standalone).
export function startShardStatsPusher(client: Client, shardId: number, intervalMs = 10_000): NodeJS.Timeout | undefined {
  if (typeof process.send !== "function") {
    return undefined;
  }

  const push = () => {
    try {
      process.send?.(buildWorkerStatsMessage(client, shardId));
    } catch (error) {
      console.error("[bot] failed to push shard stats to parent", error);
    }
  };

  push();
  const timer = setInterval(push, intervalMs);
  // unref so this timer never holds the event loop open during shutdown.
  timer.unref();
  return timer;
}
