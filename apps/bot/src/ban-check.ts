import type {
  AutocompleteInteraction,
  ButtonInteraction,
  ChatInputCommandInteraction,
  ModalSubmitInteraction,
  StringSelectMenuInteraction,
} from "discord.js";
import { EmbedBuilder, MessageFlags } from "discord.js";
import { BackendClient } from "./backend-client.js";
import type { BanCategory } from "./ban-categories.js";
import { loadConfig } from "./config.js";

const BANNED_EMBED_COLOR = 0xd31239;
const CACHE_TTL_MS = 30_000;

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
  try {
    const response = await getBackend().getUserBan(getWorkerID(), discordId);
    return new Set(response.categories as BanCategory[]);
  } catch (error) {
    console.error("[bot] ban lookup failed", { discordId, error });
    return new Set<BanCategory>();
  }
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

export async function enforceBanForAutocomplete(
  interaction: AutocompleteInteraction,
  category: BanCategory,
): Promise<boolean> {
  const banned = await isUserBannedFromCategory(interaction.user.id, category);
  if (!banned) {
    return false;
  }
  try {
    await interaction.respond([]);
  } catch {
    // autocomplete can silently fail if Discord already timed out
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
  const banned = await isUserBannedFromCategory(interaction.user.id, category);
  if (!banned) {
    return false;
  }
  await replyBannedEphemeral(interaction);
  return true;
}
