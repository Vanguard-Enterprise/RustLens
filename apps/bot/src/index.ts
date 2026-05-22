import { startObservability } from "./observability.js";
import { BackendClient, type ShardStatSnapshot, type WorkerStatSnapshot } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { ShardRuntime } from "./shard-runtime.js";
import { computeShardDelta } from "./shards.js";
import type { WorkerStatsMessage } from "./shard-stats.js";
import { logger } from "./logger.js";

function isWorkerStatsMessage(value: unknown): value is WorkerStatsMessage {
  return (
    typeof value === "object"
    && value !== null
    && (value as { type?: unknown }).type === "shard-stats"
  );
}

async function main() {
  const config = loadConfig();
  const stopObservability = await startObservability();
  process.once("SIGTERM", () => void stopObservability());
  process.once("SIGINT", () => void stopObservability());

  if (!config.discordToken) {
    throw new Error("DISCORD_TOKEN is required");
  }

  const backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
  const runtime = new ShardRuntime(config.discordToken, config.workerId);

  const shardSnapshots = new Map<number, ShardStatSnapshot>();
  const workerProcessSnapshot = { uptimeSec: 0, rssBytes: 0 };

  runtime.onChildMessage((shardID, message) => {
    if (!isWorkerStatsMessage(message)) {
      return;
    }
    shardSnapshots.set(shardID, message.shard);
    // Track the most recent worker-process metrics from any shard. Each shard
    // child reports its own process uptime/rss; on a multi-shard worker these
    // are nearly identical so picking the latest is fine.
    workerProcessSnapshot.uptimeSec = message.uptimeSec;
    workerProcessSnapshot.rssBytes = message.rssBytes;
  });

  const register = await backend.register(config.workerId);
  let currentTargetShards = register.targetShards;

  logger.info({ workerId: config.workerId, targetShards: register.targetShards }, "registered worker");

  const reconcile = async () => {
    const assignment = await backend.assignments(config.workerId);
    currentTargetShards = assignment.targetShards;

    const current = runtime.activeShards();
    const delta = computeShardDelta(current, assignment.shardIds);

    for (const shardID of delta.toStart) {
      runtime.start(shardID, currentTargetShards);
    }

    for (const shardID of delta.toStop) {
      runtime.stop(shardID);
      shardSnapshots.delete(shardID);
    }
  };

  const buildStatsPayload = (): WorkerStatSnapshot | undefined => {
    if (shardSnapshots.size === 0) {
      return undefined;
    }
    const shards = [...shardSnapshots.values()].sort((a, b) => a.shardId - b.shardId);
    return {
      uptimeSec: workerProcessSnapshot.uptimeSec,
      rssBytes: workerProcessSnapshot.rssBytes,
      shards,
    };
  };

  const heartbeat = async () => {
    await backend.heartbeat(config.workerId, runtime.activeShards(), buildStatsPayload());
  };

  await reconcile();
  await heartbeat();

  // If we can't reach the backend for long enough that our lease would have
  // expired, the backend may have reassigned our shards to another worker.
  // Continuing to run them would produce duplicate Discord gateway sessions.
  // Shed them defensively after this many consecutive failures.
  const reconcileFailuresBeforeRelease = Math.max(
    2,
    Math.ceil((register.heartbeatTtlSeconds * 1000) / config.reconcileIntervalMs),
  );
  let consecutiveReconcileFailures = 0;

  const reconcileTimer = setInterval(() => {
    reconcile()
      .then(() => {
        consecutiveReconcileFailures = 0;
      })
      .catch((error) => {
        consecutiveReconcileFailures += 1;
        logger.error(
          { error, consecutiveReconcileFailures, reconcileFailuresBeforeRelease },
          "reconcile failed",
        );
        if (consecutiveReconcileFailures >= reconcileFailuresBeforeRelease) {
          const owned = runtime.activeShards();
          if (owned.length > 0) {
            logger.warn(
              { owned },
              "shedding shards after prolonged reconcile failure to avoid duplicate sessions",
            );
            for (const shardID of owned) {
              runtime.stop(shardID);
              shardSnapshots.delete(shardID);
            }
          }
        }
      });
  }, config.reconcileIntervalMs);

  const heartbeatTimer = setInterval(() => {
    heartbeat().catch((error) => logger.error({ error }, "heartbeat failed"));
  }, config.heartbeatIntervalMs);

  const shutdown = async () => {
    clearInterval(reconcileTimer);
    clearInterval(heartbeatTimer);
    runtime.stopAll();
    try {
      await backend.release(config.workerId);
    } catch (error) {
      logger.error({ error }, "release failed");
    }
    process.exit(0);
  };

  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
}

main().catch((error) => {
  logger.error({ error }, "fatal error");
  process.exit(1);
});
