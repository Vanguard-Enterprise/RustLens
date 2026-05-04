"use client";

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronsLeftIcon,
  ChevronsRightIcon,
} from "lucide-react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { guildIconUrl, userAvatarUrl } from "@/lib/discord-cdn";
import type { AdminGuildItem, AdminGuildsPage } from "@/lib/rustlens-api";

type GuildsTableProps = {
  initialQuery: string;
  pageSizeOptions: number[];
  data: AdminGuildsPage;
};

const SEARCH_DEBOUNCE_MS = 300;

function formatNumber(n: number | null): string {
  if (n === null) return "—";
  return n.toLocaleString();
}

function formatRelativeLeftAt(iso: string | null): string {
  if (!iso) return "";
  const ts = new Date(iso).getTime();
  if (!Number.isFinite(ts)) return "";
  const diff = Date.now() - ts;
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));
  if (days < 1) return "today";
  if (days === 1) return "1 day ago";
  if (days < 30) return `${days} days ago`;
  const months = Math.floor(days / 30);
  if (months === 1) return "1 month ago";
  if (months < 12) return `${months} months ago`;
  const years = Math.floor(days / 365);
  return years === 1 ? "1 year ago" : `${years} years ago`;
}

function ownerLabel(g: AdminGuildItem): string {
  return g.ownerGlobalName || g.ownerUsername || "—";
}

function ownerInitial(g: AdminGuildItem): string {
  const label = ownerLabel(g);
  return label === "—" ? "?" : label.charAt(0).toUpperCase();
}

export function GuildsTable({
  initialQuery,
  pageSizeOptions,
  data,
}: GuildsTableProps) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [searchValue, setSearchValue] = React.useState(initialQuery);

  const totalPages = Math.max(1, Math.ceil(data.total / data.pageSize));
  const canPrev = data.page > 1;
  const canNext = data.page < totalPages;

  const pushParams = React.useCallback(
    (updates: Record<string, string | null>) => {
      const next = new URLSearchParams(searchParams?.toString() ?? "");
      for (const [k, v] of Object.entries(updates)) {
        if (v === null || v === "") next.delete(k);
        else next.set(k, v);
      }
      const qs = next.toString();
      router.push(qs ? `?${qs}` : "?");
    },
    [router, searchParams],
  );

  React.useEffect(() => {
    if (searchValue === initialQuery) return;
    const t = window.setTimeout(() => {
      pushParams({ q: searchValue || null, page: null });
    }, SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(t);
  }, [searchValue, initialQuery, pushParams]);

  const goToPage = (p: number) => pushParams({ page: String(p) });
  const setPageSize = (size: number) =>
    pushParams({ pageSize: String(size), page: null });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <Input
          placeholder="Search by name, guild ID, or owner username…"
          value={searchValue}
          onChange={(e) => setSearchValue(e.target.value)}
          className="max-w-md"
          aria-label="Search guilds"
        />
        <span className="text-sm text-muted-foreground">
          {data.total} {data.total === 1 ? "guild" : "guilds"}
        </span>
      </div>

      <div className="overflow-hidden rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-12"></TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Guild ID</TableHead>
              <TableHead>Owner</TableHead>
              <TableHead className="text-right">Members</TableHead>
              <TableHead>Installed</TableHead>
              <TableHead>Configured</TableHead>
              <TableHead className="text-right">Tracked</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8} className="h-24 text-center">
                  No guilds found.
                </TableCell>
              </TableRow>
            ) : (
              data.items.map((g) => {
                const iconUrl = guildIconUrl(g.guildId, g.iconHash, 64);
                const ownerAvatar = g.ownerDiscordId
                  ? userAvatarUrl(g.ownerDiscordId, g.ownerAvatarHash, 64)
                  : null;
                return (
                  <TableRow key={g.guildId}>
                    <TableCell>
                      <Avatar className="size-8">
                        {iconUrl && <AvatarImage src={iconUrl} alt={g.name} />}
                        <AvatarFallback>
                          {g.name ? g.name.charAt(0).toUpperCase() : "?"}
                        </AvatarFallback>
                      </Avatar>
                    </TableCell>
                    <TableCell className="font-medium">{g.name || "—"}</TableCell>
                    <TableCell>
                      <button
                        type="button"
                        onClick={() => navigator.clipboard?.writeText(g.guildId)}
                        className="font-mono text-xs text-muted-foreground hover:text-foreground"
                        title="Click to copy"
                      >
                        {g.guildId}
                      </button>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Avatar className="size-6">
                          {ownerAvatar && (
                            <AvatarImage src={ownerAvatar} alt={ownerLabel(g)} />
                          )}
                          <AvatarFallback className="text-[10px]">
                            {ownerInitial(g)}
                          </AvatarFallback>
                        </Avatar>
                        <span className="text-sm">{ownerLabel(g)}</span>
                      </div>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {formatNumber(g.approximateMemberCount)}
                    </TableCell>
                    <TableCell>
                      {g.isInstalled ? (
                        <Badge variant="default">Installed</Badge>
                      ) : (
                        <Badge variant="secondary" title={g.leftAt ?? undefined}>
                          {g.leftAt ? `Left ${formatRelativeLeftAt(g.leftAt)}` : "Left"}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      {g.isConfigured ? <Badge variant="outline">Configured</Badge> : <span className="text-muted-foreground">—</span>}
                    </TableCell>
                    <TableCell
                      className={`text-right tabular-nums ${g.trackedServerCount === 0 ? "text-muted-foreground" : ""}`}
                    >
                      {g.trackedServerCount}
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </div>

      <div className="flex items-center justify-between px-4">
        <div className="hidden flex-1 text-sm text-muted-foreground lg:flex" />
        <div className="flex w-full items-center gap-8 lg:w-fit">
          <div className="hidden items-center gap-2 lg:flex">
            <Label htmlFor="rows-per-page" className="text-sm font-medium">
              Rows per page
            </Label>
            <Select
              value={String(data.pageSize)}
              onValueChange={(value) => setPageSize(Number(value))}
            >
              <SelectTrigger size="sm" className="w-20" id="rows-per-page">
                <SelectValue placeholder={data.pageSize} />
              </SelectTrigger>
              <SelectContent side="top">
                <SelectGroup>
                  {pageSizeOptions.map((size) => (
                    <SelectItem key={size} value={String(size)}>
                      {size}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          <div className="flex w-fit items-center justify-center text-sm font-medium">
            Page {data.page} of {totalPages}
          </div>
          <div className="ml-auto flex items-center gap-2 lg:ml-0">
            <Button
              variant="outline"
              className="hidden h-8 w-8 p-0 lg:flex"
              onClick={() => goToPage(1)}
              disabled={!canPrev}
            >
              <span className="sr-only">Go to first page</span>
              <ChevronsLeftIcon />
            </Button>
            <Button
              variant="outline"
              className="size-8"
              size="icon"
              onClick={() => goToPage(data.page - 1)}
              disabled={!canPrev}
            >
              <span className="sr-only">Go to previous page</span>
              <ChevronLeftIcon />
            </Button>
            <Button
              variant="outline"
              className="size-8"
              size="icon"
              onClick={() => goToPage(data.page + 1)}
              disabled={!canNext}
            >
              <span className="sr-only">Go to next page</span>
              <ChevronRightIcon />
            </Button>
            <Button
              variant="outline"
              className="hidden size-8 lg:flex"
              size="icon"
              onClick={() => goToPage(totalPages)}
              disabled={!canNext}
            >
              <span className="sr-only">Go to last page</span>
              <ChevronsRightIcon />
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
