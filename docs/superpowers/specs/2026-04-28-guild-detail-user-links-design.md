# Guild detail page — user's Steam links

**Date:** 2026-04-28
**Status:** Draft

## Summary

On `/dashboard`, make each guild card clickable. Clicking a card navigates to a per-guild detail page that lists the current user's Discord↔Steam links scoped to that guild. v1 scope is intentionally minimal: navigation + a list of `(steamId, linkedAt)` rows.

## Motivation

The dashboard currently shows the user which RustLens-installed guilds they belong to but offers no way to drill in. Users have asked to see, per guild, which of their Steam accounts are linked there. Links are guild-scoped (`guild_steam_discord_links`, `gsl`), so a global "my links" view would not answer the question.

## Out of scope

- Unlinking, linking, or otherwise mutating links.
- Steam-side enrichment (persona name, avatar, BattleMetrics `bm_name`).
- Online / last-seen status on tracked servers.
- Sub-routes under the guild detail page (settings, members, tracked servers).
- Showing other members' links.

## User-facing change

1. Each guild card on `/dashboard` becomes a clickable link to `/dashboard/guilds/{guildId}`. Hover affordance: subtle background tint + pointer cursor. The whole card is the click target.
2. New page `/dashboard/guilds/{guildId}` shows:
   - The dashboard layout shell (sidebar, `SiteHeader`).
   - An in-page header: guild avatar + name as `<h1>`, plus a "Back to guilds" link to `/dashboard`.
   - A single section titled "Your linked Steam accounts" with a short description.
   - Either an empty state ("No Steam accounts linked in this guild yet.") or a list/table of links with two columns: `Steam ID` (monospace) and `Linked` (formatted date).
3. If the user navigates to a `guildId` they are not a member of (or that does not have RustLens installed), Next's 404 is shown.

## Architecture

### Frontend

**New route file:** `apps/frontend/app/dashboard/guilds/[guildId]/page.tsx`

- Async server component. Calls `requireSession()`.
- Fetches in parallel:
  - `getCsrfToken()`
  - `getCurrentUserSafe()`
  - `getMyInstalledGuilds()` — to resolve guild metadata (name, icon).
  - `getMyGuildLinks(guildId)` — new client function (see below).
- Resolves the matching guild via `guilds.find(g => g.id === guildId)`. If not present, calls `notFound()`.
- Renders the same `SidebarProvider` / `AppSidebar` / `SidebarInset` / `SiteHeader` shell as `app/dashboard/page.tsx`.
- Body wrapped in `<Suspense fallback={<LinksSkeleton />}>` mirroring the `GuildList` Suspense pattern.
- Errors bubble to Next's default `error.tsx` boundary; no custom error UI in v1.

**Card click target:** in `apps/frontend/app/dashboard/_components/guild-list.tsx`, wrap each `<GuildCard>` inside `GuildList`'s `.map` with `<Link href={`/dashboard/guilds/${guild.id}`}>`. Add `hover:bg-accent/30 transition-colors cursor-pointer` to the existing `Card` className. The visible `CardTitle` text serves as the link's accessible name; no extra `aria-label` needed.

**New components in `apps/frontend/app/dashboard/guilds/[guildId]/_components/`:**

- `LinksList.tsx` — async server component, fetches links via `getMyGuildLinks(guildId)`, renders empty state or a `Card` containing a simple `<table>` (responsive: stacked rows on small screens). Uses existing `Card`/`CardContent` primitives.
- `LinksSkeleton.tsx` — 3–5 `Skeleton` rows in the same `Card` shell.

**API client** in `apps/frontend/lib/rustlens-api.ts`:

```ts
export type GuildLink = { steamId: string; linkedAt: string };

export async function getMyGuildLinks(guildId: string): Promise<GuildLink[]> {
  const payload = await backendFetch<{ items: GuildLink[] }>(
    `/api/app/me/guilds/${encodeURIComponent(guildId)}/links`,
    undefined,
    { requireSession: true },
  );
  return payload.items;
}
```

### Backend

**New endpoint:** `GET /api/app/me/guilds/{guildId}/links`

- Registered in `apps/backend/internal/http/router.go` at line 215, inside the existing `appSessionMiddleware` group, alongside `/me/guilds/installed`.
- Handler lives in `apps/backend/internal/http/app_handlers.go` as `GetCurrentAppUserGuildLinks`.

**Behavior:**

1. Resolve session actor; require non-empty `discordID`. If missing → 401 (matches existing `/me/guilds/installed` behavior).
2. Read `guildId` from URL param.
3. Authorization: confirm the caller is an active member of `guildId`. Add a thin store helper `IsGuildMember(ctx, guildID, discordID) (bool, error)` in `apps/backend/internal/store/app_users.go` that runs `SELECT EXISTS(SELECT 1 FROM user_guilds WHERE guild_id=$1 AND discord_id=$2 AND is_active=true)`. If false → 403. (We add a dedicated helper because `/me/guilds/installed` derives membership implicitly from its result set; here we need an explicit per-guild check.)
4. Call existing `store.ListGuildLinksByGuildAndDiscord(ctx, guildId, discordID)`. This already filters via `hidden_discord_link_identities`, so no additional filtering is needed.
5. Map results to JSON:

```json
{
  "items": [
    { "steamId": "76561198...", "linkedAt": "2026-03-12T18:42:11Z" }
  ]
}
```

`linkedAt` is RFC3339, sourced from `created_at`. Ordering preserved from the store query (`created_at ASC`).

**OpenAPI:** add the schema (`UserGuildLink`, `UserGuildLinkListResponse`) and path entry in `apps/backend/internal/http/openapi.go` next to the existing dashboard guild schemas.

## Data flow

```
Browser
  → /dashboard/guilds/[guildId] (Next server component)
    → getMyInstalledGuilds()      (existing) → /api/app/me/guilds/installed
    → getMyGuildLinks(guildId)    (new)      → /api/app/me/guilds/{guildId}/links
                                                 → membership check on user_guilds
                                                 → ListGuildLinksByGuildAndDiscord
                                                 → JSON { items: [...] }
  ← rendered HTML with guild header + links table or empty state
```

## Error handling

| Condition | Behavior |
|---|---|
| No session | 401 from backend; frontend `requireSession: true` redirects to login. |
| Caller not a member of guild | 403 from backend → frontend treats as `notFound()`-equivalent (caught and re-thrown as 404 in the page so users can't probe arbitrary guild IDs). |
| Guild not in `getMyInstalledGuilds()` | Page calls `notFound()` before issuing the links request. |
| Backend 5xx | Bubbles to Next's default error boundary. |

## Testing

**Backend** (`apps/backend/internal/http/`):

- New table-driven test alongside `bot_guilds_handlers_test.go` covering:
  - 200 with empty `items` when caller is a member but has no links.
  - 200 with populated `items` ordered by `created_at`.
  - 403 when caller is not a member of `guildId`.
  - 401 when session is missing.
  - Hidden identities are excluded (relies on store-level filter; assert via fixture).

**Frontend:**

- Manual verification: dashboard card click navigates correctly; detail page renders for a guild with links and for a guild with none; navigating to `/dashboard/guilds/<unknown-id>` shows 404.
- No new automated frontend tests required for v1 (matches the precedent set by the dashboard installed-guilds spec).

## Files touched

**New:**
- `apps/frontend/app/dashboard/guilds/[guildId]/page.tsx`
- `apps/frontend/app/dashboard/guilds/[guildId]/_components/links-list.tsx`
- `apps/frontend/app/dashboard/guilds/[guildId]/_components/links-skeleton.tsx`

**Modified:**
- `apps/frontend/app/dashboard/_components/guild-list.tsx` — wrap `GuildCard` in `<Link>`, add hover styling.
- `apps/frontend/lib/rustlens-api.ts` — add `GuildLink` type and `getMyGuildLinks`.
- `apps/backend/internal/http/router.go` — register new route.
- `apps/backend/internal/http/app_handlers.go` — new handler.
- `apps/backend/internal/http/openapi.go` — schema + path entry.
- `apps/backend/internal/store/app_users.go` — add `IsGuildMember`.
- `apps/backend/internal/http/bot_guilds_handlers_test.go` (or sibling) — handler tests.
