# Admin Users — Plan 2: Detail Page & Safe Edits

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox syntax for tracking.

**Goal:** Ship `/admin/users/[id]` with Overview, Roles, Emails (read-only), Connections (read-only), and Audit tabs. Add three write actions — `users.edit`, `users.roles.grant`, `users.roles.revoke` — through the existing admin action dispatcher. Add detail and audit read endpoints.

**Architecture:** New detail endpoint returns a superset DTO. Audit endpoint merges `audit_events ∪ app_audit_events`. Mutations register as `AdminAction` implementations in `apps/backend/internal/admin/actions/` and are invoked from the frontend via the existing `AdminActionButton` client component. Pages are RSC; `router.refresh()` after mutation success.

**Tech Stack:** Go, chi, pgx · Next.js 16 (async params), shadcn/ui · Vitest.

**Prerequisite:** Plan 1 merged (schema + RBAC seed + filter list). Branch `development/web-administration`.

---

## File Structure

**Backend**
- Modify: `apps/backend/internal/http/admin_read_handlers.go` — add `GetAdminUser`, `ListAdminUserAudit`
- Modify: `apps/backend/internal/http/router.go` — mount new routes
- Create: `apps/backend/internal/admin/actions/users_edit.go`
- Create: `apps/backend/internal/admin/actions/users_roles_grant.go`
- Create: `apps/backend/internal/admin/actions/users_roles_revoke.go`
- Modify: `apps/backend/internal/admin/actions/registry.go` (or wherever actions are registered at boot) — register the three new actions
- Create: `apps/backend/internal/admin/actions/users_edit_test.go`
- Create: `apps/backend/internal/admin/actions/users_roles_test.go`
- Create: `apps/backend/internal/http/admin_user_detail_test.go`

**Frontend**
- Modify: `apps/frontend/lib/rustlens-api.ts` — add DTOs + `getAdminUser`, `getAdminUserAudit`
- Create: `apps/frontend/app/admin/users/[userId]/layout.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/page.tsx` (Overview)
- Create: `apps/frontend/app/admin/users/[userId]/roles/page.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/emails/page.tsx` (read-only)
- Create: `apps/frontend/app/admin/users/[userId]/connections/page.tsx` (read-only)
- Create: `apps/frontend/app/admin/users/[userId]/audit/page.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/edit-identity-form.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/role-grant-controls.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/user-detail-header.tsx`

---

## Task 1: Backend detail endpoint — `GET /api/app/admin/users/{id}`

**Files:**
- Modify: `apps/backend/internal/http/admin_read_handlers.go`
- Modify: `apps/backend/internal/http/router.go`

- [ ] **Step 1:** Add to `router.go` inside the `/admin` group: `r.Get("/users/{id}", handler.GetAdminUser)` and `r.Get("/users/{id}/audit", handler.ListAdminUserAudit)`.

- [ ] **Step 2:** Define the detail DTO and handler in `admin_read_handlers.go`:

```go
type adminUserDetail struct {
	User        adminAppUser          `json:"user"`
	PrimaryEmail string                `json:"primaryEmail"`
	Emails      []adminUserEmail      `json:"emails"`
	Connections []adminUserConnection `json:"connections"`
	Guilds      []adminUserGuild      `json:"guilds"`
	AdminRoles  []string              `json:"adminRoles"`
	SuspendedAt *string               `json:"suspendedAt,omitempty"`
	SuspendReason string              `json:"suspendReason,omitempty"`
	DeletedAt   *string               `json:"deletedAt,omitempty"`
	RecentAudit []auditEventItem      `json:"recentAudit"`
}

type adminUserEmail struct {
	Email      string `json:"email"`
	IsPrimary  bool   `json:"isPrimary"`
	IsVerified bool   `json:"isVerified"`
}

type adminUserConnection struct {
	Provider          string `json:"provider"`
	ProviderAccountID string `json:"providerAccountId"`
	Username          string `json:"username"`
	ConnectedAt       string `json:"connectedAt"`
}

type adminUserGuild struct {
	GuildID string `json:"guildId"`
	Nick    string `json:"nick,omitempty"`
}

// GetAdminUser: GET /api/app/admin/users/{id}
func (h *Handler) GetAdminUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" { writeError(w, http.StatusBadRequest, "missing id"); return }
	pool := h.postgres.Pool()

	var d adminUserDetail
	var suspendedAt, deletedAt *time.Time
	var rolesJSON string
	err := pool.QueryRow(ctx, `
		SELECT id, display_name, avatar_url, roles::text,
		       created_at, updated_at, last_login_at,
		       suspended_at, COALESCE(suspend_reason,''), deleted_at
		FROM app_users WHERE id = $1
	`, id).Scan(
		&d.User.ID, &d.User.DisplayName, &d.User.AvatarURL, &rolesJSON,
		&d.User.CreatedAt, &d.User.UpdatedAt, &d.User.LastLoginAt,
		&suspendedAt, &d.SuspendReason, &deletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) { writeError(w, http.StatusNotFound, "not found"); return }
		writeError(w, http.StatusInternalServerError, err.Error()); return
	}
	if rolesJSON != "" { _ = json.Unmarshal([]byte(rolesJSON), &d.User.Roles) }
	if d.User.Roles == nil { d.User.Roles = []string{} }
	if suspendedAt != nil { s := suspendedAt.Format(time.RFC3339); d.SuspendedAt = &s }
	if deletedAt != nil   { s := deletedAt.Format(time.RFC3339);   d.DeletedAt   = &s }

	// Emails
	erows, err := pool.Query(ctx, `
		SELECT email, is_primary, is_verified FROM user_emails WHERE user_id = $1 ORDER BY is_primary DESC, email ASC
	`, id)
	if err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
	defer erows.Close()
	for erows.Next() {
		var e adminUserEmail
		if err := erows.Scan(&e.Email, &e.IsPrimary, &e.IsVerified); err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
		if e.IsPrimary { d.PrimaryEmail = e.Email }
		d.Emails = append(d.Emails, e)
	}

	// Connections
	crows, err := pool.Query(ctx, `
		SELECT provider, provider_account_id, COALESCE(username,''), created_at
		FROM user_connected_accounts WHERE user_id = $1 ORDER BY provider, created_at DESC
	`, id)
	if err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
	defer crows.Close()
	for crows.Next() {
		var c adminUserConnection
		var ca time.Time
		if err := crows.Scan(&c.Provider, &c.ProviderAccountID, &c.Username, &ca); err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
		c.ConnectedAt = ca.Format(time.RFC3339)
		d.Connections = append(d.Connections, c)
	}

	// Admin roles
	rrows, err := pool.Query(ctx, `
		SELECT r.key FROM admin_roles r
		JOIN admin_user_roles aur ON aur.role_id = r.id
		WHERE aur.user_id = $1 ORDER BY r.key
	`, id)
	if err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
	defer rrows.Close()
	for rrows.Next() { var k string; if err := rrows.Scan(&k); err == nil { d.AdminRoles = append(d.AdminRoles, k) } }
	if d.AdminRoles == nil { d.AdminRoles = []string{} }

	// Guilds via discord connection
	var discordID string
	for _, c := range d.Connections { if c.Provider == "discord" { discordID = c.ProviderAccountID; break } }
	if discordID != "" {
		grows, err := pool.Query(ctx, `SELECT guild_id, COALESCE(nickname,'') FROM user_guilds WHERE discord_id = $1 AND left_at IS NULL ORDER BY guild_id`, discordID)
		if err == nil {
			defer grows.Close()
			for grows.Next() {
				var g adminUserGuild
				if err := grows.Scan(&g.GuildID, &g.Nick); err == nil { d.Guilds = append(d.Guilds, g) }
			}
		}
	}

	// Recent audit (5 rows) — reuse ListAdminUserAudit's query with LIMIT 5
	d.RecentAudit = fetchUserAudit(ctx, pool, id, 5, "")
	writeJSON(w, http.StatusOK, d)
}
```

- [ ] **Step 3:** Add `ListAdminUserAudit` handler:

```go
// ListAdminUserAudit: GET /api/app/admin/users/{id}/audit?limit=&before=
func (h *Handler) ListAdminUserAudit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" { writeError(w, http.StatusBadRequest, "missing id"); return }
	limit := 50; if v := r.URL.Query().Get("limit"); v != "" { parseLimit(v, &limit) }
	if limit <= 0 || limit > 500 { limit = 50 }
	before := r.URL.Query().Get("before")
	items := fetchUserAudit(ctx, h.postgres.Pool(), id, limit, before)
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// fetchUserAudit merges audit_events ∪ app_audit_events for a given target user,
// ordered by created_at desc, optional before cursor (RFC3339).
func fetchUserAudit(ctx context.Context, pool *pgxpool.Pool, userID string, limit int, before string) []auditEventItem {
	args := []any{userID}
	cursor := ""
	if before != "" {
		if t, err := time.Parse(time.RFC3339, before); err == nil {
			args = append(args, t); cursor = fmt.Sprintf(" AND created_at < $%d", len(args))
		}
	}
	args = append(args, limit)
	// UNION the two tables. audit_events.target_user_id may be TEXT after Plan 1 task 8;
	// if it is still UUID, the ::text cast makes the comparison work either way.
	q := fmt.Sprintf(`
		SELECT id::text, COALESCE(actor_user_id::text,''), COALESCE(target_user_id::text,''),
		       action_type, COALESCE(params,'{}'::jsonb), created_at
		FROM audit_events
		WHERE target_user_id::text = $1 %[1]s
		UNION ALL
		SELECT id::text, COALESCE(actor_user_id,''), COALESCE(target_user_id,''),
		       event_type, COALESCE(payload,'{}'::jsonb), created_at
		FROM app_audit_events
		WHERE target_user_id = $1 %[1]s
		ORDER BY created_at DESC
		LIMIT $%d
	`, cursor, len(args))
	rows, err := pool.Query(ctx, q, args...)
	if err != nil { return []auditEventItem{} }
	defer rows.Close()
	out := []auditEventItem{}
	for rows.Next() {
		var it auditEventItem
		var idStr string
		var payload []byte
		if err := rows.Scan(&idStr, &it.ActorUserID, &it.TargetUserID, &it.EventType, &payload, &it.CreatedAt); err != nil { continue }
		// Legacy auditEventItem.ID is int64; keep that but we fill a 0 for UUID ids; or add a StringID field.
		// Simplest: extend auditEventItem with a string ID field in parallel.
		it.Payload = map[string]any{}
		if len(payload) > 0 { _ = json.Unmarshal(payload, &it.Payload) }
		out = append(out, it)
	}
	return out
}
```

Note: if `auditEventItem.ID` is currently `int64` (per the existing code), add a `StringID string` field alongside so UUID ids from `audit_events` have a place to live. Or, simpler: change `ID` to `string` and update the one existing call site (`ListAdminAudit`) to cast `id::text`.

- [ ] **Step 4:** `go build ./...`. Commit: `feat(backend): admin user detail and per-user audit endpoints`.

---

## Task 2: Test — detail endpoint

**Files:**
- Create: `apps/backend/internal/http/admin_user_detail_test.go`

- [ ] Write tests that seed a user with emails, connections, roles, and audit rows, then GET the detail and assert shape. Also test 404 for unknown id.
- [ ] Run, verify pass, commit: `test(backend): GET /api/app/admin/users/{id}`.

---

## Task 3: Action — `users.edit`

**Files:**
- Create: `apps/backend/internal/admin/actions/users_edit.go`

- [ ] **Step 1:** Study `apps/backend/internal/admin/actions/action.go` to see the `AdminAction` interface. Study any existing action as a template.

- [ ] **Step 2:** Implement:

```go
package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rustlens/rustlens/internal/admin/rbac"
)

type UsersEditParams struct {
	UserID      string  `json:"userId"`
	DisplayName *string `json:"displayName,omitempty"`
	AvatarURL   *string `json:"avatarUrl,omitempty"`
}

type usersEditAction struct { pool *pgxpool.Pool }

func NewUsersEditAction(pool *pgxpool.Pool) AdminAction { return &usersEditAction{pool: pool} }

func (a *usersEditAction) Type() string             { return "users.edit" }
func (a *usersEditAction) Permission() string       { return rbac.PermUsersEdit }
func (a *usersEditAction) RequiresReason() bool     { return false }
func (a *usersEditAction) Dangerous() bool          { return false }
func (a *usersEditAction) SensitiveParamKeys() []string { return nil }
func (a *usersEditAction) ResourceType() string     { return "user" }

func (a *usersEditAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersEditParams
	if err := json.Unmarshal(raw, &p); err != nil { return ValidatedParams{}, fmt.Errorf("validation: %w", err) }
	if p.UserID == "" { return ValidatedParams{}, errors.New("userId is required") }
	if p.DisplayName == nil && p.AvatarURL == nil { return ValidatedParams{}, errors.New("nothing to update") }
	if p.DisplayName != nil && len(*p.DisplayName) > 80 { return ValidatedParams{}, errors.New("displayName too long") }
	if p.AvatarURL != nil && len(*p.AvatarURL) > 512 { return ValidatedParams{}, errors.New("avatarUrl too long") }
	return ValidatedParams{ResourceID: p.UserID, Params: p}, nil
}

func (a *usersEditAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	p := in.Params.(UsersEditParams)
	// Guard: not deleted
	var deletedAt *string
	if err := a.pool.QueryRow(ctx, `SELECT deleted_at::text FROM app_users WHERE id=$1`, p.UserID).Scan(&deletedAt); err != nil {
		return ActionResult{}, err
	}
	if deletedAt != nil { return ActionResult{}, ErrConflict("user is deleted") }

	tx, err := a.pool.Begin(ctx); if err != nil { return ActionResult{}, err }
	defer tx.Rollback(ctx)
	if p.DisplayName != nil {
		if _, err := tx.Exec(ctx, `UPDATE app_users SET display_name=$2, updated_at=now() WHERE id=$1`, p.UserID, *p.DisplayName); err != nil { return ActionResult{}, err }
	}
	if p.AvatarURL != nil {
		if _, err := tx.Exec(ctx, `UPDATE app_users SET avatar_url=$2, updated_at=now() WHERE id=$1`, p.UserID, *p.AvatarURL); err != nil { return ActionResult{}, err }
	}
	if err := tx.Commit(ctx); err != nil { return ActionResult{}, err }
	return ActionResult{Output: map[string]any{"userId": p.UserID}}, nil
}
```

The helpers `ValidatedParams`, `ActionInput`, `ActionResult`, and `ErrConflict` are presumed to exist in the `actions` package per the pattern from the exploration. If a helper with a different name exists, use that. Register this action wherever the other actions are registered (grep for `registry.Register` to find the spot; add `registry.Register(NewUsersEditAction(pool))`).

- [ ] **Step 3:** Add the new action to the action registry registration point.

- [ ] **Step 4:** Commit: `feat(backend): add users.edit admin action`.

---

## Task 4: Tests — `users.edit`

**Files:**
- Create: `apps/backend/internal/admin/actions/users_edit_test.go`

- [ ] Test happy path: dispatch with valid params, assert DB row updated and one `audit_events` row with `action_type='users.edit'`.
- [ ] Test validation: empty userId → 400, no fields → 400, overlong name → 400.
- [ ] Test conflict: acting on a soft-deleted user → `ErrConflict`.
- [ ] Test permission: actor without `users.edit` → 403.
- [ ] Commit: `test(backend): users.edit action`.

---

## Task 5: Actions — `users.roles.grant` and `users.roles.revoke`

**Files:**
- Create: `apps/backend/internal/admin/actions/users_roles_grant.go`
- Create: `apps/backend/internal/admin/actions/users_roles_revoke.go`

- [ ] **Step 1:** Shared helper — look up the role id by key, then INSERT/DELETE in `admin_user_roles`. Both actions:
  - Type: `users.roles.grant` / `users.roles.revoke`
  - Permission: `users.roles.assign` normally; **special case**: if `roleKey == "superadmin"`, require `admins.grant` in addition. Enforce this in `Validate()` by inspecting actor roles if the action interface exposes them, or in `Execute()` by re-checking via the permission resolver. If neither is clean, add a `RequiresSuperadminForParams(p) bool` convention in your new actions and have the dispatcher call it pre-execute. Simpler: in `Validate`, if role key is superadmin, return a sentinel error `ErrRequiresSuperadminGrant` which the dispatcher already maps to a 403 if the actor lacks `admins.grant`. If this extension isn't available, inline-check inside `Execute` using `h.rbacResolver.HasPermission`.

- [ ] **Step 2:** Grant action:

```go
func (a *usersRolesGrantAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	p := in.Params.(UsersRoleAssignParams)
	// Find role id by key
	var roleID string
	if err := a.pool.QueryRow(ctx, `SELECT id FROM admin_roles WHERE key=$1`, p.RoleKey).Scan(&roleID); err != nil {
		return ActionResult{}, ErrNotFound("role not found")
	}
	// Superadmin extra permission
	if p.RoleKey == "superadmin" {
		ok, _ := a.resolver.HasPermission(ctx, in.ActorID, "admins.grant")
		if !ok { return ActionResult{}, ErrPermissionDenied("grant of superadmin requires admins.grant") }
	}
	_, err := a.pool.Exec(ctx, `
		INSERT INTO admin_user_roles (user_id, role_id, granted_by)
		VALUES ($1,$2,$3) ON CONFLICT DO NOTHING
	`, p.UserID, roleID, in.ActorID)
	if err != nil { return ActionResult{}, err }
	return ActionResult{Output: map[string]any{"userId": p.UserID, "roleKey": p.RoleKey}}, nil
}
```

- [ ] **Step 3:** Revoke action — same shape, `DELETE FROM admin_user_roles WHERE user_id=$1 AND role_id=$2`. If deleting superadmin, also guard **last-superadmin protection**:

```go
if p.RoleKey == "superadmin" {
	var count int
	a.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_user_roles aur JOIN admin_roles r ON r.id=aur.role_id WHERE r.key='superadmin'`).Scan(&count)
	if count <= 1 { return ActionResult{}, ErrConflict("cannot revoke the last superadmin") }
}
```

- [ ] **Step 4:** Register both actions. Commit: `feat(backend): users.roles.grant/revoke admin actions`.

---

## Task 6: Tests — role grant/revoke

**Files:**
- Create: `apps/backend/internal/admin/actions/users_roles_test.go`

- [ ] Happy-path grant; idempotent (re-grant is no-op, still emits audit).
- [ ] Happy-path revoke.
- [ ] Grant superadmin without `admins.grant` → 403.
- [ ] Revoke superadmin when only one remains → 409 conflict.
- [ ] Unknown role key → 404.
- [ ] Commit: `test(backend): users.roles.grant/revoke`.

---

## Task 7: Frontend — detail DTO + API client

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] Add types `AdminUserDetail`, `AdminUserEmail`, `AdminUserConnection`, `AdminUserGuild`, `AdminUserAuditItem`.
- [ ] Add `getAdminUser(userId: string): Promise<AdminUserDetail>` and `getAdminUserAudit(userId: string, opts?: { limit?: number; before?: string }): Promise<{ items: AdminUserAuditItem[] }>`.
- [ ] Typecheck. Commit: `feat(frontend): admin user detail and audit API clients`.

---

## Task 8: Frontend — detail layout with tabs

**Files:**
- Create: `apps/frontend/app/admin/users/[userId]/layout.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/user-detail-header.tsx`

- [ ] Layout is RSC; awaits `params` (Next.js 16 async params); fetches user detail once; renders `<UserDetailHeader>` and a tab nav linking to `/admin/users/[id]/{overview,roles,emails,connections,audit}`. Use shadcn tabs-like pattern via Links (not client-only Tabs) so deep-linking works. Example:

```tsx
export default async function Layout({ children, params }: { children: React.ReactNode; params: Promise<{ userId: string }> }) {
	await requirePermission("users.read");
	const { userId } = await params;
	const user = await getAdminUser(userId);
	return (
		<div>
			<UserDetailHeader user={user} />
			<nav className="border-b mb-4 flex gap-4 text-sm">
				<TabLink href={`/admin/users/${userId}`}>Overview</TabLink>
				<TabLink href={`/admin/users/${userId}/roles`}>Roles</TabLink>
				<TabLink href={`/admin/users/${userId}/emails`}>Emails</TabLink>
				<TabLink href={`/admin/users/${userId}/connections`}>Connections</TabLink>
				<TabLink href={`/admin/users/${userId}/audit`}>Audit</TabLink>
			</nav>
			{children}
		</div>
	);
}
```

`TabLink` is a tiny client component that reads `usePathname` and applies an active class.

- [ ] Commit: `feat(frontend): user detail layout with tab nav`.

---

## Task 9: Frontend — Overview tab with edit form

**Files:**
- Create: `apps/frontend/app/admin/users/[userId]/page.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/edit-identity-form.tsx`

- [ ] Overview shows: identity card (avatar, id copy, created/updated/last login), inline `<EditIdentityForm>` for displayName + avatarUrl (gated on `users.edit` perm in session), primary email, counts (guilds, steam), recent-5 audit list.

- [ ] `EditIdentityForm` is a client component that reuses the existing `<AdminActionButton>` pattern. If `AdminActionButton` only handles single-click actions, create a minimal form that POSTs to `/api/app/admin/actions/users.edit` with `{userId, displayName, avatarUrl}` + CSRF token pulled from the page layout (the admin layout already makes CSRF available — follow existing pattern).

- [ ] Commit: `feat(frontend): user overview with edit identity form`.

---

## Task 10: Frontend — Roles tab

**Files:**
- Create: `apps/frontend/app/admin/users/[userId]/roles/page.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/role-grant-controls.tsx`

- [ ] Fetch detail + all roles (`getAdminRoles()`); render two columns:
  - **Current roles** — chip list; each has a revoke button (`<AdminActionButton action="users.roles.revoke" params={{userId, roleKey}} />`).
  - **Available roles** — any role not already granted; each has a grant button (`<AdminActionButton action="users.roles.grant" params={{userId, roleKey}} />`).
- [ ] Superadmin rows are visually distinct (red badge) and the grant/revoke buttons for them require the session to have `admins.grant` (hide if absent).
- [ ] Commit: `feat(frontend): user roles tab with grant/revoke`.

---

## Task 11: Frontend — Emails, Connections, Audit tabs (read-only)

**Files:**
- Create: `apps/frontend/app/admin/users/[userId]/emails/page.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/connections/page.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/audit/page.tsx`

- [ ] Emails: table (email / primary / verified). No actions in this plan (Plan 4 adds them).
- [ ] Connections: Discord card + Steam card(s) + guild list. Read-only.
- [ ] Audit: paginated merged timeline. Load 50 rows; "Load more" button appends by calling `getAdminUserAudit(userId, { before: lastCreatedAt })`. Guarded by `users.audit.read`.
- [ ] Commit: `feat(frontend): user emails/connections/audit read-only tabs`.

---

## Task 12: Frontend — tests

**Files:**
- Create: `apps/frontend/app/admin/users/[userId]/_components/__tests__/edit-identity-form.test.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/__tests__/role-grant-controls.test.tsx`

- [ ] Edit form: rendering, field validation (trims, length cap), form submit posts to the dispatcher with correct payload + CSRF header.
- [ ] Role controls: hides superadmin controls when session lacks `admins.grant`; clicking a grant/revoke button invokes the expected POST.
- [ ] Commit: `test(frontend): edit identity form and role controls`.

---

## Done when

- [ ] `go test ./...` green.
- [ ] `pnpm --filter frontend test && pnpm --filter frontend typecheck` green.
- [ ] Visiting `/admin/users/<id>` shows full detail; Overview edit form saves; Roles tab grants and revokes; last-superadmin revoke is blocked; audit tab paginates.
