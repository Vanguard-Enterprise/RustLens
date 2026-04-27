import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Skeleton } from "@/components/ui/skeleton";
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
  const src = guild.bannerUrl ?? guild.iconUrl;
  if (!src) {
    return (
      <div
        aria-hidden
        className="h-32 w-full bg-gradient-to-br from-muted to-muted-foreground/20"
      />
    );
  }
  return (
    // Plain <img> is intentional: discord CDN is not in next/image
    // remotePatterns. CSP allows https: img sources.
    <img
      src={src}
      alt=""
      className="h-32 w-full object-cover"
      loading="lazy"
    />
  );
}

function GuildCard({ guild }: { guild: DashboardGuild }) {
  return (
    <Card className="overflow-hidden">
      <GuildBanner guild={guild} />
      <CardHeader className="flex-row items-center gap-3">
        <Avatar size="lg" className="ring-2 ring-background">
          {guild.iconUrl ? <AvatarImage src={guild.iconUrl} alt="" /> : null}
          <AvatarFallback>{initials(guild.name)}</AvatarFallback>
        </Avatar>
        <CardTitle className="truncate">{guild.name}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {guild.owner ? (
          <div className="flex items-center gap-2 text-muted-foreground">
            <Avatar size="sm">
              {guild.owner.avatarUrl ? (
                <AvatarImage src={guild.owner.avatarUrl} alt="" />
              ) : null}
              <AvatarFallback>{initials(guild.owner.displayName)}</AvatarFallback>
            </Avatar>
            <span className="truncate text-xs">
              Owner: {guild.owner.displayName}
            </span>
          </div>
        ) : null}
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          {guild.memberCount != null ? (
            <span>{guild.memberCount.toLocaleString()} members</span>
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
        <GuildCard key={guild.id} guild={guild} />
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
