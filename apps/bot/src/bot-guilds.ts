import type { Client, Guild, User } from "discord.js";
import { BackendClient, type UpsertBotGuildPayload } from "./backend-client.js";
import { loadConfig } from "./config.js";

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
  } catch {
    return null;
  }
}

async function listHumanMemberIds(guild: Guild): Promise<string[]> {
  const members = await guild.members.fetch();
  return members.filter((m) => !m.user.bot).map((m) => m.id);
}

async function syncMembers(guild: Guild) {
  let memberIds: string[];
  try {
    memberIds = await listHumanMemberIds(guild);
  } catch (error) {
    console.error("[bot] bot-guilds: failed to fetch members", { guildId: guild.id, error });
    return;
  }

  const batches = chunk(memberIds, MEMBER_BATCH_SIZE);
  if (batches.length === 0) {
    // No members? Stamp as synced anyway with an empty payload.
    await getBackend().upsertBotGuild(getWorkerID(), {
      ...buildUpsertPayload(guild, await resolveOwner(guild)),
      members: [],
      membersSyncComplete: true,
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
        membersSyncComplete: isLast,
      });
    } catch (error) {
      console.error("[bot] bot-guilds: member batch failed", { guildId: guild.id, batchIndex: i, error });
      return;
    }
  }
}

async function upsertMetadataOnly(guild: Guild) {
  try {
    const owner = await resolveOwner(guild);
    await getBackend().upsertBotGuild(getWorkerID(), buildUpsertPayload(guild, owner));
  } catch (error) {
    console.error("[bot] bot-guilds: metadata upsert failed", { guildId: guild.id, error });
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
    console.error("[bot] bot-guilds: mark left failed", { guildId: guild.id, error });
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
    console.error("[bot] bot-guilds: reconcile failed", { shardId, error });
  }

  for (const guild of guilds) {
    let response;
    try {
      const owner = await resolveOwner(guild);
      response = await getBackend().upsertBotGuild(getWorkerID(), buildUpsertPayload(guild, owner));
    } catch (error) {
      console.error("[bot] bot-guilds: ready metadata upsert failed", { guildId: guild.id, error });
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
      console.error("[bot] bot-guilds: guildCreate handler failed", { guildId: guild.id, error });
    });
  });
  client.on("guildDelete", (guild) => {
    handleGuildDelete(guild).catch((error) => {
      console.error("[bot] bot-guilds: guildDelete handler failed", { guildId: guild.id, error });
    });
  });
  client.on("guildUpdate", (_oldGuild, guild) => {
    handleGuildUpdate(guild).catch((error) => {
      console.error("[bot] bot-guilds: guildUpdate handler failed", { guildId: guild.id, error });
    });
  });
  client.once("clientReady", () => {
    handleClientReady(client, shardId, shardCount).catch((error) => {
      console.error("[bot] bot-guilds: clientReady handler failed", { shardId, error });
    });
  });
}

export const __testables = {
  buildUpsertPayload,
  chunk,
  MEMBERS_RESYNC_INTERVAL_MS,
};
