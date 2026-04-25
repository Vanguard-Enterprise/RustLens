# Discord OAuth Sign-In and Account Linking — Design

**Date:** 2026-04-24
**Status:** Approved (pending implementation plan)

## Goal

Add Discord OAuth 2.0 to RustLens for two flows: signing in (and creating accounts when needed), and linking a Discord account to an already-signed-in user. The existing scaffolding is frontend-mediated; this design moves the OAuth flow to the backend to match the rest of the codebase's "backend owns external API calls" pattern and to remove a forgeable trust boundary.

## Existing state

- Frontend has scaffolded Discord routes (`app/auth/discord/*`, callback) and a `completeDiscordAuth` API helper. **All of this gets deleted.**
- Backend schema already has `user_connected_accounts(user_id, provider, provider_account_id, username, display_name, avatar_url, profile_email, ...)` with `PK (provider, provider_account_id)` and `UNIQUE (user_id, provider)`.
- Backend has `appauth.SerializeSession` that produces tokens byte-compatible with the frontend `rustlens_session` cookie.
- Login page already renders a Discord button when `showDiscord` is true; CSRF/error infrastructure is in place.
- Backend routes are split: `/api/internal/bot/...` (JWT-auth), `/api/app/...` (`requireAppSecret`). New OAuth routes will live under `/api/auth/...` outside both groups, since browser-driven callbacks can't pass the internal secret.

## Decisions

1. **New-user policy:** auto-create or auto-link by verified email.
2. **Logged-in user policy:** sign-in and link are separate routes; sign-in refuses when a session exists, link requires one.
3. **Email verification policy (strict):** auto-link only when Discord reports `verified=true` AND the matching `user_emails.is_verified=true`. Otherwise reject with a specific error.
4. **No email from Discord:** reject with a clear error message; do not create accounts without an email.
5. **Profile refresh:** every successful sign-in refreshes the stored Discord username/display_name/avatar/profile_email.
6. **Scope:** ship sign-in and settings-link flow together.
7. **Architecture:** backend-mediated OAuth (not frontend). Backend exchanges the code with Discord, fetches the profile, mints the session cookie itself.

## Routes

| Backend route | Method | Auth | Purpose |
|---|---|---|---|
| `GET /api/auth/discord` | GET | None (refuses if session present) | Sign-in initiate. Sets PKCE + state + intent=`signin` cookies, 302 to Discord. |
| `GET /api/auth/discord/link` | GET | `requireSession` | Link initiate. Sets PKCE + state + intent=`link` cookies, 302 to Discord. |
| `GET /api/auth/discord/callback` | GET | Either | Shared callback. Validates state, exchanges code, fetches profile, branches on intent. |

Single Discord redirect URI: `${APP_ORIGIN}/api/auth/discord/callback`.

Cookies set during the flow (HttpOnly, SameSite=Lax, 10-minute TTL, cleared on callback regardless of outcome):

- `rustlens_discord_oauth_state`
- `rustlens_discord_oauth_verifier`
- `rustlens_discord_oauth_intent` (value: `signin` or `link`)

## Backend implementation

New package `apps/backend/internal/oauth/discord/`:

- `oauth.go` — PKCE helpers (`createPkceVerifier`, `createPkceChallenge`), state generator, intent cookie constants, redirect URI builder.
- `client.go` — Discord HTTP calls: `ExchangeCode(ctx, code, verifier)` and `FetchProfile(ctx, accessToken)`. Both use 10-second timeouts and `Cache-Control: no-store`. Profile struct: `{ID, Username, GlobalName, Avatar, Email, Verified}`.
- `handlers.go` — three HTTP handlers (`Initiate`, `InitiateLink`, `Callback`), constructed via `NewHandler(cfg, repo *appauth.Repo, pool *pgxpool.Pool)`.

New methods on `appauth.Repo` (each runs in one pgx transaction):

- `FindUserByDiscordID(ctx, discordID) (userID string, err error)` — returns `ErrUserNotFound` if no row.
- `RefreshDiscordConnection(ctx, userID, profile) error` — updates `user_connected_accounts` row.
- `FindUserByEmail(ctx, email) (userID string, isVerified bool, exists bool, err error)` — drives the strict policy in callers.
- `CreateDiscordUser(ctx, profile) (userID string, err error)` — creates `app_users`, `user_emails (is_primary=true, is_verified=true)`, `user_connected_accounts` in one tx.
- `LinkDiscordToUser(ctx, userID, profile) error` — INSERT into `user_connected_accounts`; returns `ErrDiscordAlreadyLinked` on `UNIQUE(user_id, provider)` violation, `ErrDiscordLinkedToOther` on PK violation on `(provider, provider_account_id)`.

Backend's `appauth.SessionUser` struct gains `AdminRoles []string` to match the frontend payload (currently missing on the Go side).

### Sign-in callback (intent=`signin`)

1. `FindUserByDiscordID`. If found: `RefreshDiscordConnection`, `TouchLastLogin`, `LoadCurrentUser`, mint session cookie, 302 `/dashboard`. Done.
2. If `profile.Email == ""` or `!profile.Verified`: 302 `/login?error=discord-no-email`.
3. `FindUserByEmail`:
   - exists + `is_verified=true`: `LinkDiscordToUser`, `TouchLastLogin`, `LoadCurrentUser`, mint session, 302 `/dashboard`.
   - exists + `is_verified=false`: 302 `/login?error=discord-email-in-use`.
   - not exists: `CreateDiscordUser`, `LoadCurrentUser`, mint session, 302 `/dashboard`.

### Link callback (intent=`link`)

Validates session again on callback (defense-in-depth — the cookie should still be present).

1. `LinkDiscordToUser(currentSessionUserID, profile)`:
   - success: `LoadCurrentUser`, 302 `/dashboard/settings?notice=discord-linked`. Existing session cookie unchanged.
   - `ErrDiscordAlreadyLinked`: lookup the existing row — if it matches the current user, idempotent: `RefreshDiscordConnection`, redirect with `notice=discord-linked`. Otherwise: 302 `/dashboard/settings?error=discord-already-linked`.
   - `ErrDiscordLinkedToOther`: 302 `/dashboard/settings?error=discord-linked-to-other`.

### Session minting

The backend builds a `SessionUser` matching the frontend's `toSessionUser` shape, calls `appauth.SerializeSession(...)`, and sets the `rustlens_session` cookie with the same flags the frontend uses (HttpOnly, SameSite=Lax, Secure in prod, Path=/, MaxAge = 14 days).

### OpenAPI

These are browser-redirect routes, not JSON APIs. Document them in `internal/http/openapi.go` as `text/html`/302 entries with parameter descriptions; no request/response schemas.

## Frontend changes

Deletions:

- `apps/frontend/app/auth/discord/route.ts`
- `apps/frontend/app/auth/callback/discord/route.ts`
- `completeDiscordAuth(...)` from `apps/frontend/lib/rustlens-api.ts`

Edits:

- `components/auth-page.tsx` — Discord button `href` → `/api/auth/discord`. Drop frontend-callback-only codes that no longer fire (`discord-token`, `discord-profile`, `discord-auth`); keep `discord-config` and `discord-state` (the backend can still emit both). Add new codes:
  - `discord-no-email`
  - `discord-email-in-use`
  - `discord-already-signed-in`
  - `discord-linked-to-other`
  - `discord-already-linked`
- `app/dashboard/settings/page.tsx` — add a Discord connections panel:
  - When `currentUser.discordAccount` is null: render `<a href="/api/auth/discord/link">Connect Discord</a>`.
  - When present: render the connected username + avatar (a "Disconnect" affordance is out of scope for this spec).
  - Surface settings-page error/notice query params for the new codes plus `discord-linked` notice.

## Config

`apps/backend/.env` and `apps/backend/.env.example`:

```
DISCORD_CLIENT_ID=
DISCORD_CLIENT_SECRET=
APP_ORIGIN=http://localhost
```

`APP_ORIGIN` becomes a required backend config value (used to build the redirect URI and post-callback frontend redirects). Wire into `internal/config/config.go`.

`apps/frontend/.env*` — `DISCORD_CLIENT_ID`/`DISCORD_CLIENT_SECRET` are no longer used by the frontend; remove if previously set. `APP_ORIGIN` stays (frontend uses it for CSRF origin checks).

Discord developer portal (manual setup, called out in implementation plan):

1. Create application at https://discord.com/developers/applications.
2. OAuth2 → Redirects → add `http://localhost/api/auth/discord/callback` and the production equivalent.
3. Copy Client ID and Client Secret into `apps/backend/.env`.

## Error and notice codes

| Code | Where shown | Message |
|---|---|---|
| `discord-config` | `/login` | "Discord OAuth is not configured yet." |
| `discord-no-email` | `/login` | "Discord didn't share an email address. Grant the email scope when prompted, or sign up with a password." |
| `discord-email-in-use` | `/login` | "That Discord email matches an existing unverified account. Verify that account's email first, or sign up with a different one." |
| `discord-already-signed-in` | `/login` | "You're already signed in. Use Settings to manage Discord." |
| `discord-state` | `/login` | "The Discord sign-in state was invalid or expired." |
| `discord-linked-to-other` | `/dashboard/settings` | "That Discord account is linked to a different RustLens user." |
| `discord-already-linked` | `/dashboard/settings` | "You already have a Discord account linked." |
| `discord-linked` (notice) | `/dashboard/settings` | "Discord account connected." |

## Testing

### Backend (Go)

- `internal/oauth/discord/oauth_test.go`: PKCE round-trip, intent cookie helpers, redirect URI builder.
- `internal/oauth/discord/client_test.go`: token exchange and profile fetch using `httptest.NewServer` to fake Discord endpoints (no real Discord calls).
- `internal/oauth/discord/handlers_test.go`: table-driven tests for `Callback` covering all branches:
  - sign-in: existing Discord ID → refresh + session minted
  - sign-in: no email / unverified → `discord-no-email` redirect
  - sign-in: matching verified email → auto-link + session minted
  - sign-in: matching unverified email → `discord-email-in-use` redirect
  - sign-in: no email match → user created + session minted
  - link: success
  - link: `discord-already-linked`
  - link: `discord-linked-to-other`
  - link: re-link of own connection → idempotent refresh
  - state mismatch / missing cookies / expired → error redirects
- `internal/appauth/repo_test.go` additions for `FindUserByDiscordID`, `RefreshDiscordConnection`, `FindUserByEmail`, `CreateDiscordUser`, `LinkDiscordToUser` against the existing Postgres test fixture.

### Frontend

- Auth-page error message test: each new error code maps to a non-empty message.
- Settings page test: "Connect Discord" link shows when `discordAccount` is null; connected username + avatar show otherwise.

### Manual smoke

1. `DISCORD_CLIENT_ID/SECRET` unset → click button → `/login?error=discord-config`.
2. Fresh user, valid creds → full sign-in flow lands on `/dashboard`, session cookie set, `currentUser.discordAccount` populated.
3. Returning user → idempotent sign-in; visible profile refresh after changing Discord avatar.
4. Logged-in user clicks Discord button on `/login` → `/login?error=discord-already-signed-in`.
5. Settings link flow with target Discord already linked to another user → `discord-linked-to-other`.
6. Settings link flow with own Discord already linked → idempotent success.

## Out of scope

- "Disconnect Discord" affordance on the settings page.
- Storing or refreshing Discord refresh tokens (we don't need ongoing API access).
- Multi-provider OAuth abstraction (Discord-only for now; structure should make adding another provider straightforward but isn't generalised).
