# Admin users: user-app installed flag and filter

## Goal

On `/admin/users`, surface whether each user has the RustLens user app installed and let admins filter the list by that state. Mirrors the existing per-user flag-icon pattern (`Suspended`, `Deleted`, `Admin`) rendered next to the user's name in `members-table.tsx`.

## Definition of "user app installed"

A user has the user app installed when:

```sql
EXISTS (
    SELECT 1 FROM bot_user_installs bui
    WHERE bui.discord_id = u.discord_id
      AND bui.is_installed = TRUE
)
```

This matches the live-install set surfaced by `/admin/user-installs` and excludes users who later removed the app (`is_installed = FALSE` after a deauthorize webhook).

## Backend

### `internal/http/admin_users_query.go`

`buildUsersQuery` accepts a new query parameter:

| Param | Values | Default | Effect |
|---|---|---|---|
| `userApp` | `any`, `installed`, `not_installed` | `any` | Adds `EXISTS` / `NOT EXISTS` predicate against `bot_user_installs` keyed on `discord_id` and filtered to `is_installed = TRUE`. |

Unknown values return HTTP 400 (`invalid userApp`), mirroring the existing `discord` / `steam` parameter validation.

### `internal/http/admin_read_handlers.go`

Both `ListAdministrators` and `ListAdminUsers` add a new column to their `SELECT`:

```sql
EXISTS (
    SELECT 1 FROM bot_user_installs bui
    WHERE bui.discord_id = u.discord_id
      AND bui.is_installed = TRUE
) AS has_user_app_installed
```

The scanned `bool` is stored on a new `HasUserAppInstalled bool` field of `adminUserItem` and serialized as `hasUserAppInstalled` in JSON. Returning the field on both endpoints keeps the response shape uniform between the members list and the administrators list — the administrators table can choose to render it later without a second backend change.

### `internal/http/openapi.go`

- Add the `userApp` query parameter (enum: `any`, `installed`, `not_installed`) to the documented `GET /api/app/admin/users` and `GET /api/app/admin/administrators` endpoints.
- Add `hasUserAppInstalled: boolean` to the `AdminUserItem` schema.

### Indexes

None required. `bot_user_installs.discord_id` is the primary key, so the correlated `EXISTS` subquery is a single index lookup per row.

### Tests

- Extend `admin_users_list_test.go` to cover the three `userApp` filter values and the new response field.
- Add a bad-value case asserting HTTP 400 for `userApp=garbage`.

## Frontend

### `lib/rustlens-api.ts`

- `AdminUserItem` gains `hasUserAppInstalled: boolean`.
- `AdminUsersQuery` gains `userApp?: "any" | "installed" | "not_installed"`.
- `getAdminUsers` serializes `userApp` into the request URL only when set to `installed` or `not_installed`; `any` and `undefined` are omitted to keep URLs clean.

### `app/admin/users/page.tsx`

- Parse `userApp` from `searchParams` against the allowed set (`installed`, `not_installed`). Anything else (including missing) collapses to `undefined`.
- Forward the parsed value to `getAdminUsers` and pass `initialUserApp` into `MembersTable`.

### `app/admin/users/_components/members-table.tsx`

**Flag icon.** Extend `buildFlags(item)`: when `item.hasUserAppInstalled`, append:

```ts
{
  key: "user-app",
  Icon: PackageCheckIcon,
  label: "User app installed",
  className: "text-emerald-600",
}
```

The flag renders through the existing `MemberFlags` component using the same tooltip-wrapped `size-3.5` icon as the suspended/deleted/admin flags. Order matters only for visual stability — append after `admin` so existing flag positions are unchanged.

**Filter UI.** Add a new "User app" section to the Filters popover below the existing "Status" section:

```
User app
[ Any ] [ Installed ] [ Not installed ]
```

Implemented as three buttons matching the Status section's pattern (`variant={current === s ? "default" : "outline"}`, `size="sm"`). Selecting a value calls `pushParams({ userApp: value === "any" ? null : value, page: null })`.

**Active filter accounting.**

- `activeFilterCount` increments by 1 when `initialUserApp` is `installed` or `not_installed`.
- The "Clear filters" `X` button resets `userApp: null` alongside `status`, `joinedSince`, and `joinedBefore`.

**Props.** `MembersTable` gains `initialUserApp?: "installed" | "not_installed" | null` to carry the URL value into the component.

## Out of scope

- No UI changes to `/admin/administrators` (the admins list keeps using its own table). The backend field is added there for response symmetry only.
- No bulk action on user-app install state.
- No new sort column for user-app status.
- No backfill / migration — `bot_user_installs` is already populated by the existing webhook and interaction handlers.

## Risks

- **Discord ID mismatch.** Users without a `discord_id` on `app_users` (legacy rows) will never match. This matches the existing behavior of every other Discord-derived field in this table.
- **Stale `is_installed` flag.** If a deauthorize webhook is missed, a user can show as "installed" when they no longer are. The interaction-driven `TouchBotUserInstallSeen` heartbeat plus the explicit `MarkBotUserInstallRemoved` path is the existing source of truth — this design does not change it.
