import type { ButtonInteraction, ChatInputCommandInteraction, Client, StringSelectMenuInteraction, User } from "discord.js";
import { ActionRowBuilder, ApplicationIntegrationType, ButtonBuilder, ButtonStyle, EmbedBuilder, InteractionContextType, MessageFlags, SlashCommandBuilder, StringSelectMenuBuilder } from "discord.js";
import {
  BackendClient,
  type SteamAccount,
  type SteamAnalytics,
  type SteamDiscordLink,
  type SteamProfile,
} from "../backend-client.js";
import { extractBmID } from "../bm-input.js";
import { loadConfig } from "../config.js";
import {
  createEmbed,
  buildProOnlyFeatureEmbed,
  formatDiscordUserName,
} from "../shared.js";
import {
  resolveSteamInputErrorMessage,
  resolveSteamProfileInput,
} from "../steam-input.js";
import { buildProfileButtons, buildUserEmbed } from "./view.js";
import { hasProFeatureAccess, isWebsiteAdmin } from "../website-admin.js";

export const PLAYER_COMMAND_NAME = "player";

const PLAYER_SELECT_LINKED_CUSTOM_ID = "player:select:linked";
const PLAYER_SELECT_DISCORD_CUSTOM_ID = "player:select:discord";

const DISCORD_UNKNOWN_INTERACTION = 10062;

const PLAYER_EMOJIS = {
  steam: "<:steam:1444016467648581642>",
  battlemetrics: "<:battlemetrics:1444011821529956472>",
} as const;

const steamIDPattern = /^\d{17}$/;

const playerCommand = new SlashCommandBuilder()
  .setName(PLAYER_COMMAND_NAME)
  .setDescription("Look up any player by Steam ID or BattleMetrics ID")
  .setContexts(InteractionContextType.Guild, InteractionContextType.BotDM, InteractionContextType.PrivateChannel)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall, ApplicationIntegrationType.UserInstall)
  .addStringOption((opt) =>
    opt
      .setName("identifier")
      .setDescription("Steam ID, BattleMetrics ID/URL, or steamcommunity.com profile URL")
      .setRequired(false),
  )
  .addUserOption((opt) =>
    opt
      .setName("discord_user")
      .setDescription("Discord user to look up (website admins only)")
      .setRequired(false),
  );

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
    _workerID = `${config.workerId}-player`;
  }
  return { backend: _backend, workerId: _workerID! };
}

export async function ensurePlayerCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = playerCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /player command (global)");
}

export async function handlePlayerChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== PLAYER_COMMAND_NAME) {
    return false;
  }

  const identifier = interaction.options.getString("identifier", false)?.trim() ?? "";
  const discordUser = interaction.options.getUser("discord_user", false);

  if (!identifier && !discordUser) {
    if (interaction.deferred || interaction.replied) {
      await interaction.editReply({ content: "Provide either an identifier or a discord user." });
      return true;
    }

    await interaction.reply({
      content: "Provide either an identifier or a discord user.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (discordUser) {
    if (!(await deferPlayerChatInput(interaction))) {
      return true;
    }

    if (!(await hasProFeatureAccess(interaction.user.id))) {
      await interaction.editReply({
        embeds: [buildProOnlyFeatureEmbed()],
      });
      return true;
    }

    const callerIsAdmin = await isWebsiteAdmin(interaction.user.id);
    await runPlatformSyncSafely("discord", discordUser.id);
    const { embed, components } = await buildDiscordLinksEmbed(discordUser, callerIsAdmin);
    await interaction.editReply({ embeds: [embed], components });
    return true;
  }

  if (!(await deferPlayerChatInput(interaction))) {
    return true;
  }

  const lookupIdentifier = await resolveLookupIdentifier(identifier);
  if (!lookupIdentifier.ok) {
    await interaction.editReply({ content: lookupIdentifier.error });
    return true;
  }

  const lookup = await lookupAccountByIdentifier(lookupIdentifier.identifier);
  if (lookup.kind === "error") {
    await interaction.editReply({
      content: "RustLens could not look up that player right now. Try again shortly.",
    });
    return true;
  }
  if (lookup.kind === "not-found") {
    await interaction.editReply({
      content: "No record found for that Steam ID, BattleMetrics ID/URL, or Steam profile URL.",
    });
    return true;
  }

  if (lookup.kind === "bm-only") {
    const bmResult = await fetchBmOnlyPlayerProfile(lookup.bmId);
    if (bmResult.kind === "not-found") {
      await interaction.editReply({
        content: "No BattleMetrics profile found for that ID.",
      });
      return true;
    }
    if (bmResult.kind === "error") {
      await interaction.editReply({
        content: "BattleMetrics is unavailable right now. Try again shortly.",
      });
      return true;
    }
    const bmProfile = bmResult.profile;
    await interaction.editReply({
      embeds: [buildUserEmbed(bmProfile.steamId ?? "", bmProfile)],
      components: buildProfileButtons(bmProfile.steamId ?? "", bmProfile.bmId ?? lookup.bmId),
    });
    return true;
  }

  const account = lookup.account;
  const profileResult = await fetchPlayerProfile(account.steamId);
  if (profileResult.kind === "private") {
    await interaction.editReply({
      embeds: [buildPrivateProfileEmbed(account.steamId, account.bmId)],
      components: buildPrivateProfileButtons(account.steamId, account.bmId),
    });
    return true;
  }
  if (profileResult.kind === "error") {
    await interaction.editReply({ content: "RustLens could not load that player's profile right now. Try again shortly." });
    return true;
  }

  await interaction.editReply({
    embeds: [buildUserEmbed(account.steamId, profileResult.profile)],
    components: buildProfileButtons(account.steamId),
  });
  return true;
}

export async function handlePlayerSelectInteraction(interaction: StringSelectMenuInteraction) {
  if (interaction.customId === PLAYER_SELECT_DISCORD_CUSTOM_ID) {
    return handlePlayerDiscordSelectInteraction(interaction);
  }

  if (interaction.customId !== PLAYER_SELECT_LINKED_CUSTOM_ID) {
    return false;
  }

  const steamId = interaction.values[0]?.trim();
  if (!steamId || !steamIDPattern.test(steamId)) {
    await interaction.reply({
      content: "Invalid selection.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  try {
    await interaction.deferReply({ flags: MessageFlags.Ephemeral });
  } catch (error) {
    if (isUnknownInteractionError(error)) {
      logExpiredInteraction("/player select", interaction.id, interaction.createdTimestamp);
      return true;
    }
    throw error;
  }

  const lookup = await lookupAccountByIdentifier(steamId);
  if (lookup.kind === "error") {
    await interaction.editReply({ content: "RustLens could not look up that player right now. Try again shortly." });
    return true;
  }
  if (lookup.kind === "not-found" || lookup.kind === "bm-only") {
    await interaction.editReply({ content: "No record found for that Steam ID." });
    return true;
  }

  const account = lookup.account;
  const profileResult = await fetchPlayerProfile(account.steamId);
  if (profileResult.kind === "private") {
    await interaction.editReply({
      embeds: [buildPrivateProfileEmbed(account.steamId, account.bmId)],
      components: buildPrivateProfileButtons(account.steamId, account.bmId),
    });
    return true;
  }
  if (profileResult.kind === "error") {
    await interaction.editReply({ content: "RustLens could not load that player's profile right now. Try again shortly." });
    return true;
  }

  await interaction.editReply({
    embeds: [buildUserEmbed(account.steamId, profileResult.profile)],
    components: buildProfileButtons(account.steamId),
  });
  return true;
}

async function handlePlayerDiscordSelectInteraction(interaction: StringSelectMenuInteraction) {
  const discordId = interaction.values[0]?.trim();
  if (!discordId || !/^\d{16,20}$/.test(discordId)) {
    await interaction.reply({
      content: "Invalid selection.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  try {
    await interaction.deferReply({ flags: MessageFlags.Ephemeral });
  } catch (error) {
    if (isUnknownInteractionError(error)) {
      logExpiredInteraction("/player discord select", interaction.id, interaction.createdTimestamp);
      return true;
    }
    throw error;
  }

  if (!(await hasProFeatureAccess(interaction.user.id))) {
    await interaction.editReply({ embeds: [buildProOnlyFeatureEmbed()] });
    return true;
  }

  try {
    const callerIsAdmin = await isWebsiteAdmin(interaction.user.id);
    await runPlatformSyncSafely("discord", discordId);
    const discordUser = await interaction.client.users.fetch(discordId);
    const { embed, components } = await buildDiscordLinksEmbed(discordUser, callerIsAdmin);
    await interaction.editReply({ embeds: [embed], components });
  } catch (error) {
    console.error("[bot] failed to load discord links from select", { discordId, error });
    await interaction.editReply({ content: "Failed to look up that Discord user." });
  }

  return true;
}

export async function deferPlayerChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.deferred || interaction.replied) {
    return true;
  }

  const discordUser = interaction.options.getUser("discord_user", false);
  const identifier = interaction.options.getString("identifier", false)?.trim() ?? "";
  try {
    await interaction.deferReply({
      flags: discordUser || !identifier ? MessageFlags.Ephemeral : undefined,
    });
    return true;
  } catch (error) {
    if (isUnknownInteractionError(error)) {
      logExpiredInteraction("/player", interaction.id, interaction.createdTimestamp);
      return false;
    }
    throw error;
  }
}

type PlayerProfileResult =
  | { kind: "ok"; profile: SteamProfile }
  | { kind: "private" }
  | { kind: "error" };

async function fetchPlayerProfile(steamId: string): Promise<PlayerProfileResult> {
  const { backend, workerId } = getBackend();

  try {
    const profile = await backend.getSteamProfile(workerId, steamId);
    return { kind: "ok", profile };
  } catch (error) {
    if (isBmPrivateProfileError(error)) {
      return { kind: "private" };
    }
    console.error(`[bot] /player getSteamProfile failed for ${steamId}:`, error);
    return { kind: "error" };
  }
}

function isBmPrivateProfileError(error: unknown) {
  if (!error || typeof error !== "object") {
    return false;
  }

  const status = "status" in error ? error.status : undefined;
  const message = "message" in error && typeof error.message === "string" ? error.message : "";
  return status === 404 && message.includes("battlemetrics profile private");
}

function buildPrivateProfileEmbed(steamId: string, bmId: string | null) {
  const embed = createEmbed()
    .setTitle("Error")
    .setDescription("BM profile private");

  const info: string[] = [
    `${PLAYER_EMOJIS.steam} [${steamId}](https://steamcommunity.com/profiles/${steamId})`,
  ];
  if (bmId) {
    info.push(`${PLAYER_EMOJIS.battlemetrics} [${bmId}](https://www.battlemetrics.com/rcon/players/${bmId})`);
  }

  embed.addFields({ name: "Information", value: info.join("\n") });
  return embed;
}

function buildPrivateProfileButtons(steamId: string, bmId: string | null) {
  const trimmedSteam = steamId.trim();
  const trimmedBm = (bmId ?? "").trim();
  const linksId = steamIDPattern.test(trimmedSteam)
    ? `player:links:${trimmedSteam}`
    : trimmedBm
    ? `player:links-bm:${trimmedBm}`
    : "player:links:";

  return [
    new ActionRowBuilder<ButtonBuilder>().addComponents(
      new ButtonBuilder()
        .setCustomId(linksId)
        .setEmoji("🔗")
        .setStyle(ButtonStyle.Secondary),
    ),
  ];
}

async function resolveLookupIdentifier(identifier: string) {
  const { backend, workerId } = getBackend();

  try {
    const steamId = await resolveSteamProfileInput(
      identifier,
      (input) => backend.resolveSteamID(workerId, input),
    );
    return { ok: true as const, identifier: steamId ?? identifier.trim() };
  } catch (error) {
    return { ok: false as const, error: resolveSteamInputErrorMessage(error) };
  }
}

type LookupResult =
  | { kind: "steam"; account: SteamAccount }
  | { kind: "bm-only"; bmId: string }
  | { kind: "not-found" }
  | { kind: "error" };

function getErrorStatus(error: unknown): number | undefined {
  if (error && typeof error === "object" && "status" in error) {
    const value = (error as { status?: unknown }).status;
    return typeof value === "number" ? value : undefined;
  }
  return undefined;
}

async function lookupAccountByIdentifier(identifier: string): Promise<LookupResult> {
  const { backend, workerId } = getBackend();

  if (steamIDPattern.test(identifier)) {
    try {
      const account = await backend.getSteamAccount(workerId, identifier);
      return { kind: "steam", account };
    } catch (error) {
      const status = getErrorStatus(error);
      if (status === 404) {
        return { kind: "not-found" };
      }
      console.error("[bot] /player getSteamAccount failed", { steamId: identifier, error });
      return { kind: "error" };
    }
  }

  const bmID = extractBmID(identifier);
  if (bmID) {
    try {
      const account = await backend.getSteamAccountByBmID(workerId, bmID);
      return { kind: "steam", account };
    } catch (error) {
      const status = getErrorStatus(error);
      if (status === 404) {
        // No Steam linkage on file or returned by BM — fall back to BM-only.
        return { kind: "bm-only", bmId: bmID };
      }
      console.error("[bot] /player getSteamAccountByBmID failed", { bmId: bmID, error });
      return { kind: "error" };
    }
  }

  return { kind: "not-found" };
}

async function fetchBmOnlyPlayerProfile(
  bmId: string,
): Promise<
  | { kind: "ok"; profile: SteamProfile }
  | { kind: "not-found" }
  | { kind: "error" }
> {
  const { backend, workerId } = getBackend();
  try {
    const profile = await backend.getBmPlayerProfile(workerId, bmId);
    return { kind: "ok", profile };
  } catch (error) {
    const status = getErrorStatus(error);
    if (status === 404) {
      return { kind: "not-found" };
    }
    console.error("[bot] /player getBmPlayerProfile failed", { bmId, error });
    return { kind: "error" };
  }
}

export async function handlePlayerButtonInteraction(interaction: ButtonInteraction) {
  if (interaction.customId.startsWith("player:analytics-bm:")) {
    const bmId = interaction.customId.slice("player:analytics-bm:".length);
    if (!/^\d+$/.test(bmId)) {
      await replyOrEditPlayerButtonError(interaction, "Invalid analytics button.");
      return true;
    }

    if (!(await deferPlayerButton(interaction))) {
      return true;
    }

    try {
      const { backend, workerId } = getBackend();
      const analytics = await backend.getBmPlayerAnalytics(workerId, bmId);
      await interaction.editReply({ embeds: [buildAnalyticsEmbed(analytics)] });
    } catch (error) {
      console.error("[bot] failed to load player analytics (bm)", error);
      await interaction.editReply({ content: "Failed to load analytics for this player." });
    }

    return true;
  }

  if (interaction.customId.startsWith("player:analytics:")) {
    const steamId = interaction.customId.slice("player:analytics:".length);
    if (!/^\d{17}$/.test(steamId)) {
      await replyOrEditPlayerButtonError(interaction, "Invalid analytics button.");
      return true;
    }

    if (!(await deferPlayerButton(interaction))) {
      return true;
    }

    try {
      const { backend, workerId } = getBackend();
      const analytics = await backend.getSteamAnalytics(workerId, steamId);
      await interaction.editReply({ embeds: [buildAnalyticsEmbed(analytics)] });
    } catch (error) {
      console.error("[bot] failed to load player analytics", error);
      await interaction.editReply({ content: "Failed to load analytics for this player." });
    }

    return true;
  }

  if (interaction.customId.startsWith("player:links-bm:")) {
    const bmId = interaction.customId.slice("player:links-bm:".length);
    if (!/^\d+$/.test(bmId)) {
      await replyOrEditPlayerButtonError(interaction, "Invalid links button.");
      return true;
    }

    if (!(await deferPlayerButton(interaction))) {
      return true;
    }

    if (!(await hasProFeatureAccess(interaction.user.id))) {
      await interaction.editReply({ embeds: [buildProOnlyFeatureEmbed()] });
      return true;
    }

    try {
      const account = await ensureSteamAccountByBmIDSafely(bmId);
      if (account?.steamId) {
        await runPlatformSyncSafely("steam", account.steamId);
      }
      const { embed, components } = await buildBmOnlyLinksEmbed(bmId, await isWebsiteAdmin(interaction.user.id), interaction.client);
      await interaction.editReply({ embeds: [embed], components });
    } catch (error) {
      console.error("[bot] failed to load bm links", error);
      await interaction.editReply({ content: "Failed to load links for this player." });
    }

    return true;
  }

  if (interaction.customId.startsWith("player:links:")) {
    const steamId = interaction.customId.slice("player:links:".length);
    if (!/^\d{17}$/.test(steamId)) {
      await replyOrEditPlayerButtonError(interaction, "Invalid links button.");
      return true;
    }

    if (!(await deferPlayerButton(interaction))) {
      return true;
    }

    if (!(await hasProFeatureAccess(interaction.user.id))) {
      await interaction.editReply({ embeds: [buildProOnlyFeatureEmbed()] });
      return true;
    }

    try {
      await runPlatformSyncSafely("steam", steamId);
      const { embed, components } = await buildSteamLinksEmbed(steamId, await isWebsiteAdmin(interaction.user.id), interaction.client);
      await interaction.editReply({ embeds: [embed], components });
    } catch (error) {
      console.error("[bot] failed to load steam links", error);
      await interaction.editReply({ content: "Failed to load links for this player." });
    }

    return true;
  }

  return false;
}

export async function deferPlayerButton(interaction: ButtonInteraction) {
  if (!interaction.deferred && !interaction.replied) {
    try {
      await interaction.deferReply({ flags: MessageFlags.Ephemeral });
    } catch (error) {
      if (isUnknownInteractionError(error)) {
        logExpiredInteraction("player button", interaction.id, interaction.createdTimestamp);
        return false;
      }
      throw error;
    }
  }
  return true;
}

function isUnknownInteractionError(error: unknown) {
  if (typeof error !== "object" || error === null || !("code" in error)) {
    return false;
  }

  return (error as { code?: unknown }).code === DISCORD_UNKNOWN_INTERACTION;
}

function logExpiredInteraction(kind: string, id: string, createdTimestamp: number) {
  const config = loadConfig();
  console.warn("[bot] skipped expired interaction", {
    kind,
    id,
    ageMs: Date.now() - createdTimestamp,
    workerId: config.workerId,
    shardId: Number(process.env.SHARD_ID ?? "0"),
    shardCount: Number(process.env.SHARD_COUNT ?? "1"),
    pid: process.pid,
  });
}

async function replyOrEditPlayerButtonError(interaction: ButtonInteraction, content: string) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ content, components: [] });
    return;
  }
  await interaction.reply({ content, flags: MessageFlags.Ephemeral });
}

async function runPlatformSyncSafely(platform: "steam" | "discord", id: string): Promise<void> {
  const { backend, workerId } = getBackend();
  try {
    await backend.syncPlatformIdentity(workerId, platform, id);
  } catch (error) {
    console.error("[bot] platformsync sync failed", { platform, id, error });
  }
}

async function ensureSteamAccountByBmIDSafely(bmId: string): Promise<SteamAccount | null> {
  const { backend, workerId } = getBackend();
  try {
    return await backend.getSteamAccountByBmID(workerId, bmId);
  } catch {
    return null;
  }
}

async function buildBmOnlyLinksEmbed(
  bmId: string,
  isAdmin: boolean,
  client?: Client,
): Promise<{
  embed: EmbedBuilder;
  components: ActionRowBuilder<StringSelectMenuBuilder>[];
}> {
  const { backend, workerId } = getBackend();
  // Try to resolve a Steam linkage from the BM ID. If found, render the
  // standard links embed; otherwise return a graceful "no links" embed.
  let account: SteamAccount | null = null;
  try {
    account = await backend.getSteamAccountByBmID(workerId, bmId);
  } catch {
    account = null;
  }
  if (account?.steamId) {
    return buildSteamLinksEmbed(account.steamId, isAdmin, client);
  }

  const embed = createEmbed()
    .setTitle(account?.bmName ? `Links - ${account.bmName}` : "Links");

  embed.addFields({
    name: "Information",
    value: `${PLAYER_EMOJIS.battlemetrics} [${bmId}](https://www.battlemetrics.com/rcon/players/${bmId})`,
  });
  embed.addFields({ name: "Strong links", value: "> * None" });
  embed.addFields({
    name: "What do these mean?",
    value: "> * No Steam linkage on file for this BattleMetrics player, so no Discord links could be found.",
  });
  return { embed, components: [] };
}

async function buildSteamLinksEmbed(steamId: string, isAdmin: boolean, client?: Client): Promise<{
  embed: EmbedBuilder;
  components: ActionRowBuilder<StringSelectMenuBuilder>[];
}> {
  const { backend, workerId } = getBackend();

  const [account, linksResponse] = await Promise.all([
    backend.getSteamAccount(workerId, steamId).catch(() => null),
    backend.getLinksBySteamID(workerId, steamId).catch(() => ({ items: [] as SteamDiscordLink[] })),
  ]);

  const links = linksResponse.items;
  const { strong, soft } = partitionLinksByDiscord(links);
  const discordNameById = await fetchDiscordNames(client, links.map((link) => link.discordId));

  const title = account?.bmName ? `Links - ${account.bmName}` : "Links";
  const embed = createEmbed()
    .setTitle(title);

  if (account?.avatarImage) {
    embed.setThumbnail(account.avatarImage);
  }

  const info: string[] = [
    `${PLAYER_EMOJIS.steam} [${steamId}](https://steamcommunity.com/profiles/${steamId})`,
  ];
  if (account?.bmId) {
    info.push(`${PLAYER_EMOJIS.battlemetrics} [${account.bmId}](https://www.battlemetrics.com/rcon/players/${account.bmId})`);
  }
  embed.addFields({ name: "Information", value: info.join("\n") });

  embed.addFields({
    name: "Strong links",
    value: strong.length > 0
      ? strong.map((id) => `> * <@${id}>`).join("\n")
      : "> * None",
  });

  if (isAdmin) {
    embed.addFields({
      name: "Soft links",
      value: soft.length > 0
        ? soft.map((id) => `> * <@${id}>`).join("\n")
        : "> * None",
    });
    embed.addFields({
      name: "What do these mean?",
      value: "> * A **strong link** is a link that is certainly valid\n> * A **soft link** is a link that is usually valid, but can also be invalid",
    });
  } else {
    embed.addFields({
      name: "What do these mean?",
      value: "> * A **strong link** is a link that is certainly valid",
    });
  }

  return { embed, components: buildLinkedDiscordSelect(links, isAdmin, discordNameById) };
}

function partitionLinksByDiscord(links: readonly SteamDiscordLink[]) {
  const strongDiscordIds = new Set<string>();
  for (const link of links) {
    if (link.type === "hard") {
      strongDiscordIds.add(link.discordId);
    }
  }
  const softDiscordIds = new Set<string>();
  for (const link of links) {
    if (link.type === "soft" && !strongDiscordIds.has(link.discordId)) {
      softDiscordIds.add(link.discordId);
    }
  }
  return {
    strong: [...strongDiscordIds],
    soft: [...softDiscordIds],
  };
}

async function ensureSteamAccountWithBm(steamId: string): Promise<SteamAccount | null> {
  const { backend, workerId } = getBackend();
  try {
    const account = await backend.getSteamAccount(workerId, steamId);
    if (account.bmId) {
      return account;
    }
    // Try the same fallback /player uses — the backend lazily backfills bm_id
    // through the steam profile endpoint.
    try {
      await backend.getSteamProfile(workerId, steamId);
    } catch {
      // ignore — return what we have
    }
    try {
      return await backend.getSteamAccount(workerId, steamId);
    } catch {
      return account;
    }
  } catch {
    return null;
  }
}

async function buildDiscordLinksEmbed(discordUser: User, isAdmin: boolean): Promise<{
  embed: EmbedBuilder;
  components: ActionRowBuilder<StringSelectMenuBuilder>[];
}> {
  const { backend, workerId } = getBackend();

  const linksResponse = await backend
    .getLinksByDiscordID(workerId, discordUser.id)
    .catch(() => ({ items: [] as SteamDiscordLink[] }));

  const links = filterVisibleLinks(linksResponse.items, isAdmin);
  const { strongSteamIds, softSteamIds } = partitionLinksBySteam(links);

  const [strongAccounts, softAccounts] = await Promise.all([
    Promise.all(strongSteamIds.map((sid) => ensureSteamAccountWithBm(sid))),
    Promise.all(softSteamIds.map((sid) => ensureSteamAccountWithBm(sid))),
  ]);

  const discordName = formatDiscordUserName(discordUser);
  const embed = createEmbed()
    .setTitle(`Links - ${discordName}`);

  const avatarUrl = discordUser.displayAvatarURL({ size: 256 });
  if (avatarUrl) {
    embed.setThumbnail(avatarUrl);
  }

  embed.addFields({ name: "Information", value: `<@${discordUser.id}>` });
  embed.addFields({
    name: "Strong links",
    value: formatSteamAccountList(strongSteamIds, strongAccounts),
  });
  if (isAdmin) {
    embed.addFields({
      name: "Soft links",
      value: formatSteamAccountList(softSteamIds, softAccounts),
    });
    embed.addFields({
      name: "What do these mean?",
      value: "> * A **strong link** is a link that is certainly valid\n> * A **soft link** is a link that is usually valid, but can also be invalid",
    });
  } else {
    embed.addFields({
      name: "What do these mean?",
      value: "> * A **strong link** is a link that is certainly valid",
    });
  }

  const accountBySteamId = new Map<string, SteamAccount | null>();
  for (let i = 0; i < strongSteamIds.length; i++) {
    accountBySteamId.set(strongSteamIds[i], strongAccounts[i]);
  }
  for (let i = 0; i < softSteamIds.length; i++) {
    if (!accountBySteamId.has(softSteamIds[i])) {
      accountBySteamId.set(softSteamIds[i], softAccounts[i]);
    }
  }
  const components = buildLinkedAccountsSelect(links, accountBySteamId);

  return { embed, components };
}

function filterVisibleLinks(links: readonly SteamDiscordLink[], isAdmin: boolean) {
  if (isAdmin) {
    return [...links];
  }
  return links.filter((link) => link.type === "hard");
}

function buildLinkedAccountsSelect(
  links: readonly SteamDiscordLink[],
  accountBySteamId: ReadonlyMap<string, SteamAccount | null>,
): ActionRowBuilder<StringSelectMenuBuilder>[] {
  const winnerByStream = new Map<string, SteamDiscordLink>();
  for (const link of links) {
    const existing = winnerByStream.get(link.steamId);
    if (!existing) {
      winnerByStream.set(link.steamId, link);
      continue;
    }
    const upgrade =
      (existing.type === "soft" && link.type === "hard") ||
      (existing.type === link.type && link.createdAt > existing.createdAt);
    if (upgrade) {
      winnerByStream.set(link.steamId, link);
    }
  }

  const recent = [...winnerByStream.values()]
    .sort((a, b) => b.createdAt.localeCompare(a.createdAt))
    .slice(0, 25);

  if (recent.length === 0) {
    return [];
  }

  const select = new StringSelectMenuBuilder()
    .setCustomId(PLAYER_SELECT_LINKED_CUSTOM_ID)
    .setPlaceholder("Look up a linked Steam account")
    .addOptions(
      recent.map((link) => ({
        label: truncateOption(accountBySteamId.get(link.steamId)?.bmName || link.steamId, 100),
        value: link.steamId,
        description: truncateOption(`${link.type === "hard" ? "Strong" : "Soft"} link - Steam ID: ${link.steamId}`, 100),
      })),
    );

  return [new ActionRowBuilder<StringSelectMenuBuilder>().addComponents(select)];
}

function buildLinkedDiscordSelect(
  links: readonly SteamDiscordLink[],
  isAdmin: boolean,
  discordNameById: ReadonlyMap<string, string> = new Map(),
): ActionRowBuilder<StringSelectMenuBuilder>[] {
  const visibleLinks = filterVisibleLinks(links, isAdmin);
  const winnerByDiscord = new Map<string, SteamDiscordLink>();
  for (const link of visibleLinks) {
    const existing = winnerByDiscord.get(link.discordId);
    if (!existing) {
      winnerByDiscord.set(link.discordId, link);
      continue;
    }
    const upgrade =
      (existing.type === "soft" && link.type === "hard") ||
      (existing.type === link.type && link.createdAt > existing.createdAt);
    if (upgrade) {
      winnerByDiscord.set(link.discordId, link);
    }
  }

  const recent = [...winnerByDiscord.values()]
    .sort((a, b) => b.createdAt.localeCompare(a.createdAt))
    .slice(0, 25);

  if (recent.length === 0) {
    return [];
  }

  const select = new StringSelectMenuBuilder()
    .setCustomId(PLAYER_SELECT_DISCORD_CUSTOM_ID)
    .setPlaceholder("Look up a linked Discord account")
    .addOptions(
      recent.map((link) => ({
        label: truncateOption(discordNameById.get(link.discordId) || link.discordId, 100),
        value: link.discordId,
        description: truncateOption(`${link.type === "hard" ? "Strong" : "Soft"} link`, 100),
      })),
    );

  return [new ActionRowBuilder<StringSelectMenuBuilder>().addComponents(select)];
}

async function fetchDiscordNames(client: Client | undefined, discordIds: readonly string[]): Promise<Map<string, string>> {
  if (!client) {
    return new Map();
  }
  const unique = [...new Set(discordIds)];
  const entries = await Promise.all(
    unique.map(async (discordId) => {
      try {
        const user = await client.users.fetch(discordId);
        return [discordId, formatDiscordUserName(user)] as const;
      } catch {
        return null;
      }
    }),
  );

  const names = new Map<string, string>();
  for (const entry of entries) {
    if (entry) {
      names.set(entry[0], entry[1]);
    }
  }
  return names;
}

function truncateOption(value: string, max: number) {
  if (value.length <= max) {
    return value;
  }
  return value.slice(0, Math.max(0, max - 1)) + "…";
}

function partitionLinksBySteam(links: readonly SteamDiscordLink[]) {
  const strongSteamIds = new Set<string>();
  for (const link of links) {
    if (link.type === "hard") {
      strongSteamIds.add(link.steamId);
    }
  }
  const softSteamIds = new Set<string>();
  for (const link of links) {
    if (link.type === "soft" && !strongSteamIds.has(link.steamId)) {
      softSteamIds.add(link.steamId);
    }
  }
  return {
    strongSteamIds: [...strongSteamIds],
    softSteamIds: [...softSteamIds],
  };
}

function formatSteamAccountList(
  steamIds: readonly string[],
  accounts: ReadonlyArray<SteamAccount | null>,
): string {
  if (steamIds.length === 0) {
    return "> * None";
  }

  const blocks: string[] = [];
  for (let i = 0; i < steamIds.length; i++) {
    const steamId = steamIds[i];
    const account = accounts[i];
    const lines: string[] = [
      `> *  ${PLAYER_EMOJIS.steam} [${steamId}](https://steamcommunity.com/profiles/${steamId})`,
    ];
    if (account?.bmId) {
      lines.push(`> *   ${PLAYER_EMOJIS.battlemetrics} [${account.bmId}](https://www.battlemetrics.com/rcon/players/${account.bmId})`);
    }
    blocks.push(lines.join("\n"));
  }
  return blocks.join("\n");
}

function formatGapDurationDays(fromUnix: number, toUnix: number) {
  const durationSeconds = Math.max(0, toUnix - fromUnix);
  const durationDays = Math.max(1, Math.round(durationSeconds / 86400));
  return `${durationDays} day${durationDays === 1 ? "" : "s"}`;
}

export function buildAnalyticsEmbed(analytics: SteamAnalytics) {
  const title = analytics.bmName ? `Analytics - ${analytics.bmName}` : "Analytics";
  const embed = createEmbed()
    .setTitle(title);

  if (analytics.avatarImage) {
    embed.setThumbnail(analytics.avatarImage);
  }

  const info: string[] = [];
  info.push(`${PLAYER_EMOJIS.steam} [${analytics.steamId}](https://steamcommunity.com/profiles/${analytics.steamId})`);
  if (analytics.bmId) {
    info.push(`${PLAYER_EMOJIS.battlemetrics} [${analytics.bmId}](https://www.battlemetrics.com/rcon/players/${analytics.bmId})`);
  }
  embed.addFields({ name: "Information", value: info.join("\n") });

  if (analytics.gaps.length > 0) {
    const sortedGaps = [...analytics.gaps]
      .sort((a, b) => b.toUnix - a.toUnix)
      .slice(0, 10);
    const gapLines = sortedGaps.map((gap) =>
      `<t:${gap.fromUnix}:R> - <t:${gap.toUnix}:R> (${formatGapDurationDays(gap.fromUnix, gap.toUnix)})`
    );
    embed.addFields({ name: "**Did not play:**", value: gapLines.join("\n") });
  }

  if (analytics.totalGames !== null && analytics.topGames.length > 0) {
    const hasPlaytime = analytics.topGames.some((game) => game.hours > 0);
    const value = hasPlaytime
      ? analytics.topGames.map((game) => `> * ${game.name} **${game.hours}** hours`).join("\n")
      : "*Playtime is private*";
    embed.addFields({
      name: `Games owned: ${analytics.totalGames}`,
      value,
    });
  }

  if (analytics.topServers.length > 0) {
    const serverLines = analytics.topServers.map((server) => `> * ${server.name} **${server.hours}** hours`);
    embed.addFields({
      name: `Servers played: ${analytics.totalServers}`,
      value: serverLines.join("\n"),
    });
  }

  return embed;
}

export const __testables = {
  buildPrivateProfileEmbed,
  buildAnalyticsEmbed,
  buildLinkedDiscordSelect,
  filterVisibleLinks,
  isBmPrivateProfileError,
};
