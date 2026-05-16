# Admin API Keys — Design

**Date:** 2026-05-16
**Status:** Approved

## Summary

Add API keys for internal/admin tooling. Keys authenticate the existing `/api/app/admin/*` and `/api/internal/bot/*` HTTP surfaces — no new public API is introduced. A key is opaque (`rl_admin_<32-byte base62>`), shown to the creator exactly once at issuance, and stored only as a SHA-256 hash. Each key carries a snapshot of permission keys drawn from the existing RBAC catalog, and is bound to its creator's `app_users.id`; at request time the effective permission set is `intersect(key permissions, creator's current permissions)`, so revoking a permission from the creator (or suspending/deleting them) immediately constrains every key they minted.

Two new RBAC permission keys are added:

- `api_keys.manage` — create, rotate, revoke API keys. Seeded onto `infra_admin`. The creator may only grant a key permissions they themselves hold (superadmin sees the full catalog).
- `bot_routes.read` / `bot_routes.write` — gate API-key access to `/api/internal/bot/*` (read = GET/HEAD, write = everything else). Not seeded onto any system role; must be granted explicitly. Superadmin has them implicitly via the wildcard.

UI lives at `/admin/api-keys` under a new "Infrastructure" sidebar group that also houses the existing Proxies and BattleMetrics Servers pages. Lifecycle mutations (`api_keys.create`, `api_keys.rotate`, `api_keys.revoke`) route through the existing admin action dispatcher so they inherit audit logging and required-reason capture for free.

## Permission gating

- **Page**: server-side `requirePermission("api_keys.manage")`. Non-holders are redirected to `/admin`.
- **Sidebar group "Infrastructure"**: rendered when the current session has any of `proxies.read`, `bm_servers.read`, or `api_keys.manage`. The **API Keys** entry within it is rendered only when `api_keys.manage` is present.
- **All three lifecycle actions**: dispatcher rejects requests from actors lacking `api_keys.manage`.
- **Grant constraint**: `api_keys.create` and `api_keys.rotate` additionally call a new guard `AssertPermissionsSubsetOfActor(requested, actor)` that 403s with `permission_escalation` if the actor tries to grant a permission they do not currently hold. Superadmin (implicit all-permissions) passes this guard for any input.

## Data model

### New table: `api_keys`

Auto-migrated via `EnsureSchema()` in `apps/backend/internal/store/postgres.go`.

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | `gen_random_uuid()` |
| `name` | `text NOT NULL` | Human label, ≤64 chars |
| `prefix` | `text NOT NULL UNIQUE` | First 16 chars of the secret (`rl_admin_` + 7 random chars, ~3.5B combos — collision-free at any realistic scale), e.g. `rl_admin_a3f2x7q` |
| `last4` | `text NOT NULL` | Last 4 chars of the secret, for disambiguation in the UI |
| `secret_hash` | `bytea NOT NULL` | SHA-256 of the full secret |
| `created_by_user_id` | `uuid NOT NULL REFERENCES app_users(id) ON DELETE CASCADE` | The cascade is OK because lookup also revokes on `app_users.deleted_at`/`suspended_at` (see middleware) |
| `permissions` | `text[] NOT NULL` | Permission keys snapshot at create/rotate |
| `expires_at` | `timestamptz NULL` | Optional |
| `revoked_at` | `timestamptz NULL` | Set on manual revoke or creator suspend/delete. Expired keys are rejected by the middleware on every request, so no separate "expired" revoke state is stored. |
| `revoked_reason` | `text NULL` | One of `manual`, `creator_suspended`, `creator_deleted` |
| `last_used_at` | `timestamptz NULL` | Coalesced fire-and-forget write (see middleware) |
| `last_used_ip` | `inet NULL` | Same |
| `created_at` | `timestamptz NOT NULL DEFAULT now()` | |
| `updated_at` | `timestamptz NOT NULL DEFAULT now()` | Bumped on rotate / revoke |

Indexes:

- `UNIQUE (prefix)` — primary lookup path
- `(created_by_user_id, revoked_at)` — per-user list query

### Modified table: `audit_events`

(`audit_events` is the table that the admin dispatcher writes to via `internal/admin/audit/audit.go` — distinct from the browser-auth `app_audit_events` table, which is unaffected by this design.)

Two new nullable columns, auto-migrated:

| Column | Type | Notes |
|---|---|---|
| `via_api_key_id` | `uuid NULL REFERENCES api_keys(id) ON DELETE SET NULL` | |
| `via_api_key_name` | `text NULL` | Denormalized snapshot so the row remains readable after rename / FK null |

`actor_user_id` keeps pointing at the **creator** (the human). The two new columns are the metadata that say "this human's action was performed via API key X".

No backfill required; existing rows have `NULL` in both columns. The `audit.Entry` / `audit.Row` Go structs and `Store.Record` / `Store.Query` are extended additively to carry these fields.

## Auth flow

### `APIKeyAuth` middleware

Lives in `apps/backend/internal/appauth/api_key_middleware.go`. Mounted on **both** the `/api/app/admin/*` subrouter and the `/api/internal/bot/*` subrouter, in each case **before** the existing auth middleware (session cookie for admin routes, bot JWT for internal routes).

Flow per request:

1. Read `Authorization` header. If absent or does not start with `Bearer rl_admin_`, pass through with no change. The downstream auth middleware then handles dashboard sessions / bot JWT as it does today.
2. Extract the secret. Compute `sha256(secret)`. Look up by `prefix` (the first 12 chars).
3. Reject with `401 invalid_api_key` if: prefix not found, hash mismatch (constant-time compare), or row exists but `secret_hash` length differs.
4. Reject with `401 api_key_revoked` if `revoked_at IS NOT NULL`.
5. Reject with `401 api_key_expired` if `expires_at IS NOT NULL AND expires_at < now()`.
6. Load the creator via `created_by_user_id`. If the creator is suspended or soft-deleted, revoke the key in place (set `revoked_at = now()`, `revoked_reason = creator_suspended | creator_deleted`) and reject with `401 api_key_revoked`.
7. On success: stash `apiKeyAuth{KeyID, KeyName, UserID, KeyPermissions []string}` on the request context. Continue.

After the handler returns, the middleware schedules a best-effort `last_used_at` / `last_used_ip` update. Updates are coalesced per-key by an in-process map with a 60-second debounce so hot keys do not cause one DB write per request. The coalescer is purely advisory — losing an update on shutdown is acceptable.

### Permission resolution

`internal/admin/rbac/resolver.go` gets one new branch. When the request context carries `apiKeyAuth`, the effective permission set is `intersect(apiKeyAuth.KeyPermissions, currentUserPermissions(apiKeyAuth.UserID))`. The intersection is computed at request time on the user's *current* role permissions, so revoking a permission from the creator immediately constrains the key without touching the key row. Superadmin's implicit all-permissions semantics keep working because the resolver continues to short-circuit on superadmin before computing the intersection.

### Bot-route scope enforcement

A new middleware `requireBotRouteScope` mounts on `/api/internal/bot/*` **after** `APIKeyAuth` and **before** the existing JWT middleware:

- If the request was authenticated by an API key (`apiKeyAuth` is on the context), require `bot_routes.read` for `GET` / `HEAD` and `bot_routes.write` for everything else, using the same resolver as admin routes. Reject with `403 forbidden` on miss.
- If `apiKeyAuth` is *not* on the context, pass through unchanged — bot-worker JWT traffic keeps its blanket access.

### `/api/events/stream`

Out of scope. The SSE endpoint stays cookie-authenticated only.

## Audit attribution

### Admin actions (existing dispatcher path)

`internal/admin/actions/dispatcher.go` populates `via_api_key_id` and `via_api_key_name` from `apiKeyAuth` on the context, alongside the existing `actor_user_id`. No other dispatcher behavior changes.

### Bot-route writes (new path)

Bot routes do not flow through the dispatcher. `requireBotRouteScope` writes one `audit_events` row per API-key-authenticated bot-route **write** (POST/PUT/DELETE/PATCH) on the way out, after the handler has produced a status code, using the same `audit.Store.Record` entry point the dispatcher uses:

| Field | Value |
|---|---|
| `actor_user_id` | `apiKeyAuth.UserID` (the human creator) |
| `via_api_key_id` / `via_api_key_name` | from `apiKeyAuth` |
| `action_type` | `bot_route.write` |
| `resource_type` | `bot_route` |
| `resource_id` | the request path, e.g. `/api/internal/bot/setup/guilds/123/track-groups` |
| `params` | `{"method": "POST", "status": 200}` |
| `result` | `success` if status < 400, `error` otherwise |
| `ip_address` / `user_agent` | from the request, same as dispatcher |
| `reason` | nil (bot routes do not carry the dispatcher's reason capture) |

Reads (`GET`/`HEAD`) are **not** individually audited — `api_keys.last_used_at` + `last_used_ip` is the visibility for those. This keeps the audit log usable.

### API-key lifecycle events

Three new dispatcher actions, each in its own file under `apps/backend/internal/admin/actions/` (matching the existing one-file-per-action convention):

| Action | Params | Behavior |
|---|---|---|
| `api_keys.create` | `{ name, permissions: string[], expiresAt?: string }` | Validates `name` non-empty ≤64 chars; validates every permission key against `rbac.IsValidPermission`; runs `AssertPermissionsSubsetOfActor`. Generates 32 bytes from `crypto/rand`, base62-encodes, prepends `rl_admin_`. Inserts the `api_keys` row with `secret_hash = sha256(secret)`. Returns `{ apiKey: {...metadata...}, secret: "rl_admin_..." }` — the only response shape that includes the plaintext secret. |
| `api_keys.rotate` | `{ keyId }` | Loads the row; rejects if `revoked_at IS NOT NULL`. Generates a new secret, updates `secret_hash`, `prefix`, `last4`, `updated_at`. Preserves `id`, `name`, `permissions`, `created_by_user_id`, `expires_at`, `created_at` so audit history stays attached. Returns `{ apiKey, secret }`. |
| `api_keys.revoke` | `{ keyId }` | Sets `revoked_at = now()`, `revoked_reason = manual`. Idempotent: re-revoking an already-revoked key is a no-op (200, no audit row). |

All three:

- `Permission()` returns `rbac.PermApiKeysManage`.
- `RequiresReason()` returns `true`.
- `Dangerous()` returns `false`.
- `SensitiveParamKeys()` returns `nil` (no sensitive content goes through params — the generated secret is never in the request, only in the response).

The `secret` field in `api_keys.create` / `api_keys.rotate` responses is redacted from audit `event_data` via the existing redactor in `internal/admin/actions/redact.go`.

## Backend reads

### New: `GET /api/app/admin/api-keys`

Mounted under `/api/app/admin/` in `internal/http/router.go`. Inherits `requireSession` from the admin route group and is additionally gated by `api_keys.manage` inside the handler.

Query params:

- `q` (optional) — substring match on `name` or `prefix`
- `includeRevoked` (optional bool, default `false`)
- `page` (default 1), `pageSize` (default 20, capped at 100)

Response:

```json
{
  "items": [
    {
      "id": "...",
      "name": "deploy-bot",
      "prefix": "rl_admin_a3f2x7q",
      "last4": "x7q2",
      "permissions": ["users.read", "bm_servers.read"],
      "createdByUserId": "...",
      "createdByDisplayName": "Sem",
      "expiresAt": "2026-08-14T00:00:00Z",
      "revokedAt": null,
      "revokedReason": null,
      "lastUsedAt": "2026-05-16T09:21:14Z",
      "lastUsedIp": "203.0.113.7",
      "createdAt": "2026-05-16T08:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "pageSize": 20
}
```

Note: the full `secret` is **never** returned by this endpoint. It is shown exactly once in the `api_keys.create` / `api_keys.rotate` action response.

## Frontend

### Sidebar

Add an **Infrastructure** group to `apps/frontend/components/app-sidebar.tsx`:

- Proxies — existing, `proxies.read`
- BattleMetrics Servers — existing, `bm_servers.read`
- **API Keys** — new, `api_keys.manage`

The group renders only if the session has any of the three. Each entry renders only if the user has its specific permission.

### Page: `app/admin/api-keys/page.tsx`

```
┌─ API Keys ─────────────────────────────────  [+ New API key] ─┐
│  [search by name or prefix]      ☐ Show revoked                │
│                                                                │
│  Name           Prefix         Permissions  Last used  Expires │
│  ─────────────  ─────────────  ───────────  ─────────  ─────── │
│  deploy-bot     rl_admin_a3f2x7q  3 perms ⓘ   2m ago     never   │
│  ci-runner      rl_admin_77abk2n  1 perm  ⓘ   12d ago    in 18d  │
│  old-script     rl_admin_dde1m4p  revoked     —          —       │
└────────────────────────────────────────────────────────────────┘
```

Row click opens a side sheet with full detail (created-by, IP, full permission list, raw expiry). Row actions in a dropdown:

- **Rotate** — opens confirmation dialog; on confirm, calls `api_keys.rotate` and switches to the one-time-secret reveal panel.
- **Revoke** — opens confirmation dialog; on confirm, calls `api_keys.revoke`. Revoked rows are visually muted and hidden by default.

Both actions require a `reason` (dispatcher enforces it).

### Create dialog

Fields:

- **Name** (required, ≤64 chars)
- **Permissions** — checkbox grid built from `GET /api/app/admin/permissions` (already exposed for the roles page), filtered client-side to permissions the current user currently holds. Permissions are grouped by family (Users, Proxies, BM Servers, Bot Routes, …) so the elevated scope of `bot_routes.*` is visually obvious. Superadmin sees the full catalog ungated.
- **Expires** — radio: `Never`, `30 days`, `90 days`, `1 year`, `Custom` (date picker).
- **Reason** (required).

On submit, calls `POST /api/app/admin/actions/api_keys.create`. The dialog then **replaces** its content with a one-time reveal panel:

```
┌─ Copy your key ──────────────────────────────────────────┐
│  This is the only time the full secret will be shown.    │
│                                                          │
│  rl_admin_a3f2x7q2…                          [📋 Copy]  │
│                                                          │
│  Store it somewhere safe (password manager, secret       │
│  store). If you lose it, rotate the key.                 │
│                                                          │
│                                              [ I copied it ] │
└──────────────────────────────────────────────────────────┘
```

The `I copied it` button is the only way to dismiss the dialog. The same reveal panel is used after `Rotate`.

## Testing

Tests live with the code they cover.

### `internal/appauth/api_key_middleware_test.go` (new)

- Missing / non-matching `Authorization` header → request passes through to session auth (admin) or JWT auth (bot routes), middleware mutates nothing.
- Valid key → `apiKeyAuth` populated on context with correct `UserID`, `KeyID`, `KeyName`, `KeyPermissions`.
- Unknown prefix → 401 `invalid_api_key`.
- Correct prefix, wrong secret → 401 `invalid_api_key`; verified via constant-time compare branch.
- Revoked → 401 `api_key_revoked`.
- Expired → 401 `api_key_expired`.
- Creator suspended → 401 `api_key_revoked` AND row is auto-revoked with `revoked_reason = creator_suspended`.
- Creator deleted → same with `revoked_reason = creator_deleted`.
- `last_used_at` coalescer: two consecutive requests within 60s produce exactly one DB write; a third request after 60s produces a second write.

### `internal/admin/rbac/resolver_test.go` (extend)

- With `apiKeyAuth` on the context: effective permissions = `intersect(KeyPermissions, currentUserPermissions)`.
- Creator loses a permission after key creation → request that requires that permission 403s without any write to the key row.
- Superadmin actor short-circuits intersection (still sees all permissions).

### `internal/admin/actions/api_keys_*_test.go` (new files)

- `api_keys_create_test.go` — requires `api_keys.manage`; `AssertPermissionsSubsetOfActor` 403s with `permission_escalation` on escalation; happy path returns the secret and writes an audit row; invalid permission key rejects; `name` length validation; `expiresAt` parsing.
- `api_keys_rotate_test.go` — preserves `id`, `name`, `permissions`, `created_by_user_id`; new hash differs from old; revoked keys cannot be rotated; audit row written; secret redacted in audit `event_data`.
- `api_keys_revoke_test.go` — sets `revoked_at` and `revoked_reason = manual`; re-revoke is idempotent (no second audit row); audit row written.

### `internal/admin/audit/audit_test.go` (extend)

- An admin-dispatcher action performed via API key writes an `audit_events` row with `via_api_key_id` + `via_api_key_name` populated; `actor_user_id` still = the creator.

### `internal/http/bot_route_scope_test.go` (new)

- JWT-authenticated bot request → `requireBotRouteScope` is a no-op.
- API-key authenticated GET → requires `bot_routes.read`; 403 on miss; 200 with the permission.
- API-key authenticated POST → requires `bot_routes.write`; 403 on miss; 200 with the permission.
- API-key authenticated POST with permission → writes one `audit_events` row with `action_type = bot_route.write`, `resource_id = <path>`, and `params` containing `{method, status}`.
- API-key authenticated GET with permission → writes **no** `audit_events` row.

### `internal/admin/e2e_test.go` (extend)

- Create key via dispatcher → call `/api/app/admin/users` with the key → audit row has `via_api_key_*` populated → revoke → same call now 401 `api_key_revoked`.

### Frontend

No new test file required; `make lint` runs `tsc --noEmit`. Manual verification of create / one-time reveal / rotate / revoke flows in `make dev`.

## Out of scope (deferred)

- **Per-key rate limiting.** Not added at v1. If a runaway script becomes a real problem, revisit with a per-key Redis sliding window reusing `lib/rate-limit.ts`.
- **A separate `/api/v1/*` public surface.** All v1 traffic rides on existing `/api/app/admin/*` and `/api/internal/bot/*`. If we ever open a public developer API, that is its own design.
- **Per-IP allowlists on keys.** Track `last_used_ip` only.
- **Audit of read-only bot-route calls.** Visibility on those comes from `last_used_at` + `last_used_ip` only.
- **API keys on `/api/events/stream`.** SSE stays cookie-only.
