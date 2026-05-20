# Admin User Usage — Filters, Search, and Target Capture

**Date:** 2026-05-20
**Scope:** `/admin/users/{userId}` Usage tab
**Status:** Design

## Goals

Make the per-user Usage tab usable for incident investigation, not just a recent-history scroll.

- **Filter** by outcome, interaction type, command name, guild, and time window.
- **Search** across command, target text, and recipient names/IDs.
- **Show recipients** for fan-out commands (`/announce` DMs, `/summon` moves, `/nickname` edits, etc.) instead of just the role/scope the user supplied.
- **Resolve IDs** (Discord user, guild, role) to friendly names + icons, with links to other admin pages where possible.
- **Match the servers/audit UI/UX** — sortable table, search input, filter chips, URL-driven state.

## Non-goals

- No custom date range picker (4 fixed windows: 1d / 7d / 30d / 90d).
- No backend full-text search index — `q` is `ILIKE` against a small joined surface.
- No live Discord lookups at read time. Resolution uses our own tables only.

## Architecture

Three layers change in lockstep:

1. **Schema** — new `command_usage_targets` child table.
2. **Bot capture** — fan-out commands pass `targets[]` to the existing fire-and-forget recorder.
3. **Backend read API** — `command-usage` endpoint gains filter params and embeds enriched `targets[]`; new `filter-options` sub-endpoint.
4. **Frontend** — `UsageSection` rebuilt around a sortable `Table` with a filter bar, URL-driven state, and stacked-avatar recipient previews.

## 1 · Schema

```sql
CREATE TABLE IF NOT EXISTS command_usage_targets (
    id                BIGSERIAL PRIMARY KEY,
    command_usage_id  BIGINT NOT NULL REFERENCES command_usage(id) ON DELETE CASCADE,
    kind              TEXT NOT NULL,        -- 'user'|'role'|'channel'|'guild'|'steam'
    snowflake         TEXT NOT NULL,        -- Discord snowflake or Steam ID
    display_name      TEXT,                 -- snapshot at capture time
    avatar_hash       TEXT,                 -- snapshot at capture time
    relation          TEXT NOT NULL,        -- 'recipient'|'scope'|'subject'
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT command_usage_targets_kind_chk
        CHECK (kind IN ('user','role','channel','guild','steam')),
    CONSTRAINT command_usage_targets_relation_chk
        CHECK (relation IN ('recipient','scope','subject'))
);

CREATE INDEX IF NOT EXISTS command_usage_targets_usage_idx
    ON command_usage_targets(command_usage_id);
CREATE INDEX IF NOT EXISTS command_usage_targets_snowflake_idx
    ON command_usage_targets(snowflake);
```

`relation` values:

- `subject` — the entity the action is *about* (`/manage ban @bob` → bob).
- `scope` — the *filter* the invoker supplied (`/announce role:@Members` → the role).
- `recipient` — the *actual* fan-out targets the bot acted on (each DM'd user from `/announce`, each member moved by `/summon`, each nick edited by `/nickname`).

`display_name` + `avatar_hash` are **snapshotted at write time** so renames later don't lose context. At read time we still prefer live values from `app_users` / `bot_guilds` joins where available, falling back to the snapshot.

**Retention** — purged via FK ON DELETE CASCADE when the parent `command_usage` row is purged by the existing nightly 90-day cleanup. No new cleanup job needed.

## 2 · Bot Capture

`apps/bot/src/command-usage.ts` payload gains an optional `targets`:

```ts
type Target = {
  kind:        "user" | "role" | "channel" | "guild" | "steam";
  snowflake:   string;
  displayName?: string | null;
  avatarHash?:  string | null;
  relation:    "recipient" | "scope" | "subject";
};

type CommandUsagePayload = {
  // ... existing fields
  targets?: Target[];
};
```

The shared extractor stays unchanged. Each fan-out handler builds its own `targets[]` **after** the action runs (so partial failures log what *actually* happened) and threads it through `backend-client.recordCommandUsage`.

### Per-command capture matrix

| Command | Subject | Scope | Recipient |
|---|---|---|---|
| `/announce {scope} {message}` | — | role | each successfully DM'd user (snapshot username + avatarHash) |
| `/summon {scope}` | — | role | each member moved into voice (snapshot) |
| `/nickname` | — | — | each member whose nickname was successfully edited (snapshot) |
| `/admin setsteam {user} {steam_id}` | user (snapshot) + steam | — | — |
| `/admin removesteam {user}` | user (snapshot) | — | — |
| `/manage ban {user}` modal submit | user (snapshot) | — | — |
| `/setsteam {steam_id}` | steam | — | — |
| `/transfer` | — | — | each Discord user a link was created for (snapshot) |
| `/create invite` button (accept) | requester (snapshot) | — | inviter (snapshot) |
| `/track add {player}` / `/track remove {player}` | resolved steam IDs | — | — |

Everything else writes `command_usage` as today and emits no targets. The Recipients/Target cell renders "—" for them.

### Caps

- Backend hard-caps `targets[]` length at **500 per record**. Excess truncated server-side.
- `display_name` capped at **256 chars** (existing string-truncation helper).
- Response carries `targetTotal` so the UI can render "showing 500 of N".

### Validation

- `kind` must match the CHECK constraint.
- `snowflake` validated against the same `isSnowflake` helper for `user|role|channel|guild`; `steam` accepts 17-digit numeric.
- Invalid entries are silently dropped (consistent with the existing fire-and-forget posture — never block the bot reply).

## 3 · Backend Read API

### `GET /api/app/admin/users/{id}/command-usage`

New query params (all optional, AND together):

| Param | Type | Notes |
|---|---|---|
| `q` | string | `ILIKE '%q%'` against `command`, `subcommand`, `target`, and any row in `command_usage_targets` (`display_name` or `snowflake`) for that usage row |
| `outcome` | CSV | `success,error,banned,denied` — OR within param |
| `interactionType` | CSV | `command,button,modal,string_select` |
| `command` | string | exact match on `command_usage.command` |
| `guildId` | string | exact match on `command_usage.guild_id` |
| `windowDays` | int | `1`, `7`, `30`, `90` (default `90`); narrower than 90 because `command_usage` already lives in a 90-day window |
| `page` / `pageSize` | int | unchanged (default 25, cap 100) |

Response gains `targets: Target[]` per item and `targetTotal` (only when truncated; otherwise omitted).

There are two distinct caps in this design — they serve different jobs:

- **Storage cap (500)** — the bot+backend will persist at most 500 `command_usage_targets` rows per `command_usage` row. Anything beyond 500 is dropped at write time. Protects the table from a pathological `/announce` to a 5,000-person role.
- **Inline read cap (100)** — list responses embed at most 100 targets per row. When the stored count exceeds 100, `targetTotal` is set so the UI knows to lazy-fetch the remainder via `…/command-usage/{usageId}/targets`. Protects the list payload from N×100KB JSON blobs when the user has many large fan-outs in the same page.

### `GET /api/app/admin/users/{id}/command-usage/filter-options`

```json
{
  "commands": ["track", "announce", "player", ...],
  "guilds":   [
    { "guildId": "123…", "name": "RustLens HQ", "iconHash": "abc…" },
    ...
  ]
}
```

Populated from the user's last 90 days of `command_usage` joined to `bot_guilds`. Used to populate the command + guild comboboxes in one round-trip.

### `GET /api/app/admin/users/{id}/command-usage/{usageId}/targets`

Returns the full target list for one record (no inline cap). Used by the expanded-row "Recipients" grid when `targetTotal > 100`.

### Read-time enrichment

A response `Target` is the snapshot row plus optional live fields:

```go
type AdminTarget struct {
    Kind        string  `json:"kind"`
    Snowflake   string  `json:"snowflake"`
    Relation    string  `json:"relation"`
    DisplayName *string `json:"displayName"` // snapshot OR live (live wins)
    AvatarHash  *string `json:"avatarHash"`  // snapshot OR live (live wins)
    AppUserID   *string `json:"appUserId"`   // only for kind='user' if app_users hit
}
```

- `kind='user'` LEFT JOIN `app_users ON discord_id = snowflake` → if found, live `appUserId`, live display, live avatar.
- `kind='guild'` LEFT JOIN `bot_guilds` → live name + icon.
- `kind='role'` / `'channel'` / `'steam'` — no live source; snapshot is canonical.

### SQL shape

The list query is a CTE: first paginate `command_usage` with the filters, then `LEFT JOIN LATERAL` aggregate `command_usage_targets` rows as JSON, plus the enrichment LEFT JOINs. Join cost is bounded to `pageSize`, not the 90-day window.

The `q` filter uses `EXISTS (SELECT 1 FROM command_usage_targets t WHERE t.command_usage_id = cu.id AND (t.display_name ILIKE $q OR t.snowflake ILIKE $q))` OR-ed against the column matches on `cu`.

## 4 · Frontend UI

`UsageSection` is rebuilt to match `ServersSection` patterns (sortable table, search, filter chips, URL-driven state).

### Layout

1. **Header** — unchanged title + subtitle.
2. **Summary cards** — unchanged 3-up (Total / Error rate / Top commands).
3. **Filter bar** (flex-wrap row):
   - `Input` — "Search command, target, recipient…" (`max-w-md`)
   - `ToggleGroup` chips — Outcome: All / Success / Error / Banned / Denied
   - `Select` — Type: All / Command / Button / Modal / Select
   - `Combobox` — Command (autocomplete from `filter-options.commands`)
   - `Combobox` — Guild (autocomplete with icons from `filter-options.guilds`)
   - `Select` — Window: 1d / 7d / 30d / 90d
   - All controls wrap to a second row on narrow screens (no "More filters" popover)
4. **Result line** — `"N of M shown"` + active-filter chips with × to clear individually + "Clear all" link.
5. **`Table`** — sortable columns using the existing `SortHeader` from servers-section (extracted to a shared helper):

| Column | Sortable | Content |
|---|---|---|
| When | yes (default desc) | `<RelativeTime iso={createdAt} />` |
| Type | no | `<Badge variant="outline">{interactionType}</Badge>` |
| Command | yes (page-local) | `<code>/{command}{subcommand && ` ${subcommand}`}</code>` |
| Target / Recipients | no | see below |
| Outcome | yes | existing variant logic |
| Latency | yes (page-local) | `tabular-nums` ms |

### Target/Recipients cell

- No targets → `target` text if present, else "—".
- One subject → single avatar+name chip via `UserLink` (links to `/admin/users/{appUserId}` when resolved).
- One scope, no recipients → small chip (`role: @Members`, `guild: RustLens HQ`).
- Recipients ≥ 1 → **stacked avatar cluster** wrapped in `<HoverCard>`:
  - Up to 5 `<Avatar>` rendered overlapping (`-ml-2 ring-2 ring-background`).
  - When `recipients.length > 5`, the trailing avatar is a `+N` count badge.
  - Hovering opens a scrollable list of every recipient — full avatar + name + link (when resolvable).
  - On touch devices the hover-card primitive opens on tap; the expanded-row grid below remains the canonical full view.

### Row expansion (click row to toggle)

- **Guild row** — `bot_guilds` icon + name + copy-on-click guild ID (same pattern as servers-section).
- **"What they put in"** — existing JSON `<pre>` block (unchanged).
- **Targets** — sub-grouped by relation:
  - `Subject` → single chip with `UserLink`
  - `Scope` → kind-labelled chip (e.g., `role @Members`)
  - `Recipients` → full grid; app-user recipients link to `/admin/users/{appUserId}`, unresolved Discord users render as `<code>` snowflake. If `targetTotal > 100`, lazy-fetch the full list via the new `/targets` sub-endpoint on first expand.
- **Outcome row** — outcome badge + errorCode + latency, right-aligned.

### URL state

Filters live in querystring (`?usageQ=…&usageOutcome=error&usageGuildId=…&usagePage=2#usage`). Pattern:

- The route page reads `searchParams` and passes filter values into `getAdminUserCommandUsage(userId, filters)`.
- `UsageSection` is a client component; filter controls call `router.replace(…)` with the new querystring; the server roundtrip re-renders with filtered data.
- The existing `usagePage` querystring keeps its semantics; the pagination links carry the active filters.

### Sort

- **When** and **Outcome** sort server-side (`ORDER BY created_at`, `ORDER BY outcome` with a stable tiebreaker on `id`).
- **Command** and **Latency** sort client-side over the already-loaded 25 items (cheap; admin tool).

## Files touched

### Backend
- `apps/backend/internal/store/postgres.go` — add `command_usage_targets` to `EnsureSchema()` block (CREATE TABLE + indexes).
- `apps/backend/internal/store/command_usage.go` — extend `CommandUsageInsert` with `Targets []CommandUsageTargetInsert`; add `InsertCommandUsage` transaction that inserts the row + cascades targets; extend `ListUserCommandUsage` to accept filter params and embed targets via the lateral-join CTE; add `ListCommandUsageTargets(usageID)`.
- `apps/backend/internal/store/command_usage_test.go` — coverage for filter combinations and target embedding.
- `apps/backend/internal/http/admin_command_usage_handlers.go` — parse filter params, call enriched list, add `filter-options` + `/{usageId}/targets` sub-routes.
- `apps/backend/internal/http/admin_read_handlers.go` / `router.go` — register new sub-routes.
- `apps/backend/internal/http/admin_command_usage_handlers_test.go` — filter happy-path + targets enrichment.

### Bot
- `apps/bot/src/command-usage.ts` — extend `CommandUsagePayload` with `targets?: Target[]`; helper for callers to push snapshots.
- `apps/bot/src/backend-client.ts` — forward the field.
- `apps/bot/src/commands/{announce,summon,nickname,admin,setsteam,transfer,track,manage}.ts` and `create.ts` — collect `targets[]` and pass to `recordCommandUsage`.
- `apps/bot/src/command-usage.test.ts` — assert targets propagate.

### Frontend
- `apps/frontend/lib/rustlens-api.ts` — extend `AdminCommandUsageItem` with `targets: AdminTarget[]` + `targetTotal?: number`; extend `getAdminUserCommandUsage` to accept filters; add `getAdminUserCommandUsageFilterOptions` and `getAdminUserCommandUsageTargets`.
- `apps/frontend/app/admin/users/[userId]/page.tsx` — read filter querystring; thread filters into API call.
- `apps/frontend/app/admin/users/_components/usage-section.tsx` — full rewrite to table layout, filter bar, stacked-avatar hover-card, expanded panel with grouped targets.
- New: `apps/frontend/app/admin/users/_components/usage-recipient-stack.tsx` — the stacked avatars + HoverCard list.
- New: `apps/frontend/app/admin/users/_components/usage-filter-bar.tsx` — the filter row.
- Reuse: `SortHeader` extracted from `servers-section.tsx` into `apps/frontend/components/admin/sort-header.tsx`.

### Docs
- `AGENTS.md` — update the `command_usage` paragraph and the admin route bullet for `…/command-usage` to mention new params, `targets[]`, the `filter-options` and `/targets` sub-endpoints, and the new `command_usage_targets` table.

## Test plan

### Backend
- `command_usage_targets` schema bootstraps via `EnsureSchema`.
- Insert with `Targets` writes parent + children in one tx; rollback on child failure.
- Filter combinations (outcome+command+window, q matching across columns + targets, guild filter) return expected rows.
- `q` ILIKE matches `display_name` AND `snowflake` AND the existing command columns.
- Target inline cap (100) honored; `targetTotal` set only when truncated.
- `/targets` sub-endpoint returns the full list ordered by `relation` then `id`.
- `filter-options` returns only distinct commands/guilds present in this user's window.

### Bot
- Each fan-out command builds the expected `targets[]` shape (kind/relation/snapshot fields).
- Partial failures (some DMs fail in `/announce`) only log the successful recipients.
- `targets` omitted from the payload for non-fan-out commands (unchanged behavior).

### Frontend
- Renders existing summary cards unchanged.
- Filter controls drive URL state and re-fetch.
- Stacked-avatar cluster collapses at 5+ and hover-card shows full list.
- Expanded row groups targets by relation; lazy-fetches `/targets` when `targetTotal` is set.
- `usageSection.test.tsx` — null/empty `targets` array still renders the row.
