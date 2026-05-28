import type { Metadata } from "next";
import { Suspense } from "react"

import { AppSidebar } from "./_components/app-sidebar"
import { DiscordRoleConnectionCard } from "./_components/discord-role-connection-card"
import { DashboardNoticeToast } from "./_components/notice-toast"
import { GuildList, GuildListSkeleton } from "./_components/guild-list"
import { SiteHeader } from "@/components/site-header"
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { getCsrfToken } from "@/lib/csrf"
import { getCurrentUserSafe } from "@/lib/rustlens-api"
import { requireSession } from "@/lib/session"

export const metadata: Metadata = { title: "Dashboard" };

type DashboardPageProps = {
  searchParams: Promise<{ notice?: string | string[]; error?: string | string[] }>
}

function firstParam(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value
}

export default async function Page({ searchParams }: DashboardPageProps) {
  const params = await searchParams
  const notice = firstParam(params.notice)
  const error = firstParam(params.error)
  const session = await requireSession()
  const [csrfToken, currentUser] = await Promise.all([
    getCsrfToken(),
    getCurrentUserSafe(),
  ])

  // Resolve admin status from live backend state (currentUser) rather than the
  // cookie session. The cookie is a snapshot captured at login and doesn't
  // reflect role grants made afterwards — falling back to it would force a
  // logout/login dance every time an admin role changes.
  const livePermissions = currentUser?.permissions ?? session.permissions ?? []
  const liveEntitlements = currentUser?.entitlements ?? session.entitlements ?? []
  const liveRoles = currentUser?.user.roles ?? session.roles ?? []
  const isInternalAdmin =
    livePermissions.length > 0 ||
    liveRoles.includes("internal_admin")

  const user = {
    displayName: session.displayName,
    avatarUrl: session.avatarUrl || undefined,
    discordUsername: currentUser?.user.discordUsername,
    isInternalAdmin,
  }

  const installBotUrl = "/install-bot"

  return (
    <>
    <DashboardNoticeToast notice={notice} error={error} />
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
        installBotUrl={installBotUrl}
        user={user}
        variant="inset"
      />
      <SidebarInset>
        <SiteHeader />
        <div className="flex flex-1 flex-col">
          <div className="@container/main flex flex-1 flex-col gap-2">
            <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
              <div className="px-4 lg:px-6">
                <DiscordRoleConnectionCard
                  summary={currentUser?.discordRoleConnection ?? null}
                  hasPro={liveEntitlements.includes("pro")}
                />
              </div>
              <div className="flex flex-col gap-1 px-4 lg:px-6">
                <h1 className="font-heading text-xl">Your guilds</h1>
                <p className="text-sm text-muted-foreground">
                  Discord servers you&apos;re a member of that have RustLens
                  installed.
                </p>
              </div>
              <div className="px-4 lg:px-6">
                <Suspense fallback={<GuildListSkeleton />}>
                  <GuildList />
                </Suspense>
              </div>
            </div>
          </div>
        </div>
      </SidebarInset>
    </SidebarProvider>
    </>
  )
}
