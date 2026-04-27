import { loadConfig } from "./config.js";

let cached: { ownerGuildId: string; ownerDiscordIds: string[] } | undefined;

function get() {
  if (!cached) {
    const config = loadConfig();
    cached = {
      ownerGuildId: config.ownerGuildId,
      ownerDiscordIds: config.ownerDiscordIds,
    };
  }
  return cached;
}

export function getOwnerGuildID(): string {
  return get().ownerGuildId;
}

export function getOwnerDiscordIDs(): string[] {
  return get().ownerDiscordIds;
}

export function isOwnerCommandConfigured(): boolean {
  const { ownerGuildId, ownerDiscordIds } = get();
  return ownerGuildId.length > 0 && ownerDiscordIds.length > 0;
}

export function isAuthorizedOwner(interaction: {
  guildId: string | null;
  user: { id: string };
}): boolean {
  const { ownerGuildId, ownerDiscordIds } = get();
  return interaction.guildId === ownerGuildId && ownerDiscordIds.includes(interaction.user.id);
}

// resetCachedOwnerGuard is exposed for tests so they can reset the cached
// config between cases that mutate process.env.
export function resetCachedOwnerGuard(): void {
  cached = undefined;
}
