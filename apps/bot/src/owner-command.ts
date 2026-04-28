import {
  ActionRowBuilder,
  EmbedBuilder,
  ModalBuilder,
  TextInputBuilder,
  TextInputStyle,
} from "discord.js";
import type { OwnerProxy, OwnerStatsResponse, ShardStatSnapshot } from "./backend-client.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const OWNER_PROXY_ADD_MODAL_CUSTOM_ID = "owner_proxy_add_modal";
export const OWNER_PROXY_ADD_FIELD_CUSTOM_ID = "owner_proxy_add_endpoints";

function formatNumber(value: number) {
  return value.toLocaleString("en-US");
}

function formatBytes(bytes: number) {
  if (bytes <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB"];
  let size = bytes;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  const rounded = size >= 100 ? size.toFixed(0) : size.toFixed(1);
  return `${rounded} ${units[unitIndex]}`;
}

function formatUptime(uptimeSec: number) {
  if (uptimeSec <= 0) {
    return "—";
  }
  const days = Math.floor(uptimeSec / 86_400);
  const hours = Math.floor((uptimeSec % 86_400) / 3600);
  const minutes = Math.floor((uptimeSec % 3600) / 60);

  if (days > 0) {
    return `${days}d ${hours}h`;
  }
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  return `${minutes}m`;
}

function summarizeShards(shards: ShardStatSnapshot[], targetShards: number) {
  if (shards.length === 0) {
    return `Shards: 0/${targetShards} reporting`;
  }

  const ready = shards.filter((shard) => shard.status === "ready");
  const degraded = shards.filter((shard) => shard.status !== "ready");

  const avgPing = ready.length > 0
    ? Math.round(ready.reduce((sum, shard) => sum + shard.pingMs, 0) / ready.length)
    : 0;

  const lines: string[] = [];
  lines.push(`Shards: ${ready.length}/${targetShards} ready · ⌀ ping ${avgPing}ms`);

  if (degraded.length > 0) {
    const detail = degraded
      .map((shard) => `shard ${shard.shardId} ${shard.status}`)
      .join(", ");
    lines.push(`⚠ ${detail}`);
  }

  return lines.join("\n");
}

export function proxyChoiceLabel(proxy: OwnerProxy) {
  const host = proxy.endpoint.replace(/^https?:\/\//, "");
  return host.length <= 100 ? host : `${host.slice(0, 97)}...`;
}

export function buildOwnerProxyListEmbed(proxies: OwnerProxy[]) {
  const description = proxies.length === 0
    ? "No outbound proxies configured."
    : proxies.map((proxy, index) => `${index + 1}. \`${proxyChoiceLabel(proxy)}\``).join("\n");

  return new EmbedBuilder()
    .setTitle("Outbound Proxies")
    .setDescription(description.length <= 4096 ? description : `${description.slice(0, 4093)}...`)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

export function buildOwnerProxyAddModal() {
  return new ModalBuilder()
    .setCustomId(OWNER_PROXY_ADD_MODAL_CUSTOM_ID)
    .setTitle("Add Outbound Proxies")
    .addComponents(
      new ActionRowBuilder<TextInputBuilder>().addComponents(
        new TextInputBuilder()
          .setCustomId(OWNER_PROXY_ADD_FIELD_CUSTOM_ID)
          .setLabel("Proxy URLs")
          .setStyle(TextInputStyle.Paragraph)
          .setRequired(true)
          .setPlaceholder("http://username:password@45.127.250.10:5619,http://username:password@82.22.249.124:6961"),
      ),
    );
}

export function parseOwnerProxyInput(input: string) {
  return Array.from(
    new Set(
      input
        .split(/[\s,]+/)
        .map((value) => value.trim())
        .filter((value) => value.length > 0),
    ),
  );
}

export function buildOwnerStatsEmbed(stats: OwnerStatsResponse) {
  const allShards = stats.workers.flatMap((worker) => worker.shards);
  const totalRSS = stats.workers.reduce((sum, worker) => sum + worker.rssBytes, 0);
  const maxUptime = stats.workers.reduce((max, worker) => Math.max(max, worker.uptimeSec), 0);

  const embed = new EmbedBuilder()
    .setTitle("Bot Statistics")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL })
    .addFields(
      {
        name: "📊 Servers",
        value: formatNumber(stats.guildCount),
        inline: true,
      },
      {
        name: "👥 Total Members",
        value: formatNumber(stats.memberCount),
        inline: true,
      },
      {
        name: "⚙️ Configured Guilds",
        value: formatNumber(stats.configuredGuilds),
        inline: true,
      },
      {
        name: "🔗 Linked Steam IDs",
        value: formatNumber(stats.steamAccountCount),
        inline: true,
      },
      {
        name: "🔒 Hard Links",
        value: formatNumber(stats.hardLinkCount),
        inline: true,
      },
      {
        name: "👤 Unique Linked Users",
        value: formatNumber(stats.uniqueLinkedUsers),
        inline: true,
      },
      {
        name: "🛰️ Tracked Servers",
        value: formatNumber(stats.trackedServerCount),
        inline: true,
      },
      {
        name: "🟢 Online Tracked",
        value: formatNumber(stats.onlineTrackedPlayers),
        inline: true,
      },
      {
        name: "⏰ Active Wipe Reacts",
        value: formatNumber(stats.activeWipeReacts),
        inline: true,
      },
      {
        name: "Workers",
        value: [
          summarizeShards(allShards, stats.targetShards),
          `Uptime: ${formatUptime(maxUptime)} · Memory: ${formatBytes(totalRSS)}`,
        ].join("\n"),
        inline: false,
      },
    );

  return embed;
}
