import { Client, GatewayIntentBits } from "discord.js";

const shardID = Number(process.env.SHARD_ID ?? "0");
const shardCount = Number(process.env.SHARD_COUNT ?? "1");
const token = process.env.DISCORD_TOKEN;

if (!token) {
  throw new Error("DISCORD_TOKEN is required for shard worker process");
}

const client = new Client({
  intents: [GatewayIntentBits.Guilds],
  shards: [shardID],
  shardCount,
});

client.once("ready", () => {
  console.log(`[bot] shard ${shardID}/${shardCount} ready as ${client.user?.tag}`);
});

client.login(token).catch((error) => {
  console.error(`[bot] shard ${shardID} login failed`, error);
  process.exit(1);
});

process.on("SIGINT", async () => {
  await client.destroy();
  process.exit(0);
});

process.on("SIGTERM", async () => {
  await client.destroy();
  process.exit(0);
});