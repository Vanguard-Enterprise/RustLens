import { Suspense } from "react"

import { AppSidebar } from "./_components/app-sidebar"
import { GuildList, GuildListSkeleton } from "./_components/guild-list"
import { SiteHeader } from "@/components/site-header"
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { getCsrfToken } from "@/lib/csrf"
import { getCurrentUserSafe } from "@/lib/rustlens-api"
import { requireSession } from "@/lib/session"

export default async function Page() {
  const session = await requireSession()
  const [csrfToken, currentUser] = await Promise.all([
    getCsrfToken(),
    getCurrentUserSafe(),
  ])

  const user = {
    displayName: session.displayName,
    avatarUrl: session.avatarUrl || undefined,
    discordUsername: currentUser?.user.discordUsername,
    isInternalAdmin:
      (session.adminRoles?.length ?? 0) > 0 ||
      session.roles.includes("internal_admin"),
  }

  const discordClientId = process.env.DISCORD_CLIENT_ID
  const installBotUrl = discordClientId
    ? `https://discord.com/oauth2/authorize?client_id=${discordClientId}&permissions=8&scope=bot+applications.commands`
    : undefined

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
        installBotUrl={installBotUrl}
        user={user}
        variant="inset"
      />
      <SidebarInset>
        <SiteHeader />
        <div className="flex flex-1 flex-col">
          <div className="@container/main flex flex-1 flex-col gap-2">
            <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
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
  )
}
