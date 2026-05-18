"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronsLeftIcon,
  ChevronsRightIcon,
  FileIcon,
} from "lucide-react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
import type { AdminUserItem, AdminUsersPage } from "@/lib/rustlens-api";

type MembersTableProps = {
  initialQuery: string;
  pageSizeOptions: number[];
  data: AdminUsersPage;
};

const SEARCH_DEBOUNCE_MS = 300;

function formatRelative(iso: string | null | undefined): string {
  if (!iso) return "—";
  const ts = new Date(iso).getTime();
  if (!Number.isFinite(ts)) return "—";
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

function memberLabel(u: AdminUserItem): string {
  return u.discordGlobalName || u.discordUsername || u.user.displayName || "—";
}

function memberInitial(u: AdminUserItem): string {
  const label = memberLabel(u);
  return label === "—" ? "?" : label.charAt(0).toUpperCase();
}

export function MembersTable({
  initialQuery,
  pageSizeOptions,
  data,
}: MembersTableProps) {
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
          aria-label="Search members"
        />
      </div>

      <div className="overflow-hidden rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-12"></TableHead>
              <TableHead>User</TableHead>
              <TableHead>Discord ID</TableHead>
              <TableHead>Joined</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={4} className="h-24 text-center">
                  No members found.
                </TableCell>
              </TableRow>
            ) : (
              data.items.map((u) => {
                const avatarUrl = userAvatarUrl(u.discordId, u.discordAvatar ?? null, 64);
                return (
                  <TableRow
                    key={u.user.id}
                    className="cursor-pointer hover:bg-muted/50"
                  >
                    <TableCell>
                      <Link href={`/admin/users/${u.user.id}`} className="block">
                        <Avatar className="size-8">
                          {avatarUrl && <AvatarImage src={avatarUrl} alt={memberLabel(u)} />}
                          <AvatarFallback>{memberInitial(u)}</AvatarFallback>
                        </Avatar>
                      </Link>
                    </TableCell>
                    <TableCell className="font-medium">
                      <Link href={`/admin/users/${u.user.id}`} className="block">
                        <div className="flex flex-col">
                          <span>{memberLabel(u)}</span>
                          {u.discordUsername ? (
                            <span className="text-xs text-muted-foreground">
                              @{u.discordUsername}
                            </span>
                          ) : null}
                        </div>
                      </Link>
                    </TableCell>
                    <TableCell>
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation();
                          navigator.clipboard?.writeText(u.discordId);
                        }}
                        className="font-mono text-xs text-muted-foreground hover:text-foreground"
                        title="Click to copy"
                      >
                        {u.discordId}
                      </button>
                    </TableCell>
                    <TableCell
                      className="text-sm text-muted-foreground"
                      title={u.user.createdAt}
                    >
                      <Link href={`/admin/users/${u.user.id}`} className="block">
                        {formatRelative(u.user.createdAt)}
                      </Link>
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
          {data.total} {data.total === 1 ? "member" : "members"}
        </div>
      </div>
    </div>
  );
}
