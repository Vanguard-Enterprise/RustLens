import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";

const shardID = process.env.SHARD_ID ?? "0";

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(
      config.backendUrl,
      config.jwtSecret,
      config.jwtIssuer,
      config.jwtAudience,
    );
    _workerID = `${config.workerId}-website-admin-${shardID}`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

const ADMIN_CACHE_TTL_MS = 30_000;
const adminCache = new Map<string, { isAdmin: boolean; expiresAt: number }>();

export async function isWebsiteAdmin(discordId: string): Promise<boolean> {
  const cached = adminCache.get(discordId);
  const now = Date.now();
  if (cached && cached.expiresAt > now) {
    return cached.isAdmin;
  }

  try {
    const { isAdmin } = await getBackend().isDiscordWebsiteAdmin(getWorkerID(), discordId);
    adminCache.set(discordId, { isAdmin, expiresAt: now + ADMIN_CACHE_TTL_MS });
    return isAdmin;
  } catch (error) {
    console.error("[bot] failed to check website admin status", error);
    return false;
  }
}
