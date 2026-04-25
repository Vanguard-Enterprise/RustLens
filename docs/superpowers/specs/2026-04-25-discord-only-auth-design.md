# Discord-Only Auth — Design

**Date:** 2026-04-25
**Status:** Approved (brainstorm)
**Branch:** `development/web-administration`

## Goal

Remove email/password authentication entirely. Discord OAuth becomes the single sign-in path for all users, including admins. Discord email is **not** captured.

## Background

The repo currently has a full email/password auth surface (login, register, email verification, password reset) plus a Discord OAuth flow added in commits `bc6d1e2..198b153`. Discord was added as a *connected account* on top of email-based identity. The userbase will not want to use email, so we collapse to Discord-only and delete the email surface.

Pre-launch — no real user data exists. Existing rows in `app_users`, `user_emails`, `user_password_credentials` are dev/seed data and will be wiped.

## Identity model

`app_users` becomes the single source of identity, keyed on Discord:

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | Existing `usr_<24hex>` format, unchanged. |
| `discord_id` | TEXT UNIQUE NOT NULL | New. Sole identity column. |
| `discord_username` | TEXT NOT NULL | New. Refreshed on every login. |
| `discord_global_name` | TEXT NULL | New. Refreshed on every login. |
| `discord_avatar` | TEXT NULL | New. Refreshed on every login. |
| `roles` | JSONB | Existing. Default `["user"]`; `internal_admin` for promoted users. |
| `created_at` / `updated_at` | TIMESTAMPTZ | Existing. |

Email is not stored. If a future feature needs email, it is a separate, additive change.

## Sign-in flow

1. User clicks "Sign in with Discord".
2. Existing Discord OAuth Initiate handler runs PKCE + state.
3. Callback handler exchanges the code, fetches Discord profile.
4. Look up `app_users` by `discord_id`:
   - **Hit:** refresh `discord_username` / `discord_global_name` / `discord_avatar`, mint session.
   - **Miss:** INSERT new `app_users` row with `roles=["user"]` and the Discord profile fields, mint session.
5. Set session cookie via the existing `SessionFromCookie` machinery.

There is no "link" branch — sign-in is the only branch. The previously-added `LinkDiscordToUser` and `FindUserByEmail` repo methods are deleted.

## Removal scope

### Frontend routes (delete)
- `apps/frontend/app/auth/login/route.ts`
- `apps/frontend/app/auth/register/route.ts`
- `apps/frontend/app/auth/email/verify/route.ts`
- `apps/frontend/app/auth/email/verify/request/route.ts`
- `apps/frontend/app/auth/password/reset/consume/route.ts`
- `apps/frontend/app/auth/password/reset/request/route.ts`

### Frontend pages (delete — duplicate / email-only scaffolding)
- `apps/frontend/app/sign-in/page.tsx`
- `apps/frontend/app/sign-up/page.tsx`
- `apps/frontend/app/signin/page.tsx`
- `apps/frontend/app/signup/page.tsx`
- `apps/frontend/app/forgot-password/page.tsx`
- `apps/frontend/app/reset-password/page.tsx`

`/login` is the single retained sign-in page.

### Admin actions (delete — email/password-specific)
All are under `apps/backend/internal/admin/actions/`:
- `users_email_add.go` (+ test)
- `users_email_remove.go` (+ test)
- `users_email_set_primary.go` (+ test)
- `users_email_resend_verification.go` (+ test)
- `users_password_force_reset.go` (+ test)
- `users_invite.go` (+ test) — email-based invite, useless without email
- `users_connections_unlink.go` (+ test) — `user_connected_accounts` is dropped

`users_purge.go` and other non-email admin actions stay. The dispatcher in `apps/backend/internal/admin/actions/dispatcher.go` is updated to remove the deleted action registrations.

### Backend (delete)
- bcrypt password hashing/verification helpers
- Email-token issue/consume handlers (sign-in tokens, verify tokens, password reset tokens)
- `FindUserByEmail` repo method
- Discord OAuth `Link` branch (callback collapses to a single sign-in branch)

### Schema changes
The schema is bootstrapped in Go (`apps/backend/internal/store/postgres.go`), not via SQL migration files. Changes are applied to the bootstrap code; the running dev DB is wiped and recreated.

### DB migration (drop)
- `user_emails`
- `user_password_credentials`
- `email_sign_in_tokens`
- `email_verification_tokens`
- `password_reset_tokens`
- `user_connected_accounts` — Discord moves onto `app_users`. If any other provider was using this table, flag it during implementation; if Discord was the sole consumer, drop it.

### Add (migration)
- `ALTER TABLE app_users ADD COLUMN discord_id TEXT UNIQUE NOT NULL`
- `ALTER TABLE app_users ADD COLUMN discord_username TEXT NOT NULL`
- `ALTER TABLE app_users ADD COLUMN discord_global_name TEXT`
- `ALTER TABLE app_users ADD COLUMN discord_avatar TEXT`

Because the DB is wiped, these can be `NOT NULL` from day one without a backfill.

## UI changes

- **Login page:** single "Sign in with Discord" button. Removes email input, password input, "forgot password" link, register link.
- **Settings → Discord connection panel** (added in `3051191`): replaced with a read-only profile block showing Discord username/avatar plus a "Sign out" action. Discord is the account, not a connection — there is nothing to connect or disconnect.

## Bootstrap & admin promotion

The wipe removes any existing admin. First admin is bootstrapped manually:

1. Deploy with empty `app_users`.
2. Admin signs in via Discord → row created with `roles=["user"]`.
3. Operator runs either:
   - **Direct SQL:** `UPDATE app_users SET roles='["user","internal_admin"]'::jsonb WHERE discord_id='<id>';`
   - **CLI:** `pnpm admin:promote <discord_id> [role]` (default role `internal_admin`)

The CLI lives wherever existing ops scripts live (likely `scripts/` or `apps/backend/cmd/`) and reuses the existing DB env config. It looks up by `discord_id`, appends the role to the `roles` JSONB if missing, and prints before/after. A symmetric `pnpm admin:demote` removes a role.

**Out of scope:** in-app role editor UI. CLI / direct DB is sufficient for now.

## Sessions

No changes. The existing cookie/session helper (`SessionFromCookie`, added in `0368aa7`) remains the single session entry point.

## Error handling

The login page handles:

- **User denies Discord consent:** redirect back to `/auth/login?error=cancelled` with a calm "Sign-in cancelled" message.
- **Discord token exchange fails / Discord 5xx:** redirect to `/auth/login?error=unavailable` with "Discord sign-in is temporarily unavailable, try again."
- **Account disabled** (future role e.g. `banned`): no session minted; redirect to `/auth/login?error=disabled` with "Account disabled."

State/PKCE mismatch errors continue to use whatever the current Discord OAuth handler returns — unchanged.

## Testing

### Backend
Integration tests against the OAuth callback covering:
- New user: callback creates `app_users` row with `roles=["user"]` and Discord profile fields populated.
- Returning user: callback finds existing row, refreshes profile fields, does not create a new row.
- Profile refresh: changing the Discord username between two callbacks updates `discord_username` on the second.
- Denied consent / Discord API failure: appropriate redirect, no session, no DB write.

No bcrypt or email-token tests — those code paths are deleted.

### Frontend
- `/auth/login` renders the Discord button.
- Visiting a protected route while unauthenticated redirects to `/auth/login`.

### Migration
Dry-run against a copy of the dev DB to confirm:
- Table drops succeed (no dangling FKs in unrelated tables).
- New columns on `app_users` apply cleanly to an empty table.

## Out of scope

- In-app role editor UI.
- Account deletion / data export flow.
- Re-introducing email capture (the schema change is reversible if needed later).
- Migrating real user data — there is none.

## Open items for the implementation plan

- Confirm `user_connected_accounts` has no consumers other than Discord before dropping. If other providers are present, downgrade the migration to "remove Discord rows; leave table".
- Locate the existing ops-script convention (Node script vs Go cmd) and put the CLI there.
- Confirm whether the Settings page has any other panels that depended on `user_emails` or `user_connected_accounts` and update them accordingly.
