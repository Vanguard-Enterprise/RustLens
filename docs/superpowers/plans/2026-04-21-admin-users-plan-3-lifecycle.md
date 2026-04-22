# Admin Users — Plan 3: Account Lifecycle

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans.

**Goal:** Add account lifecycle actions — `users.suspend`, `users.unsuspend`, `users.delete`, `users.restore`, `users.purge`, `users.password.forceReset` — and a Security tab in the detail page. Wire session revocation via the `session_epoch` mechanism added in Plan 1. Enforce self-action and last-superadmin guards.

**Architecture:** Session validation rejects any signed session whose embedded `epoch` is less than the current `app_users.session_epoch`. Suspend and delete increment the epoch, transactionally with the status column write. Purge is superadmin-only, bypasses the permission key, and requires a `confirmId == userId` echo. Force-password-reset generates a one-time token and returns the URL; UI shows it in the existing one-time-token modal pattern (to be created here).

**Prerequisite:** Plan 2 merged. Branch `development/web-administration`.

---

## File Structure

**Backend**
- Modify: `apps/backend/internal/appauth/session.go` — embed and validate `epoch` in session tokens; reject stale.
- Create: `apps/backend/internal/users/sessions.go` — small helper: `RevokeUserSessions(ctx, pool, userID)` bumps `session_epoch` atomically.
- Create: `apps/backend/internal/admin/actions/users_suspend.go`
- Create: `apps/backend/internal/admin/actions/users_unsuspend.go`
- Create: `apps/backend/internal/admin/actions/users_delete.go`
- Create: `apps/backend/internal/admin/actions/users_restore.go`
- Create: `apps/backend/internal/admin/actions/users_purge.go`
- Create: `apps/backend/internal/admin/actions/users_password_force_reset.go`
- Modify: registration site (same file that now registers `users.edit` etc.)
- Test: one `*_test.go` per action.
- Modify: `apps/backend/internal/admin/actions/guards.go` (new) — helpers `errActingOnSelf`, `errLastSuperadmin`, `guardActingOnSelf(actorID, targetID)`, `guardLastSuperadmin(ctx, pool)` usable across actions.

**Frontend**
- Modify: `apps/frontend/lib/rustlens-api.ts` — add DTOs for action outputs that return `tokenUrl`.
- Create: `apps/frontend/components/admin/one-time-token-modal.tsx` — reusable modal.
- Create: `apps/frontend/app/admin/users/[userId]/security/page.tsx` — Security tab.
- Create: `apps/frontend/app/admin/users/[userId]/_components/suspend-panel.tsx` — reason textarea + confirm.
- Create: `apps/frontend/app/admin/users/[userId]/_components/delete-restore-panel.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/purge-panel.tsx` — requires typed user id.
- Create: `apps/frontend/app/admin/users/[userId]/_components/force-reset-button.tsx` — on success opens token modal.

---

## Task 1: Session epoch — sign and validate

**Files:**
- Modify: `apps/backend/internal/appauth/session.go`

- [ ] **Step 1:** Add an `Epoch int` field to `SessionUser`. Include it in the signed payload (signature covers epoch, so it can't be forged). On login, read `SELECT session_epoch FROM app_users WHERE id = $1` and embed in the token.

- [ ] **Step 2:** On `Validate(token)`:
  - Parse + verify signature as today.
  - Look up current `session_epoch` for the user; if `token.epoch < current`, return `ErrSessionRevoked` which the middleware maps to 401 (forces re-login). Cache the lookup in a tiny in-memory LRU (~1000 users, 30s TTL) if the fetch is in the hot path and the Postgres round-trip matters; otherwise just do the lookup per request — correctness first.

- [ ] **Step 3:** Extend `appauth_test.go` (create if needed) to test: fresh login works; after bumping `session_epoch`, the old token is rejected.

- [ ] **Step 4:** Commit: `feat(backend): session_epoch revocation on signed sessions`.

---

## Task 2: `RevokeUserSessions` helper

**Files:**
- Create: `apps/backend/internal/users/sessions.go`

- [ ] Implement a tiny package-level helper:

```go
package users

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RevokeUserSessions increments the user's session_epoch, invalidating all
// previously-signed sessions for this user. Safe to call inside a transaction
// if pgxpool is substituted with tx via a narrow interface.
func RevokeUserSessions(ctx context.Context, db interface{ Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) }, userID string) error {
	_, err := db.Exec(ctx, `UPDATE app_users SET session_epoch = session_epoch + 1, updated_at = now() WHERE id = $1`, userID)
	return err
}
```

(Adjust the `db` type to whatever narrow interface matches both `*pgxpool.Pool` and `pgx.Tx` in this codebase — check what the existing code uses.)

- [ ] Commit: `feat(backend): RevokeUserSessions bumps session_epoch`.

---

## Task 3: Shared guards

**Files:**
- Create: `apps/backend/internal/admin/actions/guards.go`

- [ ] Helpers:

```go
package actions

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

func GuardActingOnSelf(actorID, targetID string) error {
	if actorID != "" && actorID == targetID { return ErrConflict("cannot perform this action on your own account") }
	return nil
}

// GuardLastSuperadmin returns ErrConflict if removing/disabling the target would
// leave zero users with the 'superadmin' role. Pass the predicate appropriate
// to the action: isRevokingSuperadmin=true only if this action is actually
// removing superadmin; otherwise pass false (only relevant for suspend/delete
// of a superadmin-holding user).
func GuardLastSuperadminByUser(ctx context.Context, pool *pgxpool.Pool, userID string) error {
	var hasSuperadmin bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM admin_user_roles aur JOIN admin_roles r ON r.id=aur.role_id WHERE aur.user_id=$1 AND r.key='superadmin')
	`, userID).Scan(&hasSuperadmin); err != nil { return err }
	if !hasSuperadmin { return nil }
	var total int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT aur.user_id) FROM admin_user_roles aur JOIN admin_roles r ON r.id=aur.role_id WHERE r.key='superadmin'
	`).Scan(&total); err != nil { return err }
	if total <= 1 { return ErrConflict("cannot perform this action on the last superadmin") }
	return nil
}
```

- [ ] Commit: `feat(backend): shared guards for self-action and last-superadmin`.

---

## Task 4: Actions — `users.suspend` / `users.unsuspend`

**Files:**
- Create: `apps/backend/internal/admin/actions/users_suspend.go`
- Create: `apps/backend/internal/admin/actions/users_unsuspend.go`

**Suspend:**
- Type `users.suspend`; perm `users.suspend`; `RequiresReason` can be `false` (reason is an optional param, not a hard requirement) or `true` if the existing `RequiresReason` convention implies a reason prompt — check how `AdminActionButton` handles `RequiresReason` and match the intent; if unsure, make it `true` (matches the "suspend wants a reason" UX). Dangerous: `true`.
- Validate: `{userId, reason?}`. reason ≤ 500 chars.
- Execute:
  - `GuardActingOnSelf(actor, userID)`.
  - `GuardLastSuperadminByUser(ctx, pool, userID)`.
  - In a transaction: `UPDATE app_users SET suspended_at = now(), suspended_by = $actor, suspend_reason = $reason WHERE id = $1 AND suspended_at IS NULL RETURNING id` — if 0 rows, 409.
  - Same tx: `UPDATE app_users SET session_epoch = session_epoch + 1 WHERE id = $1`.
  - Commit; return `{userId}`.

**Unsuspend:**
- Perm `users.suspend`. Dangerous: `false`.
- `UPDATE ... SET suspended_at=NULL, suspended_by=NULL, suspend_reason=NULL WHERE id=$1 AND suspended_at IS NOT NULL`.
- No epoch bump needed (sessions are already gone from when it was suspended; if a user is unsuspended, they log in fresh).

- [ ] Implement both, register, commit per action.

---

## Task 5: Tests — suspend/unsuspend

- [ ] Happy path: suspend sets columns + bumps epoch; subsequent session validation with old epoch fails.
- [ ] Self-suspend → 409.
- [ ] Last-superadmin suspend → 409.
- [ ] Suspend already-suspended user → 409 (idempotent-fail).
- [ ] Unsuspend clears columns.
- [ ] Commit: `test(backend): users.suspend/unsuspend`.

---

## Task 6: Actions — `users.delete` / `users.restore`

**Files:**
- Create: `apps/backend/internal/admin/actions/users_delete.go`
- Create: `apps/backend/internal/admin/actions/users_restore.go`

**Delete (soft):**
- Perm `users.delete`. Dangerous: `true`. Reason optional.
- Guards: self, last-superadmin.
- Tx: `UPDATE app_users SET deleted_at=now(), deleted_by=$actor WHERE id=$1 AND deleted_at IS NULL`; same tx bump `session_epoch`.

**Restore:**
- Perm `users.delete`. Dangerous: `false`.
- `UPDATE app_users SET deleted_at=NULL, deleted_by=NULL WHERE id=$1 AND deleted_at IS NOT NULL`.

- [ ] Tests: happy paths, self-delete guard, last-super guard, restore path.
- [ ] Commit per action + tests commit.

---

## Task 7: Action — `users.purge` (hard delete, superadmin only)

**Files:**
- Create: `apps/backend/internal/admin/actions/users_purge.go`

- [ ] Dispatcher pattern: the `Permission()` method returns empty string or a sentinel like `"_superadmin_only"`. If the existing dispatcher doesn't support superadmin-bypass natively, add a method `AdminAction.SuperadminOnly() bool` default false; purge returns true; dispatcher checks `actorHasRole("superadmin")` when true and skips the permission-key check.

- [ ] Validate: `{userId, confirmId}`. Must equal. Must have `userId` exist.

- [ ] Execute (before delete write audit row so it has a target):
  1. `GuardActingOnSelf`.
  2. `GuardLastSuperadminByUser`.
  3. Write the audit row manually (or rely on the dispatcher's pre-audit if it happens before Execute; verify). If not, write via `h.auditStore.Write(...)` with `action_type='users.purged'` and the user's current identity snapshot in `params`.
  4. `DELETE FROM app_users WHERE id=$1` — FK cascades handle `user_emails`, `user_password_credentials`, `user_connected_accounts`, `admin_user_roles`. Verify these tables have `ON DELETE CASCADE`; if not, delete them first.

- [ ] Tests: happy purge (row gone + FK children gone), bad `confirmId` → 400, self-purge → 409, last-super-purge → 409, non-superadmin caller → 403.
- [ ] Commit: `feat(backend): users.purge admin action (superadmin only)`.

---

## Task 8: Action — `users.password.forceReset`

**Files:**
- Create: `apps/backend/internal/admin/actions/users_password_force_reset.go`

- [ ] Perm `users.security.reset`. Dangerous: `true`.
- [ ] Validate: `{userId}`.
- [ ] Execute:
  1. Verify user exists + not deleted.
  2. Invalidate any outstanding unused `password_reset_tokens` for this user: `UPDATE password_reset_tokens SET consumed_at=now() WHERE user_id=$1 AND consumed_at IS NULL AND expires_at > now()`.
  3. Generate a new token: 32 bytes of `crypto/rand`, base64url-encode. Hash (sha256) and store the hash in `password_reset_tokens(user_id, token_hash, expires_at, created_at)`. Expiry 1h.
  4. Construct the URL: `{APP_BASE_URL}/reset-password?token={rawToken}`. Read `APP_BASE_URL` from config; fail the action if unset.
  5. Return `{tokenUrl}` in the action output.

- [ ] **Important:** the dispatcher's audit redactor must mark `SensitiveParamKeys: nil` (there are none to redact) and the **action output** containing the `tokenUrl` must NOT be logged into `audit_events.params`. The existing dispatcher writes `params` but not `output` — verify. If `output` is logged anywhere, redact `tokenUrl` or strip it from the audit payload explicitly.

- [ ] Tests: happy path returns a URL with a valid-looking token; DB row exists with a hash (not the raw token); a second call invalidates the first; actor without perm → 403.
- [ ] Commit: `feat(backend): users.password.forceReset with one-time token`.

---

## Task 9: Frontend — one-time-token modal component

**Files:**
- Create: `apps/frontend/components/admin/one-time-token-modal.tsx`

- [ ] Reusable modal:

```tsx
"use client";
import { useState } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export function OneTimeTokenModal({ open, url, title, onClose }:
	{ open: boolean; url: string | null; title: string; onClose: () => void }) {
	const [copied, setCopied] = useState(false);
	if (!url) return null;
	return (
		<Dialog open={open}
			onOpenChange={(v) => { /* block close unless explicit */ if (!v) return; }}>
			<DialogContent onEscapeKeyDown={(e) => e.preventDefault()}
				onPointerDownOutside={(e) => e.preventDefault()}>
				<DialogHeader><DialogTitle>{title}</DialogTitle></DialogHeader>
				<p className="text-sm text-muted-foreground">This link is shown <strong>once</strong>. Copy it before closing — it cannot be retrieved again.</p>
				<Input readOnly value={url} aria-label="One-time URL" onFocus={(e) => e.currentTarget.select()} />
				<DialogFooter className="gap-2">
					<Button variant="outline" onClick={() => { navigator.clipboard.writeText(url); setCopied(true); }}>
						{copied ? "Copied" : "Copy link"}
					</Button>
					<Button onClick={onClose}>I've saved this link</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] Component test (Vitest + RTL): renders, Esc does nothing, outside-click does nothing, "I've saved" calls `onClose`, Copy writes to clipboard (mock `navigator.clipboard`).
- [ ] Commit: `feat(frontend): one-time token modal`.

---

## Task 10: Frontend — Security tab

**Files:**
- Create: `apps/frontend/app/admin/users/[userId]/security/page.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/suspend-panel.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/delete-restore-panel.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/purge-panel.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/force-reset-button.tsx`

- [ ] Security page is RSC; fetches detail; renders:
  - **Suspend panel** — if not suspended: reason textarea + "Suspend" button; if suspended: shows reason + "Unsuspend" button. Gated on `users.suspend`.
  - **Force password reset** — button; on success opens `<OneTimeTokenModal>` with the returned `tokenUrl`. Gated on `users.security.reset`.
  - **Delete / Restore** — if not deleted: "Soft delete" button with confirm; if deleted: "Restore" button. Gated on `users.delete`.
  - **Purge (Danger Zone)** — only visible if session has role `superadmin`. Requires typing the user id. On success, navigates back to `/admin/users`.
  - Buttons for actions on self are disabled with a tooltip.

- [ ] For force-reset and any action returning an output that needs to be shown inline (token URL), the button is a client component that calls `fetch("/api/app/admin/actions/users.password.forceReset", ...)` directly (same pattern as existing `AdminActionButton`, adapted to capture the response body and show a modal). Either extend `AdminActionButton` to support an `onSuccess(result)` callback or write a small `<ForceResetButton>` that does the fetch manually.

- [ ] Commit: `feat(frontend): user security tab (suspend/delete/purge/force-reset)`.

---

## Task 11: Frontend — component tests

**Files:**
- Create: `apps/frontend/app/admin/users/[userId]/_components/__tests__/suspend-panel.test.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/__tests__/purge-panel.test.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/__tests__/force-reset-button.test.tsx`
- Create: `apps/frontend/components/admin/__tests__/one-time-token-modal.test.tsx`

- [ ] Suspend panel: disabled when self; requires reason optional; submits correct payload.
- [ ] Purge panel: Purge button disabled until typed id matches; on match, posts with `confirmId`.
- [ ] Force reset: after click, shows modal with returned URL (mock fetch).
- [ ] Modal: see Task 9.
- [ ] Commit: `test(frontend): lifecycle panels + token modal`.

---

## Done when

- [ ] `go test ./...` green.
- [ ] Frontend test + typecheck green.
- [ ] Suspending a user immediately logs them out on their next request; force-reset URL can be used once; purge cascades.
