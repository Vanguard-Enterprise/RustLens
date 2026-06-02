"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  ArrowUpDownIcon,
  BanIcon,
  CalendarIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronsLeftIcon,
  ChevronsRightIcon,
  FileIcon,
  FilterIcon,
  PackageCheckIcon,
  SearchIcon,
  ShieldIcon,
  Trash2Icon,
  UserCheckIcon,
  XIcon,
} from "lucide-react";

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import { Calendar } from "@/components/ui/calendar";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
import { toast } from "sonner";

import { userAvatarUrl } from "@/lib/discord-cdn";
import type {
  AdminRoleItem,
  AdminBanCategory,
  AdminUserItem,
  AdminUsersPage,
  AdminUsersQuery,
} from "@/lib/rustlens-api";

import {
  bulkUserAction,
  deleteUserAction,
  setUserBanAction,
  suspendUserAction,
  unsuspendUserAction,
} from "../actions";
import { DeleteDialog } from "./delete-dialog";
import { SuspendDialog } from "./suspend-dialog";
import { UserBanDialog } from "./user-ban-dialog";

type MemberStatus = NonNullable<AdminUsersQuery["status"]>;
type UserAppFilter = "installed" | "not_installed";

const STATUSES: ReadonlyArray<MemberStatus> = [
  "active",
  "suspended",
  "deleted",
  "any",
];

type MembersTableProps = {
  initialQuery: string;
  initialStatus: MemberStatus;
  initialJoinedSince?: string | null;
  initialJoinedBefore?: string | null;
  initialSort?: string | null;
  initialUserApp?: UserAppFilter | null;
  initialRoles?: string[];
  initialEntitlement?: string | null;
  roles: AdminRoleItem[];
  pageSizeOptions: number[];
  data: AdminUsersPage;
  canSuspend: boolean;
  canDelete?: boolean;
};

type SortField = "createdAt" | "displayName";
type SortDir = "asc" | "desc";

function parseSort(sort: string | null | undefined): {
  field: SortField | null;
  dir: SortDir;
} {
  if (!sort) return { field: null, dir: "desc" };
  const dir: SortDir = sort.startsWith("-") ? "desc" : "asc";
  const field = sort.replace(/^-/, "");
  if (field === "createdAt" || field === "displayName") {
    return { field, dir };
  }
  return { field: null, dir: "desc" };
}

function buildSortParam(field: SortField, dir: SortDir): string {
  return dir === "desc" ? `-${field}` : field;
}

type SuspendTarget = {
  mode: "suspend" | "unsuspend";
  userId: string;
  displayName: string;
};

type DeleteTarget = {
  userId: string;
  displayName: string;
};

type RestrictionTarget = {
  userId: string;
  displayName: string;
  categories: AdminBanCategory[];
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
  if (minutes < 60) return `${minutes} ${minutes === 1 ? "minute" : "minutes"} ago`;
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

type FlagDef = {
  key: string;
  Icon: React.ComponentType<{ className?: string }>;
  label: string;
  className: string;
};

function buildFlags(item: AdminUserItem): FlagDef[] {
  const flags: FlagDef[] = [];
  if (item.deletedAt) {
    flags.push({
      key: "deleted",
      Icon: Trash2Icon,
      label: "Deleted",
      className: "text-muted-foreground",
    });
  }
  if (item.suspendedAt) {
    flags.push({
      key: "suspended",
      Icon: BanIcon,
      label: "Suspended",
      className: "text-destructive",
    });
  }
  const banCategories = item.banCategories ?? [];
  if (banCategories.length > 0) {
    flags.push({
      key: "restricted",
      Icon: BanIcon,
      label: `Restricted: ${banCategories.join(", ")}`,
      className: "text-destructive",
    });
  }
  if (item.adminRoles.length > 0) {
    flags.push({
      key: "admin",
      Icon: ShieldIcon,
      label: `Admin: ${item.adminRoles.join(", ")}`,
      className: "text-amber-500",
    });
  }
  if ((item.entitlements ?? []).includes("pro")) {
    flags.push({
      key: "pro",
      Icon: UserCheckIcon,
      label: "PRO",
      className: "text-yellow-600",
    });
  }
  if (item.hasUserAppInstalled) {
    flags.push({
      key: "user-app",
      Icon: PackageCheckIcon,
      label: "User app installed",
      className: "text-emerald-600",
    });
  }
  return flags;
}

function MemberFlags({ item }: { item: AdminUserItem }) {
  const flags = buildFlags(item);
  if (flags.length === 0) return null;
  return (
    <TooltipProvider>
      <span className="inline-flex items-center gap-1 leading-none">
        {flags.map((f) => (
          <Tooltip key={f.key}>
            <TooltipTrigger
              asChild
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
              }}
            >
              <span
                className={`inline-flex items-center ${f.className}`}
                aria-label={f.label}
              >
                <f.Icon className="size-3.5" />
              </span>
            </TooltipTrigger>
            <TooltipContent>{f.label}</TooltipContent>
          </Tooltip>
        ))}
      </span>
    </TooltipProvider>
  );
}

function parseDateString(value: string | null): Date | undefined {
  if (!value) return undefined;
  // Use UTC midnight so the displayed date matches the API param exactly.
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

export function MembersTable({
  initialQuery,
  initialStatus,
  initialJoinedSince = null,
  initialJoinedBefore = null,
  initialSort = null,
  initialUserApp = null,
  initialRoles = [],
  initialEntitlement = null,
  roles,
  pageSizeOptions,
  data,
  canSuspend,
  canDelete = false,
}: MembersTableProps) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [searchValue, setSearchValue] = React.useState(initialQuery);
  const [suspendTarget, setSuspendTarget] = React.useState<SuspendTarget | null>(null);
  const [deleteTarget, setDeleteTarget] = React.useState<DeleteTarget | null>(null);
  const [restrictionTarget, setRestrictionTarget] = React.useState<RestrictionTarget | null>(null);
  const [bulkAction, setBulkAction] = React.useState<BulkAction | null>(null);
  const [selectedIds, setSelectedIds] = React.useState<Set<string>>(() => new Set());

  const joinedSinceDate = React.useMemo(
    () => parseDateString(initialJoinedSince),
    [initialJoinedSince],
  );
  const joinedBeforeDate = React.useMemo(
    () => parseDateString(initialJoinedBefore),
    [initialJoinedBefore],
  );

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
    (updates: Record<string, string | string[] | null>) => {
      const next = new URLSearchParams(searchParams?.toString() ?? "");
      for (const [k, v] of Object.entries(updates)) {
        if (v === null || v === "") {
          next.delete(k);
        } else if (Array.isArray(v)) {
          next.delete(k);
          for (const item of v) next.append(k, item);
        } else {
          next.set(k, v);
        }
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
  const setStatus = (s: MemberStatus) =>
    pushParams({ status: s === "any" ? null : s, page: null });
  const setUserApp = (value: UserAppFilter | "any") =>
    pushParams({ userApp: value === "any" ? null : value, page: null });

  const [selectedRoles, setSelectedRoles] = React.useState<Set<string>>(
    () => new Set(initialRoles),
  );
  const roleKey = initialRoles.slice().sort().join(",");
  const [prevRoleKey, setPrevRoleKey] = React.useState(roleKey);
  if (prevRoleKey !== roleKey) {
    setPrevRoleKey(roleKey);
    setSelectedRoles(new Set(initialRoles));
  }
  const toggleRole = (key: string) => {
    const next = new Set(selectedRoles);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    setSelectedRoles(next);
    pushParams({ role: Array.from(next), page: null });
  };

  const setEntitlement = (value: string | null) =>
    pushParams({ entitlement: value, page: null });

  const sortState = parseSort(initialSort);
  const cycleSort = (field: SortField) => {
    let next: string | null;
    if (sortState.field !== field) {
      // First click defaults to the most intuitive direction per column.
      next = buildSortParam(field, field === "displayName" ? "asc" : "desc");
    } else if (sortState.dir === "desc") {
      next = buildSortParam(field, "asc");
    } else {
      // Third click clears the sort.
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
  const activeFilterCount =
    (initialStatus !== "any" ? 1 : 0) +
    (hasJoinedRange ? 1 : 0) +
    (initialUserApp ? 1 : 0) +
    selectedRoles.size +
    (initialEntitlement ? 1 : 0);
  const hasActiveFilters = activeFilterCount > 0;

  const joinedRangeLabel = hasJoinedRange
    ? `${formatDateLabel(joinedSinceDate) || "…"} – ${formatDateLabel(joinedBeforeDate) || "…"}`
    : "All time";

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-1 items-center gap-2">
        <div className="relative w-full max-w-sm flex-1">
          <SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            placeholder="Search by username, global name, or Discord ID…"
            value={searchValue}
            onChange={(e) => setSearchValue(e.target.value)}
            className="pl-8"
            aria-label="Search members"
          />
        </div>
        <ButtonGroup>
          <Popover>
            <PopoverTrigger asChild>
              <Button variant="outline" className="gap-2">
                <FilterIcon className="size-4" />
                Filters
                {hasActiveFilters ? (
                  <Badge variant="secondary" className="ml-1 px-1.5">
                    {activeFilterCount}
                  </Badge>
                ) : null}
              </Button>
            </PopoverTrigger>
            <PopoverContent align="start" className="w-80">
              <div className="flex flex-col gap-4">
                <div className="flex flex-col gap-1.5">
                  <span className="text-xs font-medium uppercase text-muted-foreground">
                    Status
                  </span>
                  <div className="flex flex-wrap gap-1">
                    {STATUSES.map((s) => (
                      <Button
                        key={s}
                        variant={initialStatus === s ? "default" : "outline"}
                        size="sm"
                        onClick={() => setStatus(s)}
                      >
                        {s}
                      </Button>
                    ))}
                  </div>
                </div>
                <div className="flex flex-col gap-1.5">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-medium uppercase text-muted-foreground">
                      Roles
                    </span>
                    {selectedRoles.size > 0 ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-auto px-1 py-0 text-xs"
                        onClick={() => {
                          setSelectedRoles(new Set());
                          pushParams({ role: [], page: null });
                        }}
                      >
                        Clear
                      </Button>
                    ) : null}
                  </div>
                  <div className="flex flex-wrap gap-1">
                    {roles.length === 0 ? (
                      <span className="text-xs text-muted-foreground">No roles available.</span>
                    ) : (
                      roles.map((r) => (
                        <Button
                          key={r.key}
                          variant={selectedRoles.has(r.key) ? "default" : "outline"}
                          size="sm"
                          onClick={() => toggleRole(r.key)}
                        >
                          {r.name}
                        </Button>
                      ))
                    )}
                  </div>
                </div>
                <div className="flex flex-col gap-1.5">
                  <span className="text-xs font-medium uppercase text-muted-foreground">
                    Plan
                  </span>
                  <div className="flex flex-wrap gap-1">
                    <Button
                      variant={initialEntitlement == null ? "default" : "outline"}
                      size="sm"
                      onClick={() => setEntitlement(null)}
                    >
                      Any
                    </Button>
                    <Button
                      variant={initialEntitlement === "pro" ? "default" : "outline"}
                      size="sm"
                      onClick={() => setEntitlement("pro")}
                    >
                      PRO
                    </Button>
                  </div>
                </div>
                <div className="flex flex-col gap-1.5">
                  <span className="text-xs font-medium uppercase text-muted-foreground">
                    User app
                  </span>
                  <div className="flex flex-wrap gap-1">
                    <Button
                      variant={initialUserApp == null ? "default" : "outline"}
                      size="sm"
                      onClick={() => setUserApp("any")}
                    >
                      Any
                    </Button>
                    <Button
                      variant={initialUserApp === "installed" ? "default" : "outline"}
                      size="sm"
                      onClick={() => setUserApp("installed")}
                    >
                      Installed
                    </Button>
                    <Button
                      variant={initialUserApp === "not_installed" ? "default" : "outline"}
                      size="sm"
                      onClick={() => setUserApp("not_installed")}
                    >
                      Not installed
                    </Button>
                  </div>
                </div>
                <div className="flex flex-col gap-1.5">
                  <div className="flex items-center justify-between">
                    <Label className="text-xs font-medium uppercase text-muted-foreground">
                      Joined
                    </Label>
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
                    onSelect={(range) =>
                      setJoinedRange(range?.from, range?.to)
                    }
                    className="w-full rounded-md border"
                  />
                </div>
              </div>
            </PopoverContent>
          </Popover>
          {hasActiveFilters ? (
            <Button
              variant="outline"
              className="px-2"
              aria-label="Clear filters"
              title="Clear filters"
              onClick={() => {
                setSelectedRoles(new Set());
                pushParams({
                  status: null,
                  joinedSince: null,
                  joinedBefore: null,
                  userApp: null,
                  role: [],
                  entitlement: null,
                  page: null,
                });
              }
              }
            >
              <XIcon className="size-4" />
            </Button>
          ) : null}
        </ButtonGroup>
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
              <TableHead className="w-full">
                <button
                  type="button"
                  onClick={() => cycleSort("displayName")}
                  className="-ml-2 inline-flex items-center gap-1 rounded px-2 py-1 font-medium hover:bg-muted"
                  aria-label="Sort by user"
                >
                  User
                  {sortIconFor("displayName")}
                </button>
              </TableHead>
              <TableHead className="whitespace-nowrap text-center">
                <button
                  type="button"
                  onClick={() => cycleSort("createdAt")}
                  className="inline-flex items-center gap-1 rounded px-2 py-1 font-medium hover:bg-muted"
                  aria-label="Sort by joined date"
                >
                  Joined
                  {sortIconFor("createdAt")}
                </button>
              </TableHead>
              {canSuspend || canDelete ? (
                <TableHead className="w-1 whitespace-nowrap text-right">
                  Actions
                </TableHead>
              ) : null}
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.items.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={canSuspend || canDelete ? 5 : 4}
                  className="h-24 text-center"
                >
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
                          <span className="flex items-center gap-1.5">
                            <span>{memberLabel(u)}</span>
                            <MemberFlags item={u} />
                          </span>
                          {u.discordUsername ? (
                            <span className="text-xs text-muted-foreground">
                              @{u.discordUsername}
                            </span>
                          ) : null}
                        </div>
                      </Link>
                    </TableCell>
                    <TableCell
                      className="whitespace-nowrap text-center text-sm text-muted-foreground"
                      title={u.user.createdAt}
                    >
                      <Link href={`/admin/users/${u.user.id}`} className="block">
                        {formatRelative(u.user.createdAt)}
                      </Link>
                    </TableCell>
                    {canSuspend || canDelete ? (
                      <TableCell
                        className="whitespace-nowrap text-right"
                        onClick={(e) => e.stopPropagation()}
                      >
                        <div className="flex items-center justify-end gap-1">
                          {canSuspend ? (
                            <Button
                              variant="ghost"
                              size="icon"
                              className="size-8"
                              onClick={() =>
                                setRestrictionTarget({
                                  userId: u.user.id,
                                  displayName: memberLabel(u),
                                  categories: u.banCategories ?? [],
                                })
                              }
                              aria-label="Manage user restrictions"
                              title="Manage user restrictions"
                            >
                              <ShieldIcon className="size-4" />
                            </Button>
                          ) : null}
                          {canSuspend ? (
                            <Button
                              variant="ghost"
                              size="icon"
                              className="size-8"
                              onClick={() =>
                                setSuspendTarget({
                                  mode: isSuspended ? "unsuspend" : "suspend",
                                  userId: u.user.id,
                                  displayName: memberLabel(u),
                                })
                              }
                              aria-label={
                                isSuspended ? "Unsuspend user" : "Suspend user"
                              }
                              title={isSuspended ? "Unsuspend user" : "Suspend user"}
                            >
                              {isSuspended ? (
                                <UserCheckIcon className="size-4" />
                              ) : (
                                <BanIcon className="size-4 text-destructive" />
                              )}
                            </Button>
                          ) : null}
                          {canDelete ? (
                            <Button
                              variant="ghost"
                              size="icon"
                              className="size-8"
                              onClick={() =>
                                setDeleteTarget({
                                  userId: u.user.id,
                                  displayName: memberLabel(u),
                                })
                              }
                              aria-label="Delete user"
                              title="Delete user"
                            >
                              <Trash2Icon className="size-4 text-destructive" />
                            </Button>
                          ) : null}
                        </div>
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
          <UserBanDialog
            open={restrictionTarget !== null}
            onOpenChange={(o) => {
              if (!o) setRestrictionTarget(null);
            }}
            userId={restrictionTarget?.userId ?? ""}
            displayName={restrictionTarget?.displayName ?? ""}
            initialCategories={restrictionTarget?.categories ?? []}
            onConfirm={async (userId, categories, reason) => {
              const res = await setUserBanAction(userId, categories, reason);
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

      {canDelete ? (
        <DeleteDialog
          open={deleteTarget !== null}
          onOpenChange={(o) => {
            if (!o) setDeleteTarget(null);
          }}
          userId={deleteTarget?.userId ?? ""}
          displayName={deleteTarget?.displayName ?? ""}
          onConfirm={async (userId, reason, typed) => {
            const res = await deleteUserAction(userId, reason, typed);
            if (!res.ok) throw new Error(res.error);
            toast.success("User deleted");
            router.refresh();
          }}
        />
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
