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
import { userAvatarUrl } from "@/lib/discord-cdn";
import type {
  AdminUserInstallItem,
  AdminUserInstallsPage,
} from "@/lib/rustlens-api";

type UserInstallsTableProps = {
  initialQuery: string;
  pageSizeOptions: number[];
  data: AdminUserInstallsPage;
};

const SEARCH_DEBOUNCE_MS = 300;

function formatRelative(iso: string | null): string {
  if (!iso) return "";
  const ts = new Date(iso).getTime();
  if (!Number.isFinite(ts)) return "";
  const diff = Date.now() - ts;
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 1) return "today";
  if (days === 1) return "1 day ago";
  if (days < 30) return `${days} days ago`;
  const months = Math.floor(days / 30);
  if (months === 1) return "1 month ago";
  if (months < 12) return `${months} months ago`;
  const years = Math.floor(days / 365);
  return years === 1 ? "1 year ago" : `${years} years ago`;
}

function userLabel(u: AdminUserInstallItem): string {
  return u.globalName || u.username || "—";
}

function userInitial(u: AdminUserInstallItem): string {
  const label = userLabel(u);
  return label === "—" ? "?" : label.charAt(0).toUpperCase();
}

export function UserInstallsTable({
  initialQuery,
  pageSizeOptions,
  data,
}: UserInstallsTableProps) {
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
          placeholder="Search by username, global name, or Discord ID…"
          value={searchValue}
          onChange={(e) => setSearchValue(e.target.value)}
          className="max-w-md"
          aria-label="Search user app installs"
        />
        <span className="text-sm text-muted-foreground">
          {data.total} {data.total === 1 ? "user app" : "user apps"}
        </span>
      </div>

      <div className="overflow-hidden rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-12"></TableHead>
              <TableHead>User</TableHead>
              <TableHead>Discord ID</TableHead>
              <TableHead>Scopes</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Installed</TableHead>
              <TableHead>Last seen</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} className="h-24 text-center">
                  No user app installs found.
                </TableCell>
              </TableRow>
            ) : (
              data.items.map((u) => {
                const avatarUrl = userAvatarUrl(u.discordId, u.avatarHash, 64);
                return (
                  <TableRow key={u.discordId}>
                    <TableCell>
                      <Avatar className="size-8">
                        {avatarUrl && <AvatarImage src={avatarUrl} alt={userLabel(u)} />}
                        <AvatarFallback>{userInitial(u)}</AvatarFallback>
                      </Avatar>
                    </TableCell>
                    <TableCell className="font-medium">
                      <div className="flex flex-col">
                        <span>{userLabel(u)}</span>
                        {u.globalName && u.username && u.globalName !== u.username ? (
                          <span className="text-xs text-muted-foreground">
                            @{u.username}
                          </span>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell>
                      <button
                        type="button"
                        onClick={() => navigator.clipboard?.writeText(u.discordId)}
                        className="font-mono text-xs text-muted-foreground hover:text-foreground"
                        title="Click to copy"
                      >
                        {u.discordId}
                      </button>
                    </TableCell>
                    <TableCell>
                      {u.scopes.length === 0 ? (
                        <span className="text-muted-foreground">—</span>
                      ) : (
                        <div className="flex flex-wrap gap-1">
                          {u.scopes.map((s) => (
                            <Badge key={s} variant="outline" className="font-mono text-[10px]">
                              {s}
                            </Badge>
                          ))}
                        </div>
                      )}
                    </TableCell>
                    <TableCell>
                      {u.isInstalled ? (
                        <Badge variant="default">Installed</Badge>
                      ) : (
                        <Badge variant="secondary" title={u.leftAt ?? undefined}>
                          {u.leftAt ? `Left ${formatRelative(u.leftAt)}` : "Left"}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell
                      className="text-sm text-muted-foreground"
                      title={u.installedAt}
                    >
                      {formatRelative(u.installedAt)}
                    </TableCell>
                    <TableCell
                      className="text-sm text-muted-foreground"
                      title={u.lastSeenAt ?? undefined}
                    >
                      {u.lastSeenAt ? formatRelative(u.lastSeenAt) : "—"}
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
