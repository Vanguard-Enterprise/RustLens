import type {
  AutocompleteInteraction,
  ButtonInteraction,
  ChatInputCommandInteraction,
  Client,
  ModalSubmitInteraction,
} from "discord.js";
import { ActionRowBuilder, ButtonBuilder, ButtonStyle, EmbedBuilder, MessageFlags, SlashCommandBuilder } from "discord.js";
import { isValidCategory, type BanCategory } from "./ban-categories.js";
import {
  BAN_CATEGORIES_FIELD_CUSTOM_ID,
  BAN_MODAL_PREFIX,
  buildBanEmbed,
  buildBanModal,
  formatCategoriesDescription,
  parseTargetFromModalCustomId,
} from "./ban-command.js";
import { invalidateBanCache } from "./ban-check.js";
import { BackendClient, type OwnerProxy, type TrackedServer } from "./backend-client.js";
import { CheckboxGroupBuilder, ModalV2Builder, RadioGroupBuilder, TextInputBuilder } from "./builders/index.js";
import { loadConfig } from "./config.js";
import { PAGE_NEXT_EMOJI, PAGE_PREVIOUS_EMOJI } from "./embed-paginator.js";
import {
  getOwnerGuildID,
  isAuthorizedOwner,
  isOwnerCommandConfigured,
} from "./owner-guard.js";
import {
  buildOwnerProxyAddModal,
  buildOwnerProxyListEmbed,
  buildOwnerStatsEmbed,
  OWNER_PROXY_ADD_FIELD_CUSTOM_ID,
  OWNER_PROXY_ADD_MODAL_CUSTOM_ID,
  parseOwnerProxyInput,
  proxyChoiceLabel,
} from "./owner-command.js";
import { createEmbed, buildFooter, FOOTER_TEXT, getFieldValues } from "./shared.js";

export const MANAGE_COMMAND_NAME = "manage";

const SERVER_GROUP = "server";
const PROXY_GROUP = "proxy";
const STATS_SUB = "stats";
const BAN_SUB = "ban";

const SERVER_ADD_SUB = "add";
const SERVER_EDIT_SUB = "edit";
const SERVER_REMOVE_SUB = "remove";
const SERVER_LIST_SUB = "list";
const SERVER_TARGET_OPTION = "server";

const PROXY_ADD_SUB = "add";
const PROXY_EDIT_SUB = "edit";
const PROXY_LIST_SUB = "list";
const PROXY_REMOVE_SUB = "remove";
const PROXY_TARGET_OPTION = "proxy";

const BAN_USER_OPTION = "user";

// Modal custom IDs for /manage server add/edit. Kept distinct from any legacy
// /server modal IDs so old in-flight modals from before the rename are ignored.
const SERVER_MODAL_PREFIX = "manage_server_modal:";
const SERVER_MODAL_ADD_CUSTOM_ID = `${SERVER_MODAL_PREFIX}add`;
const SERVER_MODAL_EDIT_PREFIX = `${SERVER_MODAL_PREFIX}edit:`;
const SERVER_NAME_FIELD = "server_name";
const SERVER_BM_ID_FIELD = "server_bm_id";
const SERVER_CATEGORIES_FIELD = "server_categories";
const SERVER_FREQUENCY_FIELD = "server_frequency";
const SERVER_LIST_PAGE_SIZE = 20;
const SERVER_LIST_PAGE_PREFIX = "manage:server:list:page:";

const PROXY_MODAL_EDIT_PREFIX = "manage_proxy_modal:edit:";
const PROXY_ENABLED_FIELD = "proxy_enabled";

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

type TrackedServerDraft = Omit<TrackedServer, "lastPolledAt" | "lastPollError">;

const manageCommand = new SlashCommandBuilder()
  .setName(MANAGE_COMMAND_NAME)
  .setDescription("Owner-only RustLens management commands")
  .setDMPermission(false)
  .addSubcommandGroup((group) =>
    group
      .setName(SERVER_GROUP)
      .setDescription("Manage tracked BattleMetrics servers")
      .addSubcommand((sub) => sub.setName(SERVER_ADD_SUB).setDescription("Add a tracked BattleMetrics server"))
      .addSubcommand((sub) =>
        sub
          .setName(SERVER_EDIT_SUB)
          .setDescription("Edit a tracked BattleMetrics server")
          .addStringOption((opt) =>
            opt
              .setName(SERVER_TARGET_OPTION)
              .setDescription("Tracked server")
              .setRequired(true)
              .setAutocomplete(true),
          ),
      )
      .addSubcommand((sub) =>
        sub
          .setName(SERVER_REMOVE_SUB)
          .setDescription("Remove a tracked BattleMetrics server")
          .addStringOption((opt) =>
            opt
              .setName(SERVER_TARGET_OPTION)
              .setDescription("Tracked server")
              .setRequired(true)
              .setAutocomplete(true),
          ),
      )
      .addSubcommand((sub) => sub.setName(SERVER_LIST_SUB).setDescription("List tracked BattleMetrics servers")),
  )
  .addSubcommandGroup((group) =>
    group
      .setName(PROXY_GROUP)
      .setDescription("Manage outbound proxies")
      .addSubcommand((sub) => sub.setName(PROXY_ADD_SUB).setDescription("Add encrypted outbound proxies"))
      .addSubcommand((sub) =>
        sub
          .setName(PROXY_EDIT_SUB)
          .setDescription("Enable or disable an outbound proxy")
          .addStringOption((opt) =>
            opt
              .setName(PROXY_TARGET_OPTION)
              .setDescription("Proxy to edit")
              .setRequired(true)
              .setAutocomplete(true),
          ),
      )
      .addSubcommand((sub) => sub.setName(PROXY_LIST_SUB).setDescription("List outbound proxies"))
      .addSubcommand((sub) =>
        sub
          .setName(PROXY_REMOVE_SUB)
          .setDescription("Remove an outbound proxy")
          .addStringOption((opt) =>
            opt
              .setName(PROXY_TARGET_OPTION)
              .setDescription("Proxy to remove")
              .setRequired(true)
              .setAutocomplete(true),
          ),
      ),
  )
  .addSubcommand((sub) => sub.setName(STATS_SUB).setDescription("Show owner-only RustLens bot statistics"))
  .addSubcommand((sub) =>
    sub
      .setName(BAN_SUB)
      .setDescription("Restrict a user from RustLens command categories")
      .addUserOption((opt) =>
        opt.setName(BAN_USER_OPTION).setDescription("Discord user to restrict").setRequired(true),
      ),
  );

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
    _workerID = `${config.workerId}-manage`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function trimChoiceLabel(value: string, maxLength = 100) {
  return value.length <= maxLength ? value : `${value.slice(0, maxLength - 3)}...`;
}

function buildTrackedServerChoiceName(server: Pick<TrackedServer, "bmId" | "name">) {
  return trimChoiceLabel(`${server.name} (${server.bmId})`);
}

function buildEmbed(title: string, description: string) {
  return createEmbed()
    .setTitle(title)
    .setDescription(description);
}

function buildTrackedServerModal(server?: TrackedServer) {
  const title = server ? "Edit Tracked Server" : "Add Tracked Server";
  const customId = server ? `${SERVER_MODAL_EDIT_PREFIX}${server.bmId}` : SERVER_MODAL_ADD_CUSTOM_ID;

  return new ModalV2Builder()
    .setTitle(title)
    .setCustomId(customId)
    .addComponent(
      "Server Name",
      new TextInputBuilder()
        .setCustomId(SERVER_NAME_FIELD)
        .setRequired(true)
        .setMaxLength(100)
        .setPlaceholder("Vitality EU Main")
        .setValue(server?.name ?? ""),
    )
    .addComponent(
      "BattleMetrics ID",
      new TextInputBuilder()
        .setCustomId(SERVER_BM_ID_FIELD)
        .setRequired(true)
        .setMaxLength(32)
        .setPlaceholder("16741517")
        .setValue(server?.bmId ?? ""),
    )
    .addComponent(
      "Server Categories",
      new CheckboxGroupBuilder()
        .setCustomId(SERVER_CATEGORIES_FIELD)
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
    )
    .addComponent(
      "Poll Frequency",
      new CheckboxGroupBuilder()
        .setCustomId(SERVER_FREQUENCY_FIELD)
        .setRequired(false)
        .setMinValues(0)
        .setMaxValues(1)
        .addOptions({
          label: "High Frequency",
          value: "high_frequency",
          description: "Poll this server in the 3 high-frequency slots.",
          default: server?.highFrequency ?? false,
        }),
    );
}

function buildProxyEditModal(proxy: OwnerProxy) {
  return new ModalV2Builder()
    .setTitle("Edit Outbound Proxy")
    .setCustomId(`${PROXY_MODAL_EDIT_PREFIX}${proxy.id}`)
    .addComponent(
      proxyChoiceLabel(proxy),
      new RadioGroupBuilder()
        .setCustomId(PROXY_ENABLED_FIELD)
        .addOptions(
          {
            label: "Enabled",
            value: "enabled",
            description: "Use this proxy for outbound BattleMetrics requests.",
            default: proxy.enabled,
          },
          {
            label: "Disabled",
            value: "disabled",
            description: "Keep this proxy saved but out of rotation.",
            default: !proxy.enabled,
          },
        ),
    );
}

function formatTrackedServerCategories(server: Pick<TrackedServer, "isVitality" | "isAtlas" | "isVital" | "isModded" | "isOfficial" | "isBuild">) {
  const labels = TRACKED_SERVER_CATEGORY_DEFINITIONS
    .filter((category) => server[category.key])
    .map((category) => category.label);
  return labels.length > 0 ? labels.join(", ") : "None";
}

function formatTrackedServerFrequency(server: Pick<TrackedServer, "highFrequency">) {
  return server.highFrequency ? "High" : "Low";
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

function buildTrackedServerListDescription(items: TrackedServer[], page = 0) {
  if (items.length === 0) {
    return "No tracked BattleMetrics servers configured.";
  }

  let description = items.map((item, index) =>
    `${page * SERVER_LIST_PAGE_SIZE + index + 1}. **${item.name}**\n` +
    `BM: \`${item.bmId}\` | Categories: ${formatTrackedServerCategories(item)}\n` +
    `Frequency: ${formatTrackedServerFrequency(item)}\n` +
    `${formatTrackedServerPollStatus(item)}`
  ).join("\n\n");

  if (description.length > 4096) {
    description = `${description.slice(0, 4093)}...`;
  }

  return description;
}

function buildTrackedServerListEmbed(items: TrackedServer[], page: number) {
  const totalPages = Math.max(1, Math.ceil(items.length / SERVER_LIST_PAGE_SIZE));
  const normalizedPage = Math.max(0, Math.min(page, totalPages - 1));
  const start = normalizedPage * SERVER_LIST_PAGE_SIZE;
  const pageItems = items.slice(start, start + SERVER_LIST_PAGE_SIZE);

  return buildEmbed("Tracked Servers", buildTrackedServerListDescription(pageItems, normalizedPage))
    .setFooter(buildFooter(`${FOOTER_TEXT} • Page ${normalizedPage + 1}/${totalPages}`));
}

function buildTrackedServerListButtons(page: number, totalPages: number) {
  if (totalPages <= 1) {
    return [];
  }

  return [
    new ActionRowBuilder<ButtonBuilder>().addComponents(
      new ButtonBuilder()
        .setCustomId(`${SERVER_LIST_PAGE_PREFIX}${page - 1}`)
        .setEmoji(PAGE_PREVIOUS_EMOJI)
        .setStyle(ButtonStyle.Secondary)
        .setDisabled(page <= 0),
      new ButtonBuilder()
        .setCustomId(`${SERVER_LIST_PAGE_PREFIX}${page + 1}`)
        .setEmoji(PAGE_NEXT_EMOJI)
        .setStyle(ButtonStyle.Secondary)
        .setDisabled(page >= totalPages - 1),
    ),
  ];
}

function parseTrackedServerListPageCustomId(customId: string) {
  if (!customId.startsWith(SERVER_LIST_PAGE_PREFIX)) {
    return null;
  }

  const page = Number(customId.slice(SERVER_LIST_PAGE_PREFIX.length));
  return Number.isInteger(page) && page >= 0 ? page : null;
}

function buildTrackedServerListResponse(items: TrackedServer[], page: number) {
  const totalPages = Math.max(1, Math.ceil(items.length / SERVER_LIST_PAGE_SIZE));
  const normalizedPage = Math.max(0, Math.min(page, totalPages - 1));
  return {
    embed: buildTrackedServerListEmbed(items, normalizedPage),
    components: buildTrackedServerListButtons(normalizedPage, totalPages),
  };
}

function buildTrackedServerDraft(name: string, bmId: string, selectedCategories: readonly string[], selectedFrequency: readonly string[]): TrackedServerDraft {
  const categories = new Set(selectedCategories);
  return {
    name: name.trim().split(/\s+/).filter((part) => part.length > 0).join(" "),
    bmId: bmId.trim(),
    isVitality: categories.has("vitality"),
    isAtlas: categories.has("atlas"),
    isVital: categories.has("vital"),
    isModded: categories.has("modded"),
    isOfficial: categories.has("official"),
    isBuild: categories.has("build"),
    highFrequency: selectedFrequency.includes("high_frequency"),
  };
}

function validateTrackedServerDraft(draft: TrackedServerDraft) {
  if (!draft.bmId) return "BattleMetrics server ID is required.";
  if (!draft.name) return "Tracked server name is required.";
  return null;
}

async function fetchTrackedServers() {
  return getBackend().listTrackedServers(getWorkerID());
}

async function findOwnerProxyById(id: number) {
  const { items } = await getBackend().listOwnerProxies(getWorkerID());
  return items.find((item) => item.id === id) ?? null;
}

async function findTrackedServerByBmId(bmId: string) {
  const { items } = await fetchTrackedServers();
  return items.find((item) => item.bmId === bmId) ?? null;
}

async function replyEphemeralEmbed(
  interaction: ChatInputCommandInteraction | ModalSubmitInteraction,
  embed: EmbedBuilder,
) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ embeds: [embed] });
    return;
  }
  await interaction.reply({ embeds: [embed], flags: MessageFlags.Ephemeral });
}

async function replyText(
  interaction: ChatInputCommandInteraction | ModalSubmitInteraction,
  content: string,
) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ content, embeds: [] });
    return;
  }
  await interaction.reply({ content, flags: MessageFlags.Ephemeral });
}

async function deleteOwnerGuildCommand(client: Client, guildId: string, name: string) {
  const globalCommands = await client.application!.commands.fetch();
  const existingGlobal = globalCommands.find((command) => command.name === name);
  if (existingGlobal) {
    await client.application!.commands.delete(existingGlobal.id);
    console.log(`[bot] removed legacy global /${name} command`);
  }

  const commands = await client.application!.commands.fetch({ guildId });
  const existing = commands.find((command) => command.name === name);
  if (existing) {
    await client.application!.commands.delete(existing.id, guildId);
    console.log(`[bot] removed legacy /${name} command from owner guild ${guildId}`);
  }
}

export async function ensureManageCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  if (!isOwnerCommandConfigured()) {
    console.warn("[bot] skipping /manage command registration because BOT_OWNER_GUILD_ID or BOT_OWNER_DISCORD_IDS is missing");
    return;
  }

  const guildId = getOwnerGuildID();

  // Remove the legacy commands replaced by /manage subcommands. /server is now
  // a global admin command, so we only delete its old owner-guild registration
  // here; ensureServerCommand re-creates it globally.
  for (const legacyName of ["server", "stats", "proxy", "ban", "owner"]) {
    try {
      await deleteOwnerGuildCommand(client, guildId, legacyName);
    } catch (error) {
      console.warn(`[bot] failed to remove legacy /${legacyName} command; continuing`, error);
    }
  }

  const data = manageCommand.toJSON();
  const commands = await client.application.commands.fetch({ guildId });
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data, guildId);
  } else {
    await client.application.commands.create(data, guildId);
  }

  console.log(`[bot] ensured /manage command in owner guild ${guildId}`);
}

export async function handleManageAutocomplete(interaction: AutocompleteInteraction) {
  if (interaction.commandName !== MANAGE_COMMAND_NAME) {
    return false;
  }

  if (!isOwnerCommandConfigured() || !isAuthorizedOwner(interaction)) {
    await interaction.respond([]);
    return true;
  }

  const group = interaction.options.getSubcommandGroup(false);
  const subcommand = interaction.options.getSubcommand(false);
  const focused = interaction.options.getFocused(true);

  try {
    if (group === SERVER_GROUP && (subcommand === SERVER_EDIT_SUB || subcommand === SERVER_REMOVE_SUB) && focused.name === SERVER_TARGET_OPTION) {
      const { items } = await fetchTrackedServers();
      const query = focused.value.trim().toLocaleLowerCase();
      const filtered = items
        .filter((item) =>
          query.length === 0 ||
          item.name.toLocaleLowerCase().includes(query) ||
          item.bmId.includes(query),
        )
        .slice(0, 25)
        .map((item) => ({ name: buildTrackedServerChoiceName(item), value: item.bmId }));
      await interaction.respond(filtered);
      return true;
    }

    if (group === PROXY_GROUP && (subcommand === PROXY_EDIT_SUB || subcommand === PROXY_REMOVE_SUB) && focused.name === PROXY_TARGET_OPTION) {
      const { items } = await getBackend().listOwnerProxies(getWorkerID());
      const focusedValue = focused.value.toLowerCase();
      const choices = items
        .filter((proxy) => proxyChoiceLabel(proxy).toLowerCase().includes(focusedValue))
        .slice(0, 25)
        .map((proxy) => ({
          name: trimChoiceLabel(`${proxy.enabled ? "Enabled" : "Disabled"} · ${proxyChoiceLabel(proxy)}`),
          value: String(proxy.id),
        }));
      await interaction.respond(choices);
      return true;
    }
  } catch (error) {
    console.error("[bot] /manage autocomplete failed", error);
  }

  await interaction.respond([]);
  return true;
}

async function handleManageServer(interaction: ChatInputCommandInteraction, subcommand: string) {
  if (subcommand === SERVER_ADD_SUB) {
    await interaction.showModal(buildTrackedServerModal().toJSON() as any);
    return;
  }

  if (subcommand === SERVER_EDIT_SUB) {
    const bmId = interaction.options.getString(SERVER_TARGET_OPTION, true).trim();
    const server = await findTrackedServerByBmId(bmId);
    if (!server) {
      await replyText(interaction, "That tracked server could not be found.");
      return;
    }
    await interaction.showModal(buildTrackedServerModal(server).toJSON() as any);
    return;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  if (subcommand === SERVER_REMOVE_SUB) {
    const bmId = interaction.options.getString(SERVER_TARGET_OPTION, true).trim();
    const server = await findTrackedServerByBmId(bmId);
    try {
      await getBackend().deleteTrackedServer(getWorkerID(), bmId);
      await replyEphemeralEmbed(
        interaction,
        buildEmbed(
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
      await replyEphemeralEmbed(interaction, buildEmbed("Tracked Server Remove Failed", description));
    }
    return;
  }

  if (subcommand === SERVER_LIST_SUB) {
    try {
      const { items } = await fetchTrackedServers();
      const response = buildTrackedServerListResponse(items, 0);
      await interaction.editReply({ embeds: [response.embed], components: response.components });
    } catch {
      await replyEphemeralEmbed(
        interaction,
        buildEmbed("Tracked Server List Failed", "RustLens could not load the tracked server list."),
      );
    }
    return;
  }

  await replyText(interaction, "Unknown subcommand.");
}

async function handleManageProxy(interaction: ChatInputCommandInteraction, subcommand: string) {
  if (subcommand === PROXY_ADD_SUB) {
    await interaction.showModal(buildOwnerProxyAddModal());
    return;
  }

  if (subcommand === PROXY_EDIT_SUB) {
    const rawID = interaction.options.getString(PROXY_TARGET_OPTION, true);
    const proxyID = Number(rawID);
    if (!Number.isInteger(proxyID) || proxyID <= 0) {
      await replyText(interaction, "Select a proxy from autocomplete.");
      return;
    }
    const proxy = await findOwnerProxyById(proxyID);
    if (!proxy) {
      await replyText(interaction, "That proxy could not be found.");
      return;
    }
    await interaction.showModal(buildProxyEditModal(proxy).toJSON() as any);
    return;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  if (subcommand === PROXY_LIST_SUB) {
    try {
      const { items } = await getBackend().listOwnerProxies(getWorkerID());
      await interaction.editReply({ embeds: [buildOwnerProxyListEmbed(items)] });
    } catch (error) {
      console.error("[bot] /manage proxy list failed", error);
      await interaction.editReply({ content: "Failed to load proxies." });
    }
    return;
  }

  if (subcommand === PROXY_REMOVE_SUB) {
    const rawID = interaction.options.getString(PROXY_TARGET_OPTION, true);
    const proxyID = Number(rawID);
    if (!Number.isInteger(proxyID) || proxyID <= 0) {
      await interaction.editReply({ content: "Select a proxy from autocomplete." });
      return;
    }
    try {
      await getBackend().deleteOwnerProxy(getWorkerID(), proxyID);
      await interaction.editReply({ content: "Removed proxy." });
    } catch (error) {
      console.error("[bot] /manage proxy remove failed", error);
      await interaction.editReply({ content: "Failed to remove proxy." });
    }
    return;
  }

  await interaction.editReply({ content: "Unknown subcommand." });
}

async function handleManageStats(interaction: ChatInputCommandInteraction) {
  await interaction.deferReply();
  try {
    const stats = await getBackend().ownerStats(getWorkerID());
    await interaction.editReply({ embeds: [buildOwnerStatsEmbed(stats)] });
  } catch (error) {
    console.error("[bot] /manage stats failed", error);
    await interaction.editReply({ content: "Failed to load bot statistics." });
  }
}

async function handleManageBan(interaction: ChatInputCommandInteraction) {
  const targetUser = interaction.options.getUser(BAN_USER_OPTION, true);
  if (targetUser.bot) {
    await replyText(interaction, "You cannot ban a bot account.");
    return;
  }

  let current: string[] = [];
  try {
    const response = await getBackend().getUserBan(getWorkerID(), targetUser.id);
    current = response.categories ?? [];
  } catch (error) {
    console.error("[bot] /manage ban failed to load current bans", { targetId: targetUser.id, error });
    await replyText(interaction, "RustLens could not load current restrictions for that user.");
    return;
  }

  const modal = buildBanModal(targetUser.id, new Set(current));
  await interaction.showModal(modal.toJSON() as any);
}

export async function handleManageChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== MANAGE_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyText(interaction, "This command can only be used in a server.");
    return true;
  }

  if (!isOwnerCommandConfigured()) {
    await replyText(interaction, "BOT_OWNER_GUILD_ID and BOT_OWNER_DISCORD_IDS must be configured before /manage can be used.");
    return true;
  }

  if (!isAuthorizedOwner(interaction)) {
    await replyText(interaction, "Only the configured bot owner can use /manage from the owner guild.");
    return true;
  }

  const group = interaction.options.getSubcommandGroup(false);
  const subcommand = interaction.options.getSubcommand(true);

  if (group === SERVER_GROUP) {
    await handleManageServer(interaction, subcommand);
    return true;
  }

  if (group === PROXY_GROUP) {
    await handleManageProxy(interaction, subcommand);
    return true;
  }

  if (subcommand === STATS_SUB) {
    await handleManageStats(interaction);
    return true;
  }

  if (subcommand === BAN_SUB) {
    await handleManageBan(interaction);
    return true;
  }

  await replyText(interaction, "Unknown command.");
  return true;
}

export async function handleManageButtonInteraction(interaction: ButtonInteraction) {
  const page = parseTrackedServerListPageCustomId(interaction.customId);
  if (page === null) {
    return false;
  }

  if (!isOwnerCommandConfigured() || !isAuthorizedOwner(interaction)) {
    await interaction.reply({ content: "Only the configured bot owner can use this control.", flags: MessageFlags.Ephemeral });
    return true;
  }

  try {
    const { items } = await fetchTrackedServers();
    const response = buildTrackedServerListResponse(items, page);
    await interaction.update({ embeds: [response.embed], components: response.components });
  } catch (error) {
    console.error("[bot] /manage server list page failed", error);
    await interaction.reply({ content: "RustLens could not load the tracked server list.", flags: MessageFlags.Ephemeral });
  }

  return true;
}

async function handleTrackedServerModalSubmit(interaction: ModalSubmitInteraction) {
  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  const name = getFieldValues(interaction, SERVER_NAME_FIELD)[0] ?? "";
  const bmId = getFieldValues(interaction, SERVER_BM_ID_FIELD)[0] ?? "";
  const categories = getFieldValues(interaction, SERVER_CATEGORIES_FIELD);
  const frequency = getFieldValues(interaction, SERVER_FREQUENCY_FIELD);
  const draft = buildTrackedServerDraft(name, bmId, categories, frequency);

  const validationError = validateTrackedServerDraft(draft);
  if (validationError) {
    await replyEphemeralEmbed(interaction, buildEmbed("Tracked Server Save Failed", validationError));
    return;
  }

  const currentBmId = interaction.customId.startsWith(SERVER_MODAL_EDIT_PREFIX)
    ? interaction.customId.slice(SERVER_MODAL_EDIT_PREFIX.length)
    : null;

  try {
    const saved = currentBmId
      ? await getBackend().updateTrackedServer(getWorkerID(), currentBmId, draft)
      : await getBackend().createTrackedServer(getWorkerID(), draft);

    await replyEphemeralEmbed(
      interaction,
      buildEmbed(
        currentBmId ? "Tracked Server Updated" : "Tracked Server Added",
        `**${saved.name}**\nBattleMetrics ID: \`${saved.bmId}\`\nCategories: ${formatTrackedServerCategories(saved)}\nFrequency: ${formatTrackedServerFrequency(saved)}`,
      ),
    );
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    const description = message.includes("409")
      ? `BattleMetrics ID \`${draft.bmId}\` is already being tracked.`
      : currentBmId
        ? "RustLens could not update that tracked server."
        : "RustLens could not add that tracked server.";

    await replyEphemeralEmbed(
      interaction,
      buildEmbed(currentBmId ? "Tracked Server Update Failed" : "Tracked Server Add Failed", description),
    );
  }
}

async function handleProxyAddModalSubmit(interaction: ModalSubmitInteraction) {
  const endpoints = parseOwnerProxyInput(interaction.fields.getTextInputValue(OWNER_PROXY_ADD_FIELD_CUSTOM_ID));
  if (endpoints.length === 0) {
    await interaction.reply({ content: "Enter at least one proxy URL.", flags: MessageFlags.Ephemeral });
    return;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  const added: { endpoint: string }[] = [];
  const failed: string[] = [];
  for (const endpoint of endpoints) {
    try {
      added.push(await getBackend().createOwnerProxy(getWorkerID(), endpoint));
    } catch (error) {
      const status = typeof error === "object" && error !== null && "status" in error
        ? (error as { status?: unknown }).status
        : undefined;
      failed.push(status === 409 ? `${endpoint} (already exists)` : endpoint);
    }
  }

  const lines = [`Added ${added.length}/${endpoints.length} proxies.`];
  if (added.length > 0) {
    lines.push(`Saved: ${added.map((proxy) => `\`${proxyChoiceLabel(proxy as never)}\``).join(", ")}`);
  }
  if (failed.length > 0) {
    const detail = failed.map((endpoint) => `\`${endpoint.replace(/^https?:\/\//, "")}\``).join(", ");
    lines.push(`Failed: ${detail.length <= 1500 ? detail : `${detail.slice(0, 1497)}...`}`);
  }

  await interaction.editReply({ content: lines.join("\n") });
}

async function handleProxyEditModalSubmit(interaction: ModalSubmitInteraction) {
  const rawID = interaction.customId.slice(PROXY_MODAL_EDIT_PREFIX.length);
  const proxyID = Number(rawID);
  if (!Number.isInteger(proxyID) || proxyID <= 0) {
    await replyText(interaction, "Invalid proxy edit modal target.");
    return;
  }

  const selected = getFieldValues(interaction, PROXY_ENABLED_FIELD)[0];
  if (selected !== "enabled" && selected !== "disabled") {
    await replyText(interaction, "Choose whether the proxy should be enabled or disabled.");
    return;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });
  try {
    const proxy = await getBackend().updateOwnerProxy(getWorkerID(), proxyID, selected === "enabled");
    await interaction.editReply({ content: `Proxy \`${proxyChoiceLabel(proxy)}\` is now ${proxy.enabled ? "enabled" : "disabled"}.` });
  } catch (error) {
    console.error("[bot] /manage proxy edit failed", error);
    await interaction.editReply({ content: "Failed to update proxy." });
  }
}

async function handleBanModalSubmitInternal(interaction: ModalSubmitInteraction) {
  const targetID = parseTargetFromModalCustomId(interaction.customId);
  if (!targetID) {
    await replyText(interaction, "Invalid ban modal target.");
    return;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

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
    console.error("[bot] /manage ban save failed", { targetID, error });
    await replyEphemeralEmbed(
      interaction,
      buildBanEmbed("Restriction Save Failed", "RustLens could not save the restrictions for that user."),
    );
    return;
  }

  invalidateBanCache(targetID);

  const title = selected.length === 0 ? "User unrestricted" : "User restrictions updated";
  const description =
    selected.length === 0
      ? `Removed all restrictions for <@${targetID}>.`
      : `Applied restrictions to <@${targetID}>:\n${formatCategoriesDescription(selected)}`;

  await replyEphemeralEmbed(interaction, buildBanEmbed(title, description));
}

export async function handleManageModalSubmit(interaction: ModalSubmitInteraction) {
  const customId = interaction.customId;
  const isManageServerModal =
    customId === SERVER_MODAL_ADD_CUSTOM_ID || customId.startsWith(SERVER_MODAL_EDIT_PREFIX);
  const isProxyAddModal = customId === OWNER_PROXY_ADD_MODAL_CUSTOM_ID;
  const isProxyEditModal = customId.startsWith(PROXY_MODAL_EDIT_PREFIX);
  const isBanModal = customId.startsWith(BAN_MODAL_PREFIX);

  if (!isManageServerModal && !isProxyAddModal && !isProxyEditModal && !isBanModal) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyText(interaction, "This command can only be used in a server.");
    return true;
  }

  if (!isOwnerCommandConfigured() || !isAuthorizedOwner(interaction)) {
    await replyText(interaction, "Only the configured bot owner can use /manage modals.");
    return true;
  }

  if (isManageServerModal) {
    await handleTrackedServerModalSubmit(interaction);
    return true;
  }

  if (isProxyAddModal) {
    await handleProxyAddModalSubmit(interaction);
    return true;
  }

  if (isProxyEditModal) {
    await handleProxyEditModalSubmit(interaction);
    return true;
  }

  if (isBanModal) {
    await handleBanModalSubmitInternal(interaction);
    return true;
  }

  return false;
}
