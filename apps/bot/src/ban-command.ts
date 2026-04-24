import type {
  ChatInputCommandInteraction,
  Client,
  ModalSubmitInteraction,
} from "discord.js";
import { EmbedBuilder, MessageFlags, SlashCommandBuilder } from "discord.js";
import { BackendClient } from "./backend-client.js";
import { BAN_CATEGORIES, isValidCategory, labelForCategory, type BanCategory } from "./ban-categories.js";
import { invalidateBanCache } from "./ban-check.js";
import { CheckboxGroupBuilder, ModalV2Builder } from "./builders/index.js";
import { loadConfig } from "./config.js";
import { getFieldValues, RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const BAN_COMMAND_NAME = "ban";

const BAN_USER_OPTION_NAME = "user";
const BAN_MODAL_PREFIX = "ban_modal:";
const BAN_CATEGORIES_FIELD_CUSTOM_ID = "ban_categories";

const banCommand = new SlashCommandBuilder()
  .setName(BAN_COMMAND_NAME)
  .setDescription("Restrict a user from RustLens command categories")
  .setDMPermission(false)
  .addUserOption((option) =>
    option
      .setName(BAN_USER_OPTION_NAME)
      .setDescription("Discord user to restrict")
      .setRequired(true),
  );

let _backend: BackendClient | undefined;
let _workerID: string | undefined;
let _ownerGuildId: string | undefined;
let _ownerDiscordIds: string[] | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(
      config.backendUrl,
      config.jwtSecret,
      config.jwtIssuer,
      config.jwtAudience,
    );
    _workerID = `${config.workerId}-ban-cmd`;
    _ownerGuildId = config.ownerGuildId;
    _ownerDiscordIds = config.ownerDiscordIds;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function getOwnerGuildID() {
  getBackend();
  return _ownerGuildId ?? "";
}

function getOwnerDiscordIDs() {
  getBackend();
  return _ownerDiscordIds ?? [];
}

function isOwnerCommandConfigured() {
  return getOwnerGuildID().length > 0 && getOwnerDiscordIDs().length > 0;
}

function isAuthorizedOwner(interaction: { guildId: string | null; user: { id: string } }) {
  return interaction.guildId === getOwnerGuildID() && getOwnerDiscordIDs().includes(interaction.user.id);
}

function buildBanEmbed(title: string, description: string) {
  return new EmbedBuilder()
    .setTitle(title)
    .setDescription(description)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

function buildBanModal(targetDiscordID: string, currentCategories: ReadonlySet<string>) {
  return new ModalV2Builder()
    .setTitle("Restrict User")
    .setCustomId(`${BAN_MODAL_PREFIX}${targetDiscordID}`)
    .addComponent(
      "Feature categories to block",
      new CheckboxGroupBuilder()
        .setCustomId(BAN_CATEGORIES_FIELD_CUSTOM_ID)
        .setRequired(false)
        .setMinValues(0)
        .setMaxValues(BAN_CATEGORIES.length)
        .addOptions(
          ...BAN_CATEGORIES.map((category) => ({
            label: category.label,
            value: category.value,
            default: currentCategories.has(category.value),
          })),
        ),
    );
}

async function replyOwnerOnly(
  interaction: ChatInputCommandInteraction | ModalSubmitInteraction,
  content: string,
) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ content, embeds: [], components: [] });
    return;
  }
  await interaction.reply({ content, flags: MessageFlags.Ephemeral });
}

async function replyEphemeralEmbed(
  interaction: ChatInputCommandInteraction | ModalSubmitInteraction,
  embed: EmbedBuilder,
) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ embeds: [embed], components: [] });
    return;
  }
  await interaction.reply({ embeds: [embed], flags: MessageFlags.Ephemeral });
}

async function deferEphemeral(interaction: ModalSubmitInteraction) {
  if (interaction.deferred || interaction.replied) {
    return;
  }
  await interaction.deferReply({ flags: MessageFlags.Ephemeral });
}

function parseTargetFromModalCustomId(customId: string) {
  return customId.startsWith(BAN_MODAL_PREFIX) ? customId.slice(BAN_MODAL_PREFIX.length) : null;
}

function formatCategoriesDescription(categories: BanCategory[]) {
  if (categories.length === 0) {
    return "No restrictions — user has full access.";
  }
  return categories.map((value) => `• ${labelForCategory(value)}`).join("\n");
}

export async function ensureBanCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  if (!isOwnerCommandConfigured()) {
    console.warn("[bot] skipping /ban command registration because BOT_OWNER_GUILD_ID or BOT_OWNER_DISCORD_IDS is missing");
    return;
  }

  const guildId = getOwnerGuildID();
  const data = banCommand.toJSON();

  const globalCommands = await client.application.commands.fetch();
  const existingGlobal = globalCommands.find((command) => command.name === data.name);
  if (existingGlobal) {
    await client.application.commands.delete(existingGlobal.id);
  }

  const commands = await client.application.commands.fetch({ guildId });
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data, guildId);
  } else {
    await client.application.commands.create(data, guildId);
  }

  console.log(`[bot] ensured /ban command in guild ${guildId}`);
}

export async function handleBanChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== BAN_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyOwnerOnly(interaction, "This command can only be used in a server.");
    return true;
  }

  if (!isOwnerCommandConfigured()) {
    await replyOwnerOnly(
      interaction,
      "BOT_OWNER_GUILD_ID and BOT_OWNER_DISCORD_IDS must be configured before /ban can be used.",
    );
    return true;
  }

  if (!isAuthorizedOwner(interaction)) {
    await replyOwnerOnly(
      interaction,
      "Only the configured bot owner can use /ban from the owner guild.",
    );
    return true;
  }

  const targetUser = interaction.options.getUser(BAN_USER_OPTION_NAME, true);

  if (targetUser.bot) {
    await replyOwnerOnly(interaction, "You cannot ban a bot account.");
    return true;
  }

  let current: string[] = [];
  try {
    const response = await getBackend().getUserBan(getWorkerID(), targetUser.id);
    current = response.categories ?? [];
  } catch (error) {
    console.error("[bot] /ban failed to load current bans", { targetId: targetUser.id, error });
    await replyOwnerOnly(interaction, "RustLens could not load current restrictions for that user.");
    return true;
  }

  const modal = buildBanModal(targetUser.id, new Set(current));
  await interaction.showModal(modal.toJSON() as any);
  return true;
}

export async function handleBanModalSubmit(interaction: ModalSubmitInteraction) {
  if (!interaction.customId.startsWith(BAN_MODAL_PREFIX)) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyOwnerOnly(interaction, "This command can only be used in a server.");
    return true;
  }

  if (!isOwnerCommandConfigured() || !isAuthorizedOwner(interaction)) {
    await replyOwnerOnly(interaction, "Only the configured bot owner can use /ban from the owner guild.");
    return true;
  }

  const targetID = parseTargetFromModalCustomId(interaction.customId);
  if (!targetID) {
    await replyOwnerOnly(interaction, "Invalid ban modal target.");
    return true;
  }

  await deferEphemeral(interaction);

  const raw = getFieldValues(interaction, BAN_CATEGORIES_FIELD_CUSTOM_ID);
  const seen = new Set<string>();
  const selected: BanCategory[] = [];
  for (const value of raw) {
    if (!isValidCategory(value)) continue;
    if (seen.has(value)) continue;
    seen.add(value);
    selected.push(value);
  }

  try {
    await getBackend().setUserBan(getWorkerID(), targetID, selected, interaction.user.id);
  } catch (error) {
    console.error("[bot] /ban save failed", { targetID, error });
    await replyEphemeralEmbed(
      interaction,
      buildBanEmbed("Restriction Save Failed", "RustLens could not save the restrictions for that user."),
    );
    return true;
  }

  invalidateBanCache(targetID);

  const title = selected.length === 0 ? "User unrestricted" : "User restrictions updated";
  const description =
    selected.length === 0
      ? `Removed all restrictions for <@${targetID}>.`
      : `Applied restrictions to <@${targetID}>:\n${formatCategoriesDescription(selected)}`;

  await replyEphemeralEmbed(interaction, buildBanEmbed(title, description));
  return true;
}
