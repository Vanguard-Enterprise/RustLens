import type { Client, Guild, GuildMember, PartialGuildMember, User } from "discord.js";
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
    _workerID = `${config.workerId}-user-guilds-${shardID}`;
  }

  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

type UserGuildEventMember = {
  id: string;
  guild: Guild;
  user: User;
};

async function markActive(member: UserGuildEventMember) {
  if (member.user.bot) {
    return;
  }

  await getBackend().createUserGuildLink(getWorkerID(), member.id, member.guild.id);
}

async function markLeft(member: GuildMember | PartialGuildMember) {
  // guildMemberRemove can deliver a PartialGuildMember whose user is null
  // when the member wasn't cached. Fetch the user so we can read the bot
  // flag — skipping the wrong way (treating bots as users) would dirty
  // user_guilds rows that we then have to clean up by hand.
  let user = member.user;
  if (!user) {
    try {
      user = await member.client.users.fetch(member.id);
    } catch (error) {
      console.warn("[bot] markLeft: could not resolve user for partial member", {
        guildId: member.guild.id,
        discordId: member.id,
        error,
      });
      return;
    }
  }
  if (user.bot) {
    return;
  }

  const links = await getBackend().listGuildLinksByDiscord(getWorkerID(), member.guild.id, member.id);
  for (const link of links.items) {
    await getBackend().deleteGuildLink(getWorkerID(), member.guild.id, member.id, link.steamId);
  }

  await getBackend().markUserGuildLeft(getWorkerID(), member.id, member.guild.id);
}

export function registerUserGuildSync(client: Client) {
  client.on("guildMemberAdd", (member: GuildMember) => {
    markActive(member).catch((error) => {
      console.error("[bot] failed to mark user guild active", {
        guildId: member.guild.id,
        discordId: member.id,
        error,
      });
    });
  });

  client.on("guildMemberRemove", (member: GuildMember | PartialGuildMember) => {
    markLeft(member).catch((error) => {
      console.error("[bot] failed to mark user guild inactive", {
        guildId: member.guild.id,
        discordId: member.id,
        error,
      });
    });
  });
}
