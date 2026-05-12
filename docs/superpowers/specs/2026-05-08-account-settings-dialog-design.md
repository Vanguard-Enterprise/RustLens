# Account Settings Dialog — Design

**Status:** Approved 2026-05-08
**Owner:** Frank Dobrovolny

## Summary

The bottom-left **Settings** entry in the dashboard sidebar currently links to `#` and does nothing. This work makes that entry open a modal dialog where the signed-in user manages their account preferences. The dialog is structured to grow over time; the only section shipped now is **Notifications**, which exposes two toggles backed by a new `user_notification_preferences` table and two new authenticated endpoints.

The two flags are storage-only at this point — no delivery system reads them yet — but storing them now lets future in-app and Discord-DM delivery code honour user choice from day one.

## Goals

1. The sidebar Settings item opens a modal instead of navigating.
2. The dialog has a labelled **Notifications** section with two switches:
   - **In-app notifications** — alerts inside the dashboard about guild activity.
   - **Discord product updates** — bot DMs about RustLens product/announcement content (guild-event DMs are unaffected).
3. Preferences are persisted per `app_users.id` and survive logout / device change.
4. The dialog is structured so adding future sections (Profile, Security) is a copy of the same pattern, not a refactor.

## Non-Goals

- Actually delivering in-app notifications. There is no in-app notification system today; this design only stores the flag for future delivery code.
- Actually sending marketing/announcement DMs through the bot. Same reasoning — flag now, delivery later.
- Email preferences, security alert toggles, or guild-scoped notification routing.
- Tabs in the dialog. Only one section today; tabs would be YAGNI.
- Migrating any existing data. There are no preferences in the database to migrate from.

## Database

A new table is created via `store.EnsureSchema()` (matches existing convention — the project has no migration tool and uses idempotent `CREATE TABLE IF NOT EXISTS`).

```sql
CREATE TABLE IF NOT EXISTS user_notification_preferences (
    user_id                       TEXT PRIMARY KEY REFERENCES app_users(id) ON DELETE CASCADE,
    notify_in_app                 BOOLEAN     NOT NULL DEFAULT TRUE,
    notify_discord_announcements  BOOLEAN     NOT NULL DEFAULT TRUE,
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**Defaults:** both `TRUE` (opt-out model). New users are opted in by default.

**Cascade:** deleting an `app_users` row removes the preferences row automatically.

**Lazy creation:** the `GET` handler upserts a default row on first read, so the rest of the system never has to handle a "missing preferences" branch. The `PATCH` handler upserts atomically as well.

A short comment in the schema-creation site notes that neither flag is wired to a delivery path yet, so future readers don't assume there's a bug.

## Backend API

Two new routes added under the existing `RequireSession`-gated `/api/app/me` group. Implementation lives in `apps/backend/internal/appauth/preferences.go` (handlers + repo methods stay in the same package as `Me`/`LoadCurrentUser` since this is part of the same authenticated-user domain).

### `GET /api/app/me/preferences`

Returns the caller's current preferences. Auto-creates the default row if none exists.

**Response 200:**
```json
{
  "notifyInApp": true,
  "notifyDiscordAnnouncements": true
}
```

**Response 401:** `{ "error": "session_required" }` — handled by middleware.

### `PATCH /api/app/me/preferences`

Updates a subset of fields and returns the row.

**Request body** (both fields optional; absent fields are unchanged):
```json
{
  "notifyInApp": false,
  "notifyDiscordAnnouncements": true
}
```

**Validation:**
- Strict JSON decode (`DisallowUnknownFields`) — unknown keys → 400 `bad_request`.
- Each present field must be a JSON boolean — anything else → 400 `bad_request`.
- Empty body (no fields supplied) is accepted as a no-op and returns the current state.

**Response 200:** same shape as `GET`.

**Response 401:** `{ "error": "session_required" }`.

### OpenAPI

`apps/backend/internal/http/openapi.go` is updated with both path entries, request/response schemas, and parameter descriptions, per the project convention that the OpenAPI document is the source of truth for REST shapes.

## Frontend

### Sidebar wiring

The Settings entry currently sits in `NAV_SECONDARY` inside `apps/frontend/components/dashboard-app-sidebar.tsx` (lines 94–98) and is rendered by `apps/frontend/components/nav-secondary.tsx`, which only knows how to render anchor tags.

`NavSecondary`'s item type is widened to accept an optional `onSelect` callback. When `onSelect` is present, the rendered element is a `<button type="button">` instead of an `<a>`, with `url` becoming optional. Existing items (Get Help, Search) keep working unchanged because they only specify `url`.

The Settings item in `DashboardAppSidebar` becomes:
```ts
{ title: "Settings", icon: <Settings2Icon />, onSelect: () => setSettingsOpen(true) }
```

Open state (`settingsOpen`) lives in `DashboardAppSidebar`. The `<AccountSettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} />` is rendered as a sibling of `<Sidebar>` so the dialog overlay isn't constrained by sidebar layout.

This widening keeps the layout identical (Settings still sits with Get Help / Search at the bottom of the dashboard sidebar) and avoids creating a parallel rendering path. Admin sidebars don't render `NavSecondary`, so admin behaviour is unchanged.

### `account-settings-dialog.tsx`

New file: `apps/frontend/components/account-settings-dialog.tsx`. Client component.

**Structure** (shadcn `Dialog`):
- `DialogContent` titled **Account settings**.
- A single labelled `<section>` titled **Notifications** with two `Switch` rows:
  - **In-app notifications** — *Show alerts in the dashboard about activity in your guilds.*
  - **Discord product updates** — *Allow the bot to DM you about RustLens product updates and announcements. Guild-event DMs are unaffected.*
- Footer: **Cancel** (closes without saving) and **Save changes** (disabled when no field differs from the loaded state, and during the in-flight `PATCH`).

**Data flow:**
- On `open` becoming `true`: `fetch("/api/app/me/preferences", { credentials: "include" })` and seed local form state. While loading, switches render in a disabled skeleton state. Errors render an inline alert with a retry button.
- On Save: `fetch("/api/app/me/preferences", { method: "PATCH", credentials: "include", headers: { "Content-Type": "application/json" }, body: JSON.stringify(diff) })` sending only fields whose value changed. On success, close the dialog and toast "Preferences saved." On failure, show an inline error and leave the dialog open.
- Cancel discards the local form state.

**Why `fetch` and not SWR/React-Query:** the rest of the dashboard's small forms use plain `fetch` with `credentials: "include"`. Adding a query library for one popup is unjustified.

**Section structure justification:** wrapping the lone Notifications section in a labelled `<section>` (with its own heading and description) means adding "Profile" or "Security" later is a copy-and-edit of the same pattern, not a structural refactor of the dialog.

## Error Handling

| Layer | Failure | Behaviour |
|---|---|---|
| Backend | DB unreachable on `GET` | 500, generic error code; handler logs context |
| Backend | Invalid JSON / unknown field on `PATCH` | 400 `bad_request` |
| Backend | Non-boolean field value on `PATCH` | 400 `bad_request` |
| Backend | Missing session | 401 `session_required` (middleware) |
| Frontend | `GET` fails | Inline alert with **Retry** button inside the Notifications section; switches stay disabled |
| Frontend | `PATCH` fails | Inline alert under the footer; dialog stays open; Save re-enabled |
| Frontend | Both flags unchanged | Save button disabled — no request issued |

## Testing

**Backend (`apps/backend/internal/appauth/preferences_test.go`):**
- `GET` for a user with no row creates defaults and returns `{true, true}`.
- `GET` for a user with an existing row returns its values.
- `PATCH` with `{notifyInApp: false}` flips only that flag and leaves the other unchanged.
- `PATCH` with both flags applies both and bumps `updated_at`.
- `PATCH` with empty body returns current state unchanged.
- `PATCH` with unknown fields → 400.
- `PATCH` with non-boolean value → 400.
- Unauthenticated request → 401 (handled by middleware test pattern used elsewhere in the package).

**Frontend:** behaviour-level test for the dialog if a colocated test pattern already exists in `apps/frontend/components/`. If not, manual verification of: open from sidebar, load shows current state, toggling and saving persists across reload, Cancel discards changes, network failure surfaces inline error.

## Out-of-Scope Follow-ups (Future Work)

- Build an in-app notification centre that reads `notify_in_app` before showing alerts.
- Add a bot job that DMs opted-in users about RustLens announcements, gated on `notify_discord_announcements`.
- Add a Profile section (display name, avatar override) to the same dialog.
- Add email-preference toggles once an email pipeline exists.
