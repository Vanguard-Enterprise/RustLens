const CDN_BASE = "https://cdn.discordapp.com";

function extFromHash(hash: string): "gif" | "png" {
  return hash.startsWith("a_") ? "gif" : "png";
}

export function guildIconUrl(guildId: string, hash: string | null, size = 256): string | null {
  if (!hash) return null;
  return `${CDN_BASE}/icons/${guildId}/${hash}.${extFromHash(hash)}?size=${size}`;
}

export function guildBannerUrl(guildId: string, hash: string | null, size = 1024): string | null {
  if (!hash) return null;
  return `${CDN_BASE}/banners/${guildId}/${hash}.${extFromHash(hash)}?size=${size}`;
}

export function userAvatarUrl(userId: string, hash: string | null, size = 128): string | null {
  if (!hash) return null;
  return `${CDN_BASE}/avatars/${userId}/${hash}.${extFromHash(hash)}?size=${size}`;
}
