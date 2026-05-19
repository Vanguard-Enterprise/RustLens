"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import {
  BanIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronsLeftIcon,
  ChevronsRightIcon,
  FileIcon,
  UserCheckIcon,
} from "lucide-react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
import { toast } from "sonner";

import { userAvatarUrl } from "@/lib/discord-cdn";
import type { AdminUserItem, AdminUsersPage } from "@/lib/rustlens-api";

import { bulkUserAction, suspendUserAction, unsuspendUserAction } from "../actions";
import { SuspendDialog } from "./suspend-dialog";

type MembersTableProps = {
  initialQuery: string;
  pageSizeOptions: number[];
  data: AdminUsersPage;
  canSuspend: boolean;
};

type SuspendTarget = {
  mode: "suspend" | "unsuspend";
  userId: string;
  displayName: string;
};

const SEARCH_DEBOUNCE_MS = 300;

type BulkAction = "suspend" | "unsuspend";

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
  canSuspend,
}: MembersTableProps) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [searchValue, setSearchValue] = React.useState(initialQuery);
  const [suspendTarget, setSuspendTarget] = React.useState<SuspendTarget | null>(null);
  const [bulkAction, setBulkAction] = React.useState<BulkAction | null>(null);
  const [selectedIds, setSelectedIds] = React.useState<Set<string>>(() => new Set());

  // Reset selection whenever the server-rendered data prop changes
  // (pagination, search, page-size). The cascade is intentional.
  React.useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSelectedIds(new Set());
  }, [data]);

  const pageIds = React.useMemo(() => data.items.map((u) => u.user.id), [data.items]);
  const selectedOnPageCount = pageIds.reduce(
    (n, id) => (selectedIds.has(id) ? n + 1 : n),
    0,
  );
  const allOnPageSelected =
    pageIds.length > 0 && selectedOnPageCount === pageIds.length;
  const someOnPageSelected =
    selectedOnPageCount > 0 && selectedOnPageCount < pageIds.length;

  const toggleRow = (id: string, checked: boolean) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (checked) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  const toggleAllOnPage = (checked: boolean) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (checked) for (const id of pageIds) next.add(id);
      else for (const id of pageIds) next.delete(id);
      return next;
    });
  };

  const copySelectedIds = async () => {
    const ids = data.items
      .filter((u) => selectedIds.has(u.user.id))
      .map((u) => u.discordId);
    if (ids.length === 0) return;
    try {
      await navigator.clipboard.writeText(ids.join("\n"));
      toast.success(
        `Copied ${ids.length} Discord ${ids.length === 1 ? "ID" : "IDs"}`,
      );
    } catch {
      toast.error("Could not copy to clipboard");
    }
  };

  const runBulk = async (action: BulkAction, reason: string) => {
    const ids = Array.from(selectedIds);
    const res = await bulkUserAction(action, ids, {}, reason);
    if (!res.ok) throw new Error(res.error);
    const { succeeded, failed, results } = res.result;
    const verb = action === "suspend" ? "suspended" : "unsuspended";
    if (failed === 0) {
      toast.success(`${succeeded} ${succeeded === 1 ? "user" : "users"} ${verb}`);
      setSelectedIds(new Set());
    } else {
      const firstError = results.find((r) => !r.ok)?.error;
      toast.warning(
        `${succeeded} of ${succeeded + failed} ${verb}. ${failed} failed.`,
        firstError ? { description: firstError } : undefined,
      );
      setSelectedIds(new Set(results.filter((r) => !r.ok).map((r) => r.userId)));
    }
    router.refresh();
  };

  const clearSelection = () => setSelectedIds(new Set());

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

      {selectedIds.size > 0 ? (
        <div className="flex items-center gap-2 rounded-lg border bg-muted/30 px-3 py-2">
          <span className="text-sm font-medium">
            {selectedIds.size} selected
          </span>
          <div className="flex-1" />
          <Button variant="ghost" size="sm" onClick={clearSelection}>
            Clear
          </Button>
          {canSuspend ? (
            <>
              <Button
                variant="destructive"
                size="sm"
                onClick={() => setBulkAction("suspend")}
              >
                Suspend
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setBulkAction("unsuspend")}
              >
                Unsuspend
              </Button>
            </>
          ) : null}
          <Button variant="outline" size="sm" onClick={copySelectedIds}>
            Copy IDs
          </Button>
        </div>
      ) : null}

      <div className="overflow-hidden rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-10">
                <Checkbox
                  aria-label="Select all"
                  checked={
                    allOnPageSelected
                      ? true
                      : someOnPageSelected
                        ? "indeterminate"
                        : false
                  }
                  onCheckedChange={(value) => toggleAllOnPage(value === true)}
                />
              </TableHead>
              <TableHead className="w-12"></TableHead>
              <TableHead>User</TableHead>
              <TableHead>Discord ID</TableHead>
              <TableHead>Joined</TableHead>
              {canSuspend ? <TableHead className="w-12 text-right">Actions</TableHead> : null}
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={canSuspend ? 6 : 5} className="h-24 text-center">
                  No members found.
                </TableCell>
              </TableRow>
            ) : (
              data.items.map((u) => {
                const avatarUrl = userAvatarUrl(u.discordId, u.discordAvatar ?? null, 64);
                const isSuspended = !!u.suspendedAt;
                return (
                  <TableRow
                    key={u.user.id}
                    className="cursor-pointer hover:bg-muted/50"
                  >
                    <TableCell
                      className="w-10"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <Checkbox
                        aria-label="Select row"
                        checked={selectedIds.has(u.user.id)}
                        onCheckedChange={(value) => toggleRow(u.user.id, value === true)}
                      />
                    </TableCell>
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
                    {canSuspend ? (
                      <TableCell className="text-right">
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-8"
                          onClick={(e) => {
                            e.stopPropagation();
                            setSuspendTarget({
                              mode: isSuspended ? "unsuspend" : "suspend",
                              userId: u.user.id,
                              displayName: memberLabel(u),
                            });
                          }}
                          aria-label={isSuspended ? "Unsuspend user" : "Suspend user"}
                          title={isSuspended ? "Unsuspend user" : "Suspend user"}
                        >
                          {isSuspended ? (
                            <UserCheckIcon className="size-4" />
                          ) : (
                            <BanIcon className="size-4 text-destructive" />
                          )}
                        </Button>
                      </TableCell>
                    ) : null}
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </div>

      {canSuspend ? (
        <>
          <SuspendDialog
            open={suspendTarget !== null}
            onOpenChange={(o) => {
              if (!o) setSuspendTarget(null);
            }}
            mode={suspendTarget?.mode ?? "suspend"}
            userId={suspendTarget?.userId ?? ""}
            displayName={suspendTarget?.displayName ?? ""}
            onConfirm={async (userId, reason) => {
              const action =
                suspendTarget?.mode === "unsuspend" ? unsuspendUserAction : suspendUserAction;
              const res = await action(userId, reason);
              if (!res.ok) throw new Error(res.error);
              router.refresh();
            }}
          />
          <SuspendDialog
            variant="bulk"
            open={bulkAction !== null}
            onOpenChange={(o) => {
              if (!o) setBulkAction(null);
            }}
            mode={bulkAction ?? "suspend"}
            count={selectedIds.size}
            onConfirm={async (reason) => {
              if (!bulkAction) return;
              await runBulk(bulkAction, reason);
            }}
          />
        </>
      ) : null}

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
