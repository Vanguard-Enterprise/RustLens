import type { Collection, Guild, GuildMember, Snowflake } from "discord.js";

const DEFAULT_GUILD_MEMBER_CACHE_TTL_MS = 30_000;

type GuildMemberCacheEntry = {
  fetchedAt: number;
  members: Collection<Snowflake, GuildMember>;
};

const guildMemberCache = new Map<string, GuildMemberCacheEntry>();
const inflightGuildMemberFetches = new Map<string, Promise<Collection<Snowflake, GuildMember>>>();

export async function getGuildMembers(
  guild: Guild,
  options?: { maxAgeMs?: number },
) {
  const maxAgeMs = options?.maxAgeMs ?? DEFAULT_GUILD_MEMBER_CACHE_TTL_MS;
  const now = Date.now();
  const cached = guildMemberCache.get(guild.id);

  if (cached && now - cached.fetchedAt <= maxAgeMs) {
    return cached.members;
  }

  const inflight = inflightGuildMemberFetches.get(guild.id);
  if (inflight) {
    return inflight;
  }

  const fetchPromise = guild.members.fetch()
    .then((members) => {
      guildMemberCache.set(guild.id, {
        fetchedAt: Date.now(),
        members,
      });
      inflightGuildMemberFetches.delete(guild.id);
      return members;
    })
    .catch((error) => {
      inflightGuildMemberFetches.delete(guild.id);
      throw error;
    });

  inflightGuildMemberFetches.set(guild.id, fetchPromise);
  return fetchPromise;
}

export function clearGuildMembersCache(guildId?: string) {
  if (guildId) {
    guildMemberCache.delete(guildId);
    inflightGuildMemberFetches.delete(guildId);
    return;
  }

  guildMemberCache.clear();
  inflightGuildMemberFetches.clear();
}

export const __testables = {
  DEFAULT_GUILD_MEMBER_CACHE_TTL_MS,
  guildMemberCache,
  inflightGuildMemberFetches,
};
