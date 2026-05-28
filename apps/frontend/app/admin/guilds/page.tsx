import type { Metadata } from "next";
import { AppSidebar } from "../_components/app-sidebar";
import { GuildsTable } from "./_components/guilds-table";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import { getAdminGuilds, getCurrentUserSafe } from "@/lib/rustlens-api";
import { requireAdminSession } from "@/lib/session";

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

export const metadata: Metadata = { title: "Guilds" };

export default async function AdminGuildsPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const session = await requireAdminSession();
  const sp = await searchParams;

  const qRaw = Array.isArray(sp.q) ? sp.q[0] : sp.q;
  const q = qRaw ?? "";
  const page = parsePositiveInt(sp.page, 1);
  const pageSize = clampPageSize(parsePositiveInt(sp.pageSize, DEFAULT_PAGE_SIZE));

  const [csrfToken, currentUser, guilds] = await Promise.all([
    getCsrfToken(),
    getCurrentUserSafe(),
    getAdminGuilds({ q: q || undefined, page, pageSize }),
  ]);

  const user = {
    displayName: session.displayName,
    avatarUrl: session.avatarUrl || undefined,
    discordUsername: currentUser?.user.discordUsername,
    isInternalAdmin:
      (session.permissions?.length ?? 0) > 0 ||
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
                <h1 className="text-2xl font-semibold">Guilds</h1>
                <p className="text-sm text-muted-foreground">
                  All Discord guilds RustLens has been installed in.
                </p>
              </div>
              <GuildsTable
                initialQuery={q}
                pageSizeOptions={PAGE_SIZE_OPTIONS}
                data={guilds}
              />
            </div>
          </div>
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
