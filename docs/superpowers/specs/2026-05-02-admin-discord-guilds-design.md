# Admin Discord / Guilds page — Design

**Date:** 2026-05-02
**Status:** Approved

## Summary

Add a "Discord" section to the `/admin` sidebar containing a single child page at `/admin/guilds`. The page lists every guild the bot is currently or previously installed in (`bot_guilds`), with server-side pagination and a single search box that matches guild name, guild ID, and owner username. This is the first real admin sub-page in the app, so the admin sidebar gets its own clean nav structure (separate from the dashboard placeholders) at the same time.

No filters, no sorting beyond default name-ascending, no per-guild detail page.

## Sidebar

When `DashboardAppSidebar` is rendered with `area="admin"`, it renders an admin-specific nav structure instead of the shared dashboard placeholders.

- A single `NavGroup` (existing component, `apps/frontend/components/nav-group.tsx`) labeled **Discord** with one item: **Guilds → `/admin/guilds`**.
- The header (RustLens logo) and footer (`SidebarNavUser`) remain unchanged.
- `NavMain` primary-nav switching still surfaces the **Dashboard** link so admins can return to `/dashboard`.
- Dashboard-only sections (`NavDocuments`, `NavSecondary`, the Lifecycle/Analytics/Projects/Team placeholders) are NOT rendered for the admin area.

The pre-existing `getNavGroups("admin", ...)` scaffolding in `apps/frontend/components/app-shared.tsx` stays inert — it lists routes whose pages do not exist (`/admin/users`, `/admin/audit`, etc.) and surfacing them would render dead links. That scaffolding is left untouched until those pages get built.

## Backend endpoint

Repurpose the existing `GET /api/app/admin/guilds`. The current implementation reads `guild_settings` (configured guilds only, hard-capped at 500, no pagination/search) and is not consumed by any live frontend page. Rewriting it in place keeps the URL stable.

### Request

| Param | Type | Default | Notes |
|---|---|---|---|
| `q` | string | `""` | Search; matches `bot_guilds.name ILIKE '%q%'` OR `bot_guilds.guild_id ILIKE 'q%'` OR `bot_guilds.owner_username ILIKE '%q%'` |
| `page` | int | `1` | 1-based |
| `pageSize` | int | `20` | capped at `100`; values `<= 0` clamp to default |

### Response

```json
{
  "items": [
    {
      "guildId": "1234567890",
      "name": "My Guild",
      "iconHash": "abc...",
      "ownerDiscordId": "9876...",
      "ownerUsername": "alice",
      "ownerGlobalName": "Alice",
      "ownerAvatarHash": "def...",
      "approximateMemberCount": 1234,
      "isInstalled": true,
      "leftAt": null,
      "isConfigured": true,
      "trackedServerCount": 3
    }
  ],
  "total": 47,
  "page": 1,
  "pageSize": 20
}
```

`leftAt` is RFC3339 when present, `null` otherwise. `iconHash`, `ownerDiscordId`, `ownerUsername`, `ownerGlobalName`, `ownerAvatarHash`, and `approximateMemberCount` are nullable.

### Query

Single statement using a windowed `COUNT(*) OVER ()` for total and `LEFT JOIN`s for configured/tracked metadata:

```sql
SELECT bg.guild_id, bg.name, bg.icon_hash,
       bg.owner_discord_id, bg.owner_username, bg.owner_global_name, bg.owner_avatar_hash,
       bg.approximate_member_count, bg.is_installed, bg.left_at,
       (gs.guild_id IS NOT NULL) AS is_configured,
       COALESCE(gts.cnt, 0)      AS tracked_server_count,
       COUNT(*) OVER ()          AS total
FROM bot_guilds bg
LEFT JOIN guild_settings gs ON gs.guild_id = bg.guild_id
LEFT JOIN (
  SELECT guild_id, COUNT(*)::int AS cnt
  FROM guild_tracked_servers
  GROUP BY guild_id
) gts ON gts.guild_id = bg.guild_id
WHERE ($1 = ''
       OR bg.name ILIKE '%' || $1 || '%'
       OR bg.guild_id ILIKE $1 || '%'
       OR bg.owner_username ILIKE '%' || $1 || '%')
ORDER BY bg.name ASC, bg.guild_id ASC
LIMIT $2 OFFSET $3;
```

Result iteration reads `total` from the first row (or returns `0` when no rows match).

### Implementation split

- `apps/backend/internal/store/bot_guilds.go` gains a `ListAdminGuilds(ctx, ListAdminGuildsOpts) ([]AdminGuildRow, int, error)` method.
- `apps/backend/internal/http/admin_read_handlers.go` rewrites `ListAdminGuilds` to: parse query params, clamp `pageSize`/`page`, call the store, marshal to the response shape above.
- `apps/backend/internal/http/openapi.go` updates the `/api/app/admin/guilds` path entry with the new query params and response schema.

The pre-existing `adminGuildItem` struct (`admin_role_id`, `active_members`, `linked_members`, `tracked_servers`) is removed; nothing in the repo references it.

## Frontend page

### Server component — `apps/frontend/app/admin/guilds/page.tsx`

1. `await requireAdminSession()` — same gate as `apps/frontend/app/admin/page.tsx`.
2. Parse `searchParams` for `q`, `page`, `pageSize` (numeric coercion with sensible defaults).
3. `await getAdminGuilds({ q, page, pageSize })`.
4. Render `SidebarProvider` + `<AppSidebar variant="inset" csrfToken user />` + `SidebarInset` + `SiteHeader` shell, identical to `/admin/page.tsx`.
5. Inside `SidebarInset`, render `<GuildsTable initialData={...} />` (client component).

### Client component — `apps/frontend/app/admin/guilds/_components/guilds-table.tsx`

- **Search input** (top of section): debounced 300 ms; updates `?q=` via `router.push` and resets to `page=1` on change.
- **Table** (shadcn `Table`) with columns:

  | Column | Source |
  |---|---|
  | Icon | guild icon CDN URL via `lib/discord-cdn.ts`; fallback to first letter of name |
  | Name | `name` |
  | Guild ID | `guildId` (mono font, click-to-copy) |
  | Owner | small avatar + `ownerGlobalName ?? ownerUsername ?? "—"` |
  | Members | `approximateMemberCount` (locale-formatted, "—" if null) |
  | Installed | `Badge` "Installed" (green) or `Badge` "Left <relative leftAt>" (muted) |
  | Configured | `Badge` "Configured" or `—` |
  | Tracked | numeric; dim/zero text when `0` |

- **Pagination footer** copied from `components/data-table.tsx:527-609` with these adjustments:
  - Wired to URL `page` / `pageSize` params (via `router.push`) instead of TanStack table state.
  - Rows-per-page options unchanged: `[10, 20, 30, 40, 50]`. **Default `pageSize` is 20.**
  - Page X of Y where `Y = Math.max(1, Math.ceil(total / pageSize))`.
  - First / Prev / Next / Last buttons (`ChevronsLeftIcon`, `ChevronLeftIcon`, `ChevronRightIcon`, `ChevronsRightIcon`).
  - The "N of M row(s) selected" left-side text is dropped (no row selection).

- **Empty state**: "No guilds found." centered in the table body when `items.length === 0`.

### API helper — `apps/frontend/lib/rustlens-api.ts`

Replace the existing `getAdminGuilds()` with:

```ts
export type AdminGuildItem = {
  guildId: string;
  name: string;
  iconHash: string | null;
  ownerDiscordId: string | null;
  ownerUsername: string | null;
  ownerGlobalName: string | null;
  ownerAvatarHash: string | null;
  approximateMemberCount: number | null;
  isInstalled: boolean;
  leftAt: string | null;
  isConfigured: boolean;
  trackedServerCount: number;
};

export type AdminGuildsPage = {
  items: AdminGuildItem[];
  total: number;
  page: number;
  pageSize: number;
};

export type AdminGuildsQuery = { q?: string; page?: number; pageSize?: number };

export async function getAdminGuilds(params: AdminGuildsQuery = {}): Promise<AdminGuildsPage> { /* ... */ }
```

The old `AdminGuildItem` type definition is replaced.

## Files

**New**
- `apps/frontend/app/admin/guilds/page.tsx`
- `apps/frontend/app/admin/guilds/_components/guilds-table.tsx`

**Modified**
- `apps/frontend/components/dashboard-app-sidebar.tsx` — admin-area branch rendering `<NavGroup label="Discord" .../>`.
- `apps/frontend/lib/rustlens-api.ts` — replace `getAdminGuilds` and `AdminGuildItem`.
- `apps/backend/internal/http/admin_read_handlers.go` — rewrite `ListAdminGuilds`.
- `apps/backend/internal/store/bot_guilds.go` — add `ListAdminGuilds`.
- `apps/backend/internal/http/openapi.go` — update `/api/app/admin/guilds` schema.
- `AGENTS.md` — refresh the `app/admin/...` line, mention `/admin/guilds`, and update the `/api/app/admin/guilds` description so it reflects bot-guilds-sourced pagination/search.

## Tests

- `apps/backend/internal/http/admin_read_handlers_test.go` extends the existing guilds test:
  - empty `bot_guilds` returns `{items:[], total:0}`.
  - pagination — populate ~30 rows, assert `page=1 pageSize=10` and `page=2 pageSize=10` slice correctly and `total=30`.
  - search — `q` matches name substring (case-insensitive), guild-id prefix, owner-username substring; mismatch returns 0.
  - `is_configured` is true only for guilds with a `guild_settings` row.
  - `tracked_server_count` joins correctly (including 0 for guilds with no `guild_tracked_servers` rows).

No new frontend tests; existing admin pages have no page-level tests and the new page does not warrant breaking that pattern.

## Out of scope

- Filters of any kind.
- Per-guild detail at `/admin/guilds/[guildId]`.
- Sorting beyond default name-ascending.
- Surfacing other `ADMIN_NAV` entries (users, audit, roles, etc.) whose pages do not exist.
