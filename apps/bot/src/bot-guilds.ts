import type { Client, Guild, User } from "discord.js";
import { BackendClient, type UpsertBotGuildPayload } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { logger } from "./logger.js";
import { logDiscordError } from "./shared.js";

const MEMBERS_RESYNC_INTERVAL_MS = 7 * 24 * 60 * 60 * 1000;
const MEMBER_BATCH_SIZE = 1000;

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
    _workerID = `${config.workerId}-bot-guilds`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function buildUpsertPayload(guild: Guild, owner: User | null): Omit<UpsertBotGuildPayload, "members" | "membersSyncComplete"> {
  return {
    guildId: guild.id,
    name: guild.name,
    iconHash: guild.icon ?? null,
    bannerHash: guild.banner ?? null,
    ownerDiscordId: guild.ownerId ?? null,
    ownerUsername: owner?.username ?? null,
    ownerGlobalName: owner?.globalName ?? null,
    ownerAvatarHash: owner?.avatar ?? null,
    approximateMemberCount: guild.memberCount ?? null,
  };
}

function chunk<T>(items: T[], size: number): T[][] {
  if (items.length === 0) return [];
  const out: T[][] = [];
  for (let i = 0; i < items.length; i += size) {
    out.push(items.slice(i, i + size));
  }
  return out;
}

async function resolveOwner(guild: Guild): Promise<User | null> {
  try {
    return await guild.fetchOwner().then((m) => m.user);
  } catch (error) {
    logDiscordError("fetch guild owner", error, {
      guildId: guild.id,
    });
    return null;
  }
}

type FetchedMembers = {
  ids: string[];
  totalFetched: number;
};

async function listHumanMemberIds(guild: Guild): Promise<FetchedMembers> {
  const members = await guild.members.fetch();
  return {
    ids: members.filter((m) => !m.user.bot).map((m) => m.id),
    totalFetched: members.size,
  };
}

// Discord's guild.memberCount is approximate; allow a small shortfall before
// declaring the fetch incomplete. Without GuildMembers intent the fetch
// returns just the cache (which can be 0 or 1), so any significant gap
// almost always means the intent is missing or chunking was interrupted.
const MEMBER_FETCH_SHORTFALL_TOLERANCE = 0.05;
const MEMBER_FETCH_ABSOLUTE_TOLERANCE = 25;

function isMemberFetchComplete(guild: Guild, totalFetched: number): boolean {
  const expected = guild.memberCount;
  if (typeof expected !== "number" || expected <= 0) return true;
  const shortfall = expected - totalFetched;
  if (shortfall <= MEMBER_FETCH_ABSOLUTE_TOLERANCE) return true;
  return shortfall / expected <= MEMBER_FETCH_SHORTFALL_TOLERANCE;
}

async function syncMembers(guild: Guild) {
  let fetched: FetchedMembers;
  try {
    fetched = await listHumanMemberIds(guild);
  } catch (error) {
    logger.error({ guildId: guild.id, error }, "bot-guilds: failed to fetch members");
    return;
  }

  const fetchComplete = isMemberFetchComplete(guild, fetched.totalFetched);
  if (!fetchComplete) {
    logger.warn({
      guildId: guild.id,
      totalFetched: fetched.totalFetched,
      approxMemberCount: guild.memberCount,
    }, "bot-guilds: members.fetch returned far fewer members than guild.memberCount");
  }

  const memberIds = fetched.ids;
  const batches = chunk(memberIds, MEMBER_BATCH_SIZE);
  if (batches.length === 0) {
    // No members? Stamp completeness based on the actual fetch result.
    await getBackend().upsertBotGuild(getWorkerID(), {
      ...buildUpsertPayload(guild, await resolveOwner(guild)),
      members: [],
      membersSyncComplete: fetchComplete,
    });
    return;
  }

  const owner = await resolveOwner(guild);
  for (let i = 0; i < batches.length; i++) {
    const isLast = i === batches.length - 1;
    try {
      await getBackend().upsertBotGuild(getWorkerID(), {
        ...buildUpsertPayload(guild, owner),
        members: batches[i],
        membersSyncComplete: isLast && fetchComplete,
      });
    } catch (error) {
      logger.error({ guildId: guild.id, batchIndex: i, error }, "bot-guilds: member batch failed");
      return;
    }
  }
}

async function upsertMetadataOnly(guild: Guild) {
  try {
    const owner = await resolveOwner(guild);
    await getBackend().upsertBotGuild(getWorkerID(), buildUpsertPayload(guild, owner));
  } catch (error) {
    logger.error({ guildId: guild.id, error }, "bot-guilds: metadata upsert failed");
  }
}

async function handleGuildCreate(guild: Guild) {
  await upsertMetadataOnly(guild);
  void syncMembers(guild);
}

async function handleGuildDelete(guild: Guild) {
  try {
    await getBackend().markBotGuildLeft(getWorkerID(), guild.id);
  } catch (error) {
    logger.error({ guildId: guild.id, error }, "bot-guilds: mark left failed");
  }
}

async function handleGuildUpdate(guild: Guild) {
  await upsertMetadataOnly(guild);
}

async function handleClientReady(client: Client, shardId: number, shardCount: number) {
  const guilds = [...client.guilds.cache.values()];
  const guildIds = guilds.map((g) => g.id);

  try {
    await getBackend().reconcileBotGuilds(getWorkerID(), { shardId, shardCount, guildIds });
  } catch (error) {
    logger.error({ shardId, error }, "bot-guilds: reconcile failed");
  }

  for (const guild of guilds) {
    let response;
    try {
      const owner = await resolveOwner(guild);
      response = await getBackend().upsertBotGuild(getWorkerID(), buildUpsertPayload(guild, owner));
    } catch (error) {
      logger.error({ guildId: guild.id, error }, "bot-guilds: ready metadata upsert failed");
      continue;
    }

    const stale =
      !response.membersSyncedAt ||
      Date.now() - new Date(response.membersSyncedAt).getTime() > MEMBERS_RESYNC_INTERVAL_MS;
    if (stale) {
      void syncMembers(guild);
    }
  }
}

export function registerBotGuildSync(client: Client, shardId: number, shardCount: number) {
  client.on("guildCreate", (guild) => {
    handleGuildCreate(guild).catch((error) => {
      logger.error({ guildId: guild.id, error }, "bot-guilds: guildCreate handler failed");
    });
  });
  client.on("guildDelete", (guild) => {
    handleGuildDelete(guild).catch((error) => {
      logger.error({ guildId: guild.id, error }, "bot-guilds: guildDelete handler failed");
    });
  });
  client.on("guildUpdate", (_oldGuild, guild) => {
    handleGuildUpdate(guild).catch((error) => {
      logger.error({ guildId: guild.id, error }, "bot-guilds: guildUpdate handler failed");
    });
  });
  client.once("clientReady", () => {
    handleClientReady(client, shardId, shardCount).catch((error) => {
      logger.error({ shardId, error }, "bot-guilds: clientReady handler failed");
    });
  });
}

export const __testables = {
  buildUpsertPayload,
  chunk,
  MEMBERS_RESYNC_INTERVAL_MS,
};
