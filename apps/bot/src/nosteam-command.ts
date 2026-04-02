import type { ChatInputCommandInteraction, Client, GuildMember } from "discord.js";
import { EmbedBuilder, MessageFlags, SlashCommandBuilder } from "discord.js";
import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { hasGuildAdminAccess } from "./guild-settings.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const NOSTEAM_COMMAND_NAME = "nosteam";

const noSteamCommand = new SlashCommandBuilder()
  .setName(NOSTEAM_COMMAND_NAME)
  .setDescription("List guild users without a linked Steam account")
  .setDMPermission(false);

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
    _workerID = `${config.workerId}-nosteam-${shardID}`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function buildNoSteamEmbed(userIds: readonly string[]) {
  return new EmbedBuilder()
    .setTitle("Unlinked Users")
    .setDescription(
      userIds.length > 0
        ? userIds.map((userId) => `> * <@${userId}>`).join("\n")
        : "> * None",
    )
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

function sortMembers(left: GuildMember, right: GuildMember) {
  const leftName = left.displayName.toLocaleLowerCase();
  const rightName = right.displayName.toLocaleLowerCase();
  if (leftName !== rightName) {
    return leftName.localeCompare(rightName);
  }

  return left.id.localeCompare(right.id);
}

export async function ensureNoSteamCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = noSteamCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /nosteam command (global)");
}

export async function handleNoSteamChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== NOSTEAM_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guild) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (!(await hasGuildAdminAccess(interaction))) {
    await interaction.reply({
      content: "Only the configured admin role or administrators can use /nosteam.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  const [members, links] = await Promise.all([
    interaction.guild.members.fetch(),
    getBackend().listGuildLinks(getWorkerID(), interaction.guildId),
  ]);

  const linkedDiscordIds = new Set(links.items.map((link) => link.discordId));
  const missingUserIds = [...members.values()]
    .filter((member) => !member.user.bot)
    .sort(sortMembers)
    .filter((member) => !linkedDiscordIds.has(member.id))
    .map((member) => member.id);

  await interaction.editReply({
    embeds: [buildNoSteamEmbed(missingUserIds)],
  });
  return true;
}
