import "dotenv/config";

export type BotConfig = {
  backendUrl: string;
  jwtSecret: string;
  jwtIssuer: string;
  jwtAudience: string;
  workerId: string;
  discordToken: string;
  reconcileIntervalMs: number;
  heartbeatIntervalMs: number;
};

export function loadConfig(): BotConfig {
  const workerFallback = `bot-${process.pid}`;

  return {
    backendUrl: process.env.BACKEND_URL ?? "http://localhost:8080",
    jwtSecret: process.env.BOT_SERVICE_JWT_SECRET ?? "dev-bot-secret",
    jwtIssuer: process.env.BOT_JWT_ISSUER ?? "rustlens-bot",
    jwtAudience: process.env.BOT_JWT_AUDIENCE ?? "rustlens-backend",
    workerId: process.env.BOT_WORKER_ID ?? workerFallback,
    discordToken: process.env.DISCORD_TOKEN ?? "",
    reconcileIntervalMs: Number(process.env.BOT_RECONCILE_INTERVAL_MS ?? "15000"),
    heartbeatIntervalMs: Number(process.env.BOT_HEARTBEAT_INTERVAL_MS ?? "8000"),
  };
}