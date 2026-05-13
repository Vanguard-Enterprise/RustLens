import type {
  AutocompleteInteraction,
  ButtonInteraction,
  ChatInputCommandInteraction,
  ModalSubmitInteraction,
  StringSelectMenuInteraction,
} from "discord.js";
import { EmbedBuilder, MessageFlags } from "discord.js";
import { BackendClient } from "./backend-client.js";
import { BAN_CATEGORIES, type BanCategory } from "./ban-categories.js";
import { loadConfig } from "./config.js";
import { logDiscordError } from "./shared.js";

const ALL_CATEGORIES: BanCategory[] = BAN_CATEGORIES.map((c) => c.value);

const BANNED_EMBED_COLOR = 0xd31239;
const CACHE_TTL_MS = 30_000;
const INTERACTION_BAN_LOOKUP_BUDGET_MS = 1_000;

const banLookupTimedOut = Symbol("banLookupTimedOut");

type CacheEntry = {
  categories: Set<BanCategory>;
  expiresAt: number;
};

const banCache = new Map<string, CacheEntry>();
const pendingLookups = new Map<string, Promise<Set<BanCategory>>>();

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
    _workerID = `${config.workerId}-ban`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

async function fetchBannedCategories(discordId: string): Promise<Set<BanCategory>> {
  const response = await getBackend().getUserBan(getWorkerID(), discordId);
  // A suspended website account is a blanket ban: every command category
  // returns the same "You have been banned" reply on the bot side.
  if (response.suspended) {
    return new Set<BanCategory>(ALL_CATEGORIES);
  }
  return new Set(response.categories as BanCategory[]);
}

async function loadBannedCategories(discordId: string): Promise<Set<BanCategory>> {
  const now = Date.now();
  const cached = banCache.get(discordId);
  if (cached && cached.expiresAt > now) {
    return cached.categories;
  }

  const pending = pendingLookups.get(discordId);
  if (pending) {
    return pending;
  }

  const lookup = fetchBannedCategories(discordId)
    .then((categories) => {
      banCache.set(discordId, {
        categories,
        expiresAt: Date.now() + CACHE_TTL_MS,
      });
      return categories;
    })
    .finally(() => {
      pendingLookups.delete(discordId);
    });

  pendingLookups.set(discordId, lookup);
  return lookup;
}

async function loadBannedCategoriesWithinInteractionBudget(discordId: string) {
  return Promise.race([
    loadBannedCategories(discordId),
    new Promise<typeof banLookupTimedOut>((resolve) => {
      setTimeout(() => resolve(banLookupTimedOut), INTERACTION_BAN_LOOKUP_BUDGET_MS);
    }),
  ]);
}

export function invalidateBanCache(discordId: string) {
  banCache.delete(discordId);
}

export async function isUserBannedFromCategory(discordId: string, category: BanCategory): Promise<boolean> {
  const categories = await loadBannedCategories(discordId);
  return categories.has(category);
}

function buildBannedEmbed() {
  return new EmbedBuilder()
    .setTitle("Error")
    .setDescription("You have been banned")
    .setColor(BANNED_EMBED_COLOR);
}

function buildBanLookupUnavailableEmbed() {
  return new EmbedBuilder()
    .setTitle("Error")
    .setDescription("Ban service is unavailable. Try again shortly.")
    .setColor(BANNED_EMBED_COLOR);
}

export async function replyBannedEphemeral(
  interaction:
    | ChatInputCommandInteraction
    | ButtonInteraction
    | ModalSubmitInteraction
    | StringSelectMenuInteraction,
) {
  const embed = buildBannedEmbed();
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ embeds: [embed], components: [] });
    return;
  }
  await interaction.reply({ embeds: [embed], flags: MessageFlags.Ephemeral });
}

async function replyBanUnavailableEphemeral(
  interaction:
    | ChatInputCommandInteraction
    | ButtonInteraction
    | ModalSubmitInteraction
    | StringSelectMenuInteraction,
) {
  const embed = buildBanLookupUnavailableEmbed();
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ embeds: [embed], components: [] });
    return;
  }
  await interaction.reply({ embeds: [embed], flags: MessageFlags.Ephemeral });
}

export async function enforceBanForAutocomplete(
  interaction: AutocompleteInteraction,
  category: BanCategory,
): Promise<boolean> {
  let categories: Set<BanCategory> | typeof banLookupTimedOut;
  try {
    categories = await loadBannedCategoriesWithinInteractionBudget(interaction.user.id);
  } catch (error) {
    console.error("[bot] ban lookup failed (autocomplete; failing closed)", {
      discordId: interaction.user.id,
      error,
    });
    try {
      await interaction.respond([]);
    } catch (error) {
      logDiscordError("respond to banned autocomplete after lookup failure", error, {
        discordId: interaction.user.id,
        category,
      });
    }
    return true;
  }

  if (categories === banLookupTimedOut) {
    console.warn("[bot] ban lookup timed out before autocomplete acknowledgement", {
      discordId: interaction.user.id,
      category,
      budgetMs: INTERACTION_BAN_LOOKUP_BUDGET_MS,
    });
    return false;
  }

  if (!categories.has(category)) {
    return false;
  }
  try {
    await interaction.respond([]);
  } catch (error) {
    logDiscordError("respond to banned autocomplete", error, {
      discordId: interaction.user.id,
      category,
    });
  }
  return true;
}

export async function enforceBan(
  interaction:
    | ChatInputCommandInteraction
    | ButtonInteraction
    | ModalSubmitInteraction
    | StringSelectMenuInteraction,
  category: BanCategory,
): Promise<boolean> {
  let categories: Set<BanCategory> | typeof banLookupTimedOut;
  try {
    categories = await loadBannedCategoriesWithinInteractionBudget(interaction.user.id);
  } catch (error) {
    console.error("[bot] ban lookup failed (failing closed)", {
      discordId: interaction.user.id,
      error,
    });
    await replyBanUnavailableEphemeral(interaction);
    return true;
  }

  if (categories === banLookupTimedOut) {
    console.warn("[bot] ban lookup timed out before interaction acknowledgement", {
      discordId: interaction.user.id,
      category,
      budgetMs: INTERACTION_BAN_LOOKUP_BUDGET_MS,
    });
    return false;
  }

  if (!categories.has(category)) {
    return false;
  }
  await replyBannedEphemeral(interaction);
  return true;
}
