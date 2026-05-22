import type { Interaction } from "discord.js";

import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";

const shardID = process.env.SHARD_ID ?? "0";

let _backend: BackendClient | undefined;
let _workerID: string | undefined;
// Test injection point. When non-null, used instead of the real lazy-init path.
let _backendOverride: BackendClient | null = null;

function getBackend(): BackendClient {
  if (_backendOverride) return _backendOverride;
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(
      config.backendUrl,
      config.jwtSecret,
      config.jwtIssuer,
      config.jwtAudience,
    );
    _workerID = `${config.workerId}-ensure-app-user-${shardID}`;
  }
  return _backend;
}

function getWorkerID(): string {
  if (_backendOverride) return "test-worker";
  getBackend();
  return _workerID!;
}

const ensureAppUserCache = new Map<string, number>();
const ENSURE_APP_USER_TTL_MS = 5 * 60_000;

// Entries are only validated on read; without a sweep, every Discord user
// ever seen by this worker stays in the map for the process lifetime.
setInterval(() => {
  const cutoff = Date.now() - ENSURE_APP_USER_TTL_MS;
  for (const [userId, last] of ensureAppUserCache) {
    if (last <= cutoff) ensureAppUserCache.delete(userId);
  }
}, ENSURE_APP_USER_TTL_MS).unref();

/**
 * Fire-and-forget: on the first call for a given Discord user (within the
 * 5-minute TTL), POST to the backend so an app_users row exists for them.
 * Subsequent calls within the window are no-ops. Errors are logged but never
 * thrown — this is on the interaction hot path.
 */
export function ensureAppUser(interaction: Interaction): void {
  const u = interaction.user;
  if (!u) return;

  const now = Date.now();
  const last = ensureAppUserCache.get(u.id);
  if (last !== undefined && now - last < ENSURE_APP_USER_TTL_MS) return;
  ensureAppUserCache.set(u.id, now);

  getBackend()
    .ensureAppUser(getWorkerID(), {
      discordId:  u.id,
      username:   u.username,
      globalName: u.globalName ?? null,
      avatarHash: u.avatar ?? null,
    })
    .catch((error) => {
      console.error(JSON.stringify({
        level:   "warn",
        message: "[bot] ensureAppUser failed",
        userId:  u.id,
        error:   String(error),
      }));
    });
}

// --- Test-only helpers ---

export function __resetEnsureAppUserCacheForTests(): void {
  ensureAppUserCache.clear();
}

export function __setBackendForTests(backend: BackendClient | null): void {
  _backendOverride = backend;
}
