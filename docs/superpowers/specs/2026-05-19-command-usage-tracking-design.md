# Bot Command Usage Tracking

**Date:** 2026-05-19
**Status:** Approved for implementation
**Surface:** `/admin/users/{id}` → Usage tab

## Problem

The admin user detail page has a Usage tab with a "not yet available" placeholder. We need to track every interaction a Discord user makes with the RustLens bot — chat commands, button presses, modal submits, and string-select menus — and surface that history on the admin Usage tab so operators can see exactly what each user has done, what input they provided, and what happened as a result.

## Scope

**In scope**
- Discord bot interactions: `ChatInputCommand`, `Button`, `ModalSubmit`, `StringSelectMenu`
- Per-invocation capture: who, where, what input, outcome, latency, raw payload
- 90-day rolling retention
- Admin read-only UI on `/admin/users/{id}` Usage tab — paginated table + summary strip

**Out of scope**
- Frontend / website actions (already covered by `app_audit_events` on the Audit tab)
- Autocomplete events (too noisy — fire on every keystroke)
- Editing or exporting usage records
- Cross-user search / analytics across the platform

## Data model

New auto-migrated table `command_usage`, defined in `EnsureSchema()`:

```sql
CREATE TABLE command_usage (
  id               BIGSERIAL PRIMARY KEY,
  app_user_id      TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
  discord_id       TEXT NOT NULL,
  guild_id         TEXT,                           -- null when DM / global
  channel_id       TEXT,
  interaction_type TEXT NOT NULL,                  -- 'command' | 'button' | 'modal' | 'string_select'
  command          TEXT NOT NULL,                  -- slash command name or customId prefix
  subcommand       TEXT,                           -- chat-input subcommand, when present
  target           TEXT,                           -- primary identifier (steamId, group, server bmId, customId suffix)
  options          JSONB NOT NULL DEFAULT '{}'::jsonb,  -- type-appropriate raw input payload
  outcome          TEXT NOT NULL,                  -- 'success' | 'error' | 'banned' | 'denied'
  error_code       TEXT,                           -- short code or first ~256 chars of error message
  latency_ms       INTEGER NOT NULL,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX command_usage_user_created_idx ON command_usage (app_user_id, created_at DESC);
CREATE INDEX command_usage_created_idx       ON command_usage (created_at DESC);
```

### Field semantics by interaction type

| Type            | `command`            | `subcommand`        | `target`                | `options` JSONB                                  |
|-----------------|----------------------|---------------------|-------------------------|--------------------------------------------------|
| `command`       | slash command name   | subcommand path     | primary option value    | `{ option_name: value, ... }`                    |
| `button`        | customId prefix      | —                   | customId suffix         | `{ customId: "track:add:abc123" }`               |
| `modal`         | customId prefix      | —                   | customId suffix         | `{ field_id: value, ... }` from ModalSubmit fields |
| `string_select` | customId prefix      | —                   | customId suffix         | `{ customId, values: ["..."] }`                  |

**Prefix convention:** customId is split on the first `:` — left half becomes `command`, the rest becomes `target`. Falls back to the full customId in `command` when there is no `:`.

### `app_user_id` guarantee

The bot calls `ensureAppUser` at the top of every `interactionCreate` (`apps/bot/src/shard-worker.ts:336`), which idempotently inserts an `app_users` row keyed on Discord ID. By the time any handler dispatches, the row exists. The write endpoint resolves `app_user_id` via `appauth.FindByDiscordID` and drops the write with a log line if the lookup misses (race-safe fallback rather than blocking the response).

### Retention

The existing `internal/cleanup/` service gains a nightly task: `DELETE FROM command_usage WHERE created_at < NOW() - INTERVAL '90 days'`. Pattern matches existing cleanup tasks; the `command_usage_created_idx` supports the predicate.

## Backend write endpoint

`POST /api/internal/bot/setup/command-usage` — JWT-protected, mounted with the other bot-internal `/setup/*` routes in `internal/http/router.go`.

Handler: `internal/http/bot_command_usage_handlers.go::RecordCommandUsage`
Store:   `internal/store/command_usage.go::InsertCommandUsage`

**Request body:**

```json
{
  "discordId":       "snowflake (required)",
  "guildId":         "snowflake | null",
  "channelId":       "snowflake | null",
  "interactionType": "command | button | modal | string_select",
  "command":         "string (required)",
  "subcommand":      "string | null",
  "target":          "string | null",
  "options":         { },
  "outcome":         "success | error | banned | denied",
  "errorCode":       "string | null (capped 256 chars server-side)",
  "latencyMs":       0
}
```

**Validation:**
- `discordId` must be a valid Discord snowflake (existing `validateSnowflakeValue`)
- `interactionType` and `outcome` must be in their enums
- `command` non-empty, ≤ 128 chars
- `errorCode` truncated to 256 chars
- `options` rejected if JSON encoded payload > 16 KiB (defensive cap; our commands won't approach it)

**Responses:**
- `204 No Content` on success
- `400` on validation failure
- `204` (with log) when Discord ID does not resolve to an `app_users` row — write silently dropped so the bot response path is never blocked

## Bot integration

New module `apps/bot/src/command-usage.ts`:

```ts
export type Outcome = 'success' | 'error' | 'banned' | 'denied';

export function buildCommandUsagePayload(
  interaction: Interaction,
  outcome: Outcome,
  errorCode: string | null,
  latencyMs: number,
): CommandUsagePayload | null;

export function recordCommandUsage(
  interaction: Interaction,
  outcome: Outcome,
  errorCode: string | null,
  startedAt: number,
): void;  // fire-and-forget; logs on failure, never throws
```

`buildCommandUsagePayload` handles each interaction type:

- `isChatInputCommand`: `command = commandName`, `subcommand = getSubcommand(false) ?? null`, `target = first option value (string-ish)`, `options = all option name→value`
- `isButton`: split `customId` on first `:` → `command` / `target`; `options = { customId }`
- `isModalSubmit`: split `customId` on first `:` → `command` / `target`; `options` collects every `TextInputComponent` field id → value via `interaction.fields.fields.entries()`
- `isStringSelectMenu`: split `customId`; `options = { customId, values: interaction.values }`
- `isAutocomplete`: returns `null` (skipped)
- Any unknown subtype: returns `null` (skipped, no error)

Wired into `shard-worker.ts` `interactionCreate`:

1. Capture `startedAt = Date.now()` at the very top, after `ensureAppUser`.
2. Wrap the existing dispatch in `try { ... await ... } catch (err) { ... }` (the dispatcher already has structure for this).
3. Resolve outcome:
   - `enforceBan` / `enforceBanForAutocomplete` short-circuit → `outcome = 'banned'`
   - Permission/owner-guard reject → `outcome = 'denied'`
   - Handler throws → `outcome = 'error'`, `errorCode = err.message.slice(0, 256)`
   - Handler returns normally → `outcome = 'success'`
4. `void recordCommandUsage(interaction, outcome, errorCode, startedAt)` — no `await`, so the user response is never delayed.

The bot's existing `backend-client.ts` gains `recordCommandUsage(workerId, payload)`. Failures are logged with structured fields; they do not propagate.

## Backend read endpoint

`GET /api/app/admin/users/{id}/command-usage?page=1&pageSize=25`

- Mounted in the existing admin router section in `internal/http/router.go`
- Auth: same gating as the rest of the admin user detail page reads
- Default `pageSize = 25`, capped at 100
- Returns rows ordered by `created_at DESC` for the target `app_user_id`

Handler: `internal/http/admin_command_usage_handlers.go::AdminGetUserCommandUsage`
Store:   `internal/store/command_usage.go::ListUserCommandUsage` + `SummarizeUserCommandUsage`

**Response shape:**

```json
{
  "items": [{
    "id":              "1234",
    "interactionType": "command",
    "command":         "track",
    "subcommand":      "create",
    "target":          "raid-night",
    "guildId":         "...",
    "channelId":       "...",
    "outcome":         "success",
    "errorCode":       null,
    "latencyMs":       142,
    "options":         { "group_name": "raid-night", "server": "12345" },
    "createdAt":       "2026-05-19T12:34:56Z"
  }],
  "total":    1234,
  "page":     1,
  "pageSize": 25,
  "summary": {
    "totalCount":   1234,
    "errorCount":   12,
    "errorRate":    0.0097,
    "topCommands":  [
      { "command": "player", "count": 420 },
      { "command": "track",  "count": 310 }
    ]
  }
}
```

Summary is always computed over the full 90-day window for the user (not the current page). One aggregate query, capped at `topCommands` length 5.

## Frontend

### Server-side fetch

`apps/frontend/app/admin/users/[id]/page.tsx` — add a parallel fetch for `commandUsage` alongside the existing detail / audit fetches and pass it into `UsageSection`.

### `usage-section.tsx` rewrite

Focus per user direction: **show what the user put in and what happened after.** No latency-heavy or operator-debug detail in the default view; raw payload available on row expand.

Layout:

1. **Summary strip** — three small cards across the top:
   - Total invocations (last 90 days)
   - Error rate (`errorCount / totalCount`, formatted as %)
   - Top commands (badges with count)
2. **Paginated table** below:
   - Columns: timestamp · type badge · command · input summary · outcome badge
   - "Input summary" is a one-line preview of `options` — for chat commands it joins `key=value` pairs; for modals it shows the field values; for buttons it shows the customId; for string-selects it shows the selected values. Truncated with ellipsis if too long.
   - Outcome rendered as colored badge: success (green/secondary), error (destructive), banned (destructive), denied (muted)
   - Row click expands an inline detail panel showing:
     - **What they put in** — pretty-printed `options` JSON
     - **What happened** — `outcome` + `errorCode` (if any)
3. **Empty state** — "No bot usage in the last 90 days."
4. Pagination via `?usagePage=N` URL param (doesn't clobber other tab params).

### Typed client

`apps/frontend/lib/rustlens-api.ts`:

```ts
export type AdminCommandUsageItem = { ... }
export type AdminCommandUsageSummary = { ... }
export async function fetchAdminUserCommandUsage(
  userId: string,
  opts: { page?: number; pageSize?: number },
): Promise<{ items, total, page, pageSize, summary }>;
```

## Testing

**Backend (`apps/backend`)**
- `store/command_usage_test.go`: insert; list ordering + pagination; summary aggregates; FK cascade on `app_users` delete
- `store/cleanup_test.go`: extended for 90-day purge of `command_usage`
- `http/bot_command_usage_handlers_test.go`: JWT required; validation paths; missing app_user → 204 + log; happy path inserts a row
- `http/admin_command_usage_handlers_test.go`: admin auth gate; pagination params; summary in response

**Bot (`apps/bot`)**
- `command-usage.test.ts`: payload extraction for each of the 4 interaction subtypes; autocomplete returns `null`; unknown subtype returns `null`; customId prefix split; field-value extraction for modals
- `command-usage.test.ts`: `recordCommandUsage` does not await, logs on backend failure, does not throw
- `shard-worker.test.ts` (or local equivalent): outcome mapping for banned / denied / error / success paths

**Frontend (`apps/frontend`)**
- `usage-section.test.tsx`: empty state; summary strip renders; table rows render input-summary preview for each type; row expansion reveals options JSON; outcome badge variant matches outcome

## File-level changes

**New files**
- `apps/backend/internal/store/command_usage.go`
- `apps/backend/internal/store/command_usage_test.go`
- `apps/backend/internal/http/bot_command_usage_handlers.go`
- `apps/backend/internal/http/bot_command_usage_handlers_test.go`
- `apps/backend/internal/http/admin_command_usage_handlers.go`
- `apps/backend/internal/http/admin_command_usage_handlers_test.go`
- `apps/bot/src/command-usage.ts`
- `apps/bot/src/command-usage.test.ts`

**Modified files**
- `apps/backend/internal/store/postgres.go` — extend `EnsureSchema()` with the `command_usage` table + indexes
- `apps/backend/internal/cleanup/` — add the 90-day purge task
- `apps/backend/internal/http/router.go` — mount write + read endpoints
- `apps/backend/internal/http/openapi.go` — document new endpoints
- `apps/bot/src/backend-client.ts` — add `recordCommandUsage(workerId, payload)`
- `apps/bot/src/shard-worker.ts` — wrap dispatcher with timing + outcome capture + fire-and-forget record
- `apps/frontend/app/admin/users/[id]/page.tsx` — fetch command usage and pass to `UsageSection`
- `apps/frontend/app/admin/users/_components/usage-section.tsx` — replace placeholder with summary + table + expandable rows
- `apps/frontend/lib/rustlens-api.ts` — typed client + types
- `AGENTS.md` — update schema + endpoint listings to mention `command_usage`

## Non-goals / explicitly deferred

- Cross-user usage analytics, dashboards, or aggregate reports
- Exporting usage data (CSV / JSON)
- Editing or redacting usage rows
- Tracking autocomplete interactions
- Sampling / rate-limiting writes (90-day window expected to stay well below problematic row counts; revisit if it doesn't)
