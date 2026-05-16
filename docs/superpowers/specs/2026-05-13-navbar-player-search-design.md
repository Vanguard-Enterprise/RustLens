# Navbar Player Search — Design

**Date:** 2026-05-13
**Branch:** `feature/ui-ux`
**Status:** approved (brainstorming)

## Goal

Add a Discord-style player search bar to the dashboard/admin navbar. Logged-in
users can look up public player info by Steam ID, BattleMetrics ID, or a
steamcommunity URL. Website admins can additionally search by BattleMetrics name
substring across all globally tracked servers. A new top-level `/search` page
hosts the full results view, and a new `/player/[steamId]` page renders the
public profile card.

## Scope

In scope:

- Navbar input on the authenticated dashboard/admin shell with live dropdown.
- New `/search` page (deep-linkable via `?q=...`).
- New `/player/[steamId]` public profile page (same shape as the bot's
  `/player` Discord embed, charts deferred).
- Hover preview on every result row, sharing the profile card with the page.
- Two new session-authed backend routes under `/api/app/players/*`.
- A small backend refactor extracting the existing identifier-resolution and
  profile logic into a shared `internal/players/` package so the new app routes
  and the existing bot-internal routes share one implementation.

Out of scope:

- Public landing-page search (search lives only inside the authenticated shell).
- Player profile charts/analytics on the website (deferred; the Discord
  `/analyze` flow stays canonical for now).
- Changes to bot search behavior or gating.
- Mobile <`sm` rendering of the navbar input (desktop dropdown only for v1; the
  `/search` page works at every size).
- End-to-end browser tests.

## Lookup behavior (mirrors the bot)

Two paths in a single endpoint, classified server-side from the query:

| Input shape | Path | Auth |
|---|---|---|
| 17-digit Steam ID | identifier | any logged-in user |
| Numeric BM ID | identifier | any logged-in user |
| `steamcommunity.com/(profiles|id)/...` URL | identifier (vanity resolved via Steam API) | any logged-in user |
| Anything else (free text) | name substring | website admin only (any `admin_user_roles` row) |

Classifier rules should match the existing parsers in `apps/bot/src/bm-input.ts`
and `apps/bot/src/steam-input.ts` so navbar and bot accept identical inputs.
Re-export or port that logic during implementation rather than re-deriving the
regex.

This matches the bot's `/player` (public, identifier-only) plus `/search`
(admin-only name substring) split, but unifies them behind a single navbar
control and removes the per-server selection requirement.

Name search is across **all** `tracked_servers`, deduped by Steam ID, capped at
20, ordered online-first then by `last_seen_at desc`.

The "include offline <24h" toggle from `/search` is exposed on the `/search`
page only (admin-only control) and defaults to `false` to match the bot
default. The navbar dropdown always uses `includeOffline=false` to keep it
fast and simple.

## Surfaces

### Frontend

- `components/navbar-player-search.tsx` (new, client) — injected into
  `app-header.tsx` to the left of the bell button. Hidden below `sm`.
  Keyboard shortcut: `/` focuses the input (ignored when another input is
  focused). Debounce 250ms, abort previous in-flight request on each keystroke,
  minimum query length 2.
- `app/search/page.tsx` (new, server) — reads `?q=` and `?includeOffline=`,
  server-fetches initial results, hydrates a client `<SearchResults>`. Admins
  see an "Include offline <24h" `Switch`.
- `app/search/_components/search-results.tsx` (new, client) — list of
  `<PlayerHoverCard>`-wrapped rows.
- `app/player/[steamId]/page.tsx` (new, server) — server-fetches the profile
  and renders `<PlayerProfileCard>`. The `[steamId]` segment also accepts BM
  IDs and steamcommunity URLs (the backend resolves).
- `app/player/[steamId]/_components/player-profile-card.tsx` (new) — reusable
  card shared between hover and full-page renders.
- `components/player-hover-card.tsx` (new, client) — wraps shadcn `HoverCard`,
  lazy-fetches the profile, in-memory `Map<steamId, profile>` cache for the
  session so re-hovers don't refetch.
- `lib/rustlens-api.ts` additions: `searchPlayers(q, includeOffline)` and
  `getPlayerProfile(identifier)`. Both pass the existing internal secret + the
  signed session token header used by every other authenticated `/api/app/*`
  caller.

### Backend

- `internal/players/` (new package) — extracts existing
  identifier-resolution, name-search, and profile logic into one place. Bot
  handlers in `internal/http/handlers.go` are refactored to call this package
  instead of holding the logic inline. No behavior change for existing routes.
  - `players.ClassifyQuery(q) → kind` (steam_id | bm_id | steam_url | name)
  - `players.ResolveByIdentifier(ctx, q) → SearchHit` (1 result; falls back to
    `bmmatch` and warms `steam_account_cache` on a miss, matching current bot
    behavior; returns `404 battlemetrics profile private` on `bmmatch` 404)
  - `players.SearchByName(ctx, q, includeOffline) → []SearchHit`
  - `players.GetProfile(ctx, steamId) → Profile` (matches the existing bot
    profile shape)
- `internal/http/players_handlers.go` (new) — two handlers mounted under the
  existing session-authed `/api/app/*` middleware:
  - `GET /api/app/players/search?q=&includeOffline=`
  - `GET /api/app/players/{identifier}/profile`

## Contracts

```ts
// /api/app/players/search response
type PlayerSearchHit = {
  steamId: string;
  bmId: string | null;
  bmName: string | null;
  avatarUrl: string | null;
  currentServer: { bmId: string; name: string } | null;
  lastSeenAt: string | null; // ISO 8601
};
type PlayerSearchResponse = {
  items: PlayerSearchHit[];
  mode: "identifier" | "name";
};
```

The profile endpoint returns the existing bot-profile shape (BM name, avatar,
first-seen, total/category BM hours, Steam Rust hours, risk factor inputs,
current/last tracked server). No new fields.

## Data flow

**Search (navbar)**

1. User types → 250ms debounce → server action `searchPlayersAction(q, false)`.
2. Server action calls `lib/rustlens-api.ts → searchPlayers()`, which sends
   `GET /api/app/players/search?q=...&includeOffline=false` with
   `X-App-Internal-Secret` and the signed `X-App-Session` header.
3. Backend `/api/app/*` middleware validates session, derives the actor from
   `app_users`.
4. Handler calls `players.ClassifyQuery(q)`. Identifier kinds → 1-item
   response from `ResolveByIdentifier`. Name kind → admin check, then
   `SearchByName(q, includeOffline)`.
5. Frontend renders top 6 in the dropdown plus a "View all results →" link to
   `/search?q=...`.

**Search (`/search` page)**

Server-renders the first response with the URL params, then identical client
fetches when the toggle changes.

**Profile (hover preview and `/player/[id]` page)**

1. `getPlayerProfileAction(identifier)` → `GET /api/app/players/{id}/profile`.
2. Backend resolves `id` to a Steam ID (same logic as identifier search) and
   calls `players.GetProfile(steamId)`.
3. Hover preview caches the response in a client-side `Map` keyed by Steam ID
   for the session.

## SQL (name search)

Single query, executed only when `players.ClassifyQuery(q) == "name"` and the
caller passed admin gating:

```sql
SELECT DISTINCT ON (sa.steam_id)
       sa.steam_id, sa.bm_id, sac.bm_name, sac.avatar_image,
       ts.bm_id AS server_bm_id, ts.name AS server_name,
       ps.is_online, ps.last_seen_at
  FROM steam_account_cache sac
  JOIN steam_accounts       sa ON sa.steam_id = sac.steam_id
  JOIN player_states        ps ON ps.steam_id = sa.steam_id
  JOIN tracked_servers      ts ON ts.bm_id    = ps.server_bm_id
 WHERE sac.bm_name ILIKE '%' || $1 || '%'
   AND (
        ps.is_online
     OR ($2 AND ps.offline_since > now() - interval '24 hours')
       )
 ORDER BY sa.steam_id, ps.is_online DESC, ps.last_seen_at DESC
 LIMIT 20;
```

Query-shape and column names should be confirmed against
`internal/store/player_states.go` during implementation; if `last_seen_at`
isn't present, substitute the equivalent column already used by
`/player-states/servers/{id}/search`.

## Auth & abuse

- Session: every `/api/app/players/*` call requires the validated
  `rustlens_session` cookie plus the `X-App-Internal-Secret` header (same
  pattern as `/api/app/me` and `/api/app/admin/*`).
- Admin gate for name search: `len(actor.AdminRoles) > 0`. Same check the
  existing internal-admin dashboard uses; do not duplicate logic — reuse the
  helper currently in `internal/users/` or `internal/admin/`.
- Rate limits (Redis, mirroring `lib/rate-limit.ts`):
  - `players_search:{userId}` — 30 req / 60 s
  - `players_profile:{userId}` — 60 req / 60 s
- Frontend: 250ms debounce, in-flight request abort, min query length 2,
  client-side hover-card cache.
- Server-side `LIMIT 20` on name-search results.

## Errors

| Status | When | Frontend behavior |
|---|---|---|
| 400 | `q` empty or <2 chars | Defended at frontend; ignored if it slips through |
| 403 | Name search by non-admin | Hide "no results"; show hint "Identifier-only search available. Paste a Steam ID, BM ID, or steamcommunity URL." |
| 404 | Identifier with no resolution after `bmmatch` | Show "No player found" |
| 404 (special) | `bmmatch` returns "BM profile private" | Show "BattleMetrics profile is private" |
| 429 | Rate-limited | Sonner toast |
| 502 | `bmmatch` upstream failure | Return cached `steam_accounts` row if any, else 404 |

Hover-card fetch failures fall back silently to the row's already-known
fields. No toast.

## Testing

- Backend
  - Table tests on `players.ClassifyQuery` covering each input shape and edge
    cases (16-digit numbers, mixed-case URLs, trailing slashes, vanity URLs).
  - Handler integration tests for the two routes covering: identifier hit,
    identifier miss, name hit, name as non-admin (403), rate-limit (429), and
    `bmmatch` fallback success/private/missing-config paths. Style follows the
    existing `/api/app/admin/*` handler tests.
- Frontend
  - Vitest test on `<NavbarPlayerSearch>` for debounce + abort behavior and
    403 → hint rendering.
  - Snapshot test on `<PlayerProfileCard>`.
- No E2E browser test — out of scope unless one already exists for the
  authenticated shell. (None observed during exploration.)

## Migration / compatibility

- No schema changes. All required columns exist on `steam_accounts`,
  `steam_account_cache`, `player_states`, and `tracked_servers`.
- Bot routes and behavior are unchanged. The shared `internal/players/`
  package is a refactor of existing logic, not a behavior change.
- AGENTS.md needs an entry under "Frontend" for the new pages and an entry
  under "Backend" for the new `/api/app/players/*` routes when implementation
  lands.

## Open follow-ups (not v1)

- Charts on `/player/[id]` (analytics, name-history gaps) — likely wraps
  existing `/api/internal/bot/setup/steam/{id}/analytics`.
- Public landing search — would require a different auth UX (login redirect on
  submit) and is intentionally deferred.
- Mobile navbar input on small screens — start with the desktop popover; if
  users want it on mobile, fold the input into the existing mobile nav sheet.
