"use client";

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  ArrowUpDownIcon,
  CalendarIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronsLeftIcon,
  ChevronsRightIcon,
  FileIcon,
  FilterIcon,
  SearchIcon,
  XIcon,
} from "lucide-react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
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
  initialSort?: string | null;
  initialJoinedSince?: string | null;
  initialJoinedBefore?: string | null;
  pageSizeOptions: number[];
  data: AdminGuildsPage;
};

const SEARCH_DEBOUNCE_MS = 300;
type SortField = "name" | "members" | "joined";
type SortDir = "asc" | "desc";

function formatNumber(n: number | null): string {
  if (n === null) return "—";
  return n.toLocaleString();
}

function formatRelative(iso: string | null | undefined): string {
  if (!iso) return "—";
  const ts = new Date(iso).getTime();
  if (!Number.isFinite(ts)) return "—";
  const diff = Date.now() - ts;
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes} ${minutes === 1 ? "minute" : "minutes"} ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days === 1) return "1 day ago";
  if (days < 30) return `${days} days ago`;
  const months = Math.floor(days / 30);
  if (months === 1) return "1 month ago";
  if (months < 12) return `${months} months ago`;
  const years = Math.floor(days / 365);
  return years === 1 ? "1 year ago" : `${years} years ago`;
}

function parseDateString(value: string | null | undefined): Date | undefined {
  if (!value) return undefined;
  const [y, m, d] = value.split("-").map((n) => Number.parseInt(n, 10));
  if (!y || !m || !d) return undefined;
  return new Date(Date.UTC(y, m - 1, d));
}

function formatDateString(date: Date | undefined): string | null {
  if (!date) return null;
  const y = date.getUTCFullYear();
  const m = String(date.getUTCMonth() + 1).padStart(2, "0");
  const d = String(date.getUTCDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

function formatDateLabel(date: Date | undefined): string {
  if (!date) return "";
  return date.toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });
}

function parseSort(sort: string | null | undefined): { field: SortField | null; dir: SortDir } {
  if (!sort) return { field: null, dir: "asc" };
  const dir: SortDir = sort.startsWith("-") ? "desc" : "asc";
  const field = sort.replace(/^-/, "");
  if (field === "name" || field === "members" || field === "joined") {
    return { field, dir };
  }
  return { field: null, dir: "asc" };
}

function buildSortParam(field: SortField, dir: SortDir): string {
  return dir === "desc" ? `-${field}` : field;
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
  initialSort = null,
  initialJoinedSince = null,
  initialJoinedBefore = null,
  pageSizeOptions,
  data,
}: GuildsTableProps) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [searchValue, setSearchValue] = React.useState(initialQuery);
  const joinedSinceDate = React.useMemo(
    () => parseDateString(initialJoinedSince),
    [initialJoinedSince],
  );
  const joinedBeforeDate = React.useMemo(
    () => parseDateString(initialJoinedBefore),
    [initialJoinedBefore],
  );

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
  const sortState = parseSort(initialSort);
  const cycleSort = (field: SortField) => {
    let next: string | null;
    if (sortState.field !== field) {
      next = buildSortParam(field, field === "members" || field === "joined" ? "desc" : "asc");
    } else if (sortState.dir === "desc") {
      next = buildSortParam(field, "asc");
    } else {
      next = null;
    }
    pushParams({ sort: next, page: null });
  };
  const sortIconFor = (field: SortField) => {
    if (sortState.field !== field) {
      return <ArrowUpDownIcon className="size-3.5 opacity-50" />;
    }
    return sortState.dir === "asc" ? (
      <ArrowUpIcon className="size-3.5" />
    ) : (
      <ArrowDownIcon className="size-3.5" />
    );
  };
  const setJoinedRange = (from: Date | undefined, to: Date | undefined) =>
    pushParams({
      joinedSince: formatDateString(from),
      joinedBefore: formatDateString(to),
      page: null,
    });
  const clearJoinedRange = () =>
    pushParams({ joinedSince: null, joinedBefore: null, page: null });
  const hasJoinedRange = !!(initialJoinedSince || initialJoinedBefore);
  const joinedRangeLabel = hasJoinedRange
    ? `${formatDateLabel(joinedSinceDate) || "…"} – ${formatDateLabel(joinedBeforeDate) || "…"}`
    : "All time";

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-1 items-center gap-2">
        <div className="relative w-full max-w-md flex-1">
          <SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="Search by name, guild ID, or owner username…"
            value={searchValue}
            onChange={(e) => setSearchValue(e.target.value)}
            className="pl-8"
            aria-label="Search guilds"
          />
        </div>
        <Popover>
          <PopoverTrigger asChild>
            <Button variant="outline" className="gap-2">
              <FilterIcon className="size-4" />
              Joined
              {hasJoinedRange ? <Badge variant="secondary" className="ml-1 px-1.5">1</Badge> : null}
            </Button>
          </PopoverTrigger>
          <PopoverContent align="start" className="w-80">
            <div className="flex flex-col gap-3">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium uppercase text-muted-foreground">
                  Joined
                </span>
                {hasJoinedRange ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 px-2 text-xs"
                    onClick={clearJoinedRange}
                  >
                    All time
                  </Button>
                ) : null}
              </div>
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <CalendarIcon className="size-4" />
                <span>{joinedRangeLabel}</span>
              </div>
              <Calendar
                mode="range"
                numberOfMonths={1}
                selected={{ from: joinedSinceDate, to: joinedBeforeDate }}
                onSelect={(range) => setJoinedRange(range?.from, range?.to)}
                className="w-full rounded-md border"
              />
            </div>
          </PopoverContent>
        </Popover>
        {hasJoinedRange ? (
          <Button
            variant="outline"
            className="px-2"
            aria-label="Clear joined filter"
            title="Clear joined filter"
            onClick={clearJoinedRange}
          >
            <XIcon className="size-4" />
          </Button>
        ) : null}
      </div>

      <div className="overflow-hidden rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-12"></TableHead>
              <TableHead>
                <button
                  type="button"
                  onClick={() => cycleSort("name")}
                  className="-ml-2 inline-flex items-center gap-1 rounded px-2 py-1 font-medium hover:bg-muted"
                  aria-label="Sort by name"
                >
                  Name
                  {sortIconFor("name")}
                </button>
              </TableHead>
              <TableHead>Guild ID</TableHead>
              <TableHead>Owner</TableHead>
              <TableHead className="text-right">
                <button
                  type="button"
                  onClick={() => cycleSort("members")}
                  className="inline-flex items-center gap-1 rounded px-2 py-1 font-medium hover:bg-muted"
                  aria-label="Sort by members"
                >
                  Members
                  {sortIconFor("members")}
                </button>
              </TableHead>
              <TableHead className="whitespace-nowrap">
                <button
                  type="button"
                  onClick={() => cycleSort("joined")}
                  className="inline-flex items-center gap-1 rounded px-2 py-1 font-medium hover:bg-muted"
                  aria-label="Sort by joined date"
                >
                  Joined
                  {sortIconFor("joined")}
                </button>
              </TableHead>
              <TableHead>Installed</TableHead>
              <TableHead>Configured</TableHead>
              <TableHead className="text-right">Tracked</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={9} className="h-24 text-center">
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
                    <TableCell
                      className="whitespace-nowrap text-sm text-muted-foreground"
                      title={g.installedAt}
                    >
                      {formatRelative(g.installedAt)}
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

      <div className="flex items-center gap-4">
        <div className="flex flex-1 items-center">
          <Select
            value={String(data.pageSize)}
            onValueChange={(value) => setPageSize(Number(value))}
          >
            <SelectTrigger
              size="sm"
              className="w-20"
              id="rows-per-page"
              aria-label="Rows per page"
            >
              <FileIcon className="size-4 text-muted-foreground" />
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
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            className="hidden size-8 lg:flex"
            size="icon"
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
          <div className="min-w-[7rem] text-center text-sm font-medium">
            Page {data.page} of {totalPages}
          </div>
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
        <div className="flex flex-1 justify-end text-sm text-muted-foreground">
          {data.total} {data.total === 1 ? "guild" : "guilds"}
        </div>
      </div>
    </div>
  );
}
