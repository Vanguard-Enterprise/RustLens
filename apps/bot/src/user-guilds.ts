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

async function markLeft(member: UserGuildEventMember) {
  if (member.user.bot) {
    return;
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
