import "dotenv/config";

export type BotConfig = {
  backendUrl: string;
  jwtSecret: string;
  jwtIssuer: string;
  jwtAudience: string;
  workerId: string;
  discordToken: string;
  ownerGuildId: string;
  ownerDiscordIds: string[];
  reconcileIntervalMs: number;
  heartbeatIntervalMs: number;
  proxiedRequestConcurrency: number;
};

function parseEnvBool(value: string | undefined) {
  return value?.trim().toLowerCase() === "true";
}

function getProxiedRequestConcurrency() {
  if (!parseEnvBool(process.env.PROXY_ENABLED)) {
    return 1;
  }

  const endpoints = (process.env.PROXY_ENDPOINTS ?? "")
    .split(",")
    .map((value) => value.trim())
    .filter((value) => value.length > 0);

  return Math.max(1, endpoints.length);
}

export function loadConfig(): BotConfig {
  const workerFallback = `bot-${process.pid}`;

  return {
    backendUrl: process.env.BACKEND_URL ?? "http://localhost:8080",
    jwtSecret: process.env.BOT_SERVICE_JWT_SECRET ?? "dev-bot-secret",
    jwtIssuer: process.env.BOT_JWT_ISSUER ?? "rustlens-bot",
    jwtAudience: process.env.BOT_JWT_AUDIENCE ?? "rustlens-backend",
    workerId: process.env.BOT_WORKER_ID ?? workerFallback,
    discordToken: process.env.DISCORD_TOKEN ?? "",
    ownerGuildId: process.env.BOT_OWNER_GUILD_ID ?? "",
    ownerDiscordIds: (process.env.BOT_OWNER_DISCORD_IDS ?? "")
      .split(",")
      .map((value) => value.trim())
      .filter((value) => value.length > 0),
    reconcileIntervalMs: Number(process.env.BOT_RECONCILE_INTERVAL_MS ?? "15000") || 15000,
    heartbeatIntervalMs: Number(process.env.BOT_HEARTBEAT_INTERVAL_MS ?? "8000") || 8000,
    proxiedRequestConcurrency: getProxiedRequestConcurrency(),
  };
}
