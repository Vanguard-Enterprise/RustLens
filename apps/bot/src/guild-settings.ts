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
import { logger } from "./logger.js";
import { logDiscordCatch } from "./shared.js";

const shardID = process.env.SHARD_ID ?? "0";

let _backend: BackendClient | undefined;
let _workerID: string | undefined;
// Test injection point. When non-null, used instead of the real lazy-init path.
let _backendOverride: BackendClient | null = null;

function getBackend() {
  if (_backendOverride) return _backendOverride;
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
  if (_backendOverride) return "test-worker";
  getBackend();
  return _workerID!;
}

export function __setBackendForTests(backend: BackendClient | null): void {
  _backendOverride = backend;
}

export type GuildAdminInteraction =
  | ButtonInteraction
  | ChatInputCommandInteraction
  | ModalSubmitInteraction
  | StringSelectMenuInteraction;

export async function fetchGuildSettings(guildId: string) {
  return getBackend().guildSettings(getWorkerID(), guildId);
}

// `isGuildLimited` is called on every slash command, so the limited flag is
// cached in-memory with a short TTL to avoid a backend round-trip per command.
const LIMITED_CACHE_TTL_MS = 60_000;
const limitedCache = new Map<string, { limited: boolean; expiresAt: number }>();

export async function isGuildLimited(guildId: string): Promise<boolean> {
  const cached = limitedCache.get(guildId);
  if (cached && cached.expiresAt > Date.now()) {
    return cached.limited;
  }

  try {
    const settings = await fetchGuildSettings(guildId);
    limitedCache.set(guildId, {
      limited: settings.limited,
      expiresAt: Date.now() + LIMITED_CACHE_TTL_MS,
    });
    return settings.limited;
  } catch (error) {
    // Fail open: a backend hiccup must not lock a guild out of every command.
    logger.warn({ guildId, error }, "isGuildLimited backend lookup failed; failing open");
    return false;
  }
}

export function invalidateGuildLimitedCache(guildId: string): void {
  limitedCache.delete(guildId);
}

export async function updateGuildSettings(guildId: string, patch: GuildSettingsPatch) {
  const result = await getBackend().updateGuildSettings(getWorkerID(), guildId, patch);
  limitedCache.set(guildId, {
    limited: result.limited,
    expiresAt: Date.now() + LIMITED_CACHE_TTL_MS,
  });
  return result;
}

function interactionMemberHasRole(interaction: GuildAdminInteraction, roleId: string) {
  const member = interaction.member;

  if (member && "roles" in member) {
    const roles = member.roles;
    if (roles && typeof roles === "object") {
      if ("cache" in roles) {
        return roles.cache.has(roleId);
      }

      if (Array.isArray(roles)) {
        return roles.includes(roleId);
      }
    }
  }

  return null;
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

  const cachedRoleMatch = interactionMemberHasRole(interaction, settings.adminRoleId);
  if (cachedRoleMatch !== null) {
    return cachedRoleMatch;
  }

  const member = await interaction.guild.members.fetch(interaction.user.id).catch(logDiscordCatch("fetch guild admin member", null, {
    guildId: interaction.guildId,
    discordId: interaction.user.id,
  }));
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
    (await guild.channels.fetch(channelId).catch(logDiscordCatch("fetch configured guild channel", null, {
      guildId: guild.id,
      channelId,
    })));

  if (!channel?.isTextBased() || !("send" in channel)) {
    return null;
  }

  return channel as GuildBasedChannel;
}
