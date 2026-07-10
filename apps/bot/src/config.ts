import "dotenv/config";
import { hostname } from "node:os";

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

function cleanWorkerIDPart(value: string) {
  return value.trim().replace(/[^a-zA-Z0-9_-]/g, "-");
}

function resolveWorkerID() {
  const resolved = process.env.BOT_WORKER_ID_RESOLVED?.trim();
  if (resolved) {
    return cleanWorkerIDPart(resolved);
  }

  const host = cleanWorkerIDPart(hostname());
  const configured = process.env.BOT_WORKER_ID?.trim();
  if (configured) {
    return `${cleanWorkerIDPart(configured)}-${host}-${process.pid}`;
  }

  return `bot-${host}-${process.pid}`;
}

export function loadConfig(): BotConfig {
  const jwtSecret = process.env.BOT_SERVICE_JWT_SECRET?.trim();
  if (!jwtSecret) {
    throw new Error("BOT_SERVICE_JWT_SECRET is required");
  }

  return {
    backendUrl: process.env.BACKEND_URL ?? "http://localhost",
    jwtSecret,
    jwtIssuer: process.env.BOT_JWT_ISSUER ?? "rustlens-bot",
    jwtAudience: process.env.BOT_JWT_AUDIENCE ?? "rustlens-backend",
    workerId: resolveWorkerID(),
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
