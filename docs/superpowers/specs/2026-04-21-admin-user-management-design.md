# Admin User Management — Design Spec

**Date:** 2026-04-21
**Scope:** `/admin/users` and `/admin/users/[userId]` — full user management on the web admin.
**Status:** Design approved; ready for implementation plan.

## 1. Goal

Provide operators with a single place to view, search, filter, edit, secure, invite, connect, and delete users of the RustLens platform. All user-lifecycle and security-sensitive actions flow through the existing Go admin action dispatcher so that permission checks, audit logging, and redaction are uniform across every write.

## 2. Architecture

```
Browser → Next.js RSC (/admin/users, /admin/users/[id]) → server actions
       → Go /api/app/admin/actions/dispatch → dispatcher
       → internal/users/* → Postgres + audit_events
Reads : Browser → Next.js RSC → Go /api/app/admin/users[/...] → Postgres
```

**Invariants**

- All writes go through the Go action dispatcher. No Next.js code writes to Postgres directly.
- Every write produces exactly one `audit_events` row with `target_user_id = app_users.id`.
- The per-user audit timeline reads `audit_events ∪ app_audit_events` merged by timestamp.
- Permission checks live in the dispatcher. Server actions only enforce "is this an authenticated admin session at all."
- One-time tokens (invite, force-reset, resend-verify) are generated server-side and returned **once** in the dispatch response for the UI to display. They are never re-retrievable.

## 3. Schema changes

All additive on `app_users`; the `admin_user_roles` rekey is destructive but safe because the table lives on an unmerged feature branch with no production data.

```sql
ALTER TABLE app_users
  ADD COLUMN suspended_at   TIMESTAMPTZ NULL,
  ADD COLUMN suspended_by   TEXT        NULL REFERENCES app_users(id),
  ADD COLUMN suspend_reason TEXT        NULL,
  ADD COLUMN deleted_at     TIMESTAMPTZ NULL,
  ADD COLUMN deleted_by     TEXT        NULL REFERENCES app_users(id);

CREATE INDEX app_users_active_idx
  ON app_users (created_at DESC)
  WHERE deleted_at IS NULL;

DROP TABLE IF EXISTS admin_user_roles;
CREATE TABLE admin_user_roles (
  user_id    TEXT        NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
  role_id    BIGINT      NOT NULL REFERENCES admin_roles(id) ON DELETE CASCADE,
  granted_by TEXT        NULL     REFERENCES app_users(id),
  granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, role_id)
);
```

Drop the orphaned `users(UUID)` table during implementation after grepping the repo and confirming no other references.

**Session revocation for suspend:** the dispatcher uses a session-revocation adapter that deletes all sessions belonging to the target `app_user_id`. The plan phase must inspect the current session store (Redis or Postgres) and implement the adapter accordingly. If revocation is infeasible without additional work, suspend degrades to "block new logins; existing sessions expire naturally" and that degradation must be explicitly called out in the plan.

**Reused unchanged:** `email_sign_in_tokens` (invites), `password_reset_tokens` (admin-forced resets), `email_verification_tokens` (resend-verify), `audit_events`, `app_audit_events`.

## 4. Backend actions

All actions are registered in `apps/backend/internal/admin/actions/registry`. The dispatcher performs permission check → JSON-schema validation → transaction → audit write → redaction.

### Identity and roles
- `users.edit` · perm `users.edit` · input `{userId, displayName?, avatarUrl?}` · audit `users.edited`.
- `users.roles.grant` · perm `users.roles.assign` (plus `admins.grant` if `roleKey == "superadmin"`) · input `{userId, roleKey}` · audit `users.role_granted`.
- `users.roles.revoke` · perm same as grant · input `{userId, roleKey}` · audit `users.role_revoked`.

### Account lifecycle
- `users.suspend` · perm `users.suspend` · input `{userId, reason?}` · sets suspend columns + revokes sessions · audit `users.suspended`.
- `users.unsuspend` · perm `users.suspend` · input `{userId}` · audit `users.unsuspended`.
- `users.delete` · perm `users.delete` · input `{userId}` · soft delete; revokes sessions · audit `users.deleted`.
- `users.restore` · perm `users.delete` · input `{userId}` · audit `users.restored`.
- `users.purge` · **superadmin only, permission key bypassed** · input `{userId, confirmId}` where `confirmId` must equal `userId` · hard delete via FK cascades · audit `users.purged` written *before* the delete.

### Security / password
- `users.password.forceReset` · perm `users.security.reset` · input `{userId}` · output `{tokenUrl}` · generates a `password_reset_tokens` row, invalidates prior outstanding tokens of the same kind · audit `users.password_reset_forced`.

### Email
- `users.email.resendVerification` · perm `users.email.manage` · input `{userId, email}` · output `{tokenUrl}` · audit `users.email_verification_sent`.
- `users.email.setPrimary` · perm `users.email.manage` · input `{userId, email}` (must already exist and be verified) · audit `users.email_primary_set`.
- `users.email.add` · perm `users.email.manage` · input `{userId, email}` · inserts `user_emails` (unverified, non-primary) · audit `users.email_added`.
- `users.email.remove` · perm `users.email.manage` · input `{userId, email}` · rejects if email is primary · audit `users.email_removed`.

### Invites
- `users.invite` · perm `users.create` · input `{email, displayName?}` · output `{userId, tokenUrl}` · creates `app_users` with no credential, inserts `user_emails` (unverified, primary), generates `email_sign_in_tokens` · audit `users.invited`.

### Connections
- `users.connections.unlink` · perm `users.connections.manage` · input `{userId, provider: "discord"|"steam", providerAccountId}` · audit `users.connection_unlinked`.

### Bulk wrapper
- `users.bulk` · perm equals the underlying action's perm, checked per-row · input `{action: "suspend"|"unsuspend"|"roles.grant"|"roles.revoke", userIds[], params}` · output `{results: [{userId, ok, error?}]}` · partial failures do not roll back siblings · sub-audit rows per row; top-level `users.bulk_<action>` summary row.

### Reads (HTTP handlers in `apps/backend/internal/http/admin_read_handlers.go`)
- `GET /api/app/admin/users` — query `?q=&role=&status=&discord=&steam=&emailVerified=&sort=&page=&pageSize=` → `{items, total, page, pageSize}`.
- `GET /api/app/admin/users/{id}` — identity, all emails, all connections, all roles, all guilds, active session metadata (no tokens), last login.
- `GET /api/app/admin/users/{id}/audit?limit=&before=` — merged timeline.
- `GET /api/app/admin/users/export.csv?<same filters>` — streaming CSV; perm `users.export`; hard cap 100k rows (returns `413` if filters would exceed).

## 5. Dispatch protocol

Single Go endpoint: `POST /api/app/admin/actions/dispatch`.

Request:
```json
{ "action": "users.suspend",
  "input":  { "userId": "usr_...", "reason": "compromised" },
  "idempotencyKey": "ui-<uuid>" }
```

Response envelope (success):
```json
{ "ok": true, "action": "users.suspend", "output": { ... } }
```

Response envelope (failure):
```json
{ "ok": false, "error": { "code":    "permission_denied" | "validation" | "not_found"
                                    | "conflict" | "rate_limited" | "internal",
                          "message": "human-readable",
                          "fields":  { "userId": "must start with usr_" } } }
```

Status codes: `200` ok, `400` validation, `401` no session, `403` permission denied, `404` target not found, `409` conflict, `429` rate limit, `500` internal.

**Idempotency:** non-empty `idempotencyKey` → dispatcher caches the response for 24 h and returns the cached response on replay.

**Rate limits:** per-admin, per-action token bucket. Mutating actions default 30/min; token-generating actions (`users.invite`, `users.password.forceReset`, `users.email.resendVerification`) default 10/min.

**CSRF + session:** every dispatch requires the existing `apps/frontend/lib/csrf.ts` token and a valid admin session cookie forwarded by the server action.

## 6. Frontend routes and components

```
apps/frontend/app/admin/users/
├── page.tsx                       RSC: reads filters from searchParams, fetches list
├── loading.tsx
├── actions.ts                     all server actions (one per Go action)
├── _components/
│   ├── users-table.tsx            client: multi-select, sort, row click → detail
│   ├── users-filter-bar.tsx       client: search + filter chips, URL sync
│   ├── users-bulk-toolbar.tsx     client: shows when selection > 0
│   ├── pagination.tsx             client: page/pageSize, URL sync
│   ├── invite-user-dialog.tsx     client: form; on-success shows token-url modal
│   └── one-time-token-modal.tsx   client: copyable link, blocks Esc + outside-click
│
└── [userId]/
    ├── page.tsx                   RSC: detail + Overview tab
    ├── layout.tsx                 RSC: tab nav (Overview/Roles/Security/Connections/Emails/Audit)
    ├── loading.tsx
    ├── roles/page.tsx
    ├── security/page.tsx
    ├── connections/page.tsx
    ├── emails/page.tsx
    └── audit/page.tsx
```

**Data flow rule:** RSC pages do all reads via `lib/rustlens-api.ts`. Mutations are always `<form action={serverAction}>` or event handlers that call server actions — never direct `fetch` from the client. After a mutation the server action calls `revalidatePath` and the RSC re-renders. No client cache, no SWR/React Query.

**URL as state:** the filter bar and pagination write to `searchParams` via `router.replace`; the RSC reads `searchParams` and passes them into the API call. Refresh, share-link, and back-button all work with no extra code.

**Permission-gated UI:** each action button calls `hasPermission("users.suspend")` from the session context at render time. Hidden buttons still don't bypass backend checks — the dispatcher is the authority; the UI just avoids showing useless controls.

## 7. UX

### List page `/admin/users`

Columns (left to right): checkbox · avatar+displayName · primary email (verified/unverified indicator) · roles (chips) · status badge (active/suspended/deleted) · Discord · Steam · last login · created.

Filter bar (URL-synced):
```
[🔍 search: name, email, id, discord]  [Role ▾] [Status: Active ▾]
[Discord: any ▾] [Steam: any ▾] [Email verified: any ▾]  [Clear]  [Invite user]
```
Default filter is `status=active`; suspended and deleted users are hidden until that chip is toggled.

Bulk toolbar (sticky above table when rows selected):
```
3 selected  [Grant role ▾] [Revoke role ▾] [Suspend] [Unsuspend] [Export CSV]  [Clear]
```

Sorting: header click toggles asc/desc, written to URL as `sort=-lastLogin`.

Empty state: empty card with active-filter summary + Clear link. Network error: inline retry banner; page does not blank.

### Detail page `/admin/users/[id]`

Sticky header: avatar, display name, `usr_...` id (copyable), badges (roles + status), quick-action buttons (Suspend/Unsuspend, Force password reset, Soft delete, Purge — each permission-gated).

Tabs:

- **Overview** — identity, created/updated/last-login, primary email, guild count, Steam count, Discord link, most recent 5 audit rows.
- **Roles** — current roles (with revoke ✕) / available roles (with grant +). Superadmin row is visually distinct and requires `admins.grant`.
- **Security** — suspend panel (with reason textarea), force-password-reset (shows one-time token modal on success), active sessions list (session id, created, ip, user-agent, revoke ✕), soft-delete / restore / purge.
- **Connections** — Discord card (username, id, unlink ✕), Steam card(s) with unlink, read-only guild membership list.
- **Emails** — table of `user_emails`: email / primary / verified / actions (set-primary, resend-verification → token modal, remove). "Add email" button.
- **Audit** — merged `audit_events ∪ app_audit_events`, paginated by `created_at`. Columns: time / actor / event / payload-preview (expandable row for full payload).

**Confirmation pattern:** destructive and security-sensitive actions use a confirmation dialog stating plain-English effects ("This will block login and sign out 2 active sessions"). Purge additionally requires typing the target user id; soft delete / suspend require only a confirmation click.

**One-time-token modal:** read-only URL input, large Copy button, warning "This link is shown **once**.", explicit "I've saved this link" button to close. Esc and outside-click are disabled to prevent accidental dismissal.

## 8. Permissions

| Action | Permission key |
|---|---|
| View list / detail | `users.read` |
| Edit display name / avatar | `users.edit` |
| Grant/revoke non-superadmin roles | `users.roles.assign` |
| Grant/revoke **superadmin** | `admins.grant` (existing) |
| Suspend / unsuspend | `users.suspend` |
| Force password reset | `users.security.reset` |
| Email ops (resend verify, change primary, add/remove) | `users.email.manage` |
| Invite / create user | `users.create` |
| Unlink Discord / Steam | `users.connections.manage` |
| Soft delete / restore | `users.delete` |
| Purge (hard delete) | superadmin only; permission bypass |
| Bulk ops | underlying action's permission per row |
| CSV export | `users.export` |
| Audit timeline | `users.audit.read` |

**Seed roles (suggested):**
- `user_support` → `users.read`, `users.audit.read`, `users.email.resendVerification`, `users.password.forceReset`, `users.edit`.
- `user_admin` → `user_support` ∪ `users.roles.assign`, `users.suspend`, `users.email.manage`, `users.connections.manage`, `users.create`, `users.delete`, `users.export`.
- `user_auditor` → `users.read`, `users.audit.read`, `users.export`.

`superadmin` retains its existing "implicit all" grant.

## 9. Errors and edge cases

| Code | When | UI treatment |
|---|---|---|
| `validation` | Bad input, known field errors | Inline field errors; dialog stays open |
| `permission_denied` | Session lacks required perm | Toast + log; button should have been hidden (bug signal) |
| `not_found` | Target gone | Toast + `revalidatePath` to refresh stale view |
| `conflict` | Illegal state transition | Inline dialog message; no state change |
| `rate_limited` | Bucket exceeded | Toast with retry-after |
| `internal` | Unhandled | Toast + correlation id |

**Pinned edge cases:**

- **Acting on self:** suspend-self, delete-self, purge-self, revoke-own-superadmin → `conflict`. UI disables these buttons when `target.id === session.userId`.
- **Last superadmin protection:** revoke/suspend/delete the last superadmin → `conflict`.
- **Bulk partial failure:** per-row results, summary toast "N succeeded, M failed" with expandable failure list; each sub-action has its own audit row plus a top-level summary row.
- **Race on soft-delete:** write paths include `deleted_at IS NULL` in their `WHERE`; return `not_found` if the row was deleted between render and action.
- **Email uniqueness:** add-email fails with `conflict` if the email exists on any user (including soft-deleted). Error message names the condition without leaking which account holds it.
- **Token rotation:** each force-reset / invite / resend-verify invalidates prior outstanding tokens of the same kind for that user.
- **CSV export cap:** 100k rows, request timeout 60s; exceeding rows → `413` with UI prompt to narrow filters.
- **Missing cookie in server action:** server actions require a valid admin session cookie; missing → `401` before any work.

## 10. Testing

**Go backend**

- Action unit tests (`internal/admin/actions/users_*_test.go`), one file per action, against a real Postgres. Assert: happy-path writes expected rows plus exactly one `audit_events` row with the expected `event_type` and `target_user_id`; permission denied returns 403 and writes no rows; validation returns 400; idempotency replay returns cached response.
- Guard tests: admin-acting-on-self, last-superadmin revoke, last-superadmin suspend/delete, email uniqueness across soft-deleted users, token rotation invalidates prior tokens.
- Bulk wrapper: mixed success/failure, per-row result shape, sub-audit rows, summary audit row.
- Read-handler tests: filter composition (search + role + status), pagination math, CSV streaming cutoff.
- E2E smoke extending `apps/backend/internal/admin/e2e_test.go`: login as admin → list users → invite → consume returned token URL → new user logs in.

**Frontend**

- Server action tests (Vitest): mock `dispatch`; assert each action calls `requireInternalAdminSession`, forwards the right payload, calls `revalidatePath` on success.
- Component tests (Vitest + Testing Library): users-filter-bar URL sync, users-bulk-toolbar visibility, one-time-token-modal blocks Esc + outside-click, purge confirmation requires typed user id.
- Type tests: `lib/rustlens-api.ts` DTOs round-trip sample fixtures captured from Go tests.

**Not tested in v1**

- Playwright / browser e2e for the admin UI.
- CSV export load testing (100k row cap is a pragmatic ceiling, not a measured SLO).
- Fuzz of dispatch input schemas (strict-struct validation on the Go side is sufficient).

**Discipline:** per the repo's TDD skill, each action lands as failing dispatcher test → action → failing server-action test → server action → component test → button. No action ships without its tests in the same commit.

## 11. Rollout

1. Schema migrations: additive `app_users` columns + `app_users_active_idx` + drop-and-recreate `admin_user_roles` keyed on `app_users(id) TEXT`. Drop the orphaned `users(UUID)` table after grepping for references.
2. Seed permissions and suggested roles in `admin/rbac/store`.
3. Land Go actions + read endpoints + tests. Backend is fully usable via `curl` at this point.
4. Replace the existing read-only `/admin/users/page.tsx` with the new list page; ship the detail routes and wire server actions. No feature flag — the old page is on an unreleased branch.
5. Post-merge: run the e2e smoke against staging, spot-check each seed role in the UI, confirm `audit_events` rows land.

**Backfill:** none. New columns are NULL-able; existing users are implicitly "active".

**Rollback:** schema migrations are reversible before any grants exist in the new-shape `admin_user_roles`. Once grants exist, rolling back that rekey requires export+import; treat it as one-way after the first grant.

**Email dependency:** none. One-time URLs are returned in responses and displayed in the token modal. When SMTP lands later, the Go actions additionally send the email; the response shape does not change.

## 12. Out of scope (explicit)

- Email sending / SMTP.
- 2FA / MFA management.
- API keys / personal access tokens.
- OAuth app management.
- Role editor (creating custom roles, editing which permissions a role contains).
- Permission matrix editor.
- Saved filter presets, date-range filters, cursor pagination.
- Bulk delete, bulk force-password-reset, bulk email ops.
- User impersonation ("log in as").
- Discord / Steam account linking from admin UI (unlink only).
- Admin pages for guilds/servers/audit/roles (this spec is only `/admin/users`).
- Browser-level e2e.
- Per-action quotas beyond simple rate-limit buckets.

## 13. Assumptions

- The session store exposes — or can cheaply be made to expose — a way to list and delete all sessions for a given `app_user_id`. If not, suspend degrades from "block login + revoke sessions" to "block login only" and that must be flagged in the plan.
- The RBAC foundation branch carrying `admin_user_roles(UUID)` has not merged to `main` and carries no production data, making the `DROP TABLE` + rekey safe.
