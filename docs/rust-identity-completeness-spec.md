# Rust Identity Service — Completeness Spec

Status: **✅ COMPLETE** — all spec items implemented, verified, and merged to
`rework/rust-backend`.
Author: migration work, 2026-06-30
Branch base: `rework/rust-backend`

## Completion summary

Every item below is done. Each was `cargo build` / `cargo clippy --all-targets`
clean and `cargo test` green, and end-to-end smoke-tested against throwaway
Postgres/Redis before merge.

| Item | Scope | PR | Status |
|---|---|---|---|
| Spec | this document | #167 | ✅ merged |
| 1 | `/api/app/me` `steamAccounts` + `guilds` | #167 | ✅ merged |
| 2 | guild sync on the OAuth callback (`user_guilds`) | #168 | ✅ merged |
| 3a | admin read routes (permissions/roles/users/administrators/audit) + auth gate | #169 | ✅ merged |
| 5 | `/api/events/stream` SSE + Redis fan-out (`rustlens-events`) | #170 | ✅ merged |
| 3b | admin action dispatcher + role/user-lifecycle mutations + event publish | #171 | ✅ merged |
| 4a | role-connection OAuth + AES-GCM token storage (`rustlens-crypto`) | #172 | ✅ merged |
| 4b | role-connection reconciler + token refresh + on-grant push hook | #173 | ✅ merged |
| — | `/api/app/me` aligned onto the header-based auth gate | #174 | ✅ merged |

Workspace grew to **6 crates**: `rustlens-session`, `rustlens-config`,
`rustlens-db`, `rustlens-events`, `rustlens-crypto`, + `services/identity`.

### Deferred follow-ups (NOT part of this spec — intentionally out of scope)

Confirmed not implemented; none are cutover-blocking for the auth surface:

- **Admin API-key auth path** (the `api_keys.manage` permission *string* exists
  in the catalog, but there is no API-key auth, no `api_keys.*` actions, and no
  `…/admin/api-keys` route). Admin routes are session+app-secret only.
- **Dispatcher rate-limiting + idempotency** (the Go token-bucket +
  `Idempotency-Key` replay; deliberately omitted — they don't change the
  normal-call contract).
- **Remaining admin routes**: guilds list+detail, user detail, user-installs,
  servers, bm-servers / proxies CRUD, command-usage, `audit/{id}` +
  `audit/event-types`.
- **Infra**: Rust Dockerfile, CI cargo job, Traefik routing — not deploying
  during this migration.

## 0. Purpose & ground rules

The Rust identity service (`apps/rust/services/identity`) currently implements
Discord-OAuth login, session minting, and a partial `GET /api/app/me`. This spec
covers the work needed to make its endpoints **true drop-in replacements** for
the Go auth surface.

Hard constraints (carried from the project memory and AGENTS.md):

- **Same Postgres, no schema changes, no migrations.** The Go backend owns
  `EnsureSchema()`. Rust only reads/writes existing tables.
- **`rustlens_session` cookie is a byte-for-byte cross-language contract** (Go
  `apps/backend/internal/appauth/session.go`, JS `apps/frontend/lib/session.ts`,
  `crates/rustlens-session`). Proven by the known-answer test in
  `rustlens-session`. Nothing here changes that format.
- **JSON responses must match the frontend TS types in
  `apps/frontend/lib/rustlens-api.ts` exactly** (field names, nesting,
  optional-vs-present). Where Go and the frontend disagree, the frontend is the
  consumer of record; we mirror the Go *handler that actually serves the route*.
- **Not deploying during this migration.** No Traefik/Docker/CI/infra wiring.

Source-of-truth note: each item below quotes the Go handler **actually wired to
the route** (verified via `router.go`), not look-alike helpers. One look-alike
(`store.GetCurrentUserByID` / `currentUserToMap`) is **not** the live `/me`
path; see §3.

---

## 1. Endpoint inventory & target ownership

All of these are served by the Go backend today. The identity service will take
them over (logically owned by the identity domain per the migration plan).

| Route | Method | Auth | Status (Rust) | Item |
|---|---|---|---|---|
| `/api/app/me` | GET | app-secret + session header | ✅ done (#167, #174) | 1 |
| `/api/auth/discord` (+`/callback`) | GET | none / state cookie | ✅ done incl. guild sync (#168) | 2 |
| `/api/app/admin/*` | GET/POST/… | app-secret + session header + per-perm | ✅ done (read routes + dispatcher; API-key auth deferred) (#169, #171) | 3 |
| `/api/auth/discord/role-connection` (+`/callback`) | GET | session cookie | ✅ done (#172) | 4 |
| `/api/app/me/discord-role-connection` | DELETE | app-secret + session header | ✅ done (#172) | 4 |
| `/api/events/stream` | GET | session cookie (SSE) | ✅ done (#170) | 5 |

---

## 2. Cross-cutting building blocks (needed by ≥2 items)

These are prerequisites that several items share. Build them once.

### 2.1 `requireSession` extractor (items 1, 3, 4, 5)

Go: `appauth.RequireSession(secret, pool)` middleware →
`appauth.SessionFromContext`. It (a) reads the `rustlens_session` cookie, (b)
verifies HMAC + expiry via the shared format, (c) runs the **DB revocation
check** (`session_epoch` < current → revoked; `suspended_at`/`deleted_at` not
null → revoked).

Rust today does this inline inside `handlers::me`. **Refactor into a reusable
axum extractor / helper** (`rustlens-db::identity::revocation_check` already
exists; wrap the cookie-parse + parse_session + revocation_check into one
`authenticate(headers) -> Result<SessionUser, Response>` helper in the identity
service, or an axum `FromRequestParts`). All session-authed handlers use it.

Error classification must match Go's JSON error codes for the SSE path
(`session_revoked`, `session_expired`, `session_required`, `invalid_session`) —
see §6.4. The `/me` path currently returns `{"error": "..."}` free text; keep
existing `/me` behavior, but standardize codes where the frontend switches on
them (it maps notices via `lib/session-errors.ts`; verify during impl).

### 2.2 `discord_id` for the acting user (items 1)

The steam/guild joins key on `app_users.discord_id`, not `app_users.id`. The
existing `load_app_user` already selects `discord_id`. Restructure
`load_current_user` to load the user first, then pass `user.discord_id` to the
new join queries.

### 2.3 App-secret header (item 3) — ✅ done; also applied to `/me` (PR #174)

Go guards `/api/app/*` (non-`/me`) behind an internal shared-secret header
(`APP_API_INTERNAL_SECRET`) for server-only Next.js calls, **bypassed when an
API-key auth context is present**. For the identity migration we mainly need the
**session** path. Decision to confirm with the user (see §8): whether the Rust
admin routes must also honor the `APP_API_INTERNAL_SECRET` header and/or admin
API keys, or session-only is acceptable for the milestone.

---

## 3. ITEM 1 — Fill `/api/app/me` `steamAccounts` + `guilds` (HIGHEST PRIORITY)

> **✅ DONE — PR #167.** Implemented as specified; `/me` populates both arrays.
> (Note: `/me` was later aligned onto the header-based auth gate in PR #174.)

### 3.1 Critical nuance (read before implementing)

The **live** Go `/me` handler is `appauth.Handler.Me`
(`internal/appauth/handlers.go:29`) → `Repo.LoadCurrentUser`
(`internal/appauth/repo.go:91`). That function **also returns empty slices**:

```go
// repo.go:108-118
return &CurrentUser{
    User:          *u,
    SteamAccounts: []LinkedSteamAccount{}, // <-- empty in live Go too
    Guilds:        []UserGuildMembership{},
    AdminRoles:    adminRoles,
    Permissions:   permissions,
    Entitlements:  entitlements,
}, nil
```

So the **current Rust code is already a faithful match of the live Go `/me`.**
The actual join logic lives in a *different, non-`/me`* path
(`store.GetCurrentUserByID` / `currentUserToMap`, wired to nothing relevant) and
in the frontend types. The dashboard reads steam/guild data from **dedicated
routes** (`/api/app/me/connections`, `/api/app/me/guilds`,
`/api/app/me/guilds/installed`, `/api/app/me/guilds/{guildID}/links`), which is
why Go leaves the `/me` arrays empty.

**Per the explicit instruction**, Item 1 = *complete* the Rust `/me` by
populating these arrays from the real join SQL. Result: Rust `/me` becomes
strictly richer than Go's stub but stays JSON-compatible (frontend tolerates
populated arrays — the types declare them non-optional). This unblocks ever
routing `/me` to Rust without depending on the side routes.

> Decision flagged in §8.1: confirm we want Rust `/me` to populate these (richer
> than Go) vs. mirror Go's empty stub. Default assumption: **populate**.

### 3.2 Target JSON shape (frontend `CurrentUser`, `rustlens-api.ts`)

```ts
type CurrentUser = {
  user: AppUser;
  steamAccounts: LinkedSteamAccount[];
  guilds: UserGuildMembership[];
  adminRoles: string[];
  permissions: string[];
  entitlements?: string[];
  discordRoleConnection?: DiscordRoleConnectionSummary | null;
};
type LinkedSteamAccount = {
  steamId: string; bmId: string; bmName: string;
  avatarImage: string; type: string; source: string;
};
type UserGuildMembership = {
  guildId: string; isActive: boolean;
  firstSeenAt: string; lastSeenAt: string; leftAt: string | null;
};
```

Go structs that this maps to (`appauth/repo.go:35-52`) carry identical json
tags. The Rust `CurrentUser` already matches `user/steamAccounts/guilds/
adminRoles/permissions/entitlements`. Two deltas:

1. Replace `steam_accounts: Vec<serde_json::Value>` / `guilds: Vec<Value>` with
   typed `Vec<LinkedSteamAccount>` / `Vec<UserGuildMembership>`.
2. Add `discord_role_connection: Option<DiscordRoleConnectionSummary>` with
   `#[serde(rename = "discordRoleConnection", skip_serializing_if = "Option::is_none")]`.
   It stays `None` until Item 4 wires the role-connection store. (Go uses
   `omitempty`; frontend marks it optional+nullable — `skip_serializing_if`
   matches `omitempty`.)

### 3.3 Exact SQL to port (from `store/app_users.go:321-356`)

**Steam accounts** (keyed on the user's `discord_id`):

```sql
SELECT DISTINCT sa.steam_id,
    COALESCE(sa.bm_id, ''),
    COALESCE(bpc.bm_name, ''),
    COALESCE(sac.avatar_image, ''),
    sdl.type,
    sdl.source
FROM steam_discord_links sdl
INNER JOIN steam_accounts sa ON sa.steam_id = sdl.steam_id
LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
LEFT JOIN steam_account_cache sac ON sac.steam_id = sa.steam_id
WHERE sdl.discord_id = $1
ORDER BY sa.steam_id
```

Column → field: `steam_id→steamId`, `bm_id→bmId`, `bpc.bm_name→bmName`,
`sac.avatar_image→avatarImage`, `sdl.type→type`, `sdl.source→source`.

> Schema note: `bm_name` lives in **`bm_profile_cache`** (keyed by `bm_id`), not
> `steam_account_cache`. `avatar_image` is in `steam_account_cache` (keyed by
> `steam_id`). Confirmed in `apps/backend/CLAUDE.md` schema section. Do **not**
> read `bm_name` from `steam_account_cache`.
>
> This query does **not** filter `hidden_discord_link_identities` (the live Go
> `/me` path doesn't either). We mirror that.

**Guild memberships:**

```sql
SELECT guild_id, is_active, first_seen_at, last_seen_at, left_at
FROM user_guilds
WHERE discord_id = $1
ORDER BY is_active DESC, last_seen_at DESC
```

Column → field: `guild_id→guildId`, `is_active→isActive`,
`first_seen_at→firstSeenAt`, `last_seen_at→lastSeenAt`, `left_at→leftAt`
(`Option<DateTime<Utc>>`, serializes as `null` when absent — matches `leftAt:
string | null`). Returns both active and inactive rows (no `is_active` filter).

### 3.4 `rustlens-db` changes

In `crates/rustlens-db/src/identity.rs`:

- Add `pub struct LinkedSteamAccount { steam_id, bm_id, bm_name, avatar_image, link_type, source }`
  with serde renames matching §3.2 (`link_type` → `"type"`).
- Add `pub struct UserGuildMembership { guild_id, is_active, first_seen_at, last_seen_at, left_at: Option<DateTime<Utc>> }`.
- Add `async fn load_linked_steam_accounts(pool, discord_id) -> Result<Vec<LinkedSteamAccount>>`.
- Add `async fn load_user_guilds(pool, discord_id) -> Result<Vec<UserGuildMembership>>`.
- Change `CurrentUser.steam_accounts`/`guilds` to the typed vecs; add
  `discord_role_connection: Option<...>` (type can be a placeholder struct now,
  finalized in Item 4).
- Update `load_current_user` to: load app user → derive `discord_id` →
  (if non-empty) run both join queries → assemble. Empty `discord_id` ⇒ empty
  vecs (matches Go's short-circuit semantics).

`chrono` `DateTime<Utc>` serializes to RFC3339 — matches the Go `time.Time`
JSON the frontend already parses as ISO strings.

### 3.5 Verification

- Unit-ish: with seeded rows (one soft + one hard link, two guilds incl. one
  with `left_at`), assert the serialized JSON equals a hand-written fixture
  matching the TS types (key order/casing/null handling).
- Smoke: throwaway PG (port 5433), seed `steam_discord_links`, `steam_accounts`,
  `bm_profile_cache`, `steam_account_cache`, `user_guilds`; mint a cookie; curl
  `/api/app/me`; diff against the Go backend's response for the same data
  (run Go `/me` against the same seeded DB if feasible, else compare to fixture).

**rustlens-db smoke DDL subset for Item 1:** `app_users`, `admin_*`,
`user_entitlements` (already used), plus `steam_accounts`, `steam_discord_links`,
`bm_profile_cache`, `steam_account_cache`, `user_guilds`.

---

## 4. ITEM 2 — Guild sync on the OAuth callback

> **✅ DONE — PR #168.** `sync_user_guilds` ports the two-statement transaction;
> log-and-continue wired into the callback. DB-gated integration test added.

### 4.1 Go behavior (`oauth/discord/handlers.go:164-173`, `store/bot_guilds.go:144-197`)

On callback, **if a guild syncer is configured**, fetch the user's guild list
and upsert into `user_guilds`. **Log-and-continue** — neither the fetch nor the
sync error blocks sign-in.

Fetch: `GET https://discord.com/api/users/@me/guilds?with_counts=true` with
`Authorization: Bearer <access_token>` (the same token from code exchange; scope
`guilds` is already requested). No pagination. Parsed fields:

```go
ID string `json:"id"`; Name string `json:"name"`; Icon *string `json:"icon"`;
Owner bool `json:"owner"`; ApproximateMemberCount *int32 `json:"approximate_member_count"`
```

Sync (single transaction, two statements):

```sql
-- (1) upsert all guilds the user IS currently in, as active
INSERT INTO user_guilds (
    discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at,
    name, icon_hash, approximate_member_count, is_owner
)
SELECT $1, t.gid, TRUE, NOW(), NOW(), NULL, t.name, t.icon, t.members, t.is_owner
FROM unnest($2::text[], $3::text[], $4::text[], $5::int[], $6::bool[])
    AS t(gid, name, icon, members, is_owner)
ON CONFLICT (discord_id, guild_id) DO UPDATE
SET is_active = TRUE, last_seen_at = NOW(), left_at = NULL,
    name = EXCLUDED.name, icon_hash = EXCLUDED.icon_hash,
    approximate_member_count = EXCLUDED.approximate_member_count,
    is_owner = EXCLUDED.is_owner;

-- (2) mark guilds the user is NO LONGER in as inactive
UPDATE user_guilds
SET is_active = FALSE, left_at = COALESCE(left_at, NOW())
WHERE discord_id = $1 AND is_active = TRUE AND guild_id <> ALL($2::text[]);
```

`first_seen_at` is only set on insert (never overwritten). Empty guild list ⇒
statement (1) is a no-op insert; statement (2) deactivates all previously-active
rows — preserve that exact behavior (pass empty arrays).

### 4.2 Rust changes

- `discord.rs`: add `fetch_user_guilds(http, access_token) -> Vec<UserGuild>`
  (struct fields per §4.1; `icon`/`approximate_member_count` are
  `Option`-typed).
- `rustlens-db::identity`: add
  `sync_user_guilds(pool, discord_id, &[UserGuild]) -> Result<()>` running both
  statements in one `sqlx` transaction. Bind the five parallel arrays via
  `unnest` (sqlx supports `Vec<String>` / `Vec<Option<i32>>` etc. as Postgres
  arrays — note `approximate_member_count` is `int4`; map `None`→NULL).
- `handlers.rs::callback`: after `touch_last_login`, call fetch+sync inside the
  existing log-and-continue block (replace the current "guild sync omitted"
  comment). Failures `tracing::warn!` and proceed to mint the cookie.

### 4.3 Verification

- Smoke with a **mocked** Discord guilds response is hard end-to-end (real OAuth
  needed). Instead: unit-test `sync_user_guilds` directly against throwaway PG —
  seed an existing active guild not in the new list, pass a new list, assert (a)
  new guilds upserted active, (b) the stale one flipped to `is_active=false`
  with `left_at` set, (c) `first_seen_at` preserved on re-sync. DDL subset:
  `user_guilds` only.

---

## 5. ITEM 3 — `/api/app/admin/*` RBAC routes

> **✅ DONE — 3a (read routes + auth gate) PR #169, 3b (dispatcher + mutations)
> PR #171.** Per the §5.3 cut line: read routes = permissions/roles/users/
> administrators/audit; dispatcher actions = the role + user-lifecycle set, each
> publishing the Item 5 events. Auth = app-secret + session header (admin API-key
> path deferred). Rate-limiting + idempotency deferred (see follow-ups at top).

This is the largest item: 28 Go routes, a 14-action mutation dispatcher, a
27-key permission catalog, plus rate-limiting, idempotency, and audit. Propose
**phased delivery** and a scope decision with the user (§8.2).

### 5.1 Permission catalog (`internal/admin/rbac/permissions.go`)

`rbac.AllPermissions()` = static list of **27** `{key, description}` entries
(e.g. `users.read`, `users.edit`, `users.roles.assign`, `users.suspend`,
`users.security.reset`, `users.delete`, `users.audit.read`, `admins.grant`,
`proxies.read/edit/rotate_credentials`, `bm_servers.read/edit`,
`guilds.read`, `roles.read`, `audit.read`, `api_keys.manage`,
`bot_routes.read/write`, …). Port as a static Rust table (a `const`/`once` slice)
— `GET /api/app/admin/permissions` returns `{ items: [{key, description}] }`
verbatim from it. **No DB.** This is the cheapest route and a good first slice.

### 5.2 Authorization model

- Actor = session user (`SessionFromContext`) **or** admin API-key context.
- Read routes: wrapped with `requireAdminPerm(<perm>)` → resolves actor user id →
  `HasPermission(userId, perm)` → 403 on miss.
- `HasPermission`: superadmin role ⇒ all perms; else union of the user's roles'
  `admin_role_permissions`. (Same logic already in `load_permissions`; reuse it
  — compute the user's permission set once, then check membership.)
- Mutations: single `POST /api/app/admin/actions/{type}` dispatcher; each action
  declares its required permission; checked before `Execute`.

### 5.3 Phased plan

**Phase 3a — read routes (MVP, no mutations):**

| Route | Perm | Response (TS) |
|---|---|---|
| `GET /admin/permissions` | `roles.read` | `{items: AdminPermissionItem[]}` (static) |
| `GET /admin/roles` | `roles.read` | `{items: AdminRoleItem[]}` |
| `GET /admin/users` | `users.read` | `{items, total, page, pageSize}` (AdminUserItem) |
| `GET /admin/administrators` | `users.read` | same page shape, admins-only |
| `GET /admin/users/{id}` | `users.read` | `AdminUserDetail` |
| `GET /admin/guilds` | `guilds.read` | `{items, total, page, pageSize}` (AdminGuildItem) |
| `GET /admin/audit` | `audit.read` | `{items: AuditEventItem[], nextCursor}` |

Verbatim SQL for the central ones is captured in the appendix (§9). `roles`
query joins `admin_roles LEFT JOIN admin_user_roles` for `memberCount`;
`permissions` resolves superadmin→all else `admin_role_permissions`. `users`
is a single big projection with correlated subqueries for guild count,
admin-role keys, entitlement keys, ban categories, and user-app-install flag
(see §9). `audit` is cursor-paginated `audit_events` UNION ALL legacy
`app_audit_events` (both tables exist in schema: `postgres.go:746` and `:1046`),
ordered `created_at DESC`, fetch `pageSize+1`, hydrate actor/target
display-name+avatar in a second query.

**Phase 3b — action dispatcher + core actions:**

Dispatcher responsibilities (port from `internal/admin/actions/dispatcher.go`):
resolve actor → rate-limit (per actor+action token bucket) → idempotency replay
(`Idempotency-Key` header) → permission check → decode `{params, reason,
typedConfirmation}` → reason enforcement → `Validate(params)` → dangerous typed
confirmation → redact sensitive params → `Execute` → write audit row → respond
`{ok: true, result}`.

Core actions to port first (role + user lifecycle, since they pair with Item 5
SSE events):

| Action | Perm | Reason | Dangerous | Notes |
|---|---|---|---|---|
| `roles.create` | `admins.grant` | yes | no | rejects `is_system` |
| `roles.update` | `admins.grant` | yes | no | rejects `is_system` |
| `roles.delete` | `admins.grant` | yes | no | rejects `is_system`; FK cascade |
| `roles.permissions.set` | `admins.grant` | yes | no | rejects `is_system`; → SSE `roles.changed` per affected user |
| `users.roles.grant` | `users.roles.assign` | yes | no | superadmin grant needs `admins.grant`; → SSE |
| `users.roles.revoke` | `users.roles.assign` | yes | no | → SSE |
| `users.suspend` | `users.suspend` | yes | no | bumps `session_epoch`; → SSE `session.revoked` |
| `users.unsuspend` | `users.suspend` | no | no | |
| `users.force_logout` | `users.security.reset` | no | no | bumps `session_epoch`; → SSE |
| `users.delete` | `users.delete` | yes | **yes** | soft-delete; typed confirm; → SSE |

**Phase 3c — deferred (propose out-of-scope for the identity milestone):**
`api_keys.*`, `proxies.*` (CRUD overlaps the outbound-proxy backend domain),
`bm-servers.*` CRUD (BattleMetrics domain), `user-installs`, `servers`,
`command-usage` sub-routes, `users.ban.set`, `users.edit`,
`users.connections.manage`. These belong to other service domains or are lower
value for "complete the auth surface." **User to confirm the cut line.**

### 5.4 Audit model (`audit_events`, `postgres.go:746`)

Columns: `id uuid` (gen_random_uuid), `action_type`, `actor_user_id`,
`actor_role_snapshot text[]`, `resource_type`, `resource_id`, `target_user_id`,
`params jsonb`, `reason`, `result` (`success|error`), `error_message`,
`ip_address inet`, `user_agent`, `via_api_key_id`, `via_api_key_name`,
`created_at`. Every dispatch writes one row (success or error), params redacted
per action's `SensitiveParamKeys()`. Read shape (`AuditEventItem`) maps
`action_type→eventType`, `params→payload`, plus hydrated actor/target names.

### 5.5 Verification

Per-route smoke against throwaway PG with seeded `admin_roles`,
`admin_role_permissions`, `admin_user_roles`, `admin_permissions`, `app_users`,
`user_entitlements`, `audit_events`. Assert JSON matches the TS page/detail
shapes. For the dispatcher: assert 403 without perm, 400 missing reason, audit
row written, and (paired with Item 5) the SSE event published.

---

## 6. ITEM 4 — Role-connection OAuth (`pro` linked roles)

> **✅ DONE — 4a (OAuth + storage + initial push + `/me` summary + schema
> registration) PR #172, 4b (reconciler + refresh-on-expiry + on-grant push hook)
> PR #173.** New `rustlens-crypto` crate (AES-256-GCM, byte-compatible with the
> Go proxycrypto). Both phases of §6.5 are complete.

### 6.1 Routes (`router.go:292-295, 345`)

- `GET /api/auth/discord/role-connection` — initiate; **requires session**
  (else redirect `/login?notice=session-required`); fresh RC-specific state +
  return-dest cookies (10 min); 302 to Discord authorize with `prompt=consent`.
- `GET /api/auth/discord/role-connection/callback` — validate state → exchange
  code for **access + refresh** tokens → confirm Discord profile id == session
  user's discord id → AES-GCM encrypt both tokens → upsert row → **synchronous**
  initial metadata push → redirect to dashboard `?notice=discord-linked`.
- `DELETE /api/app/me/discord-role-connection` — session-authed; best-effort PUT
  empty metadata (`is_pro:"0"`) → delete row → `204`.

### 6.2 OAuth specifics

- Scopes: `identify role_connections.write` (`oauth.go:62`).
- Redirect: `{app_origin}/api/auth/discord/role-connection/callback`.
- Separate cookies from the login flow (`CookieRCState`/`CookieRCVerifier`) to
  avoid collision with a concurrent re-login.
- Token exchange must request/keep the **refresh_token** (long-lived).

### 6.3 Storage & crypto (`discord_role_connections`, `postgres.go:937`)

```
user_id PK FK app_users(id) ON DELETE CASCADE, discord_id,
access_token_ct bytea, access_token_nonce bytea,
refresh_token_ct bytea, refresh_token_nonce bytea,
access_expires_at timestamptz, scopes text,
linked_at, last_pushed_at, last_pushed_hash, last_push_error, revoked_at
```

AES-256-GCM, key from **`PROXY_ENCRYPTION_KEY`** (base64 32 bytes, same as the
Go `proxycrypto.Crypto`: 12-byte random nonce, `Seal` with nil AAD). Rust:
new crate `rustlens-crypto` (or a module) using the `aes-gcm` crate; helper
`encrypt(plaintext) -> (ct, nonce)` / `decrypt(ct, nonce) -> String`.

The dashboard summary read by `/me` (Item 1's `discordRoleConnection` field) is
`DiscordRoleConnectionSummary` (`rustlens-api.ts:48`):
`{ linked, discordId|null, linkedAt|null, lastPushedAt|null, lastPushError|null, revoked }`.
Wire this into `load_current_user` once the store exists.

### 6.4 Metadata push & reconciler

- PUT `https://discord.com/api/users/@me/applications/{appId}/role-connection`
  with `{platform_name:"RustLens", platform_username:<display|"">, metadata:{...}}`.
  PRO ⇒ `{is_pro:"1", pro_since:<RFC3339 numeric offset>}`; non-PRO ⇒ `{is_pro:"0"}`.
- Fires: on initial link (sync), on `users.roles.grant`/`revoke` of `pro` (async,
  detached 30s), and a **6h reconciler** holding `pg_try_advisory_lock(7427198631011)`
  iterating non-revoked rows, 200ms apart, skipping pushes whose payload hash is
  unchanged within 24h, refreshing access tokens within 60s of expiry (marking
  `revoked_at` on `invalid_grant`).
- Startup schema registration: PUT
  `/api/applications/{appId}/role-connections/metadata` with the **bot token**
  (`DISCORD_BOT_TOKEN`), records `is_pro` (type 7) + `pro_since` (type 6).

### 6.5 Phasing / decision

Propose: **Phase 4a** = OAuth initiate/callback + storage + DELETE + `/me`
summary + synchronous initial push + startup schema registration. **Phase 4b** =
the async push-on-grant hook (couples to Item 3's grant/revoke actions) and the
6h reconciler background task. Confirm with user (§8.3) whether the reconciler
is in scope now.

---

## 7. ITEM 5 — `/api/events/stream` SSE + Redis bridge

> **✅ DONE — PR #170.** New `rustlens-events` crate (Hub + RedisBridge on
> `rustlens:user-events`). Cookie-authed SSE with the exact Go wire format, 25s
> ping, one-shot `session.expiring`; the publisher is consumed by the Item 3b
> dispatcher.

### 7.1 SSE handler (`events_handlers.go`)

- Cookie-authed via the same `ValidateSession` (epoch + suspended/deleted).
  401 JSON with code (`session_revoked|session_expired|session_required|invalid_session`).
- Headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache, no-transform`,
  `Connection: keep-alive`, `X-Accel-Buffering: no`.
- Wire: `event: <type>\ndata: <json>\n\n`.
- Events: `roles.changed` (reason grant/revoke/role_perms_updated),
  `session.revoked` (force_logout/suspended/deleted), `session.expiring`
  (one-shot when remaining TTL < 30 min; includes `expiresAt` ms), `ping`
  (heartbeat every **25s**).
- Event JSON: `{ "type": ..., "reason"?: ..., "expiresAt"?: <ms> }`.

axum supports SSE via `axum::response::sse::{Sse, Event}` + a stream; a
`tokio::time::interval` drives pings; a per-connection broadcast receiver drives
pushed events.

### 7.2 Hub + Redis bridge (`internal/events/`)

- In-process `Hub`: pub/sub keyed by `userId`, **5 conns/user** cap, 16-event
  buffer, drop-oldest. Rust: a `DashMap<String, Vec<broadcast::Sender>>` or a
  registry of `tokio::sync::broadcast`/`mpsc` channels per user.
- `RedisBridge`: Redis pub/sub channel **`rustlens:user-events`**. Envelope:
  `{ "userId": ..., "event": {type,reason?,expiresAt?}, "instanceId"?: ... }`.
  Publish delivers locally first, then to Redis; the subscriber goroutine
  ignores its own `instanceId` to avoid dup delivery, fans remote events into
  the local hub.
- Publishers: Item 3's actions call `PublishWithFanout(userId, event)`
  (grant/revoke → `roles.changed`; suspend/delete/force_logout →
  `session.revoked`; perms.set → `roles.changed` per affected user).

### 7.3 New crate

Propose `crates/rustlens-events`: the `Hub`, the Redis bridge (using the `redis`
crate or `fred`), the `Event`/envelope types, and a `Publisher` trait. The
identity service mounts the SSE route and injects the hub; the admin action
dispatcher (Item 3) depends on the `Publisher`. Needs `REDIS_URL` in config
(default `redis://localhost:6379/0`, mirror Go).

### 7.4 Verification

Smoke: two identity processes against the same throwaway Redis (separate
container, e.g. `:6380`) + PG; open SSE on process A with a valid cookie; trigger
a `users.suspend` action on process B; assert A receives `session.revoked` (Redis
fanout) and the connection is told to close; assert `ping` every ~25s.

---

## 8. Decisions (RESOLVED — owner deferred to implementer's judgment, 2026-06-30)

1. **Item 1 — POPULATE.** Rust `/me` populates `steamAccounts`/`guilds` from the
   real join SQL (richer than live Go's empty stub, still JSON-compatible).
2. **Item 3 — MVP cut.** Read routes (permissions, roles, users, administrators,
   user detail, guilds, audit) + dispatcher with the role/user-lifecycle actions
   in §5.3. Auth mirrors Go: outer `APP_API_INTERNAL_SECRET` gate on `/api/app/*`
   + session-derived actor; **admin API-key auth path deferred.** Deferred:
   api_keys/proxies/bm-servers/user-installs/command-usage/servers,
   users.edit/ban/connections.
3. **Item 4 — phased.** 4a now (OAuth + storage + DELETE + `/me` summary +
   initial sync push + startup schema registration); 4b (6h reconciler +
   push-on-grant hook) after Item 3.
4. **Architecture — single identity binary** hosts Items 3–5; new
   `crates/rustlens-events` for the Hub + Redis bridge.
5. **PRs — one per item** (Item 3 split 3a/3b), based on `rework/rust-backend`.

### Original open questions (kept for context)

1. **Item 1 — populate vs. mirror Go's empty arrays.** Live Go `/me` returns
   `steamAccounts: []` / `guilds: []`; the join SQL exists but isn't wired to
   `/me`. Default plan: **populate** (richer, still compatible) so Rust `/me` is
   complete and self-sufficient. OK?
2. **Item 3 — scope/cut line.** Propose MVP = read routes (roles, permissions,
   users, administrators, user detail, guilds, audit) + dispatcher with the
   role/user-lifecycle actions (§5.3). Defer api_keys/proxies/bm-servers/
   user-installs/command-usage/servers to their own domains. Also: do admin
   routes need the `APP_API_INTERNAL_SECRET` header + admin **API-key** auth, or
   is **session-only** acceptable for this milestone?
3. **Item 4 — reconciler now or later.** Phase 4a (OAuth + storage + initial
   push + DELETE + `/me` summary + schema registration) first; Phase 4b (6h
   reconciler + push-on-grant hook) after Item 3 lands. OK to defer 4b?
4. **Where do Items 3–5 live?** Assumption: all in the **identity** service
   binary (per migration memory: identity owns RBAC, role-connection, audit,
   and can host SSE). Confirm vs. splitting SSE/admin into separate binaries.
5. **PR granularity.** Proposed: one PR per item (Item 3 split into 3a/3b),
   each based on `rework/rust-backend`. OK?

## 8.6 Implementation order

> **✅ EXECUTED.** Actual merge order: 1 (#167) → 2 (#168) → 3a (#169) → 5 (#170)
> → 3b (#171) → 4a (#172) → 4b (#173) → `/me` header-auth alignment (#174). Item
> 5 landed before 3b so the dispatcher could publish events, as planned.

Original plan: 1 → 2 → 5(crate scaffolding) → 3a → 3b(+5 publishers) → 4. Item 1
ships first and standalone. Item 5's crate is scaffolded before 3b so the
dispatcher can publish events. Item 4 last.

---

## 9. Appendix — verbatim SQL for central admin reads

**`GET /admin/roles`** (`rbac/store.go`):
```sql
SELECT r.id, r.key, r.name, COALESCE(r.description, ''), r.is_system,
       COUNT(ur.user_id)::int AS member_count
FROM admin_roles r
LEFT JOIN admin_user_roles ur ON ur.role_id = r.id
GROUP BY r.id, r.key, r.name, r.description, r.is_system
ORDER BY r.key;
-- permissions: superadmin -> rbac.AllPermissions(); else:
SELECT permission_key FROM admin_role_permissions WHERE role_id = $1 ORDER BY permission_key;
```

**`GET /admin/users`** (`admin_read_handlers.go`, correlated-subquery projection):
```sql
SELECT u.id,
  COALESCE(NULLIF(u.display_name,''), NULLIF(u.discord_global_name,''), NULLIF(u.discord_username,''), '') AS display_name,
  COALESCE(NULLIF(u.avatar_url,''),
    CASE WHEN NULLIF(u.discord_avatar,'') IS NOT NULL AND NULLIF(u.discord_id,'') IS NOT NULL
         THEN 'https://cdn.discordapp.com/avatars/'||u.discord_id||'/'||u.discord_avatar||'.png' ELSE '' END) AS avatar_url,
  u.roles::text, u.created_at, u.updated_at, u.last_login_at, u.suspended_at, u.deleted_at,
  COALESCE(u.discord_id,''), COALESCE(u.discord_username,''), COALESCE(u.discord_global_name,''), COALESCE(u.discord_avatar,''),
  COALESCE((SELECT COUNT(*) FROM user_guilds g WHERE g.discord_id=u.discord_id AND g.left_at IS NULL),0),
  0 AS steam_count,
  COALESCE((SELECT array_agg(r.key ORDER BY r.key) FROM admin_user_roles aur JOIN admin_roles r ON r.id=aur.role_id WHERE aur.user_id=u.id), ARRAY[]::text[]),
  COALESCE((SELECT array_agg(ue.key ORDER BY ue.key) FROM user_entitlements ue WHERE ue.user_id=u.id AND (ue.expires_at IS NULL OR ue.expires_at>NOW())), ARRAY[]::text[]),
  COALESCE((SELECT ub.categories FROM user_bans ub WHERE ub.discord_id=u.discord_id), ARRAY[]::text[]),
  EXISTS (SELECT 1 FROM bot_user_installs bui WHERE bui.discord_id=u.discord_id AND bui.is_installed=TRUE)
FROM app_users u
WHERE <filters> AND <non-admins-only OR role-filter>
ORDER BY <sort> LIMIT $pageSize OFFSET $offset;
```
(Default with no `role` filter: only users with **zero** admin roles. `steam_count`
is hard-coded `0` in Go today.)

**`GET /admin/audit`** (`admin_read_handlers.go`):
```sql
SELECT id::text, COALESCE(actor_user_id::text,''), COALESCE(target_user_id::text,''),
       action_type AS event_type, COALESCE(params,'{}'::jsonb) AS payload,
       NULLIF(result,'') AS result, reason, created_at
FROM audit_events WHERE <clauses>
UNION ALL
SELECT id::text, COALESCE(actor_user_id,''), COALESCE(target_user_id,''),
       event_type, COALESCE(payload,'{}'::jsonb), NULL::text, NULL::text, created_at
FROM app_audit_events WHERE <clauses>
ORDER BY created_at DESC LIMIT $pageSize+1;
```
(`result` filter excludes the legacy table. `before=RFC3339` cursor;
`nextCursor` = last row's `created_at` when overflow.)
