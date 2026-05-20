# Discord Members Page — Design

**Date:** 2026-05-18
**Status:** Approved

## Summary

Add a new **Members** page under the admin dashboard's Discord sidebar group. The
page lists every `app_users` row that holds zero admin roles — the inverse of
the `/admin/users` page (which was recently scoped to admin-only).

To populate that list with every Discord user who has ever touched RustLens,
the bot will ensure an `app_users` row exists for the invoker on every
interaction (slash command, autocomplete, button, modal). Users who only ever
signed in via the website are already covered by the existing Discord OAuth
flow.

The first iteration is read-only — view, search, click through to the existing
admin user detail page. Mutations come in a later iteration once we see the
shape of real usage.

## Non-goals

- No suspend / grant / delete / role-promotion controls from the page itself.
  (The detail page at `/admin/users/[userId]` already provides those for users
  with `users.write` / `admins.grant`.)
- No historical backfill of past bot interactions. Members appear as they
  invoke the bot for the first time after this lands.
- No Discord guild-member roster view (i.e. members from `user_guilds` who
  never invoked the bot or logged into the site). Out of scope.
- No member-join trigger. Only direct bot interactions create rows.
- No redesign of `/admin/users/[userId]`. It already renders non-admin users.

## Architecture

Three slices:

1. **Backend** — one internal endpoint to ensure-create app_users, one admin
   endpoint to list non-admin members.
2. **Bot** — one helper hooked into the existing `interactionCreate` listener
   alongside `reportUserInstallSeen`.
3. **Frontend** — one new route + table component, one sidebar entry.

The list endpoint reuses the existing admin user list infrastructure
(`buildUsersQuery`, the `AdminUserItem` / `AdminUsersPage` types) so the
frontend can share types with `/admin/users`. Row click navigates to the
existing user detail page — no new detail route.

## Backend

### Endpoint 1 — `POST /api/internal/bot/setup/users/ensure`

Mounted in the existing `/api/internal/bot/setup` chi group
(`apps/backend/internal/http/router.go:131`), alongside `/user-guilds` and
`/bot-user-installs/seen`. Auth chain is unchanged from its neighbors:
`bridgeAPIKeyPerms` + `requireBotRouteScope` + `botJWTMiddleware`.

**Request body:**

```json
{
  "discordId":  "string (Discord snowflake, required)",
  "username":   "string",
  "globalName": "string | null",
  "avatarHash": "string | null"
}
```

**Validation:** `discordId` must pass the existing `validateSnowflakeValue`.
Other fields are tolerated as empty / null (Discord users without a global
name or avatar are legitimate).

**Behavior:** calls `appauth.Repo.FindOrCreateByDiscordID` with the supplied
profile (`apps/backend/internal/appauth/repo.go:225`). That function is
already atomic — `INSERT … ON CONFLICT (discord_id) DO UPDATE` — and already
refreshes `discord_username`, `discord_global_name`, and `discord_avatar` on a
hit. New rows are inserted with `roles=["user"]`, matching the OAuth path.

**Response:**

```json
{ "userId": "usr_<24 hex>", "created": true }
```

**Wiring:** `appauth.Repo` is not currently a dependency of the `httpapi`
package. The constructor `NewHandler` gains one new parameter
(`appauth *appauth.Repo`), threaded from the main composition root.

**Errors:**
- 400 — invalid snowflake / malformed body
- 500 — DB error (logged with `[backend] EnsureAppUser error`)

### Endpoint 2 — `GET /api/app/admin/members`

Mounted next to `r.Get("/users", handler.ListAdminUsers)`
(`router.go:309`). Gated by the same `users.read` permission via the existing
admin-auth middleware chain.

**Query params:** identical to `/admin/users` (`q`, `status`, `sort`, `page`,
`pageSize`, `discord`, `steam`). The `role` param is **ignored** on this
endpoint — members have no admin roles by definition, so a role filter is
meaningless. Document this explicitly in a code comment on the handler.

**Implementation:** reuses `buildUsersQuery` (`admin_users_query.go:17`) for
the common WHERE/ORDER/args. Then appends one additional `NOT EXISTS` clause
that excludes anyone holding any role with at least one permission grant:

```sql
AND NOT EXISTS (
  SELECT 1
  FROM admin_user_roles aur
  JOIN admin_role_permissions arp ON arp.role_id = aur.role_id
  WHERE aur.user_id = u.id
)
```

Permissions live in the `admin_role_permissions` join table
(`apps/backend/internal/store/postgres.go:652`), not as a JSONB column on
`admin_roles`. The presence of any row in that join table for a role is what
makes the role "admin"; the system `user` role has zero rows and therefore
does not exclude its holders from the members list. This matches the
frontend convention where `r.permissions.length > 0` distinguishes admin
roles from the baseline `user` role (`apps/frontend/app/admin/users/page.tsx:56`).

**Response shape:** identical to `AdminUsersPage` — the same `AdminUserItem`
records, just filtered to the inverse population. This lets the frontend
reuse all existing TypeScript types in `lib/rustlens-api.ts`.

**Errors:** same as `ListAdminUsers` — 400 on bad sort/status/discord/steam,
500 on DB error.

### Tests

- `appauth/repo_discord_test.go` already exercises `FindOrCreateByDiscordID`
  thoroughly. The new ensure endpoint only needs one handler test confirming
  it round-trips a fresh profile, returns the right shape, and is idempotent
  on a second call with the same `discordId`.
- One handler test for `ListAdminMembers` modeled on
  `admin_read_handlers_test.go:TestListAdminUsers_FiltersAndPagination`:
  fixture-load one admin user (has an admin role grant) and two non-admin
  users; assert the endpoint returns exactly the two non-admins; assert
  `q=`, pagination, and `sort=displayName` still work.

## Bot

### New file — `apps/bot/src/ensure-app-user.ts`

Modeled on the existing `reportUserInstallSeen` helper in
`shard-worker.ts:294`. In-memory dedupe cache, fire-and-forget POST, no
blocking on the interaction path.

```ts
import type { Interaction } from "discord.js";
import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";

const shardID = process.env.SHARD_ID ?? "0";
let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(
      config.backendUrl,
      config.jwtSecret,
      config.jwtIssuer,
      config.jwtAudience,
    );
    _workerID = `${config.workerId}-ensure-app-user-${shardID}`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

const ensureAppUserCache = new Map<string, number>();
const ENSURE_APP_USER_TTL_MS = 5 * 60_000;

export function ensureAppUser(interaction: Interaction): void {
  const u = interaction.user;
  if (!u) return;

  const now = Date.now();
  const last = ensureAppUserCache.get(u.id);
  if (last !== undefined && now - last < ENSURE_APP_USER_TTL_MS) return;
  ensureAppUserCache.set(u.id, now);

  getBackend()
    .ensureAppUser(getWorkerID(), {
      discordId:  u.id,
      username:   u.username,
      globalName: u.globalName ?? null,
      avatarHash: u.avatar ?? null,
    })
    .catch((error) => {
      console.error(JSON.stringify({
        level: "warn",
        message: "[bot] ensureAppUser failed",
        userId: u.id,
        error: String(error),
      }));
    });
}
```

The lazy-initialized `getBackend` / `getWorkerID` pattern is borrowed
verbatim from `apps/bot/src/guild-settings.ts:19`, which is the established
shape for module-local backend clients in this codebase.

### Call site — `apps/bot/src/shard-worker.ts:333`

Single new line added at the top of the `interactionCreate` listener, right
next to the existing call:

```ts
client.on("interactionCreate", async (interaction) => {
  reportUserInstallSeen(interaction);
  ensureAppUser(interaction);
  // … existing dispatch unchanged
});
```

This catches every interaction kind (chat input, autocomplete, button,
modal, etc.) without needing to enumerate them. The 5-minute dedupe TTL
prevents autocomplete keystroke storms from spamming the backend.

### Backend client — `apps/bot/src/backend-client.ts`

Add one method following the shape of existing `touchBotUserInstallSeen` and
`createUserGuildLink`:

```ts
ensureAppUser(
  workerId: string,
  payload: {
    discordId:  string;
    username:   string;
    globalName: string | null;
    avatarHash: string | null;
  },
): Promise<{ userId: string; created: boolean }>;
```

Calls `POST /api/internal/bot/setup/users/ensure` with the standard worker
auth headers used by the other `setup/*` calls in this client.

### Tests

- `ensure-app-user.test.ts`: assert second call within TTL is a no-op (no
  backend POST). Use a stub backend client like the existing tests in
  `backend-client.test.ts`.
- Extend `backend-client.test.ts` with one test that the new
  `ensureAppUser` method serializes the right body and URL.

## Frontend

### New route — `apps/frontend/app/admin/members/page.tsx`

Server component. Mirrors `app/admin/user-installs/page.tsx` structurally:
`requirePermission("users.read")`, parses `q` / `page` / `pageSize` from
`searchParams`, fetches CSRF + current user + the members page in parallel,
renders the standard `SidebarProvider` / `AppSidebar` / `SidebarInset` /
`SiteHeader` chrome.

Page heading: **"Members"** with subtitle **"Discord users who have used
RustLens."**

```tsx
const session = await requirePermission("users.read");
const sp = await searchParams;

const q = pickStr(sp.q) ?? "";
const page = parsePositiveInt(pickStr(sp.page), 1);
const pageSize = clampPageSize(parsePositiveInt(pickStr(sp.pageSize), DEFAULT_PAGE_SIZE));

const [csrfToken, currentUser, members] = await Promise.all([
  getCsrfToken(),
  getCurrentUserSafe(),
  getAdminMembers({ q: q || undefined, page, pageSize }),
]);
```

`DEFAULT_PAGE_SIZE = 20`, `PAGE_SIZE_OPTIONS = [10, 20, 30, 40, 50]` —
matches `/admin/user-installs`.

### New API client — `lib/rustlens-api.ts`

Add `getAdminMembers(query: AdminUsersQuery)`. Reuses the existing
`AdminUsersQuery` and `AdminUsersPage` types verbatim — the response shape is
identical. Calls `GET /api/app/admin/members` via `backendFetch<AdminUsersPage>`.

### New client component — `apps/frontend/app/admin/members/_components/members-table.tsx`

Direct sibling of `user-installs-table.tsx`. Same debounced search input,
same URL-synced page/pageSize, same pagination footer. Per the approved
"minimal" column set:

| Avatar | Name + @username | Discord ID (click-to-copy) | Joined |

- **Avatar**: `userAvatarUrl(item.discordId, item.discordAvatar, 64)`
  (existing helper at `lib/discord-cdn.ts`).
- **Name + @username**: stacked, matches the user-installs cell layout.
- **Discord ID**: click-to-copy button, same pattern as user-installs
  (`user-installs-table.tsx:168`).
- **Joined**: `formatRelative(item.user.createdAt)` — copy the existing
  `formatRelative` helper from `user-installs-table.tsx:47`. (Future
  refactor: extract to a shared module; out of scope for this spec.)

Each row is wrapped in `<Link href={`/admin/users/${item.user.id}`}>` so
click-through hits the existing user detail page. That page already handles
non-admin users — no changes needed there.

Empty state: single full-width row reading "No members yet."

### Sidebar — `apps/frontend/components/dashboard-app-sidebar.tsx`

Add one item to the Discord `NavGroup` immediately after `User Apps`
(`dashboard-app-sidebar.tsx:175-196`):

```tsx
...(permissions.includes("users.read")
  ? [{
      title: "Members",
      path: "/admin/members",
      icon: UserIcon, // lucide-react UserIcon — visually distinct from neighbors
      isActive:
        pathname === "/admin/members" ||
        pathname.startsWith("/admin/members/"),
    }]
  : []),
```

`UserIcon` (single-figure) reads distinctly from `UsersRoundIcon` (Guilds)
and `UsersIcon` (User Apps). Import added to the existing lucide block.

Gated on `users.read` to match the corresponding endpoint and the existing
admin Users entry.

### Tests

One Vitest component test for `MembersTable` (`_components/__tests__/members-table.test.tsx`):
- Renders rows from a fixture page with the expected cells.
- Renders the empty-state row when `items.length === 0`.
- Search input writes the `q` param to the URL after debounce.

Follows the patterns in `apps/frontend/app/admin/users/_components/__tests__/`.

## Open questions

None. Design is complete for the read-only iteration. Mutations and
additional column sets are deferred until concrete usage feedback surfaces.

## Rollout

No migration required — `app_users` and `admin_user_roles` already exist with
the right shape. The first time a Discord user invokes the bot after this
ships, they appear in `/admin/members`. The first time an admin loads the
page before any bot interaction has happened, they see users created via the
existing OAuth flow only.
