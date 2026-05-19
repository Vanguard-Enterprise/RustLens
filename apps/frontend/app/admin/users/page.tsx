import { AppSidebar } from "../_components/app-sidebar";
import { MembersTable } from "./_components/members-table";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import {
  getAdminUsers,
  getCurrentUserSafe,
  type AdminUsersQuery,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const STATUSES: ReadonlyArray<NonNullable<AdminUsersQuery["status"]>> = [
  "active",
  "suspended",
  "deleted",
  "any",
];

const DEFAULT_PAGE_SIZE = 20;
const PAGE_SIZE_OPTIONS = [10, 20, 30, 40, 50];

function parsePositiveInt(value: string | string[] | undefined, fallback: number): number {
  const raw = Array.isArray(value) ? value[0] : value;
  if (!raw) return fallback;
  const n = Number.parseInt(raw, 10);
  return Number.isFinite(n) && n > 0 ? n : fallback;
}

function clampPageSize(value: number): number {
  return PAGE_SIZE_OPTIONS.includes(value) ? value : DEFAULT_PAGE_SIZE;
}

export default async function AdminUsersRoute({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const session = await requirePermission("users.read");
  const sp = await searchParams;

  const qRaw = Array.isArray(sp.q) ? sp.q[0] : sp.q;
  const q = qRaw ?? "";
  const statusRaw = Array.isArray(sp.status) ? sp.status[0] : sp.status;
  const status: NonNullable<AdminUsersQuery["status"]> =
    statusRaw && (STATUSES as readonly string[]).includes(statusRaw)
      ? (statusRaw as NonNullable<AdminUsersQuery["status"]>)
      : "active";
  const page = parsePositiveInt(sp.page, 1);
  const pageSize = clampPageSize(parsePositiveInt(sp.pageSize, DEFAULT_PAGE_SIZE));

  const [csrfToken, currentUser, members] = await Promise.all([
    getCsrfToken(),
    getCurrentUserSafe(),
    getAdminUsers({ q: q || undefined, status, page, pageSize }),
  ]);

  const user = {
    displayName: session.displayName,
    avatarUrl: session.avatarUrl || undefined,
    discordUsername: currentUser?.user.discordUsername,
    isInternalAdmin:
      (session.adminRoles?.length ?? 0) > 0 ||
      session.roles.includes("internal_admin"),
  };

  return (
    <SidebarProvider
      style={
        {
          "--sidebar-width": "calc(var(--spacing) * 72)",
          "--header-height": "calc(var(--spacing) * 12)",
        } as React.CSSProperties
      }
    >
      <AppSidebar
        csrfToken={csrfToken}
        user={user}
        permissions={session.permissions ?? []}
        variant="inset"
      />
      <SidebarInset>
        <SiteHeader />
        <div className="flex flex-1 flex-col">
          <div className="@container/main flex flex-1 flex-col gap-2">
            <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6 px-4 lg:px-6">
              <div>
                <h1 className="text-2xl font-semibold">Users</h1>
                <p className="text-sm text-muted-foreground">
                  Discord users who have used RustLens.
                </p>
              </div>
              <MembersTable
                initialQuery={q}
                initialStatus={status}
                pageSizeOptions={PAGE_SIZE_OPTIONS}
                data={members}
                canSuspend={(session.permissions ?? []).includes("users.suspend")}
              />
            </div>
          </div>
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
