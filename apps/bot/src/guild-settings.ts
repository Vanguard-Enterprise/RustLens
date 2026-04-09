import type {
  ButtonInteraction,
  ChatInputCommandInteraction,
  Guild,
  GuildBasedChannel,
  ModalSubmitInteraction,
  StringSelectMenuInteraction,
} from "discord.js";
import { PermissionFlagsBits } from "discord.js";
import { BackendClient, type GuildSettingsPatch } from "./backend-client.js";
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
    _workerID = `${config.workerId}-guild-settings-${shardID}`;
  }

  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

export type GuildAdminInteraction =
  | ButtonInteraction
  | ChatInputCommandInteraction
  | ModalSubmitInteraction
  | StringSelectMenuInteraction;

export async function fetchGuildSettings(guildId: string) {
  return getBackend().guildSettings(getWorkerID(), guildId);
}

export async function updateGuildSettings(guildId: string, patch: GuildSettingsPatch) {
  return getBackend().updateGuildSettings(getWorkerID(), guildId, patch);
}

export async function hasGuildAdminAccess(interaction: GuildAdminInteraction) {
  if (interaction.memberPermissions?.has(PermissionFlagsBits.Administrator)) {
    return true;
  }

  if (!interaction.inGuild() || !interaction.guild) {
    return false;
  }

  const settings = await fetchGuildSettings(interaction.guildId);
  if (!settings.adminRoleId) {
    return false;
  }

  const member = await interaction.guild.members.fetch(interaction.user.id).catch(() => null);
  return member?.roles.cache.has(settings.adminRoleId) ?? false;
}

export async function resolveConfiguredGuildChannel(
  guild: Guild,
  channelId: string,
) {
  if (!channelId) {
    return null;
  }

  const channel =
    guild.channels.cache.get(channelId) ??
    (await guild.channels.fetch(channelId).catch(() => null));

  if (!channel?.isTextBased() || !("send" in channel)) {
    return null;
  }

  return channel as GuildBasedChannel;
}
