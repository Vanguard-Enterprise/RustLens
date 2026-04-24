import type {
  AutocompleteInteraction,
  ChatInputCommandInteraction,
  Client,
  ModalSubmitInteraction,
} from "discord.js";
import { EmbedBuilder, MessageFlags, SlashCommandBuilder } from "discord.js";
import { type TrackedServer, BackendClient } from "./backend-client.js";
import { CheckboxGroupBuilder, ModalV2Builder, RadioGroupBuilder, TextInputBuilder } from "./builders/index.js";
import { loadConfig } from "./config.js";
import { getFieldValues, RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const SERVER_COMMAND_NAME = "server";

const SERVER_ADD_SUBCOMMAND_NAME = "add";
const SERVER_EDIT_SUBCOMMAND_NAME = "edit";
const SERVER_REMOVE_SUBCOMMAND_NAME = "remove";
const SERVER_LIST_SUBCOMMAND_NAME = "list";
const SERVER_TARGET_OPTION_NAME = "server";

const SERVER_MODAL_PREFIX = "server_modal:";
const SERVER_MODAL_ADD_CUSTOM_ID = `${SERVER_MODAL_PREFIX}add`;
const SERVER_MODAL_EDIT_PREFIX = `${SERVER_MODAL_PREFIX}edit:`;
const SERVER_NAME_FIELD_CUSTOM_ID = "server_name";
const SERVER_BM_ID_FIELD_CUSTOM_ID = "server_bm_id";
const SERVER_CATEGORIES_FIELD_CUSTOM_ID = "server_categories";
const SERVER_INCLUDE_IN_GUILD_SETTINGS_FIELD_CUSTOM_ID = "server_include_in_guild_settings";
const SERVER_INCLUDE_IN_GUILD_SETTINGS_VALUE = "yes";
const SERVER_EXCLUDE_FROM_GUILD_SETTINGS_VALUE = "no";

const TRACKED_SERVER_CATEGORY_DEFINITIONS = [
  { key: "isVitality", value: "vitality", label: "Vitality" },
  { key: "isAtlas", value: "atlas", label: "Atlas" },
  { key: "isVital", value: "vital", label: "Vital" },
  { key: "isModded", value: "modded", label: "Modded" },
  { key: "isOfficial", value: "official", label: "Official" },
  { key: "isBuild", value: "build", label: "Build" },
] as const satisfies readonly {
  key: keyof Pick<TrackedServer, "isVitality" | "isAtlas" | "isVital" | "isModded" | "isOfficial" | "isBuild">;
  value: string;
  label: string;
}[];

type TrackedServerCategoryValue = (typeof TRACKED_SERVER_CATEGORY_DEFINITIONS)[number]["value"];
type TrackedServerDraft = Omit<TrackedServer, "lastPolledAt" | "lastPollError">;

const serverCommand = new SlashCommandBuilder()
  .setName(SERVER_COMMAND_NAME)
  .setDescription("Manage tracked BattleMetrics servers")
  .setDMPermission(false)
  .addSubcommand((subcommand) =>
    subcommand
      .setName(SERVER_ADD_SUBCOMMAND_NAME)
      .setDescription("Add a tracked BattleMetrics server"),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(SERVER_EDIT_SUBCOMMAND_NAME)
      .setDescription("Edit a tracked BattleMetrics server")
      .addStringOption((option) =>
        option
          .setName(SERVER_TARGET_OPTION_NAME)
          .setDescription("Tracked server to edit")
          .setRequired(true)
          .setAutocomplete(true),
      ),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(SERVER_REMOVE_SUBCOMMAND_NAME)
      .setDescription("Remove a tracked BattleMetrics server")
      .addStringOption((option) =>
        option
          .setName(SERVER_TARGET_OPTION_NAME)
          .setDescription("Tracked server to remove")
          .setRequired(true)
          .setAutocomplete(true),
      ),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(SERVER_LIST_SUBCOMMAND_NAME)
      .setDescription("List tracked BattleMetrics servers"),
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
    _workerID = `${config.workerId}-server`;
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

function normalizeName(name: string) {
  return name
    .trim()
    .split(/\s+/)
    .filter((part) => part.length > 0)
    .join(" ");
}

function buildServerEmbed(title: string, description: string) {
  return new EmbedBuilder()
    .setTitle(title)
    .setDescription(description)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

function trimChoiceLabel(value: string, maxLength = 100) {
  return value.length <= maxLength ? value : `${value.slice(0, maxLength - 3)}...`;
}

function buildTrackedServerChoiceName(server: Pick<TrackedServer, "bmId" | "name">) {
  return trimChoiceLabel(`${server.name} (${server.bmId})`);
}

function buildTrackedServerModal(server?: TrackedServer) {
  const title = server ? "Edit Tracked Server" : "Add Tracked Server";
  const customId = server
    ? `${SERVER_MODAL_EDIT_PREFIX}${server.bmId}`
    : SERVER_MODAL_ADD_CUSTOM_ID;

  return new ModalV2Builder()
    .setTitle(title)
    .setCustomId(customId)
    .addComponent(
      "Server Name",
      new TextInputBuilder()
        .setCustomId(SERVER_NAME_FIELD_CUSTOM_ID)
        .setRequired(true)
        .setMaxLength(100)
        .setPlaceholder("Vitality EU Main")
        .setValue(server?.name ?? ""),
    )
    .addComponent(
      "BattleMetrics ID",
      new TextInputBuilder()
        .setCustomId(SERVER_BM_ID_FIELD_CUSTOM_ID)
        .setRequired(true)
        .setMaxLength(32)
        .setPlaceholder("16741517")
        .setValue(server?.bmId ?? ""),
    )
    .addComponent(
      "Include in guild settings?",
      new RadioGroupBuilder()
        .setCustomId(SERVER_INCLUDE_IN_GUILD_SETTINGS_FIELD_CUSTOM_ID)
        .addOptions(
          {
            label: "Yes",
            value: SERVER_INCLUDE_IN_GUILD_SETTINGS_VALUE,
            default: server?.includeInGuildSettings ?? false,
          },
          {
            label: "No",
            value: SERVER_EXCLUDE_FROM_GUILD_SETTINGS_VALUE,
            default: server ? !server.includeInGuildSettings : true,
          },
        ),
    )
    .addComponent(
      "Server Categories",
      new CheckboxGroupBuilder()
        .setCustomId(SERVER_CATEGORIES_FIELD_CUSTOM_ID)
        .setRequired(false)
        .setMinValues(0)
        .setMaxValues(TRACKED_SERVER_CATEGORY_DEFINITIONS.length)
        .addOptions(
          ...TRACKED_SERVER_CATEGORY_DEFINITIONS.map((category) => ({
            label: category.label,
            value: category.value,
            default: server ? Boolean(server[category.key]) : false,
          })),
        ),
    );
}

function formatTrackedServerCategories(server: Pick<TrackedServer, "isVitality" | "isAtlas" | "isVital" | "isModded" | "isOfficial" | "isBuild">) {
  const labels = TRACKED_SERVER_CATEGORY_DEFINITIONS
    .filter((category) => server[category.key])
    .map((category) => category.label);
  return labels.length > 0 ? labels.join(", ") : "None";
}

function formatTrackedServerPollStatus(server: Pick<TrackedServer, "lastPolledAt" | "lastPollError">) {
  if (server.lastPollError) {
    return `Poll error: ${server.lastPollError}`;
  }

  if (server.lastPolledAt) {
    const unix = Math.floor(new Date(server.lastPolledAt).getTime() / 1000);
    return `Last polled <t:${unix}:R>`;
  }

  return "Not polled yet";
}

function buildTrackedServerListDescription(items: TrackedServer[]) {
  if (items.length === 0) {
    return "No tracked BattleMetrics servers configured.";
  }

  let description = items.map((item, index) =>
    `${index + 1}. **${item.name}**\n` +
    `BM: \`${item.bmId}\` | Categories: ${formatTrackedServerCategories(item)}\n` +
    `${formatTrackedServerPollStatus(item)}`
  ).join("\n\n");

  if (description.length > 4096) {
    description = `${description.slice(0, 4093)}...`;
  }

  return description;
}

function buildTrackedServerDraft(name: string, bmId: string, selectedCategories: readonly string[]): TrackedServerDraft {
  const categories = new Set(selectedCategories as readonly TrackedServerCategoryValue[]);
  return {
    name: normalizeName(name),
    bmId: bmId.trim(),
    includeInGuildSettings: false,
    isVitality: categories.has("vitality"),
    isAtlas: categories.has("atlas"),
    isVital: categories.has("vital"),
    isModded: categories.has("modded"),
    isOfficial: categories.has("official"),
    isBuild: categories.has("build"),
  };
}

function validateTrackedServerDraft(draft: TrackedServerDraft) {
  if (!draft.bmId) {
    return "BattleMetrics server ID is required.";
  }

  if (!draft.name) {
    return "Tracked server name is required.";
  }

  return null;
}

async function replyPublicEmbed(
  interaction: ChatInputCommandInteraction | ModalSubmitInteraction,
  embed: EmbedBuilder,
) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ embeds: [embed] });
    return;
  }

  await interaction.reply({ embeds: [embed], flags: MessageFlags.Ephemeral });
}

async function replyOwnerOnly(
  interaction: ChatInputCommandInteraction | ModalSubmitInteraction,
  content: string,
) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ content, embeds: [] });
    return;
  }

  await interaction.reply({
    content,
    flags: MessageFlags.Ephemeral,
  });
}

async function deferPublic(interaction: ChatInputCommandInteraction | ModalSubmitInteraction) {
  if (interaction.deferred || interaction.replied) {
    return;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });
}

async function fetchTrackedServers() {
  return getBackend().listTrackedServers(getWorkerID());
}

async function findTrackedServerByBmId(bmId: string) {
  const { items } = await fetchTrackedServers();
  return items.find((item) => item.bmId === bmId) ?? null;
}

function parseModalDraft(interaction: ModalSubmitInteraction) {
  const name = getFieldValues(interaction, SERVER_NAME_FIELD_CUSTOM_ID)[0] ?? "";
  const bmId = getFieldValues(interaction, SERVER_BM_ID_FIELD_CUSTOM_ID)[0] ?? "";
  const categories = getFieldValues(interaction, SERVER_CATEGORIES_FIELD_CUSTOM_ID);
  const includeInGuildSettings = getFieldValues(interaction, SERVER_INCLUDE_IN_GUILD_SETTINGS_FIELD_CUSTOM_ID)
    .includes(SERVER_INCLUDE_IN_GUILD_SETTINGS_VALUE);
  return {
    ...buildTrackedServerDraft(name, bmId, categories),
    includeInGuildSettings,
  };
}

function parseEditModalTarget(customId: string) {
  return customId.startsWith(SERVER_MODAL_EDIT_PREFIX)
    ? customId.slice(SERVER_MODAL_EDIT_PREFIX.length)
    : null;
}

export async function ensureServerCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  if (!isOwnerCommandConfigured()) {
    console.warn("[bot] skipping /server command registration because BOT_OWNER_GUILD_ID or BOT_OWNER_DISCORD_IDS is missing");
    return;
  }

  const guildId = getOwnerGuildID();
  const data = serverCommand.toJSON();
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

  console.log(`[bot] ensured /server command in guild ${guildId}`);
}

export async function handleServerAutocomplete(interaction: AutocompleteInteraction) {
  if (interaction.commandName !== SERVER_COMMAND_NAME) {
    return false;
  }

  if (!isOwnerCommandConfigured() || !isAuthorizedOwner(interaction)) {
    await interaction.respond([]);
    return true;
  }

  const subcommand = interaction.options.getSubcommand();
  if (subcommand !== SERVER_EDIT_SUBCOMMAND_NAME && subcommand !== SERVER_REMOVE_SUBCOMMAND_NAME) {
    await interaction.respond([]);
    return true;
  }

  const focused = interaction.options.getFocused(true);
  if (focused.name !== SERVER_TARGET_OPTION_NAME) {
    await interaction.respond([]);
    return true;
  }

  try {
    const { items } = await fetchTrackedServers();
    const query = focused.value.trim().toLocaleLowerCase();
    const filtered = items
      .filter((item) =>
        query.length === 0 ||
        item.name.toLocaleLowerCase().includes(query) ||
        item.bmId.includes(query),
      )
      .slice(0, 25)
      .map((item) => ({
        name: buildTrackedServerChoiceName(item),
        value: item.bmId,
      }));

    await interaction.respond(filtered);
  } catch {
    await interaction.respond([]);
  }

  return true;
}

export async function handleServerChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== SERVER_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyOwnerOnly(interaction, "This command can only be used in a server.");
    return true;
  }

  if (!isOwnerCommandConfigured()) {
    await replyOwnerOnly(
      interaction,
      "BOT_OWNER_GUILD_ID and BOT_OWNER_DISCORD_IDS must be configured before /server can be used.",
    );
    return true;
  }

  if (!isAuthorizedOwner(interaction)) {
    await replyOwnerOnly(
      interaction,
      "Only the configured bot owner can use /server from the owner guild.",
    );
    return true;
  }

  const subcommand = interaction.options.getSubcommand();

  if (subcommand === SERVER_ADD_SUBCOMMAND_NAME) {
    await interaction.showModal(buildTrackedServerModal().toJSON() as any);
    return true;
  }

  if (subcommand === SERVER_EDIT_SUBCOMMAND_NAME) {
    const bmId = interaction.options.getString(SERVER_TARGET_OPTION_NAME, true).trim();
    const server = await findTrackedServerByBmId(bmId);
    if (!server) {
      await replyOwnerOnly(interaction, "That tracked server could not be found.");
      return true;
    }

    await interaction.showModal(buildTrackedServerModal(server).toJSON() as any);
    return true;
  }

  await deferPublic(interaction);

  if (subcommand === SERVER_REMOVE_SUBCOMMAND_NAME) {
    const bmId = interaction.options.getString(SERVER_TARGET_OPTION_NAME, true).trim();
    const server = await findTrackedServerByBmId(bmId);

    try {
      await getBackend().deleteTrackedServer(getWorkerID(), bmId);
      await replyPublicEmbed(
        interaction,
        buildServerEmbed(
          "Tracked Server Removed",
          server
            ? `Removed **${server.name}** (\`${bmId}\`) from tracking.`
            : `Removed BattleMetrics ID \`${bmId}\` from tracking.`,
        ),
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      const description = message.includes("404")
        ? `BattleMetrics ID \`${bmId}\` is not currently tracked.`
        : "RustLens could not remove that tracked server.";
      await replyPublicEmbed(
        interaction,
        buildServerEmbed("Tracked Server Remove Failed", description),
      );
    }

    return true;
  }

  try {
    const { items } = await fetchTrackedServers();
    await replyPublicEmbed(
      interaction,
      buildServerEmbed("Tracked Servers", buildTrackedServerListDescription(items)),
    );
  } catch {
    await replyPublicEmbed(
      interaction,
      buildServerEmbed("Tracked Server List Failed", "RustLens could not load the tracked server list."),
    );
  }

  return true;
}

export async function handleServerModalSubmit(interaction: ModalSubmitInteraction) {
  if (
    interaction.customId !== SERVER_MODAL_ADD_CUSTOM_ID &&
    !interaction.customId.startsWith(SERVER_MODAL_EDIT_PREFIX)
  ) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyOwnerOnly(interaction, "This command can only be used in a server.");
    return true;
  }

  if (!isOwnerCommandConfigured() || !isAuthorizedOwner(interaction)) {
    await replyOwnerOnly(interaction, "Only the configured bot owner can use /server from the owner guild.");
    return true;
  }

  await deferPublic(interaction);

  const draft = parseModalDraft(interaction);
  const validationError = validateTrackedServerDraft(draft);
  if (validationError) {
    await replyPublicEmbed(
      interaction,
      buildServerEmbed("Tracked Server Save Failed", validationError),
    );
    return true;
  }

  const currentBmId = parseEditModalTarget(interaction.customId);

  try {
    const saved = currentBmId
      ? await getBackend().updateTrackedServer(getWorkerID(), currentBmId, draft)
      : await getBackend().createTrackedServer(getWorkerID(), draft);

    await replyPublicEmbed(
      interaction,
        buildServerEmbed(
          currentBmId ? "Tracked Server Updated" : "Tracked Server Added",
          `**${saved.name}**\nBattleMetrics ID: \`${saved.bmId}\`\nIncluded in guild settings: ${saved.includeInGuildSettings ? "Yes" : "No"}\nCategories: ${formatTrackedServerCategories(saved)}`,
        ),
      );
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    const description = message.includes("409")
      ? `BattleMetrics ID \`${draft.bmId}\` is already being tracked.`
      : currentBmId
        ? "RustLens could not update that tracked server."
        : "RustLens could not add that tracked server.";

    await replyPublicEmbed(
      interaction,
      buildServerEmbed(
        currentBmId ? "Tracked Server Update Failed" : "Tracked Server Add Failed",
        description,
      ),
    );
  }

  return true;
}

export const __testables = {
  buildTrackedServerChoiceName,
  buildTrackedServerListDescription,
  buildTrackedServerModal,
  formatTrackedServerCategories,
};
