# Admin Users — Plan 4: Email, Connections, Invite

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans.

**Goal:** Add email management, connection unlinking, and the invite flow. Reuses the one-time-token modal from Plan 3.

**Prerequisite:** Plan 3 merged.

---

## File Structure

**Backend** (all new actions under `apps/backend/internal/admin/actions/`)
- `users_email_resend_verification.go` + test
- `users_email_set_primary.go` + test
- `users_email_add.go` + test
- `users_email_remove.go` + test
- `users_invite.go` + test
- `users_connections_unlink.go` + test
- Register all in the actions registry.

**Frontend**
- Modify: `apps/frontend/lib/rustlens-api.ts` — any new DTOs for action outputs.
- Modify: `apps/frontend/app/admin/users/[userId]/emails/page.tsx` — make editable.
- Modify: `apps/frontend/app/admin/users/[userId]/connections/page.tsx` — add unlink buttons.
- Create: `apps/frontend/app/admin/users/_components/invite-user-dialog.tsx`
- Modify: `apps/frontend/app/admin/users/_components/users-filter-bar.tsx` — add "Invite user" button that opens the dialog.
- Create: `apps/frontend/app/admin/users/[userId]/_components/email-actions.tsx` — set-primary, resend-verify, remove, add-email form.

---

## Task 1: Action — `users.email.resendVerification`

- [ ] Perm `users.email.manage`. Dangerous: false.
- [ ] Validate: `{userId, email}`. Email must exist in `user_emails` for this user.
- [ ] Execute: invalidate outstanding `email_verification_tokens` for (user, email); generate fresh token (32 bytes, sha256-hashed in DB, raw in response); return `{tokenUrl: APP_BASE_URL + "/verify-email?token=" + raw}`. Expiry 24h.
- [ ] Test: happy path, non-existent email → 404, idempotent rotation.
- [ ] Commit.

---

## Task 2: Action — `users.email.setPrimary`

- [ ] Perm `users.email.manage`. Dangerous: false.
- [ ] Validate: `{userId, email}`. Email must exist + be verified.
- [ ] Execute (tx):
  ```
  UPDATE user_emails SET is_primary = (email = $2) WHERE user_id = $1;
  ```
  Then verify exactly one row is primary. If email wasn't verified, return 409.
- [ ] Test: happy path, unverified email → 409, missing email → 404.
- [ ] Commit.

---

## Task 3: Action — `users.email.add`

- [ ] Perm `users.email.manage`. Dangerous: false.
- [ ] Validate: `{userId, email}`. Email syntactically valid (simple regex — full RFC-5322 is overkill; `/^[^\s@]+@[^\s@]+\.[^\s@]+$/` is fine). Normalize to lowercase.
- [ ] Execute: `INSERT INTO user_emails (user_id, email, is_primary, is_verified) VALUES ($1, $2, false, false)`. On unique-violation (email already exists anywhere, including soft-deleted users), return 409 with message "email already in use" (do not leak which account holds it).
- [ ] Test: happy path, duplicate (same user) → 409, duplicate (other user) → 409, bad format → 400.
- [ ] Commit.

---

## Task 4: Action — `users.email.remove`

- [ ] Perm `users.email.manage`. Dangerous: true.
- [ ] Validate: `{userId, email}`.
- [ ] Execute: if `(user_id, email)` has `is_primary = true`, return 409 "cannot remove primary email". Else `DELETE FROM user_emails WHERE user_id=$1 AND email=$2`; 0 rows → 404.
- [ ] Test: happy path, primary → 409, missing → 404.
- [ ] Commit.

---

## Task 5: Action — `users.invite`

- [ ] Perm `users.create`. Dangerous: true.
- [ ] Validate: `{email, displayName?}`. Email normalized and unique (check `user_emails`).
- [ ] Execute (tx):
  1. Generate `usr_` + 24 hex chars: `"usr_" + hex(crypto.rand 12 bytes)`. Retry once on unlikely collision.
  2. `INSERT INTO app_users (id, display_name, avatar_url, roles) VALUES ($1, $2, '', '["user"]'::jsonb)`.
  3. `INSERT INTO user_emails (user_id, email, is_primary, is_verified) VALUES ($1, $2, true, false)`.
  4. Generate `email_sign_in_tokens` row with 24h expiry; store hash.
  5. Construct URL: `APP_BASE_URL + "/sign-in?token=" + raw`.
- [ ] Return `{userId, tokenUrl}`.
- [ ] Test: happy path; duplicate email → 409; invalid email → 400; verify `email_sign_in_tokens` row present; verify `app_users` row is not suspended/deleted.
- [ ] Commit.

---

## Task 6: Action — `users.connections.unlink`

- [ ] Perm `users.connections.manage`. Dangerous: true.
- [ ] Validate: `{userId, provider, providerAccountId}` with provider in {`"discord"`, `"steam"`}.
- [ ] Execute: `DELETE FROM user_connected_accounts WHERE user_id=$1 AND provider=$2 AND provider_account_id=$3`; 0 rows → 404.
- [ ] Test: happy path, unknown provider → 400, missing row → 404.
- [ ] Commit.

---

## Task 7: Frontend — invite dialog

**Files:**
- Create: `apps/frontend/app/admin/users/_components/invite-user-dialog.tsx`
- Modify: `apps/frontend/app/admin/users/_components/users-filter-bar.tsx` — render `<InviteUserDialog>` trigger button on the right.

- [ ] Dialog contains: email input (required, type=email), display name input (optional), "Send invite" button. On submit:
  1. POST to `/api/app/admin/actions/users.invite` with CSRF token.
  2. On success, close the invite dialog and open `<OneTimeTokenModal>` with `title="Invite link for {email}"` and the returned `tokenUrl`.
  3. On error, inline error in the dialog.
- [ ] The button is hidden if session lacks `users.create` perm.
- [ ] Commit: `feat(frontend): invite user dialog`.

---

## Task 8: Frontend — editable emails tab

**Files:**
- Modify: `apps/frontend/app/admin/users/[userId]/emails/page.tsx`
- Create: `apps/frontend/app/admin/users/[userId]/_components/email-actions.tsx`

- [ ] Emails table columns: email · primary · verified · actions.
- [ ] Actions per row, gated on `users.email.manage`:
  - If not primary and verified: "Set primary" (`<AdminActionButton action="users.email.setPrimary" ...>`).
  - If unverified: "Send verification" (`<AdminActionButton action="users.email.resendVerification" ...>`; on success opens `<OneTimeTokenModal>` with the URL).
  - If not primary: "Remove" (with DangerousConfirmDialog).
- [ ] Below the table: "Add email" form (input + button → POST `users.email.add`; on success, page refresh).
- [ ] Self-guard: actions on the session user's own emails are allowed (email management on self is fine) — unless behavior says otherwise. Leave allowed; the backend allows it.
- [ ] Commit: `feat(frontend): editable emails tab`.

---

## Task 9: Frontend — connections tab with unlink

**Files:**
- Modify: `apps/frontend/app/admin/users/[userId]/connections/page.tsx`

- [ ] Each Discord and Steam row gets an "Unlink" action (dangerous confirm), gated on `users.connections.manage`.
- [ ] Commit: `feat(frontend): connections tab with unlink`.

---

## Task 10: Frontend — tests

- [ ] Invite dialog: validation (required email), success flow shows modal with token URL, error flow shows inline error.
- [ ] Email actions: set-primary / resend / add / remove dispatch correct payloads; add-email empty/invalid inputs blocked client-side.
- [ ] Connections unlink: confirm dialog appears; confirm sends correct payload.
- [ ] Commit: `test(frontend): email, connections, invite`.

---

## Done when

- [ ] Backend green, frontend green.
- [ ] End-to-end: admin invites `x@example.com`, copies URL from modal, pastes in an incognito browser, completes sign-up, new user visible in list.
- [ ] Admin unlinks a Discord connection and it disappears from the user's detail.
- [ ] Admin adds an email to a user, triggers resend-verify, token URL appears in modal.
