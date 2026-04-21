# Admin Foundation Design

**Date:** 2026-04-21
**Status:** Draft — pending implementation plan
**Scope:** Shared foundation for the RustLens administrative dashboard

## Motivation

The existing `/admin` area (users, guilds, servers, audit, settings) is a read-only v1 scaffold gated by a single `internal_admin` boolean. The next phase requires real operator and moderation capability across four areas:

- **C — Outbound proxy management**
- **D — User moderation**
- **E — Suspicious-link review**
- **F — Guild oversight**

Each of those areas will get its own spec. Before any of them can be built cleanly, the admin surface needs shared plumbing: a real permission model, a safe way to perform mutations, a richer audit trail, and consistent UX patterns. This document designs that foundation.

Non-goals of this spec: the feature-level designs for C/D/E/F, bot-fleet health (A), and BattleMetrics polling oversight (B). Those are downstream.

## Architecture Overview

Four cross-cutting components:

1. **RBAC model** — roles bundling permissions, assigned to users; replaces the `internal_admin` flag.
2. **Action framework** — every admin mutation is a typed, registered action; a single dispatcher enforces permission, reason capture, dangerous-confirmation, and audit writes.
3. **Audit log** — extended schema capturing actor, resource, params, reason, result, and request metadata; feeds the existing `/admin/audit` page with filters.
4. **Admin shell & UX primitives** — permission-filtered nav, shared data-table and detail-layout components, shared confirmation dialogs.

Feature specs (C/D/E/F) consume these primitives and add per-resource logic, list/detail pages, and new action handlers.

## RBAC Model

### Tables (new)

- **`admin_permissions`** — atomic capabilities. Seeded from code; mirrored in DB for FK integrity. Namespaced strings, e.g. `users.suspend`, `users.force_unlink_steam`, `proxies.edit`, `proxies.rotate_credentials`, `links.review`, `links.approve`, `guilds.untrack_server`, `admins.grant`, `audit.read`.
  - Columns: `key TEXT PRIMARY KEY`, `description TEXT NOT NULL`, `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`.
- **`admin_roles`** — named bundles. Editable in the admin UI except for the hardcoded `superadmin` role.
  - Columns: `id UUID PRIMARY KEY`, `key TEXT UNIQUE NOT NULL`, `name TEXT NOT NULL`, `description TEXT`, `is_system BOOLEAN NOT NULL DEFAULT FALSE`, `created_at`, `updated_at`.
  - Seeded: `superadmin` (is_system, ungrantable by runtime edits), `moderator`, `infra_admin`, `guild_ops`.
- **`admin_role_permissions`** — M:N role→permission.
  - Columns: `role_id UUID REFERENCES admin_roles(id) ON DELETE CASCADE`, `permission_key TEXT REFERENCES admin_permissions(key) ON DELETE CASCADE`, `PRIMARY KEY (role_id, permission_key)`.
- **`admin_user_roles`** — M:N user→role.
  - Columns: `user_id UUID REFERENCES users(id) ON DELETE CASCADE`, `role_id UUID REFERENCES admin_roles(id) ON DELETE CASCADE`, `granted_by_user_id UUID`, `granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `PRIMARY KEY (user_id, role_id)`.

### `superadmin` role

`superadmin` is hardcoded as the escape hatch. Its permission set at runtime is "all registered permissions" — not stored in `admin_role_permissions`. Only an existing `superadmin` can grant `superadmin` to another user. This prevents `admins.grant` from being used to self-elevate through a merely-configured role.

### Permission check

Session middleware resolves a user's roles → permission set once per request and caches it on the request context. Call sites use:

```go
if !session.HasPermission(ctx, "users.suspend") {
    return httpx.Forbidden(w)
}
```

### Legacy bridge

On deploy, a migration creates the `superadmin` role and assigns it to every user with `internal_admin = true`. The `internal_admin` column stays readable for one release to keep old reads working, then is dropped in a follow-up migration. `requireInternalAdminSession` in the frontend is replaced with `requireAdminSession` (any role) plus route-level permission checks.

## Action Framework

### Interface

```go
type AdminAction interface {
    Type() string                        // e.g. "users.suspend"
    Permission() string                  // required permission key
    RequiresReason() bool                // default true for mutations
    Dangerous() bool                     // default false; triggers typed confirmation
    SensitiveParamKeys() []string        // redacted before audit write
    ResourceType() string                // "user", "proxy", "guild", "link", ...
    Validate(params json.RawMessage) error
    Execute(ctx context.Context, in ActionInput) (ActionResult, error)
}

type ActionInput struct {
    Actor             Admin
    Params            json.RawMessage
    Reason            string
    TypedConfirmation string
    ResourceID        string  // target id, pulled from params by Validate
}

type ActionResult struct {
    ResourceID string
    TargetUserID *uuid.UUID // optional, for convenience querying
    Payload    any          // returned to the client
}
```

### Registry

`apps/backend/internal/admin/actions/registry.go` holds a `map[string]AdminAction`. Actions self-register in `init()`. Boot fails if two actions share a `Type()` or if any registered `Permission()` is not in the permission set.

### Dispatcher

Single route: `POST /api/admin/actions/:type`.

Flow:

1. Look up action by `:type` → 404 if missing.
2. `session.HasPermission(actor, action.Permission())` → 403 if denied.
3. If `action.RequiresReason()` and reason missing/empty → 400.
4. If `action.Dangerous()` and `TypedConfirmation` does not match the canonical target string (defined by the action's `Validate`) → 400.
5. `action.Validate(params)` → 400 on validation error.
6. Begin transaction. `action.Execute(ctx, in)` → either returns `Result` or an `error`.
7. Regardless of success or error, write an audit row (see next section). Audit write happens *inside* the same transaction on success, and in a separate transaction on error (so the audit row persists even when the action rolls back).
8. Commit or roll back the action transaction; return typed JSON.

### Read vs write split

Read endpoints stay conventional REST under `/api/admin/*` (`GET /api/admin/users`, `GET /api/admin/users/:id`, etc.), each protected by a single permission check. Only *mutations* go through the action dispatcher. This keeps list/detail pages fast and avoids shoehorning reads into the action pattern.

### Frontend component

`<AdminActionButton action="users.suspend" params={{ userId }} onSuccess={...} />`:

- Reads the action's declared `RequiresReason` and `Dangerous` flags from a shared client-side manifest (generated from the registry at build time, or fetched once per session).
- Opens a modal with a reason field (if required) and a typed-confirmation input (if dangerous).
- POSTs to `/api/admin/actions/:type` with CSRF token.
- Surfaces validation/permission/execution errors inline.

Per-action custom UIs remain possible (e.g., a full proxy editor form), but the default button handles routine cases.

## Audit Log

### Schema (migration, additive then rename)

Extend `audit_events`:

| column | type | notes |
|---|---|---|
| `id` | uuid | existing |
| `action_type` | text | renamed from `event_type`; matches `AdminAction.Type()` |
| `actor_user_id` | uuid | existing; nullable for system actions |
| `actor_role_snapshot` | text[] | role keys the actor held at action time |
| `resource_type` | text | `user`, `guild`, `proxy`, `server`, `link`, etc. |
| `resource_id` | text | text to cover non-uuid ids |
| `target_user_id` | uuid | kept for convenience querying; nullable |
| `params` | jsonb | post-validation, post-redaction action params |
| `reason` | text | nullable when action does not require reason |
| `result` | text | `success` or `error` |
| `error_message` | text | populated when `result = 'error'` |
| `ip_address` | inet | request source |
| `user_agent` | text | request UA |
| `created_at` | timestamptz | existing |

Indexes: `(actor_user_id, created_at DESC)`, `(resource_type, resource_id, created_at DESC)`, `(action_type, created_at DESC)`, `(target_user_id, created_at DESC)`.

### Secret redaction

Before persisting `params`, the dispatcher walks the JSON and replaces any key listed in `action.SensitiveParamKeys()` with the string `"***"`. Proxy passwords, rotated credentials, etc., never reach the log.

### Read UI

`/admin/audit` gains filters (actor, resource_type + resource_id, action_type, date range, result) and a row-click detail drawer showing full `params` + `reason` + `error_message`. No CSV export in this spec.

### Retention

Unlimited retention for now. Revisit if volume becomes a problem; likely move to partitioned tables by month at that point.

## Admin Shell & Navigation

The existing `AppShell variant="admin"` stays. Two changes:

1. **Permission-filtered nav** — a nav link renders only if the current admin has at least one permission in that section's set. Section → permission mapping lives alongside the nav config so both stay in sync.
2. **Section layout:**
   - **Overview** (`/admin`) — existing dashboard; later augmented with bot/poll health strip (out of scope here).
   - **Users** (`/admin/users`, `/admin/users/[id]`)
   - **Link Review** (`/admin/links`)
   - **Guilds** (`/admin/guilds`, `/admin/guilds/[id]`)
   - **Proxies** (`/admin/proxies`, `/admin/proxies/[id]`)
   - **Audit** (`/admin/audit`)
   - **Roles** (`/admin/roles`) — RBAC admin; edit role→permission mappings, assign roles to users. Gated by `admins.grant`. `superadmin` is displayed but not editable.

### Shared UX primitives

Built once in this spec, consumed by feature specs:

- **`<AdminDataTable>`** — server-rendered table with URL-synced filters, pagination, and column sort. One component used by users, guilds, proxies, audit.
- **`<AdminDetailLayout>`** — two-pane layout: resource summary + actions sidebar. Actions render from a registry lookup keyed by `ResourceType`, filtered by the current admin's permissions.
- **`<AdminActionButton>`** — see Action Framework above.
- **`<ReasonDialog>`** and **`<DangerousConfirmDialog>`** — modal flows for reason capture and typed confirmation.

## Components and Boundaries

| Unit | Responsibility | Consumers |
|---|---|---|
| `internal/admin/rbac` | permission registry, role resolution, `HasPermission` | session middleware, admin routes |
| `internal/admin/actions` | `AdminAction` interface, registry, dispatcher handler | `/api/admin/actions/:type` route |
| `internal/admin/audit` | audit row write, redaction, query helpers | action dispatcher, `/api/admin/audit` route |
| `internal/admin/http` | read-side admin REST handlers (users/guilds/proxies/audit list+detail) | `AppShell` admin pages |
| frontend `components/admin/*` | `AdminDataTable`, `AdminDetailLayout`, `AdminActionButton`, dialogs | admin pages |
| frontend `app/admin/roles` | role/permission admin UI | RBAC admins |

Feature specs (C/D/E/F) add action files under `internal/admin/actions/<area>/`, read handlers under `internal/admin/http/<area>.go`, and pages under `app/admin/<area>/`.

## Data Flow (representative mutation)

1. Admin clicks "Suspend user" on `/admin/users/[id]`.
2. `<AdminActionButton action="users.suspend" params={{ userId }} />` opens `ReasonDialog`.
3. Admin enters reason, submits. Client POSTs to `/api/admin/actions/users.suspend` with `{ params, reason, csrfToken }`.
4. Dispatcher: loads action, checks `users.suspend` permission, enforces reason, validates params, runs `Execute` in a transaction that flips `users.suspended_at` and writes an audit row.
5. Response returned; frontend refreshes the detail view.
6. An entry appears in `/admin/audit` with actor, target, reason, params, timestamp.

## Error Handling

- **Permission denied** → 403 with `{ error: "permission_denied", required: "users.suspend" }`.
- **Reason missing** → 400 with `{ error: "reason_required" }`.
- **Dangerous confirmation mismatch** → 400 with `{ error: "confirmation_mismatch", expected: "<target>" }`.
- **Validation error** → 400 with field-level detail.
- **Execution error** → 500 (or appropriate) with `{ error: "execution_failed", message: "..." }`; audit row is written with `result='error'`.
- Frontend `AdminActionButton` surfaces all above inline in the dialog; no toast-only failures.

## Testing Strategy

- **Unit tests** for: each `AdminAction.Validate` and `Execute`, permission resolution, redaction of sensitive params, dispatcher flow (permission check, reason enforcement, dangerous confirm, audit write on success and error).
- **Integration tests** against Postgres for: action dispatch end-to-end including audit row assertion, RBAC migration from legacy `internal_admin`, role edit in UI.
- **Frontend component tests** for: `AdminActionButton` flows (reason, dangerous, success, error), `AdminDataTable` URL-synced filters, permission-filtered nav.
- No mocks of the database for integration tests.

## Migration & Rollout

1. Migration 1: create RBAC tables, seed permissions, seed `superadmin` / `moderator` / `infra_admin` / `guild_ops` roles, grant `superadmin` to all current `internal_admin` users.
2. Migration 2: extend `audit_events` with new columns; rename `event_type` → `action_type`.
3. Backend: add RBAC, action framework, audit extensions, role admin endpoints. Read handlers stay on the old permission-gate path until converted.
4. Frontend: add shared primitives, roles page, and swap the admin layout's `internal_admin` check for `requireAdminSession` + per-route permission checks.
5. Follow-up PR: drop `internal_admin` column once all call sites are on roles.

Feature specs C/D/E/F are unblocked once steps 1–4 land.

## Open Questions

None at design time — deferred decisions are explicitly called out as out-of-scope (retention policy, two-person approval, CSV export, bot/poll health strip).
