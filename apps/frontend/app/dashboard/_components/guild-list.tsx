import Image from "next/image";
import Link from "next/link";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Skeleton } from "@/components/ui/skeleton";
import { guildBannerUrl, guildIconUrl, userAvatarUrl } from "@/lib/discord-cdn";
import { getMyInstalledGuilds, type DashboardGuild } from "@/lib/rustlens-api";

function initials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]!.toUpperCase())
    .join("");
}

function GuildBanner({ guild }: { guild: DashboardGuild }) {
  const bannerSrc = guildBannerUrl(guild.id, guild.bannerHash);
  if (bannerSrc) {
    return (
      <img
        src={bannerSrc}
        alt=""
        className="h-32 w-full object-cover object-center"
        loading="lazy"
      />
    );
  }
  return (
    <div
      aria-hidden
      className="relative -mt-4 flex h-32 w-full items-center justify-center overflow-hidden rounded-t-xl bg-[linear-gradient(135deg,_color-mix(in_srgb,var(--primary)_85%,black)_0%,_color-mix(in_srgb,var(--primary)_70%,black)_50%,_color-mix(in_srgb,var(--primary)_92%,black)_100%)]"
    >
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_center,_rgba(255,255,255,0.18)_0%,_transparent_60%)]" />
      <Image
        src="/RustLens.png"
        alt=""
        width={96}
        height={96}
        className="relative h-16 w-16 object-contain drop-shadow-md [filter:brightness(0)_invert(1)] opacity-95"
      />
    </div>
  );
}

function GuildCard({ guild }: { guild: DashboardGuild }) {
  const iconSrc = guildIconUrl(guild.id, guild.iconHash) ?? undefined;
  const ownerName = guild.owner?.globalName ?? guild.owner?.username ?? null;
  const ownerAvatarSrc =
    guild.owner ? userAvatarUrl(guild.owner.id, guild.owner.avatarHash) ?? undefined : undefined;

  return (
    <Card className="overflow-hidden transition-colors hover:bg-accent/30 cursor-pointer h-full">
      <GuildBanner guild={guild} />
      <CardHeader className="flex-row items-center gap-3">
        <Avatar size="lg" className="ring-2 ring-background">
          {iconSrc ? <AvatarImage src={iconSrc} alt="" /> : null}
          <AvatarFallback>{initials(guild.name)}</AvatarFallback>
        </Avatar>
        <CardTitle className="truncate">{guild.name}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {guild.owner && ownerName ? (
          <div className="flex items-center gap-2 text-muted-foreground">
            <Avatar size="sm">
              {ownerAvatarSrc ? <AvatarImage src={ownerAvatarSrc} alt="" /> : null}
              <AvatarFallback>{initials(ownerName)}</AvatarFallback>
            </Avatar>
            <span className="truncate text-xs">Owner: {ownerName}</span>
          </div>
        ) : null}
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          {guild.approximateMemberCount != null ? (
            <span>{guild.approximateMemberCount.toLocaleString()} members</span>
          ) : null}
          <span>
            {guild.trackedServerCount} tracked server
            {guild.trackedServerCount === 1 ? "" : "s"}
          </span>
        </div>
      </CardContent>
    </Card>
  );
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed py-16 text-center">
      <div className="font-heading text-base">No RustLens guilds yet</div>
      <p className="max-w-md text-sm text-muted-foreground">
        You aren&apos;t in any Discord servers that have RustLens installed.
        Once you join one—or invite the bot to your own server—it will show up
        here.
      </p>
    </div>
  );
}

export async function GuildList() {
  const guilds = await getMyInstalledGuilds();

  if (guilds.length === 0) {
    return <EmptyState />;
  }

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
      {guilds.map((guild) => (
        <Link
          key={guild.id}
          href={`/dashboard/guilds/${guild.id}`}
          className="block rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <GuildCard guild={guild} />
        </Link>
      ))}
    </div>
  );
}

export function GuildListSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
      {Array.from({ length: 6 }).map((_, i) => (
        <Card key={i} className="overflow-hidden">
          <Skeleton className="h-32 w-full rounded-none" />
          <CardHeader className="flex-row items-center gap-3">
            <Skeleton className="size-10 rounded-full" />
            <Skeleton className="h-4 w-32" />
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            <Skeleton className="h-3 w-40" />
            <Skeleton className="h-3 w-24" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
