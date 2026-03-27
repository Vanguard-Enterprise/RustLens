import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { ShardRuntime } from "./shard-runtime.js";
import { computeShardDelta } from "./shards.js";

async function main() {
  const config = loadConfig();

  if (!config.discordToken) {
    throw new Error("DISCORD_TOKEN is required");
  }

  const backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
  const runtime = new ShardRuntime(config.discordToken);

  const register = await backend.register(config.workerId);
  let currentTargetShards = register.targetShards;

  console.log(`[bot] registered worker=${config.workerId} targetShards=${register.targetShards}`);

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
    }
  };

  const heartbeat = async () => {
    await backend.heartbeat(config.workerId, runtime.activeShards());
  };

  await reconcile();
  await heartbeat();

  const reconcileTimer = setInterval(() => {
    reconcile().catch((error) => console.error("[bot] reconcile failed", error));
  }, config.reconcileIntervalMs);

  const heartbeatTimer = setInterval(() => {
    heartbeat().catch((error) => console.error("[bot] heartbeat failed", error));
  }, config.heartbeatIntervalMs);

  const shutdown = async () => {
    clearInterval(reconcileTimer);
    clearInterval(heartbeatTimer);
    runtime.stopAll();
    try {
      await backend.release(config.workerId);
    } catch (error) {
      console.error("[bot] release failed", error);
    }
    process.exit(0);
  };

  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
}

main().catch((error) => {
  console.error("[bot] fatal error", error);
  process.exit(1);
});