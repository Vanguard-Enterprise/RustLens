# Bot Command Usage Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Track every Discord bot interaction (chat command, button, modal submit, string-select) a user makes, persist it for 90 days, and surface it on the `/admin/users/{id}` Usage tab with the input and outcome of each invocation.

**Architecture:** New `command_usage` Postgres table (FK to `app_users`), a JWT-protected bot-internal write endpoint, a session-authed admin read endpoint, a nightly cleanup task for the 90-day window, and a bot-side fire-and-forget recorder wired into `interactionCreate`. Frontend replaces the placeholder `UsageSection` with a summary strip + paginated table that shows the user's input and result.

**Tech Stack:** Go 1.24 (chi, pgx, pgxpool), TypeScript (discord.js, vitest), Next.js 16 App Router + React 19 + Tailwind v4 + shadcn/ui.

**Spec:** `docs/superpowers/specs/2026-05-19-command-usage-tracking-design.md`

---

## File Map

**Backend — new**
- `apps/backend/internal/store/command_usage.go` — store CRUD + summary
- `apps/backend/internal/store/command_usage_test.go` — store tests
- `apps/backend/internal/http/bot_command_usage_handlers.go` — JWT-protected write handler
- `apps/backend/internal/http/bot_command_usage_handlers_test.go` — handler tests
- `apps/backend/internal/http/admin_command_usage_handlers.go` — admin read handler
- `apps/backend/internal/http/admin_command_usage_handlers_test.go` — handler tests

**Backend — modified**
- `apps/backend/internal/store/postgres.go` — extend `EnsureSchema()` with `command_usage` table + indexes
- `apps/backend/internal/cleanup/service.go` — add 90-day `command_usage` purge
- `apps/backend/internal/cleanup/service_test.go` — extend with purge test
- `apps/backend/internal/http/router.go` — mount write + read endpoints
- `apps/backend/internal/appauth/repo.go` — add `FindByDiscordID` helper (does not auto-create)
- `apps/backend/internal/http/openapi.go` — document the two new endpoints

**Bot — new**
- `apps/bot/src/command-usage.ts` — payload extraction + `recordCommandUsage` fire-and-forget
- `apps/bot/src/command-usage.test.ts` — extraction + outcome tests

**Bot — modified**
- `apps/bot/src/backend-client.ts` — add `recordCommandUsage(workerId, payload)`
- `apps/bot/src/backend-client.test.ts` — extend with client test
- `apps/bot/src/shard-worker.ts` — wrap interaction dispatcher with timing + outcome capture

**Frontend — modified**
- `apps/frontend/lib/rustlens-api.ts` — types + `getAdminUserCommandUsage`
- `apps/frontend/app/admin/users/[id]/page.tsx` — server-side fetch for `commandUsage` + pass to `UsageSection`
- `apps/frontend/app/admin/users/_components/usage-section.tsx` — replace placeholder
- `apps/frontend/app/admin/users/_components/__tests__/usage-section.test.tsx` — new vitest

**Docs**
- `AGENTS.md` — add schema + endpoint entries

---

## Task 1: Create `command_usage` schema in `EnsureSchema`

**Files:**
- Modify: `apps/backend/internal/store/postgres.go` — append a new block at the end of `EnsureSchema` (after the `app_audit_events` block, just before `return nil`)

- [ ] **Step 1: Write the schema test**

Append to `apps/backend/internal/store/postgres_schema_test.go` (matching existing schema test style — if file has a `tablesExist` helper or similar, use it; otherwise add a focused test):

```go
func TestEnsureSchema_CreatesCommandUsageTable(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	defer pool.Close()

	s := NewWithPool(pool)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'command_usage'
		)`).Scan(&exists); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !exists {
		t.Fatal("command_usage table not created")
	}

	// indexes
	for _, idx := range []string{"command_usage_user_created_idx", "command_usage_created_idx"} {
		var ok bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE schemaname = 'public' AND indexname = $1
			)`, idx).Scan(&ok); err != nil {
			t.Fatalf("index %s query: %v", idx, err)
		}
		if !ok {
			t.Fatalf("expected index %s", idx)
		}
	}
}
```

If the existing test file uses different helpers (look at `newTestPool` / `NewWithPool` names already in the file and match them), adapt the helper names but keep the assertions.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd apps/backend && go test ./internal/store/ -run TestEnsureSchema_CreatesCommandUsageTable -v
```

Expected: FAIL — `command_usage table not created`.

- [ ] **Step 3: Add the schema block**

In `apps/backend/internal/store/postgres.go`, immediately before the final `return nil` of `EnsureSchema`, add:

```go
	if _, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS command_usage (
			id               BIGSERIAL PRIMARY KEY,
			app_user_id      TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			discord_id       TEXT NOT NULL,
			guild_id         TEXT,
			channel_id       TEXT,
			interaction_type TEXT NOT NULL,
			command          TEXT NOT NULL,
			subcommand       TEXT,
			target           TEXT,
			options          JSONB NOT NULL DEFAULT '{}'::jsonb,
			outcome          TEXT NOT NULL,
			error_code       TEXT,
			latency_ms       INTEGER NOT NULL,
			created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS command_usage_user_created_idx
		ON command_usage (app_user_id, created_at DESC);

		CREATE INDEX IF NOT EXISTS command_usage_created_idx
		ON command_usage (created_at DESC);
	`); err != nil {
		return fmt.Errorf("create command_usage table: %w", err)
	}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd apps/backend && go test ./internal/store/ -run TestEnsureSchema_CreatesCommandUsageTable -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/store/postgres.go apps/backend/internal/store/postgres_schema_test.go
git commit -m "feat(backend): add command_usage table to EnsureSchema"
```

---

## Task 2: Store layer — `InsertCommandUsage`

**Files:**
- Create: `apps/backend/internal/store/command_usage.go`
- Create: `apps/backend/internal/store/command_usage_test.go`

- [ ] **Step 1: Write the failing insert + list test**

Create `apps/backend/internal/store/command_usage_test.go`:

```go
package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestCommandUsage_InsertAndList(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	defer pool.Close()

	s := NewWithPool(pool)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// seed an app_users row (FK target)
	if _, err := pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_test1', '111', 'tester', 'tester')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	opts, _ := json.Marshal(map[string]any{"group_name": "raid"})
	row := CommandUsageInsert{
		AppUserID:       "usr_test1",
		DiscordID:       "111",
		GuildID:         strPtr("g1"),
		ChannelID:       strPtr("c1"),
		InteractionType: "command",
		Command:         "track",
		Subcommand:      strPtr("create"),
		Target:          strPtr("raid"),
		Options:         opts,
		Outcome:         "success",
		ErrorCode:       nil,
		LatencyMs:       142,
	}
	if err := s.InsertCommandUsage(ctx, row); err != nil {
		t.Fatalf("InsertCommandUsage: %v", err)
	}

	page, err := s.ListUserCommandUsage(ctx, "usr_test1", 25, 0)
	if err != nil {
		t.Fatalf("ListUserCommandUsage: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("expected total=1 got %d", page.Total)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected 1 item got %d", len(page.Items))
	}
	it := page.Items[0]
	if it.Command != "track" || it.Outcome != "success" || it.LatencyMs != 142 {
		t.Fatalf("unexpected row: %+v", it)
	}
}

func TestCommandUsage_PaginationAndOrder(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	defer pool.Close()

	s := NewWithPool(pool)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_test2', '222', 'tester2', 'tester2')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for i := 0; i < 5; i++ {
		row := CommandUsageInsert{
			AppUserID:       "usr_test2",
			DiscordID:       "222",
			InteractionType: "command",
			Command:         "player",
			Options:         []byte("{}"),
			Outcome:         "success",
			LatencyMs:       i,
		}
		if err := s.InsertCommandUsage(ctx, row); err != nil {
			t.Fatalf("InsertCommandUsage %d: %v", i, err)
		}
		// ensure created_at ordering deterministic
		time.Sleep(2 * time.Millisecond)
	}

	page, err := s.ListUserCommandUsage(ctx, "usr_test2", 2, 0)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if page.Total != 5 || len(page.Items) != 2 {
		t.Fatalf("page1 expected total=5 len=2 got total=%d len=%d", page.Total, len(page.Items))
	}
	// newest first → latency 4 then 3
	if page.Items[0].LatencyMs != 4 || page.Items[1].LatencyMs != 3 {
		t.Fatalf("page1 wrong order: %+v", page.Items)
	}

	page2, err := s.ListUserCommandUsage(ctx, "usr_test2", 2, 2)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Items) != 2 || page2.Items[0].LatencyMs != 2 || page2.Items[1].LatencyMs != 1 {
		t.Fatalf("page2 wrong: %+v", page2.Items)
	}
}

func TestCommandUsage_Summary(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	defer pool.Close()

	s := NewWithPool(pool)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_test3', '333', 'tester3', 'tester3')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mk := func(cmd string, outcome string) CommandUsageInsert {
		return CommandUsageInsert{
			AppUserID:       "usr_test3",
			DiscordID:       "333",
			InteractionType: "command",
			Command:         cmd,
			Options:         []byte("{}"),
			Outcome:         outcome,
			LatencyMs:       1,
		}
	}
	for i := 0; i < 3; i++ {
		_ = s.InsertCommandUsage(ctx, mk("player", "success"))
	}
	_ = s.InsertCommandUsage(ctx, mk("track", "success"))
	_ = s.InsertCommandUsage(ctx, mk("track", "error"))

	sum, err := s.SummarizeUserCommandUsage(ctx, "usr_test3", 90*24*time.Hour)
	if err != nil {
		t.Fatalf("SummarizeUserCommandUsage: %v", err)
	}
	if sum.TotalCount != 5 || sum.ErrorCount != 1 {
		t.Fatalf("wrong totals: %+v", sum)
	}
	if len(sum.TopCommands) == 0 || sum.TopCommands[0].Command != "player" || sum.TopCommands[0].Count != 3 {
		t.Fatalf("wrong top commands: %+v", sum.TopCommands)
	}
}

func TestCommandUsage_CascadeOnUserDelete(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	defer pool.Close()

	s := NewWithPool(pool)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_test4', '444', 'tester4', 'tester4')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = s.InsertCommandUsage(ctx, CommandUsageInsert{
		AppUserID: "usr_test4", DiscordID: "444",
		InteractionType: "command", Command: "player",
		Options: []byte("{}"), Outcome: "success", LatencyMs: 1,
	})

	if _, err := pool.Exec(ctx, `DELETE FROM app_users WHERE id='usr_test4'`); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var count int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM command_usage WHERE app_user_id='usr_test4'`).Scan(&count)
	if count != 0 {
		t.Fatalf("expected cascade delete, got %d rows", count)
	}
}

func strPtr(s string) *string { return &s }
```

If `newTestPool`/`NewWithPool` helpers don't exist with those exact names in `store/`, use the matching helpers from any existing `*_test.go` file in the package — match the existing pattern.

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd apps/backend && go test ./internal/store/ -run TestCommandUsage -v
```

Expected: FAIL — `undefined: CommandUsageInsert`, `s.InsertCommandUsage undefined`, etc.

- [ ] **Step 3: Implement the store**

Create `apps/backend/internal/store/command_usage.go`:

```go
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CommandUsageInsert is the write-side input for a single command/interaction
// invocation. AppUserID is a hard FK; the caller must resolve it before
// calling InsertCommandUsage.
type CommandUsageInsert struct {
	AppUserID       string
	DiscordID       string
	GuildID         *string
	ChannelID       *string
	InteractionType string // 'command' | 'button' | 'modal' | 'string_select'
	Command         string
	Subcommand      *string
	Target          *string
	Options         json.RawMessage // JSONB; must be a valid JSON object
	Outcome         string          // 'success' | 'error' | 'banned' | 'denied'
	ErrorCode       *string
	LatencyMs       int
}

// CommandUsageRow is the read-side shape returned to handlers.
type CommandUsageRow struct {
	ID              int64           `json:"id"`
	AppUserID       string          `json:"appUserId"`
	DiscordID       string          `json:"discordId"`
	GuildID         *string         `json:"guildId"`
	ChannelID       *string         `json:"channelId"`
	InteractionType string          `json:"interactionType"`
	Command         string          `json:"command"`
	Subcommand      *string         `json:"subcommand"`
	Target          *string         `json:"target"`
	Options         json.RawMessage `json:"options"`
	Outcome         string          `json:"outcome"`
	ErrorCode       *string         `json:"errorCode"`
	LatencyMs       int             `json:"latencyMs"`
	CreatedAt       time.Time       `json:"createdAt"`
}

type CommandUsagePage struct {
	Items []CommandUsageRow
	Total int64
}

type CommandUsageTopCommand struct {
	Command string `json:"command"`
	Count   int64  `json:"count"`
}

type CommandUsageSummary struct {
	TotalCount  int64                    `json:"totalCount"`
	ErrorCount  int64                    `json:"errorCount"`
	TopCommands []CommandUsageTopCommand `json:"topCommands"`
}

func (p *PostgresStore) InsertCommandUsage(ctx context.Context, row CommandUsageInsert) error {
	if len(row.Options) == 0 {
		row.Options = []byte("{}")
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO command_usage (
			app_user_id, discord_id, guild_id, channel_id,
			interaction_type, command, subcommand, target,
			options, outcome, error_code, latency_ms
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12)
	`,
		row.AppUserID, row.DiscordID, row.GuildID, row.ChannelID,
		row.InteractionType, row.Command, row.Subcommand, row.Target,
		string(row.Options), row.Outcome, row.ErrorCode, row.LatencyMs,
	)
	if err != nil {
		return fmt.Errorf("insert command_usage: %w", err)
	}
	return nil
}

func (p *PostgresStore) ListUserCommandUsage(ctx context.Context, appUserID string, limit, offset int) (CommandUsagePage, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var page CommandUsagePage
	if err := p.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM command_usage WHERE app_user_id = $1
	`, appUserID).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count command_usage: %w", err)
	}
	if page.Total == 0 {
		return page, nil
	}

	rows, err := p.pool.Query(ctx, `
		SELECT id, app_user_id, discord_id, guild_id, channel_id,
		       interaction_type, command, subcommand, target,
		       options, outcome, error_code, latency_ms, created_at
		FROM command_usage
		WHERE app_user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, appUserID, limit, offset)
	if err != nil {
		return page, fmt.Errorf("list command_usage: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var r CommandUsageRow
		if err := rows.Scan(
			&r.ID, &r.AppUserID, &r.DiscordID, &r.GuildID, &r.ChannelID,
			&r.InteractionType, &r.Command, &r.Subcommand, &r.Target,
			&r.Options, &r.Outcome, &r.ErrorCode, &r.LatencyMs, &r.CreatedAt,
		); err != nil {
			return page, fmt.Errorf("scan command_usage: %w", err)
		}
		page.Items = append(page.Items, r)
	}
	return page, rows.Err()
}

func (p *PostgresStore) SummarizeUserCommandUsage(ctx context.Context, appUserID string, window time.Duration) (CommandUsageSummary, error) {
	var sum CommandUsageSummary
	cutoff := time.Now().Add(-window)

	if err := p.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE outcome = 'error') AS errors
		FROM command_usage
		WHERE app_user_id = $1 AND created_at >= $2
	`, appUserID, cutoff).Scan(&sum.TotalCount, &sum.ErrorCount); err != nil {
		return sum, fmt.Errorf("summarize command_usage: %w", err)
	}
	if sum.TotalCount == 0 {
		return sum, nil
	}

	rows, err := p.pool.Query(ctx, `
		SELECT command, COUNT(*) AS c
		FROM command_usage
		WHERE app_user_id = $1 AND created_at >= $2
		GROUP BY command
		ORDER BY c DESC, command ASC
		LIMIT 5
	`, appUserID, cutoff)
	if err != nil {
		return sum, fmt.Errorf("top commands command_usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tc CommandUsageTopCommand
		if err := rows.Scan(&tc.Command, &tc.Count); err != nil {
			return sum, fmt.Errorf("scan top command: %w", err)
		}
		sum.TopCommands = append(sum.TopCommands, tc)
	}
	return sum, rows.Err()
}

// PurgeCommandUsageBefore deletes rows older than cutoff. Returns rows affected.
// Used by internal/cleanup.
func (p *PostgresStore) PurgeCommandUsageBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := p.pool.Exec(ctx, `DELETE FROM command_usage WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge command_usage: %w", err)
	}
	return tag.RowsAffected(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd apps/backend && go test ./internal/store/ -run TestCommandUsage -v
```

Expected: PASS (all 4 tests).

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/store/command_usage.go apps/backend/internal/store/command_usage_test.go
git commit -m "feat(backend): add command_usage store with list, summary, purge"
```

---

## Task 3: Cleanup service — 90-day purge

**Files:**
- Modify: `apps/backend/internal/cleanup/service.go`
- Modify: `apps/backend/internal/cleanup/service_test.go`

- [ ] **Step 1: Write the failing purge test**

In `apps/backend/internal/cleanup/service_test.go`, follow the existing fake-store pattern. Add:

```go
func TestService_PurgesCommandUsage(t *testing.T) {
	ctx := context.Background()
	fake := &fakeStore{} // assume fake type matches existing tests; if not, mirror existing structure
	svc := NewService(fake, Config{
		Interval:                  time.Hour,
		WipeReactRetention:        7 * 24 * time.Hour,
		AnalyticsCacheRetention:   48 * time.Hour,
		CommandUsageRetention:     90 * 24 * time.Hour,
	})
	svc.runOnce(ctx)

	if fake.commandUsageCutoff.IsZero() {
		t.Fatal("expected PurgeCommandUsageBefore to be called")
	}
	since := time.Since(fake.commandUsageCutoff)
	// Should be ~90 days ± some seconds
	want := 90 * 24 * time.Hour
	if since < want-time.Minute || since > want+time.Minute {
		t.Fatalf("expected cutoff ~90d ago, got %v", since)
	}
}
```

If the existing `fakeStore` in `service_test.go` doesn't have `commandUsageCutoff` / `PurgeCommandUsageBefore`, add them following the same pattern as the existing fake methods (e.g. `PurgeWipeReactsBefore`).

- [ ] **Step 2: Run test to verify it fails**

```bash
cd apps/backend && go test ./internal/cleanup/ -run TestService_PurgesCommandUsage -v
```

Expected: FAIL — `unknown field CommandUsageRetention in struct literal` or `fake.commandUsageCutoff undefined`.

- [ ] **Step 3: Extend the Store interface, Config, and runOnce**

In `apps/backend/internal/cleanup/service.go`:

1. Add to the `Store` interface:
```go
PurgeCommandUsageBefore(ctx context.Context, cutoff time.Time) (int64, error)
```

2. Add to `Config`:
```go
CommandUsageRetention time.Duration
```

3. In `withDefaults`:
```go
if c.CommandUsageRetention <= 0 {
    c.CommandUsageRetention = 90 * 24 * time.Hour
}
```

4. In `runOnce`, append at the end:
```go
if rows, err := s.store.PurgeCommandUsageBefore(ctx, now.Add(-s.cfg.CommandUsageRetention)); err != nil {
    log.Printf("[backend] cleanup: purge command usage failed: %v", err)
} else if rows > 0 {
    log.Printf("[backend] cleanup: purged %d command_usage rows older than %s", rows, s.cfg.CommandUsageRetention)
}
```

Also update the package doc comment to mention command usage tracking.

In `service_test.go`, extend the `fakeStore` mock with:
```go
PurgeCommandUsageBeforeFunc func(ctx context.Context, cutoff time.Time) (int64, error)
commandUsageCutoff time.Time
```
and:
```go
func (f *fakeStore) PurgeCommandUsageBefore(ctx context.Context, cutoff time.Time) (int64, error) {
    f.commandUsageCutoff = cutoff
    if f.PurgeCommandUsageBeforeFunc != nil {
        return f.PurgeCommandUsageBeforeFunc(ctx, cutoff)
    }
    return 0, nil
}
```
(Match the field/method naming convention already used in the file for `WipeReacts` / `AnalyticsCache`.)

- [ ] **Step 4: Run all cleanup tests**

```bash
cd apps/backend && go test ./internal/cleanup/ -v
```

Expected: PASS (the new test and all existing tests).

- [ ] **Step 5: Wire CommandUsageRetention into cmd/server/main.go if cleanup.Config is constructed there**

Search for `cleanup.NewService(` in `apps/backend/cmd/server/`. If the call sets specific `Config` fields, leave it — defaults kick in via `withDefaults`. If you find a config struct that already exposes other retention windows as env vars, mirror the pattern for `CommandUsageRetention` (read `CLEANUP_COMMAND_USAGE_RETENTION_HOURS`, default 90*24). If not present, leave as-is — the default applies automatically.

```bash
grep -n "cleanup.NewService" apps/backend/cmd/server/*.go || true
```

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/cleanup/service.go apps/backend/internal/cleanup/service_test.go
git add apps/backend/cmd/server/  # only if changed in step 5
git commit -m "feat(backend): add 90-day command_usage purge to cleanup service"
```

---

## Task 4: appauth — `FindByDiscordID` helper

**Files:**
- Modify: `apps/backend/internal/appauth/repo.go`

The write endpoint needs to resolve `app_user_id` from `discord_id` without auto-creating. The bot's own `ensureAppUser` already creates the row.

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/appauth/repo_test.go` (create the file if it does not exist; follow the patterns in `appauth/session_test.go` for seeding `app_users`):

```go
func TestRepo_FindByDiscordID(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t) // or whichever helper the appauth tests use
	defer pool.Close()

	mustEnsureAppSchema(t, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_findbd', '999', 'finder', 'finder')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	repo := NewRepo(pool)

	id, err := repo.FindByDiscordID(ctx, "999")
	if err != nil {
		t.Fatalf("FindByDiscordID: %v", err)
	}
	if id != "usr_findbd" {
		t.Fatalf("expected usr_findbd got %q", id)
	}

	if _, err := repo.FindByDiscordID(ctx, "nonexistent"); err != ErrAppUserNotFound {
		t.Fatalf("expected ErrAppUserNotFound got %v", err)
	}
}
```

If the existing test infra uses different helper names, match them — but keep the assertions.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd apps/backend && go test ./internal/appauth/ -run TestRepo_FindByDiscordID -v
```

Expected: FAIL — `repo.FindByDiscordID undefined` or `ErrAppUserNotFound undefined`.

- [ ] **Step 3: Implement**

In `apps/backend/internal/appauth/repo.go`, just below the `FindOrCreateByDiscordID` function, add:

```go
// ErrAppUserNotFound is returned by FindByDiscordID when no app_users row
// matches the given Discord ID. Callers should treat this as a soft miss —
// e.g. command_usage drops the write rather than blocking the bot.
var ErrAppUserNotFound = errors.New("app_users row not found for discord id")

// FindByDiscordID returns the app_users.id for a Discord snowflake without
// creating a row. Returns ErrAppUserNotFound if no row exists.
func (r *Repo) FindByDiscordID(ctx context.Context, discordID string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		SELECT id FROM app_users WHERE discord_id = $1
	`, discordID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrAppUserNotFound
		}
		return "", fmt.Errorf("find app_users by discord id: %w", err)
	}
	return id, nil
}
```

Ensure the file imports `errors` and `github.com/jackc/pgx/v5`. If `errors` and `pgx` are not already imported, add them.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd apps/backend && go test ./internal/appauth/ -run TestRepo_FindByDiscordID -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/appauth/repo.go apps/backend/internal/appauth/repo_test.go
git commit -m "feat(backend): add appauth.FindByDiscordID without auto-create"
```

---

## Task 5: Backend write endpoint — `POST /api/internal/bot/setup/command-usage`

**Files:**
- Create: `apps/backend/internal/http/bot_command_usage_handlers.go`
- Create: `apps/backend/internal/http/bot_command_usage_handlers_test.go`
- Modify: `apps/backend/internal/http/router.go`

- [ ] **Step 1: Write the failing handler test**

Create `apps/backend/internal/http/bot_command_usage_handlers_test.go`. Mirror the structure of `bot_users_handlers_test.go`:

```go
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecordCommandUsage_HappyPath(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	defer pool.Close()
	h := newTestHandler(t, pool) // existing helper from bot_users_handlers_test.go style

	if _, err := pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_h1', '111', 'h1', 'h1')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	body := map[string]any{
		"discordId":       "111",
		"guildId":         "g1",
		"channelId":       "c1",
		"interactionType": "command",
		"command":         "track",
		"subcommand":      "create",
		"target":          "raid",
		"options":         map[string]any{"group_name": "raid"},
		"outcome":         "success",
		"latencyMs":       142,
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	h.RecordCommandUsage(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 got %d body=%s", rr.Code, rr.Body.String())
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM command_usage WHERE app_user_id='usr_h1'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row got %d", count)
	}
}

func TestRecordCommandUsage_MissingAppUserIsSilentSuccess(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	h := newTestHandler(t, pool)

	body := map[string]string{
		"discordId":       "9999999",
		"interactionType": "command",
		"command":         "player",
		"outcome":         "success",
	}
	body2 := map[string]any{
		"discordId":       "9999999",
		"interactionType": "command",
		"command":         "player",
		"options":         map[string]any{},
		"outcome":         "success",
		"latencyMs":       1,
	}
	_ = body
	b, _ := json.Marshal(body2)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	h.RecordCommandUsage(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 (silent drop) got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRecordCommandUsage_RejectsInvalidSnowflake(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	h := newTestHandler(t, pool)

	b, _ := json.Marshal(map[string]any{
		"discordId":       "not-a-snowflake",
		"interactionType": "command",
		"command":         "player",
		"outcome":         "success",
		"latencyMs":       1,
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	h.RecordCommandUsage(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "snowflake") {
		t.Fatalf("expected snowflake error, got %s", rr.Body.String())
	}
}

func TestRecordCommandUsage_RejectsBadEnum(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	h := newTestHandler(t, pool)

	b, _ := json.Marshal(map[string]any{
		"discordId":       "111",
		"interactionType": "unknown_type",
		"command":         "player",
		"outcome":         "success",
		"latencyMs":       1,
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	h.RecordCommandUsage(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", rr.Code, rr.Body.String())
	}
}
```

If `newTestPool` / `newTestHandler` helpers in `httpapi` package use different names, match the convention from `bot_users_handlers_test.go`. The key is: real Postgres pool + a Handler with `appauth` and `postgres` wired up.

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd apps/backend && go test ./internal/http/ -run TestRecordCommandUsage -v
```

Expected: FAIL — `h.RecordCommandUsage undefined`.

- [ ] **Step 3: Implement the handler**

Create `apps/backend/internal/http/bot_command_usage_handlers.go`:

```go
package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/store"
)

const maxErrorCodeLen = 256
const maxOptionsBytes = 16 * 1024

var validInteractionTypes = map[string]struct{}{
	"command":       {},
	"button":        {},
	"modal":         {},
	"string_select": {},
}

var validOutcomes = map[string]struct{}{
	"success": {},
	"error":   {},
	"banned":  {},
	"denied":  {},
}

type recordCommandUsageRequest struct {
	DiscordID       string          `json:"discordId"`
	GuildID         *string         `json:"guildId,omitempty"`
	ChannelID       *string         `json:"channelId,omitempty"`
	InteractionType string          `json:"interactionType"`
	Command         string          `json:"command"`
	Subcommand      *string         `json:"subcommand,omitempty"`
	Target          *string         `json:"target,omitempty"`
	Options         json.RawMessage `json:"options"`
	Outcome         string          `json:"outcome"`
	ErrorCode       *string         `json:"errorCode,omitempty"`
	LatencyMs       int             `json:"latencyMs"`
}

// RecordCommandUsage: POST /api/internal/bot/setup/command-usage
//
// JWT-protected. Logs a single Discord interaction by the bot. Resolves
// app_user_id from discord_id (the bot's ensureAppUser hook guarantees the
// row exists by the time any handler runs). If the lookup misses (race),
// the write is silently dropped with a log line and 204 is returned —
// the bot's response path is never blocked.
func (h *Handler) RecordCommandUsage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req recordCommandUsageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateSnowflakeValue(req.DiscordID) {
		writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
		return
	}
	if _, ok := validInteractionTypes[req.InteractionType]; !ok {
		writeError(w, http.StatusBadRequest, "interactionType invalid")
		return
	}
	if _, ok := validOutcomes[req.Outcome]; !ok {
		writeError(w, http.StatusBadRequest, "outcome invalid")
		return
	}
	if req.Command == "" || len(req.Command) > 128 {
		writeError(w, http.StatusBadRequest, "command must be 1..128 chars")
		return
	}
	if req.LatencyMs < 0 {
		req.LatencyMs = 0
	}
	if len(req.Options) > maxOptionsBytes {
		writeError(w, http.StatusBadRequest, "options payload too large")
		return
	}
	if req.ErrorCode != nil && len(*req.ErrorCode) > maxErrorCodeLen {
		trimmed := (*req.ErrorCode)[:maxErrorCodeLen]
		req.ErrorCode = &trimmed
	}
	if h.appauth == nil {
		writeError(w, http.StatusInternalServerError, "appauth repo not configured")
		return
	}

	appUserID, err := h.appauth.FindByDiscordID(r.Context(), req.DiscordID)
	if err != nil {
		if errors.Is(err, appauth.ErrAppUserNotFound) {
			log.Printf("[backend] RecordCommandUsage: no app_users row for discord_id=%s (race); dropping write", req.DiscordID)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		log.Printf("[backend] RecordCommandUsage FindByDiscordID error for %s: %v", req.DiscordID, err)
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return
	}

	if err := h.postgres.InsertCommandUsage(r.Context(), store.CommandUsageInsert{
		AppUserID:       appUserID,
		DiscordID:       req.DiscordID,
		GuildID:         req.GuildID,
		ChannelID:       req.ChannelID,
		InteractionType: req.InteractionType,
		Command:         req.Command,
		Subcommand:      req.Subcommand,
		Target:          req.Target,
		Options:         req.Options,
		Outcome:         req.Outcome,
		ErrorCode:       req.ErrorCode,
		LatencyMs:       req.LatencyMs,
	}); err != nil {
		log.Printf("[backend] RecordCommandUsage insert error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to record usage")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

The `h.appauth` field on `Handler` should already exist (used by `EnsureAppUser`). If `h.appauth.FindByDiscordID` is not callable through the existing interface that Handler holds, widen the interface declared in `apps/backend/internal/http/handler.go` (or wherever the appauth field type is declared) to include `FindByDiscordID(ctx, string) (string, error)`. Check by searching for the interface holding `FindOrCreateByDiscordID` and add the new method to it.

- [ ] **Step 4: Mount route in router.go**

In `apps/backend/internal/http/router.go`, in the bot-internal `/api/internal/bot/setup/*` Route block (around line 186 where `/users/ensure` is mounted), add:

```go
r.Post("/command-usage", handler.RecordCommandUsage)
```

Place it directly below `r.Post("/users/ensure", handler.EnsureAppUser)` for logical grouping.

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd apps/backend && go test ./internal/http/ -run TestRecordCommandUsage -v
```

Expected: PASS (all 4 tests).

- [ ] **Step 6: Run full backend test to ensure nothing else broke**

```bash
cd apps/backend && go test ./... 2>&1 | tail -30
```

Expected: all packages pass.

- [ ] **Step 7: Commit**

```bash
git add apps/backend/internal/http/bot_command_usage_handlers.go \
        apps/backend/internal/http/bot_command_usage_handlers_test.go \
        apps/backend/internal/http/router.go \
        apps/backend/internal/http/handler.go  # if interface was widened
git commit -m "feat(backend): add POST /api/internal/bot/setup/command-usage"
```

---

## Task 6: Backend admin read endpoint — `GET /api/app/admin/users/{id}/command-usage`

**Files:**
- Create: `apps/backend/internal/http/admin_command_usage_handlers.go`
- Create: `apps/backend/internal/http/admin_command_usage_handlers_test.go`
- Modify: `apps/backend/internal/http/router.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/http/admin_command_usage_handlers_test.go`:

```go
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestAdminGetUserCommandUsage_ReturnsItemsAndSummary(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	defer pool.Close()
	h := newTestHandler(t, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_adminread', '777', 'reader', 'reader')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := pool.Exec(ctx, `
			INSERT INTO command_usage (
				app_user_id, discord_id, interaction_type, command,
				options, outcome, latency_ms
			) VALUES ('usr_adminread', '777', 'command', 'player', '{}'::jsonb, 'success', 1)
		`); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO command_usage (
			app_user_id, discord_id, interaction_type, command,
			options, outcome, error_code, latency_ms
		) VALUES ('usr_adminread', '777', 'command', 'track', '{}'::jsonb, 'error', 'boom', 1)
	`); err != nil {
		t.Fatalf("seed err row: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/users/usr_adminread/command-usage?page=1&pageSize=10", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "usr_adminread")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	h.AdminGetUserCommandUsage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Items    []map[string]any `json:"items"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"pageSize"`
		Summary  struct {
			TotalCount  int64 `json:"totalCount"`
			ErrorCount  int64 `json:"errorCount"`
			ErrorRate   float64 `json:"errorRate"`
			TopCommands []struct {
				Command string `json:"command"`
				Count   int64  `json:"count"`
			} `json:"topCommands"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 4 {
		t.Fatalf("expected total=4 got %d", resp.Total)
	}
	if len(resp.Items) != 4 {
		t.Fatalf("expected 4 items got %d", len(resp.Items))
	}
	if resp.Summary.TotalCount != 4 || resp.Summary.ErrorCount != 1 {
		t.Fatalf("wrong summary: %+v", resp.Summary)
	}
	wantRate := 0.25
	if resp.Summary.ErrorRate < wantRate-0.001 || resp.Summary.ErrorRate > wantRate+0.001 {
		t.Fatalf("wrong error rate: %v", resp.Summary.ErrorRate)
	}
	if len(resp.Summary.TopCommands) == 0 || resp.Summary.TopCommands[0].Command != "player" {
		t.Fatalf("wrong top commands: %+v", resp.Summary.TopCommands)
	}
}

func TestAdminGetUserCommandUsage_PaginationParams(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	defer pool.Close()
	h := newTestHandler(t, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_pag', '888', 'pag', 'pag')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for i := 0; i < 5; i++ {
		_, _ = pool.Exec(ctx, fmt.Sprintf(`
			INSERT INTO command_usage (
				app_user_id, discord_id, interaction_type, command,
				options, outcome, latency_ms
			) VALUES ('usr_pag', '888', 'command', 'p%d', '{}'::jsonb, 'success', 1)
		`, i))
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/users/usr_pag/command-usage?page=2&pageSize=2", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "usr_pag")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	h.AdminGetUserCommandUsage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Items    []map[string]any `json:"items"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"pageSize"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Page != 2 || resp.PageSize != 2 || len(resp.Items) != 2 || resp.Total != 5 {
		t.Fatalf("bad pagination: %+v", resp)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd apps/backend && go test ./internal/http/ -run TestAdminGetUserCommandUsage -v
```

Expected: FAIL — `h.AdminGetUserCommandUsage undefined`.

- [ ] **Step 3: Implement the handler**

Create `apps/backend/internal/http/admin_command_usage_handlers.go`:

```go
package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// AdminGetUserCommandUsage: GET /api/app/admin/users/{id}/command-usage?page=&pageSize=
//
// Returns a paginated list of bot interactions for the user plus a 90-day
// summary (total, error count, error rate, top 5 commands).
func (h *Handler) AdminGetUserCommandUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}

	page := 1
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	pageSize := 25
	if v := r.URL.Query().Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}
	offset := (page - 1) * pageSize

	listed, err := h.postgres.ListUserCommandUsage(ctx, id, pageSize, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list command usage")
		return
	}

	summary, err := h.postgres.SummarizeUserCommandUsage(ctx, id, 90*24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to summarize command usage")
		return
	}

	var errorRate float64
	if summary.TotalCount > 0 {
		errorRate = float64(summary.ErrorCount) / float64(summary.TotalCount)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":    listed.Items,
		"total":    listed.Total,
		"page":     page,
		"pageSize": pageSize,
		"summary": map[string]any{
			"totalCount":  summary.TotalCount,
			"errorCount":  summary.ErrorCount,
			"errorRate":   errorRate,
			"topCommands": summary.TopCommands,
		},
	})
}
```

- [ ] **Step 4: Mount route**

In `apps/backend/internal/http/router.go`, in the admin sub-route block (around line 313 where `/users/{id}/audit` is mounted), add directly below it:

```go
r.Get("/users/{id}/command-usage", handler.AdminGetUserCommandUsage)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd apps/backend && go test ./internal/http/ -run TestAdminGetUserCommandUsage -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/http/admin_command_usage_handlers.go \
        apps/backend/internal/http/admin_command_usage_handlers_test.go \
        apps/backend/internal/http/router.go
git commit -m "feat(backend): add GET /api/app/admin/users/{id}/command-usage"
```

---

## Task 7: OpenAPI documentation

**Files:**
- Modify: `apps/backend/internal/http/openapi.go`

- [ ] **Step 1: Open openapi.go and locate the path entries for `/users/{id}/audit` and `/users/ensure`**

```bash
grep -n "users/{id}/audit\|users/ensure" apps/backend/internal/http/openapi.go
```

- [ ] **Step 2: Add two new path entries**

In the OpenAPI paths object, alongside the existing audit + users entries, add:

For `/api/internal/bot/setup/command-usage` (mirror the shape of the existing `/users/ensure` entry, including the JWT bot security scheme reference):

```yaml
"/api/internal/bot/setup/command-usage":
  post:
    summary: Record a bot interaction (command, button, modal, string-select)
    security:
      - botJWT: []
    requestBody:
      required: true
      content:
        application/json:
          schema:
            type: object
            required: [discordId, interactionType, command, outcome, latencyMs]
            properties:
              discordId:       { type: string }
              guildId:         { type: string, nullable: true }
              channelId:       { type: string, nullable: true }
              interactionType: { type: string, enum: [command, button, modal, string_select] }
              command:         { type: string }
              subcommand:      { type: string, nullable: true }
              target:          { type: string, nullable: true }
              options:         { type: object, additionalProperties: true }
              outcome:         { type: string, enum: [success, error, banned, denied] }
              errorCode:       { type: string, nullable: true }
              latencyMs:       { type: integer, minimum: 0 }
    responses:
      "204": { description: Recorded (or silently dropped if no app_users row) }
      "400": { description: Invalid request }
```

For `/api/app/admin/users/{id}/command-usage` (mirror `/users/{id}/audit`):

```yaml
"/api/app/admin/users/{id}/command-usage":
  get:
    summary: List bot command usage for a single user (paginated + 90-day summary)
    parameters:
      - in: path
        name: id
        required: true
        schema: { type: string }
      - in: query
        name: page
        schema: { type: integer, default: 1, minimum: 1 }
      - in: query
        name: pageSize
        schema: { type: integer, default: 25, minimum: 1, maximum: 100 }
    responses:
      "200":
        description: Paginated rows + summary
        content:
          application/json:
            schema:
              type: object
              properties:
                items:    { type: array, items: { type: object } }
                total:    { type: integer }
                page:     { type: integer }
                pageSize: { type: integer }
                summary:
                  type: object
                  properties:
                    totalCount: { type: integer }
                    errorCount: { type: integer }
                    errorRate:  { type: number, format: float }
                    topCommands:
                      type: array
                      items:
                        type: object
                        properties:
                          command: { type: string }
                          count:   { type: integer }
```

Format consistent with the rest of `openapi.go` (which may use raw YAML in a backtick string or a Go literal — match the existing pattern exactly).

- [ ] **Step 3: Compile to verify the embedded spec is still valid Go**

```bash
cd apps/backend && go build ./...
```

Expected: no compile errors.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/http/openapi.go
git commit -m "docs(backend): document command-usage write and read endpoints"
```

---

## Task 8: Bot backend client — `recordCommandUsage`

**Files:**
- Modify: `apps/bot/src/backend-client.ts`
- Modify: `apps/bot/src/backend-client.test.ts`

- [ ] **Step 1: Write the failing test**

In `apps/bot/src/backend-client.test.ts`, add (mirroring the existing `ensureAppUser` test in the same file):

```ts
test("recordCommandUsage POSTs to /command-usage", async () => {
  let url: string | null = null;
  let init: RequestInit | null = null;
  const client = new BackendClient(
    "http://localhost:8080",
    "secret",
    "iss",
    "aud",
    async (u, i) => {
      url = String(u);
      init = i ?? null;
      return new Response(null, { status: 204 });
    },
  );

  await client.recordCommandUsage("worker-a", {
    discordId:       "111",
    guildId:         "g1",
    channelId:       "c1",
    interactionType: "command",
    command:         "track",
    subcommand:      "create",
    target:          "raid",
    options:         { group_name: "raid" },
    outcome:         "success",
    errorCode:       null,
    latencyMs:       142,
  });

  expect(url).toBe("http://localhost:8080/api/internal/bot/setup/command-usage");
  expect(init?.method).toBe("POST");
  const body = JSON.parse(String(init?.body));
  expect(body.command).toBe("track");
  expect(body.options).toEqual({ group_name: "raid" });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
pnpm --filter @rustlens/bot test -- -t "recordCommandUsage"
```

Expected: FAIL — `client.recordCommandUsage is not a function`.

- [ ] **Step 3: Implement on BackendClient**

In `apps/bot/src/backend-client.ts`, near the existing `ensureAppUser` method:

```ts
export type CommandUsagePayload = {
  discordId:       string;
  guildId:         string | null;
  channelId:       string | null;
  interactionType: "command" | "button" | "modal" | "string_select";
  command:         string;
  subcommand:      string | null;
  target:          string | null;
  options:         Record<string, unknown>;
  outcome:         "success" | "error" | "banned" | "denied";
  errorCode:       string | null;
  latencyMs:       number;
};

// inside the BackendClient class:
recordCommandUsage(workerId: string, payload: CommandUsagePayload): Promise<void> {
  return this.postJSON(
    workerId,
    `/api/internal/bot/setup/command-usage`,
    payload,
  );
}
```

Use whichever helper method `ensureAppUser` uses (likely a private `postJSON` or `request` helper that builds JWT auth + JSON body). If `ensureAppUser` instead calls fetch directly, mirror that pattern verbatim.

- [ ] **Step 4: Run test to verify it passes**

```bash
pnpm --filter @rustlens/bot test -- -t "recordCommandUsage"
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/bot/src/backend-client.ts apps/bot/src/backend-client.test.ts
git commit -m "feat(bot): backend-client recordCommandUsage"
```

---

## Task 9: Bot payload extractor + fire-and-forget recorder

**Files:**
- Create: `apps/bot/src/command-usage.ts`
- Create: `apps/bot/src/command-usage.test.ts`

- [ ] **Step 1: Write the failing extractor tests**

Create `apps/bot/src/command-usage.test.ts`:

```ts
import { describe, expect, test, vi } from "vitest";
import {
  buildCommandUsagePayload,
  recordCommandUsage,
  __setBackendForTests,
  __resetForTests,
} from "./command-usage.js";

function fakeChatInput(opts: {
  command: string;
  subcommand?: string;
  options?: Array<{ name: string; value: unknown }>;
  guildId?: string;
  channelId?: string;
  userId?: string;
}) {
  const getOpts = opts.options ?? [];
  return {
    isChatInputCommand: () => true,
    isButton: () => false,
    isModalSubmit: () => false,
    isStringSelectMenu: () => false,
    isAutocomplete: () => false,
    commandName: opts.command,
    options: {
      getSubcommand: (_: boolean) => opts.subcommand ?? null,
      data: getOpts.map((o) => ({ name: o.name, value: o.value, type: 3 })),
    },
    guildId:  opts.guildId  ?? null,
    channelId: opts.channelId ?? null,
    user: { id: opts.userId ?? "111" },
  };
}

function fakeButton(customId: string) {
  return {
    isChatInputCommand: () => false,
    isButton: () => true,
    isModalSubmit: () => false,
    isStringSelectMenu: () => false,
    isAutocomplete: () => false,
    customId,
    guildId:  "g1",
    channelId: "c1",
    user:     { id: "111" },
  };
}

function fakeModal(customId: string, fields: Record<string, string>) {
  return {
    isChatInputCommand: () => false,
    isButton: () => false,
    isModalSubmit: () => true,
    isStringSelectMenu: () => false,
    isAutocomplete: () => false,
    customId,
    guildId:  "g1",
    channelId: "c1",
    user:     { id: "111" },
    fields: {
      fields: new Map(Object.entries(fields).map(([k, v]) => [k, { customId: k, value: v }])),
    },
  };
}

function fakeStringSelect(customId: string, values: string[]) {
  return {
    isChatInputCommand: () => false,
    isButton: () => false,
    isModalSubmit: () => false,
    isStringSelectMenu: () => true,
    isAutocomplete: () => false,
    customId,
    values,
    guildId:  "g1",
    channelId: "c1",
    user:     { id: "111" },
  };
}

describe("buildCommandUsagePayload", () => {
  test("chat input with subcommand + options", () => {
    const p = buildCommandUsagePayload(
      fakeChatInput({
        command: "track",
        subcommand: "create",
        options: [
          { name: "group_name", value: "raid" },
          { name: "server",     value: "12345" },
        ],
        guildId: "g1",
        channelId: "c1",
      }) as never,
      "success",
      null,
      142,
    );
    expect(p).not.toBeNull();
    expect(p!.interactionType).toBe("command");
    expect(p!.command).toBe("track");
    expect(p!.subcommand).toBe("create");
    expect(p!.target).toBe("raid");
    expect(p!.options).toEqual({ group_name: "raid", server: "12345" });
    expect(p!.latencyMs).toBe(142);
    expect(p!.outcome).toBe("success");
  });

  test("button splits customId on first colon", () => {
    const p = buildCommandUsagePayload(
      fakeButton("track:add:abc123") as never,
      "success",
      null,
      10,
    );
    expect(p!.interactionType).toBe("button");
    expect(p!.command).toBe("track");
    expect(p!.target).toBe("add:abc123");
    expect(p!.options).toEqual({ customId: "track:add:abc123" });
  });

  test("button with no colon uses full customId as command", () => {
    const p = buildCommandUsagePayload(
      fakeButton("standalone-button") as never,
      "success",
      null,
      10,
    );
    expect(p!.command).toBe("standalone-button");
    expect(p!.target).toBeNull();
  });

  test("modal extracts field values", () => {
    const p = buildCommandUsagePayload(
      fakeModal("wipereact:edit:42", { title: "Raid", duration: "60" }) as never,
      "success",
      null,
      20,
    );
    expect(p!.interactionType).toBe("modal");
    expect(p!.command).toBe("wipereact");
    expect(p!.target).toBe("edit:42");
    expect(p!.options).toEqual({ title: "Raid", duration: "60" });
  });

  test("string select captures values", () => {
    const p = buildCommandUsagePayload(
      fakeStringSelect("settings:tracked_servers", ["s1", "s2"]) as never,
      "success",
      null,
      5,
    );
    expect(p!.interactionType).toBe("string_select");
    expect(p!.command).toBe("settings");
    expect(p!.target).toBe("tracked_servers");
    expect(p!.options).toEqual({ customId: "settings:tracked_servers", values: ["s1", "s2"] });
  });

  test("autocomplete returns null", () => {
    const interaction = {
      isChatInputCommand: () => false,
      isButton: () => false,
      isModalSubmit: () => false,
      isStringSelectMenu: () => false,
      isAutocomplete: () => true,
      user: { id: "111" },
    };
    expect(buildCommandUsagePayload(interaction as never, "success", null, 1)).toBeNull();
  });

  test("error outcome carries errorCode (capped to 256)", () => {
    const long = "x".repeat(500);
    const p = buildCommandUsagePayload(
      fakeChatInput({ command: "player" }) as never,
      "error",
      long,
      10,
    );
    expect(p!.outcome).toBe("error");
    expect(p!.errorCode!.length).toBe(256);
  });
});

describe("recordCommandUsage", () => {
  test("fire and forget — does not throw on backend failure", () => {
    __resetForTests();
    const spy = vi.fn().mockRejectedValue(new Error("boom"));
    __setBackendForTests({ recordCommandUsage: spy } as never);

    expect(() =>
      recordCommandUsage(
        fakeChatInput({ command: "player" }) as never,
        "success",
        null,
        Date.now() - 50,
      ),
    ).not.toThrow();
    expect(spy).toHaveBeenCalledTimes(1);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
pnpm --filter @rustlens/bot test -- command-usage
```

Expected: FAIL — file not found.

- [ ] **Step 3: Implement command-usage.ts**

Create `apps/bot/src/command-usage.ts`:

```ts
import type { Interaction } from "discord.js";

import { BackendClient, type CommandUsagePayload } from "./backend-client.js";
import { loadConfig } from "./config.js";

export type Outcome = "success" | "error" | "banned" | "denied";

const shardID = process.env.SHARD_ID ?? "0";

let _backend: BackendClient | undefined;
let _workerID: string | undefined;
let _backendOverride: { recordCommandUsage: BackendClient["recordCommandUsage"] } | null = null;

function getBackend(): { recordCommandUsage: BackendClient["recordCommandUsage"] } {
  if (_backendOverride) return _backendOverride;
  if (!_backend) {
    const cfg = loadConfig();
    _backend = new BackendClient(cfg.backendUrl, cfg.jwtSecret, cfg.jwtIssuer, cfg.jwtAudience);
    _workerID = `${cfg.workerId}-command-usage-${shardID}`;
  }
  return _backend;
}

function getWorkerID(): string {
  if (_backendOverride) return "test-worker";
  getBackend();
  return _workerID!;
}

const MAX_ERROR_CODE_LEN = 256;

function splitCustomId(customId: string): { command: string; target: string | null } {
  const idx = customId.indexOf(":");
  if (idx === -1) return { command: customId, target: null };
  return {
    command: customId.slice(0, idx),
    target:  customId.slice(idx + 1) || null,
  };
}

function asString(v: unknown): string | null {
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  return null;
}

/**
 * Builds the payload for a single Discord interaction or returns null when
 * the interaction type is out of scope (autocomplete or unknown subtype).
 */
export function buildCommandUsagePayload(
  interaction: Interaction,
  outcome: Outcome,
  errorCode: string | null,
  latencyMs: number,
): CommandUsagePayload | null {
  const trimmedError =
    errorCode == null
      ? null
      : errorCode.slice(0, MAX_ERROR_CODE_LEN);

  const base = {
    discordId:  interaction.user?.id ?? "",
    guildId:    interaction.guildId ?? null,
    channelId:  interaction.channelId ?? null,
    outcome,
    errorCode:  trimmedError,
    latencyMs:  Math.max(0, Math.floor(latencyMs)),
  };

  if (interaction.isChatInputCommand?.()) {
    const subcommand = interaction.options.getSubcommand(false) ?? null;
    const opts: Record<string, unknown> = {};
    const data = (interaction.options as unknown as { data: Array<{ name: string; value: unknown; options?: unknown[] }> }).data ?? [];
    // discord.js nests options under subcommand groups; flatten the leaves.
    const collect = (arr: Array<{ name: string; value: unknown; options?: unknown[] }>) => {
      for (const o of arr) {
        if (Array.isArray(o.options) && o.options.length > 0) {
          collect(o.options as Array<{ name: string; value: unknown; options?: unknown[] }>);
        } else if (o.value !== undefined) {
          opts[o.name] = o.value;
        }
      }
    };
    collect(data);
    const firstStringValue = Object.values(opts).find((v) => asString(v) !== null);
    return {
      ...base,
      interactionType: "command",
      command:    interaction.commandName,
      subcommand,
      target:     firstStringValue == null ? null : asString(firstStringValue),
      options:    opts,
    };
  }

  if (interaction.isButton?.()) {
    const { command, target } = splitCustomId(interaction.customId);
    return {
      ...base,
      interactionType: "button",
      command,
      subcommand: null,
      target,
      options: { customId: interaction.customId },
    };
  }

  if (interaction.isModalSubmit?.()) {
    const { command, target } = splitCustomId(interaction.customId);
    const opts: Record<string, unknown> = {};
    const fieldsMap = (interaction.fields as unknown as { fields: Map<string, { customId: string; value: unknown }> }).fields;
    fieldsMap?.forEach((f) => {
      opts[f.customId] = f.value;
    });
    return {
      ...base,
      interactionType: "modal",
      command,
      subcommand: null,
      target,
      options: opts,
    };
  }

  if (interaction.isStringSelectMenu?.()) {
    const { command, target } = splitCustomId(interaction.customId);
    return {
      ...base,
      interactionType: "string_select",
      command,
      subcommand: null,
      target,
      options: { customId: interaction.customId, values: interaction.values },
    };
  }

  // autocomplete + unknown subtypes — skipped
  return null;
}

/**
 * Fire-and-forget. Builds the payload from the interaction, POSTs to the
 * backend, and swallows any errors with a log line. Never throws and never
 * blocks the bot response path.
 *
 * startedAt should be Date.now() captured at the top of the interaction
 * handler so latencyMs reflects total dispatch time.
 */
export function recordCommandUsage(
  interaction: Interaction,
  outcome: Outcome,
  errorCode: string | null,
  startedAt: number,
): void {
  const payload = buildCommandUsagePayload(
    interaction,
    outcome,
    errorCode,
    Date.now() - startedAt,
  );
  if (!payload) return;
  if (!payload.discordId) return;

  getBackend()
    .recordCommandUsage(getWorkerID(), payload)
    .catch((err: unknown) => {
      console.error(JSON.stringify({
        level:   "warn",
        message: "[bot] recordCommandUsage failed",
        userId:  payload.discordId,
        command: payload.command,
        error:   String(err),
      }));
    });
}

// --- Test-only helpers ---

export function __resetForTests(): void {
  _backendOverride = null;
}

export function __setBackendForTests(b: { recordCommandUsage: BackendClient["recordCommandUsage"] } | null): void {
  _backendOverride = b;
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
pnpm --filter @rustlens/bot test -- command-usage
```

Expected: PASS (all extractor tests + fire-and-forget test).

- [ ] **Step 5: Commit**

```bash
git add apps/bot/src/command-usage.ts apps/bot/src/command-usage.test.ts
git commit -m "feat(bot): command-usage payload extractor and fire-and-forget recorder"
```

---

## Task 10: Wire recorder into `shard-worker.ts` dispatcher

**Files:**
- Modify: `apps/bot/src/shard-worker.ts`

This task threads a `startedAt` timestamp through the existing `interactionCreate` dispatcher and emits `recordCommandUsage` once per dispatch with the resolved outcome.

- [ ] **Step 1: Import the recorder**

At the top of `apps/bot/src/shard-worker.ts`, alongside `import { ensureAppUser } from "./ensure-app-user.js";`, add:

```ts
import { recordCommandUsage, type Outcome } from "./command-usage.js";
```

- [ ] **Step 2: Capture start time and resolved outcome**

Locate the `client.on("interactionCreate", async (interaction) => {` block (around line 334). Restructure as:

```ts
client.on("interactionCreate", async (interaction) => {
  reportUserInstallSeen(interaction);
  ensureAppUser(interaction);

  // Only track non-autocomplete interactions in the usage log.
  const trackUsage = !interaction.isAutocomplete();
  const startedAt = Date.now();
  let outcome: Outcome = "success";
  let errorCode: string | null = null;

  try {
    // ... existing dispatcher body unchanged ...
  } catch (err) {
    outcome   = "error";
    errorCode = err instanceof Error ? err.message : String(err);
    throw err; // preserve existing crash/log behavior
  } finally {
    if (trackUsage) {
      recordCommandUsage(interaction, outcome, errorCode, startedAt);
    }
  }
});
```

Note: the existing dispatcher already has a `try { ... }` wrapper (look at the existing code around lines 337+). Keep the existing inner `try/catch` if there is one — just add the new outer `try/catch/finally` to capture outcome. If the existing dispatcher already throws errors out (not caught), the `catch` block above sets outcome and rethrows. If the existing code catches and logs errors internally, leave that intact and rely on enforceBan/permission returns for outcome assignment (see step 3).

- [ ] **Step 3: Mark `banned` and `denied` outcomes inline**

Search inside the dispatcher for `enforceBan(` calls. Immediately before each call that may return `true`, you can't easily intercept the return inside the existing branches, so use this pattern instead:

Wrap the existing `enforceBan` and permission rejection paths so that whenever the dispatcher early-returns `true` (banned) or rejects on owner/permission guard (denied), the outcome variable is updated before the `return`.

Concrete edits — for each occurrence of:

```ts
if (await enforceBan(interaction, category)) {
  return;
}
```

change to:

```ts
if (await enforceBan(interaction, category)) {
  outcome = "banned";
  return;
}
```

Do the same for `enforceBanForAutocomplete` (the autocomplete branch is `trackUsage=false`, so this is a no-op but stay consistent).

For owner-guard / permission denials in this file (e.g. `if (!ownerGuard(...)) { ... return; }` or similar admin/role gates), set `outcome = "denied";` immediately before the `return`. Use `grep -n "owner\|Admin\|Permission\|requireRole" apps/bot/src/shard-worker.ts` to find them; the precise edits are localized — at most a few sites.

- [ ] **Step 4: Run all bot tests to confirm nothing regressed**

```bash
pnpm --filter @rustlens/bot test
```

Expected: PASS. Existing tests should not be affected since the recorder is fire-and-forget and outcome assignment is additive.

- [ ] **Step 5: Smoke-check the build**

```bash
pnpm --filter @rustlens/bot build
```

Or if there's no build script: `pnpm --filter @rustlens/bot exec tsc --noEmit`. Expected: no type errors.

- [ ] **Step 6: Commit**

```bash
git add apps/bot/src/shard-worker.ts
git commit -m "feat(bot): record command usage for every dispatched interaction"
```

---

## Task 11: Frontend types + API client

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 1: Add types and the fetch function**

Find the existing `getAdminUserAudit` function (around line 499) and add immediately below it:

```ts
export type AdminCommandUsageItem = {
	id:              number;
	appUserId:       string;
	discordId:       string;
	guildId:         string | null;
	channelId:       string | null;
	interactionType: "command" | "button" | "modal" | "string_select";
	command:         string;
	subcommand:      string | null;
	target:          string | null;
	options:         Record<string, unknown>;
	outcome:         "success" | "error" | "banned" | "denied";
	errorCode:       string | null;
	latencyMs:       number;
	createdAt:       string;
};

export type AdminCommandUsageSummary = {
	totalCount:  number;
	errorCount:  number;
	errorRate:   number;
	topCommands: Array<{ command: string; count: number }>;
};

export type AdminCommandUsagePage = {
	items:    AdminCommandUsageItem[];
	total:    number;
	page:     number;
	pageSize: number;
	summary:  AdminCommandUsageSummary;
};

export async function getAdminUserCommandUsage(
	userId: string,
	opts: { page?: number; pageSize?: number } = {},
): Promise<AdminCommandUsagePage> {
	const sp = new URLSearchParams();
	if (opts.page)     sp.set("page",     String(opts.page));
	if (opts.pageSize) sp.set("pageSize", String(opts.pageSize));
	const qs = sp.toString();
	return backendFetch<AdminCommandUsagePage>(
		`/api/app/admin/users/${encodeURIComponent(userId)}/command-usage${qs ? `?${qs}` : ""}`,
		undefined,
		{ requireSession: true },
	);
}
```

- [ ] **Step 2: Verify type check passes**

```bash
pnpm --filter @rustlens/frontend exec tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): typed admin command-usage API client"
```

---

## Task 12: Frontend — fetch and pass `commandUsage` into UsageSection

**Files:**
- Modify: `apps/frontend/app/admin/users/[id]/page.tsx`

- [ ] **Step 1: Read the current page**

```bash
cat apps/frontend/app/admin/users/\[id\]/page.tsx
```

Find where `getAdminUser` and `getAdminUserAudit` are awaited (likely inside the default async page export, often run in parallel via `Promise.all`).

- [ ] **Step 2: Add the command-usage fetch**

Import alongside the existing API imports:

```ts
import {
	getAdminUser,
	getAdminUserAudit,
	getAdminUserCommandUsage,   // new
	// ... other existing imports
} from "@/lib/rustlens-api";
```

Read the current `usagePage` from `searchParams`. In Next.js 16 App Router, `searchParams` is a Promise — match the existing access pattern in this file. Resolve it before reading.

In the data-fetch section, add `commandUsage` to the parallel fetch:

```ts
const usagePage     = Number((searchParamsResolved.usagePage as string | undefined) ?? "1") || 1;
const usagePageSize = 25;

const [detail, audit, commandUsage] = await Promise.all([
	getAdminUser(userId),
	getAdminUserAudit(userId, { limit: 50 }),
	getAdminUserCommandUsage(userId, { page: usagePage, pageSize: usagePageSize }),
]);
```

Pass `commandUsage` into the user-detail component:

```tsx
<UserDetailPage
	detail={detail}
	roles={roles}
	audit={audit.items}
	commandUsage={commandUsage}   // new
	permissions={permissions}
/>
```

- [ ] **Step 3: Update `user-detail-page.tsx` signature**

In `apps/frontend/app/admin/users/_components/user-detail-page.tsx`, add to the `Props` type:

```ts
import type {
	AdminRoleItem,
	AdminUserAuditItem,
	AdminUserDetail,
	AdminCommandUsagePage,    // new
} from "@/lib/rustlens-api";

type Props = {
	detail:       AdminUserDetail;
	roles:        AdminRoleItem[];
	audit:        AdminUserAuditItem[];
	commandUsage: AdminCommandUsagePage;   // new
	permissions:  string[];
};
```

Pass it into `UsageSection`:

```tsx
<TabsContent value="usage">
	<UsageSection data={commandUsage} userId={detail.user.id} />
</TabsContent>
```

(The existing `<UsageSection />` call with no props will become a type error until Task 13 is done — that's fine, Task 13 follows immediately.)

- [ ] **Step 4: Skip running yet** — `UsageSection` still has the old signature. Continue to Task 13 then test together.

- [ ] **Step 5: No commit yet** — bundle this with Task 13's commit since they are one logical change.

---

## Task 13: Frontend — rewrite `UsageSection`

**Files:**
- Modify: `apps/frontend/app/admin/users/_components/usage-section.tsx`
- Create: `apps/frontend/app/admin/users/_components/__tests__/usage-section.test.tsx`

- [ ] **Step 1: Write the failing tests**

Create `apps/frontend/app/admin/users/_components/__tests__/usage-section.test.tsx`:

```tsx
import { describe, expect, test } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { UsageSection } from "../usage-section";

const userId = "usr_test";

function makeData(overrides: Partial<Parameters<typeof UsageSection>[0]["data"]> = {}) {
	return {
		items: [],
		total: 0,
		page: 1,
		pageSize: 25,
		summary: {
			totalCount:  0,
			errorCount:  0,
			errorRate:   0,
			topCommands: [],
		},
		...overrides,
	};
}

describe("UsageSection", () => {
	test("renders empty state when no items", () => {
		render(<UsageSection userId={userId} data={makeData()} />);
		expect(screen.getByText(/No bot usage in the last 90 days/i)).toBeInTheDocument();
	});

	test("renders summary strip", () => {
		const data = makeData({
			total: 42,
			summary: {
				totalCount: 42,
				errorCount: 2,
				errorRate:  2 / 42,
				topCommands: [
					{ command: "player", count: 30 },
					{ command: "track",  count: 12 },
				],
			},
			items: [{
				id: 1,
				appUserId: userId,
				discordId: "111",
				guildId: "g1",
				channelId: "c1",
				interactionType: "command",
				command: "player",
				subcommand: null,
				target: "76561198000000001",
				options: { identifier: "76561198000000001" },
				outcome: "success",
				errorCode: null,
				latencyMs: 142,
				createdAt: new Date().toISOString(),
			}],
		});
		render(<UsageSection userId={userId} data={data} />);

		expect(screen.getByText(/42/)).toBeInTheDocument();          // total
		expect(screen.getByText(/player/)).toBeInTheDocument();      // top command label
		expect(screen.getByText(/track/)).toBeInTheDocument();
	});

	test("row click expands raw options JSON", () => {
		const data = makeData({
			total: 1,
			summary: { totalCount: 1, errorCount: 0, errorRate: 0, topCommands: [{ command: "player", count: 1 }] },
			items: [{
				id: 1,
				appUserId: userId,
				discordId: "111",
				guildId: "g1",
				channelId: "c1",
				interactionType: "command",
				command: "player",
				subcommand: null,
				target: "raid",
				options: { group_name: "raid" },
				outcome: "success",
				errorCode: null,
				latencyMs: 100,
				createdAt: new Date().toISOString(),
			}],
		});
		render(<UsageSection userId={userId} data={data} />);

		const row = screen.getByRole("button", { name: /player/i });
		fireEvent.click(row);
		expect(screen.getByText(/"group_name"/)).toBeInTheDocument();
		expect(screen.getByText(/"raid"/)).toBeInTheDocument();
	});

	test("error outcome renders destructive badge", () => {
		const data = makeData({
			total: 1,
			summary: { totalCount: 1, errorCount: 1, errorRate: 1, topCommands: [] },
			items: [{
				id: 1,
				appUserId: userId,
				discordId: "111",
				guildId: null,
				channelId: null,
				interactionType: "command",
				command: "track",
				subcommand: "create",
				target: null,
				options: {},
				outcome: "error",
				errorCode: "boom",
				latencyMs: 5,
				createdAt: new Date().toISOString(),
			}],
		});
		render(<UsageSection userId={userId} data={data} />);
		expect(screen.getByText(/error/i)).toBeInTheDocument();
		// errorCode visible after expansion
		fireEvent.click(screen.getByRole("button", { name: /track/i }));
		expect(screen.getByText(/boom/)).toBeInTheDocument();
	});
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
pnpm --filter @rustlens/frontend test -- usage-section
```

Expected: FAIL — `UsageSection` signature mismatch / component renders placeholder text.

- [ ] **Step 3: Rewrite usage-section.tsx**

Replace the contents of `apps/frontend/app/admin/users/_components/usage-section.tsx`:

```tsx
"use client";

import { useState } from "react";

import { Badge } from "@/components/ui/badge";
import { RelativeTime } from "@/components/admin/relative-time";
import type {
	AdminCommandUsageItem,
	AdminCommandUsagePage,
} from "@/lib/rustlens-api";

type Props = {
	userId: string;
	data:   AdminCommandUsagePage;
};

function outcomeBadgeVariant(o: AdminCommandUsageItem["outcome"]): "secondary" | "destructive" | "outline" {
	switch (o) {
		case "success": return "secondary";
		case "error":   return "destructive";
		case "banned":  return "destructive";
		case "denied":  return "outline";
	}
}

function summarizeInput(item: AdminCommandUsageItem): string {
	const entries = Object.entries(item.options ?? {});
	if (entries.length === 0) {
		return item.target ?? "—";
	}
	const joined = entries
		.map(([k, v]) => `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`)
		.join(", ");
	return joined.length > 120 ? `${joined.slice(0, 117)}…` : joined;
}

function commandLabel(item: AdminCommandUsageItem): string {
	if (item.subcommand) return `${item.command} ${item.subcommand}`;
	return item.command;
}

export function UsageSection({ userId, data }: Props) {
	const [expandedId, setExpandedId] = useState<number | null>(null);
	const { items, total, page, pageSize, summary } = data;

	return (
		<section className="flex flex-col gap-4 rounded-lg border bg-card p-4">
			<div>
				<h2 className="text-lg font-semibold">Usage</h2>
				<p className="text-sm text-muted-foreground">
					Bot interactions this user has run in the last 90 days.
				</p>
			</div>

			<div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
				<div className="rounded-md border p-3">
					<div className="text-xs uppercase text-muted-foreground">Total (90d)</div>
					<div className="text-2xl font-semibold">{summary.totalCount}</div>
				</div>
				<div className="rounded-md border p-3">
					<div className="text-xs uppercase text-muted-foreground">Error rate</div>
					<div className="text-2xl font-semibold">
						{summary.totalCount === 0 ? "—" : `${(summary.errorRate * 100).toFixed(1)}%`}
					</div>
					<div className="text-xs text-muted-foreground">{summary.errorCount} errors</div>
				</div>
				<div className="rounded-md border p-3">
					<div className="text-xs uppercase text-muted-foreground">Top commands</div>
					<div className="mt-1 flex flex-wrap gap-1">
						{summary.topCommands.length === 0 ? (
							<span className="text-sm text-muted-foreground">—</span>
						) : (
							summary.topCommands.map((tc) => (
								<Badge key={tc.command} variant="secondary">
									{tc.command} · {tc.count}
								</Badge>
							))
						)}
					</div>
				</div>
			</div>

			{items.length === 0 ? (
				<p className="text-sm text-muted-foreground">No bot usage in the last 90 days.</p>
			) : (
				<>
					<ul className="flex flex-col divide-y">
						{items.map((it) => {
							const expanded = expandedId === it.id;
							return (
								<li key={it.id} className="py-2">
									<button
										type="button"
										onClick={() => setExpandedId(expanded ? null : it.id)}
										className="grid w-full grid-cols-12 items-center gap-2 text-left text-sm hover:bg-muted/40 rounded px-1 py-1"
									>
										<div className="col-span-2 text-xs text-muted-foreground">
											<RelativeTime iso={it.createdAt} />
										</div>
										<div className="col-span-1">
											<Badge variant="outline">{it.interactionType}</Badge>
										</div>
										<code className="col-span-3 truncate rounded bg-muted px-1 py-0.5 text-xs">
											{commandLabel(it)}
										</code>
										<div className="col-span-4 truncate text-xs text-muted-foreground">
											{summarizeInput(it)}
										</div>
										<div className="col-span-2 text-right">
											<Badge variant={outcomeBadgeVariant(it.outcome)}>{it.outcome}</Badge>
										</div>
									</button>
									{expanded && (
										<div className="mt-2 grid grid-cols-1 gap-2 rounded-md border bg-muted/30 p-3 text-xs">
											<div>
												<div className="font-semibold text-muted-foreground">What they put in</div>
												<pre className="mt-1 whitespace-pre-wrap break-words">
													{JSON.stringify(it.options ?? {}, null, 2)}
												</pre>
											</div>
											<div>
												<div className="font-semibold text-muted-foreground">What happened</div>
												<div>{it.outcome}{it.errorCode ? ` — ${it.errorCode}` : ""}</div>
											</div>
											{it.guildId && (
												<div className="text-muted-foreground">guild: {it.guildId}</div>
											)}
										</div>
									)}
								</li>
							);
						})}
					</ul>

					<UsagePagination
						userId={userId}
						page={page}
						pageSize={pageSize}
						total={total}
					/>
				</>
			)}
		</section>
	);
}

function UsagePagination({
	userId: _userId,
	page,
	pageSize,
	total,
}: { userId: string; page: number; pageSize: number; total: number }) {
	const lastPage = Math.max(1, Math.ceil(total / pageSize));
	if (lastPage <= 1) return null;

	const mkHref = (p: number) => {
		const sp = new URLSearchParams();
		sp.set("usagePage", String(p));
		return `?${sp.toString()}#usage`;
	};

	return (
		<div className="flex items-center justify-between text-xs">
			<span className="text-muted-foreground">
				Page {page} of {lastPage} · {total} total
			</span>
			<div className="flex gap-2">
				{page > 1 && (
					<a href={mkHref(page - 1)} className="underline">
						Prev
					</a>
				)}
				{page < lastPage && (
					<a href={mkHref(page + 1)} className="underline">
						Next
					</a>
				)}
			</div>
		</div>
	);
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
pnpm --filter @rustlens/frontend test -- usage-section
```

Expected: PASS (all 4 tests).

- [ ] **Step 5: Type-check the whole frontend**

```bash
pnpm --filter @rustlens/frontend exec tsc --noEmit
```

Expected: no errors. If the page `searchParams` API differs from what was assumed in Task 12, adjust there.

- [ ] **Step 6: Commit (bundled with Task 12 changes)**

```bash
git add apps/frontend/app/admin/users/\[id\]/page.tsx \
        apps/frontend/app/admin/users/_components/user-detail-page.tsx \
        apps/frontend/app/admin/users/_components/usage-section.tsx \
        apps/frontend/app/admin/users/_components/__tests__/usage-section.test.tsx
git commit -m "feat(frontend): render command usage on /admin/users/{id} Usage tab"
```

---

## Task 14: Update AGENTS.md

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Add the schema entry**

Find the "Schema" / "Backend" section in `AGENTS.md` that lists tables. Add a bullet near `app_audit_events`:

```
- `command_usage` — every Discord bot interaction (chat command, button, modal, string-select) a user runs. Written fire-and-forget by the bot via `POST /api/internal/bot/setup/command-usage`. FK to `app_users(id) ON DELETE CASCADE`. Purged nightly to a 90-day rolling window by `internal/cleanup`. Surfaced on `/admin/users/{id}` Usage tab via `GET /api/app/admin/users/{id}/command-usage`.
```

Add to the endpoint sections:

- Under bot-internal `/api/internal/bot/setup/*`:
```
- Bot command-usage route (`POST /api/internal/bot/setup/command-usage`) is JWT-protected and records a single Discord interaction (command, button, modal, string-select). The bot calls it fire-and-forget at the end of every `interactionCreate` dispatch with outcome (`success|error|banned|denied`), input payload, latency, and target identifiers. `discordId` resolves to `app_users.id` via `appauth.FindByDiscordID`; missing rows are silently dropped with a log line so the bot response is never blocked.
```

- Under `/api/app/admin/*`:
```
- The admin `GET /api/app/admin/users/{id}/command-usage?page=&pageSize=` route returns a paginated list of the user's bot interactions plus a 90-day summary (total count, error rate, top 5 commands). Backs the Usage tab on `/admin/users/{id}`.
```

- [ ] **Step 2: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document command_usage table and new endpoints in AGENTS.md"
```

---

## Task 15: End-to-end smoke check

**Files:** none

- [ ] **Step 1: Full backend test**

```bash
cd apps/backend && go test ./...
```

Expected: PASS.

- [ ] **Step 2: Full bot test + typecheck**

```bash
pnpm --filter @rustlens/bot test && pnpm --filter @rustlens/bot exec tsc --noEmit
```

Expected: PASS.

- [ ] **Step 3: Full frontend test + typecheck + build**

```bash
pnpm --filter @rustlens/frontend test
pnpm --filter @rustlens/frontend exec tsc --noEmit
pnpm --filter @rustlens/frontend build
```

Expected: PASS.

- [ ] **Step 4: Manual smoke test (optional but recommended)**

1. `make dev` to start infra + all services.
2. Sign into the website with Discord OAuth as an internal admin.
3. In Discord, run any RustLens slash command (e.g. `/player <steamid>`) and click any button on the result.
4. Navigate to `/admin/users/<your-user-id>` → Usage tab.
5. Verify the new rows appear with the input + outcome in the summary header and table; click a row to confirm the raw options JSON renders.

- [ ] **Step 5: No commit** — verification only.

---

## Self-Review Notes

Coverage check against the spec:
- Data model — Task 1 + Task 2
- Backend write endpoint with validation + silent-drop semantics — Task 4 + Task 5
- 90-day cleanup — Task 3
- Bot integration (extraction + recorder + dispatcher wiring) — Tasks 8, 9, 10
- Admin read endpoint with summary — Task 6
- OpenAPI documentation — Task 7
- Frontend (types, fetch, rewritten section, tests) — Tasks 11, 12, 13
- AGENTS.md update — Task 14
- End-to-end smoke verification — Task 15

Type consistency:
- `CommandUsagePayload` (TS) matches `recordCommandUsageRequest` (Go) field-for-field.
- `AdminCommandUsageItem` (TS) matches `CommandUsageRow` (Go) field-for-field.
- `AdminCommandUsageSummary.errorRate` is computed in the Go handler (Task 6) and consumed in the TS frontend (Tasks 11, 13).
- Outcome enum `success | error | banned | denied` is identical across spec, Go handler validation (Task 5), TS payload type (Task 8), TS bot recorder (Task 9), Go store FILTER clause (Task 2), and TS frontend badges (Task 13).
- Interaction type enum `command | button | modal | string_select` is identical across spec, Go handler validation (Task 5), TS payload type (Task 8), TS bot extractor (Task 9), and TS frontend badge (Task 13).
