# Auth Session Improvements — Design

**Date:** 2026-05-12
**Branch:** `development/5-12-26-frank`
**Author:** session brainstorm with Frank

## Problem

The current Discord-OAuth session has three user-facing gaps:

1. **No notifications.** When a session expires, a user is force-logged-out by an admin, or has their roles/permissions updated, there is no UI signal. The session expired/revoked path silently 401s on the next request and bounces to `/login` with no explanation. Role changes only take effect on the next navigation (and only on `/admin` pages, which re-fetch live state — `/dashboard` pages render from the stale cookie payload).
2. **Stale session payload.** The cookie stamps `roles`, `adminRoles`, and `permissions` at sign-in and never refreshes them, so the session is correct only at the moment of login. Sliding TTL doesn't exist either — a long-running session approaches 14d expiry with no graceful refresh.
3. **No instant updates on permission change.** Granting a user a new admin role today requires them to re-login (or navigate to an `/admin` page that re-fetches) before the new capability is visible. We want the new perms to take effect within ~1s with no re-login.

This design closes those three gaps without adding new providers, MFA, per-device session lists, or API tokens (all explicitly out of scope).

## Goals

- Sonner toasts for: **session expired**, **forced logout (by admin)**, **role/permission updated**.
- **Instant role updates.** Admin grants/revokes a permission → affected user's UI reflects it within ~1s, no page refresh, no re-login.
- **Sliding TTL.** Active users' cookies silently re-mint before TTL expiry; embedded display data stays fresh.
- **Backend epoch wiring audit.** Confirm `session_epoch` bumps on force-logout / suspension / deletion, and does **not** bump on role/permission changes (so role updates don't kick the user).

## Non-goals

- New auth providers, MFA, API tokens, per-device session lists.
- Cookie format changes — `rustlens_session` envelope (`base64url(json).hmac`) stays as-is.
- Server-side session store / database-backed sessions.

## Architecture

```
                            ┌─────────────────────────────────┐
                            │  Browser (Next.js client)       │
                            │                                 │
                            │  <SessionProvider>              │
                            │    useSessionEvents()           │
                            │    → EventSource(/api/events/.) │
                            │    on event:                    │
                            │      roles.changed →            │
                            │        refetch /api/app/me,     │
                            │        router.refresh(), toast  │
                            │      session.revoked →          │
                            │        clearSession, redirect   │
                            │      session.expiring →         │
                            │        silent refreshSession()  │
                            └────────────┬────────────────────┘
                              cookie     │ EventSource
                                         ▼
                     ┌────────────────────────────────────────┐
                     │  Backend (Go, chi)                     │
                     │                                        │
                     │  GET /api/events/stream                │
                     │     cookie auth (SessionFromCookie)    │
                     │     text/event-stream, 25s heartbeat   │
                     │     per-user cap: 5 conns              │
                     │                                        │
                     │  internal/events.Hub                   │
                     │     map[userID][]chan Event            │
                     │     Publish(userID, event)             │
                     │                                        │
                     │  Subscribers (in-process publishers):  │
                     │    admin role mutations → roles.changed│
                     │    admin force-logout   → session.revok│
                     │    suspension/deletion  → session.revok│
                     │                                        │
                     │  Redis pub/sub bridge:                 │
                     │    channel "rustlens:user-events"      │
                     │    multi-pod fanout                    │
                     └────────────────────────────────────────┘
```

### Why SSE over alternatives

| Option | Verdict |
|---|---|
| **SSE** | One-way push fits the use case exactly; native `EventSource` auto-reconnect; trivial Go impl with `http.Flusher`; works over plain HTTP through Traefik with no upgrade. **Chosen.** |
| WebSocket | Full duplex is overkill, adds Traefik/edge complexity. Not justified by any other realtime feature on the roadmap. |
| Short polling | Latency = poll interval. "Instant" requires polling ~5s, which scales badly across tabs. |
| 401-only (lazy) | No push; role changes don't propagate until next request. Doesn't meet the "instant updates" goal. |

### Why a Redis bridge now

Backend is single-pod today but the Dockerfile is already published to GHCR and the deployment will scale. Without Redis fanout, an admin mutation handled by pod A can't reach the user's SSE connection on pod B. The bridge is ~30 LOC and avoids a future migration mid-feature.

### Why `/api/events/*`, not `/api/app/events`

Today's invariant: **the browser never calls `/api/app/*` directly** — that surface requires the `X-RustLens-App-Secret` header and is reached only through Next.js server code. SSE has to be browser-callable (an `EventSource` can't add custom headers). Creating it as `/api/events/*` keeps the existing invariant intact and gives the new browser-callable surface its own router group with cookie auth.

### Why cookie becomes a hint, not source of truth

Roles in the cookie payload are stamped at login. To make role changes instant *without* bumping `session_epoch` (which would force a re-login), the live UI must read from a context that's refetched from `/api/app/me`, not from the cookie. The cookie still authoritatively carries `userId` and `epoch` (revocation anchor) — those are stable for the session's lifetime.

## Components

### Backend: `internal/events/` (new package)

```
internal/events/
  types.go            // Event types matching the TS contract
  hub.go              // in-process pub/sub; Publish, Subscribe, Unsubscribe
  hub_test.go
  redis_bridge.go     // subscribes to "rustlens:user-events", republishes to local hub
  redis_bridge_test.go
```

**`Event` type:**

```go
type Event struct {
    Type      string `json:"type"`
    Reason    string `json:"reason,omitempty"`
    ExpiresAt int64  `json:"expiresAt,omitempty"`
}
```

**`Hub` API:**

```go
type Hub struct { /* ... */ }

func New() *Hub
func (h *Hub) Publish(userID string, e Event)          // never blocks; drops on full buffer
func (h *Hub) Subscribe(userID string) (ch <-chan Event, unsub func(), err error)  // err if user has >= 5 conns
```

- Per-channel buffer of 16. If full, drop oldest (we'd rather lose a redundant `roles.changed` than block the publisher).
- `Subscribe` returns `ErrTooManyConnections` after 5 active subscribers for a single userID.

**Redis bridge:**

- On startup, subscribes to `rustlens:user-events`. Each message is `{userId, event}` JSON; bridge calls `hub.Publish(userId, event)` locally.
- On `Publish`, the public wrapper writes to **both** the local hub and Redis.
- Bridge owns one goroutine; cleanly shuts down on `ctx.Done()`.

### Backend: `GET /api/events/stream`

- Mounted under a new chi sub-router `/api/events` with cookie-based session middleware (validates via `ValidateSessionFromCookie`).
- Headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`, `Connection: keep-alive`.
- Loop:
  - Subscribe to hub for `session.UserID`.
  - Write initial `event: ping` so the client immediately knows the connection is up.
  - Every 25s, write `event: ping`. (Comments would also work; explicit `ping` lets the client log heartbeats.)
  - When `expiresAt - now() < 30 * time.Minute` and we haven't sent it yet for this conn, push `session.expiring`.
  - On received event: format as `event: <type>\ndata: <json>\n\n`, `Flusher.Flush()`.
  - On context cancel: unsubscribe and return.
- Per-user connection cap returns `429 Too Many Requests` with body `{"error":"too_many_connections"}`.

### Backend: emission points

Wire `events.Hub` as a dependency into:

1. **`internal/admin/actions/`** — every existing handler for `admins.grant`, `admins.revoke`, `roles.permissions.set` calls `hub.Publish(userID, Event{Type: "roles.changed", Reason: ...})` after the DB write succeeds. For `roles.permissions.set`, fan out: `SELECT user_id FROM admin_user_roles WHERE role_id = $1` then publish per row.
2. **New action `admins.force_logout`** — checks `admins.grant` permission, bumps `app_users.session_epoch`, writes audit row, calls `hub.Publish(userID, Event{Type: "session.revoked", Reason: "force_logout"})`. Surfaced via the same action dispatcher pattern as existing admin actions.
3. **Suspension path** — wherever `suspended_at` is set, additionally bump `session_epoch` and publish `session.revoked` with reason `suspended`. (Audit: confirm there's only one such path; if not, centralize.)
4. **Deletion path** — same treatment with reason `deleted`.

Note: role grant/revoke does **not** bump `session_epoch`. The user keeps their cookie; the frontend re-renders against fresh `/api/app/me` data.

### Frontend: `SessionProvider`

```
apps/frontend/components/session-provider.tsx
```

- Client component, mounted inside `app-shell.tsx` (wraps both `/dashboard` and `/admin` shells).
- Initial state is hydrated from a server prop populated by the existing `getSession()` + `getCurrentUser()` calls already made in the shell.
- Exposes `useSession()` returning `{ user, roles, adminRoles, permissions, refresh() }`.
- `refresh()` calls a server action that re-fetches `/api/app/me` and returns the typed `CurrentUser`.

### Frontend: `useSessionEvents` hook

```
apps/frontend/lib/events-client.ts        // EventSource lifecycle helper
apps/frontend/components/session-events.tsx  // hook + mount
```

- Opens `new EventSource("/api/events/stream")` directly to the backend. EventSource attaches the `rustlens_session` cookie automatically because the request is same-origin (Traefik routes `/api/*` to backend, both served from the same host).
- Listeners:
  - `roles.changed` → `await session.refresh()` → `router.refresh()` (Next.js 16 — will verify exact API in `node_modules/next/dist/docs/` during implementation) → `toast.success("Your permissions were updated")`.
  - `session.revoked` → `await clearSession()` server action → `router.push("/login?notice=" + reason)`.
  - `session.expiring` → `await refreshSession()` server action; on failure, fall back to `session.revoked` flow.
  - `ping` → no-op (optionally log in dev).
- On `error` (network) with `EventSource.readyState === CLOSED` and 3 consecutive failures within 30s, dispatch synthetic `session.revoked` with reason `connection_lost` (assume the user has been logged out elsewhere).
- Cleanup: `eventSource.close()` on unmount.

### Frontend: Sliding TTL — `refreshSession` server action

```
apps/frontend/lib/session-refresh.ts
```

```ts
"use server";

export async function refreshSession(): Promise<{ ok: true } | { ok: false; reason: string }>
```

- Reads the current cookie. If `expiresAt - Date.now() > 7 days`, returns `{ ok: true }` without touching anything.
- Otherwise calls `getCurrentUser()`. On 401 → return `{ ok: false, reason: "revoked" }`.
- On success, builds a new `SessionUser` via `toSessionUser(currentUser)` (the existing helper, which sets `expiresAt: Date.now() + 14d`), calls `setSession()`.
- Client triggers: on window `focus`, on `online`, and a `setInterval` every hour. Throttled with a `lastRefreshAt` ref (min 60s between attempts).

### Frontend: login notice toast

```
apps/frontend/app/login/notice-toast.tsx
```

- Client component reading `useSearchParams().get("notice")`. Maps known notices to Sonner calls in `useEffect`:
  - `force_logout` → `toast.error("You were signed out by an administrator")`
  - `suspended` → `toast.error("Your account has been suspended")`
  - `deleted` → `toast.error("Your account has been deleted")`
  - `expired` → `toast.info("Your session expired, please sign in again")`
  - `connection_lost` → `toast.warning("You were signed out. Please sign in again.")`

### Frontend: global 401 surfacing in `rustlens-api.ts`

- The existing server-side fetcher in `lib/rustlens-api.ts` currently throws generic errors on 401. Add a typed `SessionRevokedError` thrown when the backend returns `{"error": "session_revoked"}` or `{"error": "invalid_session"}`.
- Authenticated server components that bubble `SessionRevokedError` redirect via `redirect("/login?notice=expired")`. (Wrap in a shared helper to avoid duplication.)

## Event catalog

```ts
type ServerEvent =
  | { type: "roles.changed";   reason: "grant" | "revoke" | "role_perms_updated" }
  | { type: "session.revoked"; reason: "force_logout" | "suspended" | "deleted" }
  | { type: "session.expiring"; expiresAt: number }   // ms epoch
  | { type: "ping" };
```

Payloads are deliberately minimal — `roles.changed` carries no permission data, the client refetches `/api/app/me` for the authoritative state. This keeps the SSE channel a "ping me when X happened" signal and avoids leaking RBAC details over the stream.

## Data flow examples

### A. Admin grants `moderator` to user U

1. Admin clicks Grant in `/admin/users/...`. Frontend POSTs to action dispatcher.
2. Backend `admins.grant` handler inserts into `admin_user_roles`, writes audit row.
3. Handler calls `hub.Publish(U, Event{Type:"roles.changed", Reason:"grant"})`.
4. Local hub fans out to U's subscribers (including the Redis bridge, which publishes to `rustlens:user-events`).
5. Pods subscribed to Redis receive the message and call their local `hub.Publish`.
6. U's SSE handler reads from its channel, writes `event: roles.changed\ndata: {"type":"roles.changed","reason":"grant"}\n\n`, flushes.
7. U's browser `EventSource` `onmessage` fires.
8. Hook calls `session.refresh()` → `/api/app/me` → updated context.
9. `router.refresh()` re-renders server components with the new perms.
10. `toast.success("Your permissions were updated")`.

### B. Admin force-logs-out user U

1. Admin clicks Force Logout. Frontend POSTs to action dispatcher with reason.
2. Backend `admins.force_logout` handler: `UPDATE app_users SET session_epoch = session_epoch + 1 WHERE id = U`, writes audit row.
3. Handler calls `hub.Publish(U, Event{Type:"session.revoked", Reason:"force_logout"})`.
4. U's SSE delivers the event.
5. Hook calls `clearSession()` server action, then `router.push("/login?notice=force_logout")`.
6. Login page renders, `<NoticeToast/>` reads `notice=force_logout`, fires `toast.error("You were signed out by an administrator")`.
7. Parallel: any other tab U has open also has an SSE connection and receives the same event; all tabs converge to `/login`.
8. If U's cookie somehow survives and U makes a request: backend `ValidateSession` sees `session.Epoch < current_epoch` and returns `401 session_revoked` — backstop.

### C. Sliding TTL refresh

1. U has been signed in 8 days; cookie has 6 days left.
2. U focuses a tab. Hook fires `refreshSession()`.
3. Server action sees `6d < 7d` threshold, calls `getCurrentUser()`.
4. Success → re-mints cookie with `Date.now() + 14d`. Returns `{ok: true}`.
5. No UI change. (Optional: dev-only log.)

## Edge cases

- **Multi-tab convergence.** Each tab holds its own SSE connection. The per-user cap (5) is enforced server-side. On revoke, all tabs receive and redirect — no orchestration needed.
- **Browser asleep / laptop closed.** `EventSource` auto-reconnects on resume. On reconnect, the next event delivery resumes; missed events are not replayed (acceptable — the client refetches `/me` on the first `roles.changed` it sees, getting the current truth).
- **Cookie missing on EventSource open.** Backend returns 401, `EventSource` retries, eventually triggers the `connection_lost` fallback.
- **Race: admin grants role while user opens a new tab.** New tab's `SessionProvider` hydrates from live `/api/app/me`, sees the new role. The redundant SSE event arrives; refetch and toast — slight redundancy, acceptable.
- **Race: user logs out tab A while tab B has SSE open.** Cookie clears in browser; tab B's connection survives until next request. Tab B's hook will not receive new events from B's cookie because the cookie is gone in browser; but the EventSource was opened with the old cookie. On the next reconnect (typically within 1–2 min), the connection 401s and the `connection_lost` fallback fires. Acceptable.
- **Traefik idle timeout.** Traefik v3 default is 90s; our 25s heartbeat keeps the connection warm. Integration smoke test through Traefik (`make dev`) is required.
- **Per-user connection cap reached.** Returns `429`. Client backs off and retries with exponential backoff capped at 60s. Surfaces a dev-mode warning.
- **Suspended user reconnecting.** Cookie may still validate signature, but `ValidateSession` already treats user-not-found as revoked. We additionally need: `ValidateSession` rejects sessions where `app_users.suspended_at IS NOT NULL OR deleted_at IS NOT NULL` regardless of epoch. (Audit during implementation.)

## Testing

| Layer | Test |
|---|---|
| Go | `events.Hub`: publish-before-subscribe drops cleanly; multi-subscriber fanout; cleanup on context cancel; per-user cap returns error |
| Go | `events.Hub`: full buffer drops oldest, doesn't block publisher |
| Go | `/api/events/stream`: cookie required (401 without); emits initial ping; emits real event after `hub.Publish`; closes on context cancel; 429 over cap |
| Go | `events.RedisBridge`: publish on one bridge → consume on another → local Hub fanout (miniredis) |
| Go | Admin actions: `admins.grant` invokes `Publish("roles.changed")`; `roles.permissions.set` fans out to all role holders; `admins.force_logout` bumps epoch + publishes `session.revoked` |
| Go | `ValidateSession` rejects suspended / deleted users (new behavior) |
| TS | `useSessionEvents` with mock EventSource: each event type triggers correct effect (toast text, refetch, navigation) |
| TS | `SessionProvider` rerenders when context updates after `roles.changed` |
| TS | `refreshSession`: < 7d remaining → no-op; > 7d → re-mints; 401 → returns `{ok:false, reason:"revoked"}` |
| TS | `<NoticeToast/>` renders correct Sonner call per known notice value; unknown notice is silently ignored |
| TS | `rustlens-api.ts` throws `SessionRevokedError` on `{"error":"session_revoked"}` 401 |

## File layout summary

**Backend (new):**
```
internal/events/
  types.go
  hub.go
  hub_test.go
  redis_bridge.go
  redis_bridge_test.go
internal/http/
  events_handlers.go          // SSE handler
  events_handlers_test.go
internal/admin/actions/
  force_logout.go             // new action + test
```

**Backend (modified):**
```
internal/http/router.go              // mount /api/events sub-router with cookie auth
internal/admin/actions/grant.go      // call hub.Publish after DB write
internal/admin/actions/revoke.go     // call hub.Publish
internal/admin/actions/permissions.go// fan-out publish for roles.permissions.set
internal/appauth/session.go          // reject suspended/deleted in ValidateSession
internal/admin/users/suspension.go   // bump epoch + publish on suspend
internal/admin/users/delete.go       // bump epoch + publish on delete
cmd/server/main.go                   // wire events.Hub + RedisBridge into deps
```

**Frontend (new):**
```
components/session-provider.tsx
components/session-events.tsx
lib/events-client.ts
lib/session-refresh.ts
app/login/notice-toast.tsx
```

**Frontend (modified):**
```
components/app-shell.tsx       // wrap with <SessionProvider><SessionEvents/>
lib/rustlens-api.ts            // SessionRevokedError + global handling
app/login/page.tsx             // include <NoticeToast/>
```

## Risk register

1. **Next.js 16 specifics.** `router.refresh()`, server actions, and cookie APIs may differ from training-data Next. Per `apps/frontend/AGENTS.md`, read `node_modules/next/dist/docs/` before writing any frontend code. The design treats `router.refresh()` as the mechanism, but the exact API will be verified during implementation.
2. **Traefik buffering.** Confirm with a smoke test that `X-Accel-Buffering: no` (and Traefik's default behavior) doesn't buffer the SSE stream. If buffering occurs, ensure Go writes use chunked encoding.
3. **In-process hub during hot reload (`air`).** Old hub may linger briefly. The Redis bridge masks this — any events lost in the gap re-publish via cookie reconnect.
4. **Per-user cap UX.** A user with 6+ tabs hits 429. Acceptable: SSE is a nicety, not load-bearing for auth correctness. Tabs without SSE still get the lazy 401-driven flow on their next request.

## Open questions resolved during brainstorm

- **Fanout on `roles.permissions.set`:** publish to **all role holders** (not just direct grant/revoke targets). Implementation: `SELECT user_id FROM admin_user_roles WHERE role_id = $1`, fan out individual publishes.
- **Force re-auth on permission change:** **No.** Role/permission changes do NOT bump `session_epoch`. Only force-logout / suspension / deletion bump epoch.
- **Cookie as source of truth:** **No.** Cookie carries `userId` + `epoch` for revocation anchoring. Live roles/permissions come from `SessionContext`, populated by `/api/app/me`.
- **Transport:** **SSE.** WebSocket/polling/lazy ruled out (see Architecture).
- **Out of scope:** per-device session list, additional providers, MFA, API tokens.
