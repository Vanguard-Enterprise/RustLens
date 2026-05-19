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
const proCache = new Map<string, { hasRole: boolean; expiresAt: number }>();

// In-flight lookup maps. Without these, two concurrent /search invocations
// from the same Discord user on a cold cache would both fetch from the
// backend; ban-check.ts uses the same pattern.
const adminInflight = new Map<string, Promise<boolean>>();
const proInflight = new Map<string, Promise<boolean>>();

/**
 * Drop cached admin/PRO entries for a Discord user. Call after we know the
 * user's role assignments changed (e.g. SSE-driven revocation) so we don't
 * grant elevated access for the remainder of the 30s TTL window.
 */
export function invalidateWebsiteAdminCache(discordId: string) {
  adminCache.delete(discordId);
  proCache.delete(discordId);
}

export async function isWebsiteAdmin(discordId: string): Promise<boolean> {
  const cached = adminCache.get(discordId);
  const now = Date.now();
  if (cached && cached.expiresAt > now) {
    return cached.isAdmin;
  }
  const inflight = adminInflight.get(discordId);
  if (inflight) return inflight;

  const promise = (async () => {
    try {
      const { isAdmin } = await getBackend().isDiscordWebsiteAdmin(getWorkerID(), discordId);
      adminCache.set(discordId, { isAdmin, expiresAt: Date.now() + ADMIN_CACHE_TTL_MS });
      return isAdmin;
    } catch (error) {
      console.error("[bot] failed to check website admin status", error);
      return false;
    } finally {
      adminInflight.delete(discordId);
    }
  })();
  adminInflight.set(discordId, promise);
  return promise;
}

export async function hasProFeatureAccess(discordId: string): Promise<boolean> {
  if (await hasProRole(discordId)) {
    return true;
  }
  return isWebsiteAdmin(discordId);
}

async function hasProRole(discordId: string): Promise<boolean> {
  const cached = proCache.get(discordId);
  const now = Date.now();
  if (cached && cached.expiresAt > now) {
    return cached.hasRole;
  }
  const inflight = proInflight.get(discordId);
  if (inflight) return inflight;

  const promise = (async () => {
    try {
      const { hasRole } = await getBackend().hasDiscordProRole(getWorkerID(), discordId);
      proCache.set(discordId, { hasRole, expiresAt: Date.now() + ADMIN_CACHE_TTL_MS });
      return hasRole;
    } catch (error) {
      console.error("[bot] failed to check PRO role status", error);
      return false;
    } finally {
      proInflight.delete(discordId);
    }
  })();
  proInflight.set(discordId, promise);
  return promise;
}
