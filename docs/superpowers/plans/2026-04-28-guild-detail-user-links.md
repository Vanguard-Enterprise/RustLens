# Guild Detail Page — User Steam Links Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-guild detail page at `/dashboard/guilds/{guildId}` that lists the current user's Discord↔Steam links scoped to that guild, and make each dashboard guild card navigate there on click.

**Architecture:** Backend exposes a new authenticated endpoint `GET /api/app/me/guilds/{guildId}/links` that verifies guild membership and returns the caller's `guild_steam_discord_links` rows. Frontend adds a Next.js server-component route that reuses the dashboard layout shell, fetches guild metadata via existing `/me/guilds/installed`, and renders an empty state or simple table of `(steamId, linkedAt)` rows. Existing dashboard guild card is wrapped with `next/link`.

**Tech Stack:** Go 1.23 (chi router, pgx), Next.js 16 server components, React 19, Tailwind, shadcn/ui card primitives.

**Spec:** `docs/superpowers/specs/2026-04-28-guild-detail-user-links-design.md`

---

## File Structure

**New files:**
- `apps/frontend/app/dashboard/guilds/[guildId]/page.tsx` — server component for the detail page.
- `apps/frontend/app/dashboard/guilds/[guildId]/_components/links-list.tsx` — async server component rendering the list.
- `apps/frontend/app/dashboard/guilds/[guildId]/_components/links-skeleton.tsx` — Suspense fallback skeleton.

**Modified files:**
- `apps/frontend/app/dashboard/_components/guild-list.tsx` — wrap `GuildCard` in `<Link>` with hover styling.
- `apps/frontend/lib/rustlens-api.ts` — add `GuildLink` type and `getMyGuildLinks(guildId)`.
- `apps/backend/internal/store/app_users.go` — add `IsGuildMember` helper.
- `apps/backend/internal/http/app_handlers.go` — add `GetCurrentAppUserGuildLinks` handler.
- `apps/backend/internal/http/router.go` — register new route.
- `apps/backend/internal/http/bot_guilds_handlers_test.go` — add unauthenticated-request test for the new handler.

**Note on OpenAPI:** the precedent endpoint `/api/app/me/guilds/installed` is not present in `openapi.go` either, so we are not adding the new endpoint to the OpenAPI doc in this plan. (If/when `installed` is added, this one should be added at the same time.)

---

## Task 1: Add `IsGuildMember` store helper

**Files:**
- Modify: `apps/backend/internal/store/app_users.go` (append a new method on `*PostgresStore`)
- Test: `apps/backend/internal/store/app_users_test.go` (create if absent — see Step 1)

- [ ] **Step 1: Inspect for existing test file**

Run: `ls apps/backend/internal/store/ | grep app_users_test`
If the file exists, append; otherwise create it with package header `package store`.

Decision rule: if no DB-touching tests exist in this package yet, **skip the unit test for the SQL itself** (no infrastructure to write it against) and rely on the handler-level test in Task 5 covering the membership branch indirectly. Add only a compile-level test: ensure the new method exists with the expected signature.

- [ ] **Step 2: Write a compile-only signature test**

Create or append to `apps/backend/internal/store/app_users_test.go`:

```go
package store

import (
	"context"
	"testing"
)

// Compile-time guarantee that IsGuildMember has the expected signature.
var _ = func(p *PostgresStore, ctx context.Context, guildID, discordID string) (bool, error) {
	return p.IsGuildMember(ctx, guildID, discordID)
}

func TestIsGuildMember_SignatureCompiles(t *testing.T) {
	t.Helper()
	// Intentionally empty: the var assignment above fails the build if the
	// signature drifts.
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/store/ -run TestIsGuildMember_SignatureCompiles`
Expected: FAIL — build error: `p.IsGuildMember undefined`.

- [ ] **Step 4: Implement `IsGuildMember`**

Append to `apps/backend/internal/store/app_users.go`:

```go
// IsGuildMember returns true if the given Discord user is recorded as an
// active member of the given guild in user_guilds.
func (p *PostgresStore) IsGuildMember(ctx context.Context, guildID, discordID string) (bool, error) {
	if guildID == "" || discordID == "" {
		return false, nil
	}
	var exists bool
	err := p.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM user_guilds
			WHERE guild_id = $1
			  AND discord_id = $2
			  AND is_active = TRUE
		)
	`, guildID, discordID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd apps/backend && go test ./internal/store/ -run TestIsGuildMember_SignatureCompiles`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/store/app_users.go apps/backend/internal/store/app_users_test.go
git commit -m "feat(backend): add IsGuildMember store helper"
```

---

## Task 2: Add `GetCurrentAppUserGuildLinks` handler

**Files:**
- Modify: `apps/backend/internal/http/app_handlers.go` (append at end of file)
- Test: `apps/backend/internal/http/bot_guilds_handlers_test.go` (append)

- [ ] **Step 1: Write failing handler test (no session)**

Append to `apps/backend/internal/http/bot_guilds_handlers_test.go`:

```go
func TestGetCurrentAppUserGuildLinks_RequiresSession(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/app/me/guilds/123456789012345678/links", nil)
	w := httptest.NewRecorder()
	h.GetCurrentAppUserGuildLinks(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/http/ -run TestGetCurrentAppUserGuildLinks_RequiresSession`
Expected: FAIL — build error: `h.GetCurrentAppUserGuildLinks undefined`.

- [ ] **Step 3: Implement the handler**

Append to `apps/backend/internal/http/app_handlers.go`:

```go
// GetCurrentAppUserGuildLinks returns the caller's Discord<->Steam links scoped
// to a single guild. Membership in the guild is required.
func (h *Handler) GetCurrentAppUserGuildLinks(w http.ResponseWriter, r *http.Request) {
	actor, ok, status, message := requireAppActor(r, h.postgres)
	if !ok {
		writeError(w, status, message)
		return
	}

	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	discordID, err := h.postgres.GetAppUserDiscordID(r.Context(), actor.ID)
	if err != nil {
		log.Printf("[backend] GetAppUserDiscordID error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load guild links")
		return
	}
	if discordID == "" {
		writeError(w, http.StatusForbidden, "not a member of this guild")
		return
	}

	isMember, err := h.postgres.IsGuildMember(r.Context(), guildID, discordID)
	if err != nil {
		log.Printf("[backend] IsGuildMember error for %s guild=%s: %v", actor.ID, guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to load guild links")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not a member of this guild")
		return
	}

	rows, err := h.postgres.ListGuildLinksByGuildAndDiscord(r.Context(), guildID, discordID)
	if err != nil {
		log.Printf("[backend] ListGuildLinksByGuildAndDiscord error for %s guild=%s: %v", actor.ID, guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to load guild links")
		return
	}

	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"steamId":  row.SteamID,
			"linkedAt": row.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd apps/backend && go test ./internal/http/ -run TestGetCurrentAppUserGuildLinks_RequiresSession`
Expected: PASS (401 returned because `requireAppActor` rejects the missing session).

- [ ] **Step 5: Run full backend test suite to confirm no regressions**

Run: `cd apps/backend && go test ./...`
Expected: PASS for all packages.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/http/app_handlers.go apps/backend/internal/http/bot_guilds_handlers_test.go
git commit -m "feat(backend): add /me/guilds/{guildID}/links handler"
```

---

## Task 3: Register the new route

**Files:**
- Modify: `apps/backend/internal/http/router.go`

- [ ] **Step 1: Add route registration**

In `apps/backend/internal/http/router.go`, locate the `appSessionMiddleware` group at line ~211–217:

```go
r.Group(func(r chi.Router) {
    r.Use(appSessionMiddleware(handler.appSessions))
    r.Post("/auth/email/verify/request", handler.RequestEmailVerification)
    r.Get("/me/connections", handler.GetCurrentAppUserConnections)
    r.Get("/me/guilds", handler.GetCurrentAppUserGuilds)
    r.Get("/me/guilds/installed", handler.GetCurrentAppUserInstalledGuilds)
})
```

Add the new route below the installed-guilds line:

```go
r.Get("/me/guilds/{guildID}/links", handler.GetCurrentAppUserGuildLinks)
```

- [ ] **Step 2: Build to verify the route compiles**

Run: `cd apps/backend && go build ./...`
Expected: PASS (exit 0, no output).

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/http/router.go
git commit -m "feat(backend): register /me/guilds/{guildID}/links route"
```

---

## Task 4: Add `getMyGuildLinks` to the frontend API client

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 1: Add the type**

In `apps/frontend/lib/rustlens-api.ts`, near the `DashboardGuild` types (around line 60–80), append:

```ts
export type GuildLink = {
	steamId: string;
	linkedAt: string;
};
```

- [ ] **Step 2: Add the fetch function**

Below the existing `getMyInstalledGuilds` function (around line 320), add:

```ts
/**
 * Returns the current user's Discord<->Steam links scoped to a single guild.
 * Throws if the caller is not a member of the guild (backend returns 403).
 */
export async function getMyGuildLinks(guildId: string): Promise<GuildLink[]> {
	const payload = await backendFetch<{ items: GuildLink[] }>(
		`/api/app/me/guilds/${encodeURIComponent(guildId)}/links`,
		undefined,
		{ requireSession: true },
	);
	return payload.items;
}
```

- [ ] **Step 3: Type-check the frontend**

Run: `cd apps/frontend && npx tsc --noEmit`
Expected: PASS (no errors).

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): add getMyGuildLinks API client function"
```

---

## Task 5: Build the links list components

**Files:**
- Create: `apps/frontend/app/dashboard/guilds/[guildId]/_components/links-skeleton.tsx`
- Create: `apps/frontend/app/dashboard/guilds/[guildId]/_components/links-list.tsx`

- [ ] **Step 1: Create the skeleton component**

Create `apps/frontend/app/dashboard/guilds/[guildId]/_components/links-skeleton.tsx`:

```tsx
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

export function LinksSkeleton() {
	return (
		<Card>
			<CardHeader>
				<CardTitle>Your linked Steam accounts</CardTitle>
			</CardHeader>
			<CardContent className="flex flex-col gap-3">
				{Array.from({ length: 4 }).map((_, i) => (
					<div key={i} className="flex items-center justify-between gap-4">
						<Skeleton className="h-4 w-48" />
						<Skeleton className="h-4 w-24" />
					</div>
				))}
			</CardContent>
		</Card>
	);
}
```

- [ ] **Step 2: Create the links list component**

Create `apps/frontend/app/dashboard/guilds/[guildId]/_components/links-list.tsx`:

```tsx
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { getMyGuildLinks, type GuildLink } from "@/lib/rustlens-api";

function formatLinkedAt(iso: string): string {
	const date = new Date(iso);
	if (Number.isNaN(date.getTime())) {
		return iso;
	}
	return new Intl.DateTimeFormat(undefined, {
		year: "numeric",
		month: "short",
		day: "numeric",
	}).format(date);
}

function EmptyState() {
	return (
		<div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed py-12 text-center">
			<div className="font-heading text-base">
				No Steam accounts linked in this guild yet
			</div>
			<p className="max-w-md text-sm text-muted-foreground">
				When you link a Steam account through the bot in this server, it
				will show up here.
			</p>
		</div>
	);
}

function LinksTable({ links }: { links: GuildLink[] }) {
	return (
		<Card>
			<CardHeader>
				<CardTitle>Your linked Steam accounts</CardTitle>
			</CardHeader>
			<CardContent className="p-0">
				<table className="w-full text-sm">
					<thead className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
						<tr>
							<th className="px-6 py-3 font-medium">Steam ID</th>
							<th className="px-6 py-3 font-medium">Linked</th>
						</tr>
					</thead>
					<tbody>
						{links.map((link) => (
							<tr key={link.steamId} className="border-b last:border-0">
								<td className="px-6 py-3 font-mono">{link.steamId}</td>
								<td className="px-6 py-3 text-muted-foreground">
									{formatLinkedAt(link.linkedAt)}
								</td>
							</tr>
						))}
					</tbody>
				</table>
			</CardContent>
		</Card>
	);
}

export async function LinksList({ guildId }: { guildId: string }) {
	const links = await getMyGuildLinks(guildId);
	if (links.length === 0) {
		return <EmptyState />;
	}
	return <LinksTable links={links} />;
}
```

- [ ] **Step 3: Type-check the frontend**

Run: `cd apps/frontend && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/app/dashboard/guilds/
git commit -m "feat(frontend): add LinksList and LinksSkeleton components"
```

---

## Task 6: Build the guild detail page

**Files:**
- Create: `apps/frontend/app/dashboard/guilds/[guildId]/page.tsx`

- [ ] **Step 1: Create the page**

Create `apps/frontend/app/dashboard/guilds/[guildId]/page.tsx`:

```tsx
import Link from "next/link";
import { notFound } from "next/navigation";
import { Suspense } from "react";

import { AppSidebar } from "../../_components/app-sidebar";
import { LinksList } from "./_components/links-list";
import { LinksSkeleton } from "./_components/links-skeleton";
import { SiteHeader } from "@/components/site-header";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { guildIconUrl } from "@/lib/discord-cdn";
import { getCsrfToken } from "@/lib/csrf";
import { getCurrentUserSafe, getMyInstalledGuilds } from "@/lib/rustlens-api";
import { requireSession } from "@/lib/session";

function initials(name: string): string {
	return name
		.split(/\s+/)
		.filter(Boolean)
		.slice(0, 2)
		.map((part) => part[0]!.toUpperCase())
		.join("");
}

type PageProps = { params: Promise<{ guildId: string }> };

export default async function Page({ params }: PageProps) {
	const { guildId } = await params;
	const session = await requireSession();
	const [csrfToken, currentUser, guilds] = await Promise.all([
		getCsrfToken(),
		getCurrentUserSafe(),
		getMyInstalledGuilds(),
	]);

	const guild = guilds.find((g) => g.id === guildId);
	if (!guild) {
		notFound();
	}

	const user = {
		displayName: session.displayName,
		avatarUrl: session.avatarUrl || undefined,
		discordUsername: currentUser?.user.discordUsername,
		isInternalAdmin:
			(session.adminRoles?.length ?? 0) > 0 ||
			session.roles.includes("internal_admin"),
	};

	const iconSrc = guildIconUrl(guild.id, guild.iconHash) ?? undefined;

	return (
		<SidebarProvider
			style={
				{
					"--sidebar-width": "calc(var(--spacing) * 72)",
					"--header-height": "calc(var(--spacing) * 12)",
				} as React.CSSProperties
			}
		>
			<AppSidebar csrfToken={csrfToken} user={user} variant="inset" />
			<SidebarInset>
				<SiteHeader />
				<div className="flex flex-1 flex-col">
					<div className="@container/main flex flex-1 flex-col gap-2">
						<div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
							<div className="flex flex-col gap-2 px-4 lg:px-6">
								<Link
									href="/dashboard"
									className="text-sm text-muted-foreground hover:underline"
								>
									← Back to guilds
								</Link>
								<div className="flex items-center gap-3">
									<Avatar size="lg" className="ring-2 ring-background">
										{iconSrc ? <AvatarImage src={iconSrc} alt="" /> : null}
										<AvatarFallback>{initials(guild.name)}</AvatarFallback>
									</Avatar>
									<h1 className="font-heading text-xl">{guild.name}</h1>
								</div>
								<p className="text-sm text-muted-foreground">
									Your Discord-to-Steam account links scoped to this guild.
								</p>
							</div>
							<div className="px-4 lg:px-6">
								<Suspense fallback={<LinksSkeleton />}>
									<LinksList guildId={guildId} />
								</Suspense>
							</div>
						</div>
					</div>
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
```

- [ ] **Step 2: Type-check the frontend**

Run: `cd apps/frontend && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/app/dashboard/guilds/[guildId]/page.tsx
git commit -m "feat(frontend): add /dashboard/guilds/[guildId] detail page"
```

---

## Task 7: Make dashboard guild cards clickable

**Files:**
- Modify: `apps/frontend/app/dashboard/_components/guild-list.tsx`

- [ ] **Step 1: Add the import**

At the top of `apps/frontend/app/dashboard/_components/guild-list.tsx`, after the `next/image` import, add:

```ts
import Link from "next/link";
```

- [ ] **Step 2: Wrap `GuildCard` in `<Link>` and add hover styling**

In `guild-list.tsx`, change the `GuildList` component's `.map` body so each card is wrapped in a `<Link>`:

Before:

```tsx
{guilds.map((guild) => (
  <GuildCard key={guild.id} guild={guild} />
))}
```

After:

```tsx
{guilds.map((guild) => (
  <Link
    key={guild.id}
    href={`/dashboard/guilds/${guild.id}`}
    className="block rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-ring"
  >
    <GuildCard guild={guild} />
  </Link>
))}
```

Also update `GuildCard`'s `<Card>` className to include hover affordance:

Before:

```tsx
<Card className="overflow-hidden">
```

After:

```tsx
<Card className="overflow-hidden transition-colors hover:bg-accent/30 cursor-pointer h-full">
```

- [ ] **Step 3: Type-check the frontend**

Run: `cd apps/frontend && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 4: Manual verification**

Run the frontend dev server (in a separate shell):

```bash
cd apps/frontend && npm run dev
```

In the browser, log in and visit `/dashboard`. Verify:
- Hovering a guild card shows the hover background tint and pointer cursor.
- Clicking a card navigates to `/dashboard/guilds/{id}`.
- The detail page shows the guild header, "Back to guilds" link, and either an empty state or a table of links.
- Clicking "Back to guilds" returns to `/dashboard`.
- Visiting `/dashboard/guilds/000000000000000000` (a guild you are not a member of) shows the 404 page.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/app/dashboard/_components/guild-list.tsx
git commit -m "feat(frontend): make dashboard guild cards link to detail page"
```

---

## Task 8: Final verification

- [ ] **Step 1: Run backend tests**

Run: `cd apps/backend && go test ./...`
Expected: PASS across all packages.

- [ ] **Step 2: Run frontend type-check**

Run: `cd apps/frontend && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 3: Run frontend lint (if configured)**

Run: `cd apps/frontend && npm run lint --if-present`
Expected: PASS (or skipped if not configured).

- [ ] **Step 4: Confirm no stray uncommitted changes**

Run: `git status`
Expected: working tree clean (all task commits in place).
