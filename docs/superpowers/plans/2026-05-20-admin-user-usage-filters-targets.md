# Admin User Usage — Filters, Search, and Target Capture — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/admin/users/{userId}` Usage tab usable for incident investigation by adding filtering, searching, recipient capture for fan-out commands, ID resolution, and a sortable-table UI mirroring the Servers tab.

**Architecture:** New child table `command_usage_targets` (FK ON DELETE CASCADE off `command_usage`) snapshots subject/scope/recipient entities per interaction at write time. The bot captures these per-command in fan-out handlers, fire-and-forget. The backend gains filter query params + two sub-endpoints (`/filter-options`, `/{usageId}/targets`); list reads enrich snapshots with live joins to `app_users` and `bot_guilds`. The frontend rebuilds `UsageSection` as a sortable `Table` with a filter bar, URL-driven state, and stacked-avatar HoverCard recipient previews.

**Tech Stack:** Go 1.24 (pgx, chi) backend; Discord.js TypeScript bot; Next.js 16 / React 19 / Tailwind v4 / shadcn frontend; Postgres 16.

**Spec:** `docs/superpowers/specs/2026-05-20-admin-user-usage-filters-targets-design.md`

---

## Phase A — Schema + Store + Storage Write Path

### Task A1: Add `command_usage_targets` table to EnsureSchema

**Files:**
- Modify: `apps/backend/internal/store/postgres.go` (insert after line 927, before `return nil`)

- [ ] **Step 1: Add the table + indexes**

In `postgres.go`, immediately after the `command_usage_created_idx` block and before `return nil`:

```go
	if _, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS command_usage_targets (
			id               BIGSERIAL PRIMARY KEY,
			command_usage_id BIGINT NOT NULL REFERENCES command_usage(id) ON DELETE CASCADE,
			kind             TEXT NOT NULL,
			snowflake        TEXT NOT NULL,
			display_name     TEXT,
			avatar_hash      TEXT,
			relation         TEXT NOT NULL,
			created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CONSTRAINT command_usage_targets_kind_chk
				CHECK (kind IN ('user','role','channel','guild','steam')),
			CONSTRAINT command_usage_targets_relation_chk
				CHECK (relation IN ('recipient','scope','subject'))
		);

		CREATE INDEX IF NOT EXISTS command_usage_targets_usage_idx
		ON command_usage_targets(command_usage_id);

		CREATE INDEX IF NOT EXISTS command_usage_targets_snowflake_idx
		ON command_usage_targets(snowflake);
	`); err != nil {
		return fmt.Errorf("create command_usage_targets table: %w", err)
	}
```

- [ ] **Step 2: Run backend tests to confirm schema bootstraps cleanly**

Run: `cd apps/backend && go test ./internal/store/... -run EnsureSchema -count=1`
Expected: PASS. If no test matches, instead run `go test ./internal/store/... -count=1` and ensure no panics on schema setup.

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/store/postgres.go
git commit -m "feat(backend): add command_usage_targets table"
```

---

### Task A2: Extend `CommandUsageInsert` with `Targets`

**Files:**
- Modify: `apps/backend/internal/store/command_usage.go`
- Test: `apps/backend/internal/store/command_usage_test.go`

- [ ] **Step 1: Add the new struct + extend insert struct**

At the top of `command_usage.go` (after the existing types around line 26), add:

```go
// CommandUsageTargetInsert is one target row to associate with a usage row.
type CommandUsageTargetInsert struct {
	Kind        string  // 'user'|'role'|'channel'|'guild'|'steam'
	Snowflake   string
	DisplayName *string
	AvatarHash  *string
	Relation    string  // 'recipient'|'scope'|'subject'
}

// CommandUsageTargetRow is the read-side shape returned to handlers.
type CommandUsageTargetRow struct {
	Kind        string  `json:"kind"`
	Snowflake   string  `json:"snowflake"`
	DisplayName *string `json:"displayName"`
	AvatarHash  *string `json:"avatarHash"`
	Relation    string  `json:"relation"`
}
```

Then extend `CommandUsageInsert` (lines 13-26) by adding a final field:

```go
	LatencyMs       int
	Targets         []CommandUsageTargetInsert // capped at 500; excess silently dropped
```

- [ ] **Step 2: Add storage cap constant near top of file**

After the imports block, before `type CommandUsageInsert`:

```go
// MaxTargetsPerUsage caps how many command_usage_targets rows we persist for
// a single command_usage row. Excess is dropped at write time.
const MaxTargetsPerUsage = 500
```

- [ ] **Step 3: Write failing test for insert with targets**

In `command_usage_test.go`, add (use the existing `newTestStore` helper from the file — if it's named differently, check the file and use the matching helper):

```go
func TestInsertCommandUsage_WithTargets(t *testing.T) {
	ctx, store, appUserID := newTestStoreWithUser(t)
	guildID := "111111111111111111"
	displayBob := "bob"
	avatarBob := "abcde"

	err := store.InsertCommandUsage(ctx, CommandUsageInsert{
		AppUserID:       appUserID,
		DiscordID:       "222222222222222222",
		GuildID:         &guildID,
		InteractionType: "command",
		Command:         "announce",
		Outcome:         "success",
		LatencyMs:       42,
		Options:         []byte(`{}`),
		Targets: []CommandUsageTargetInsert{
			{Kind: "role", Snowflake: "333333333333333333", Relation: "scope"},
			{Kind: "user", Snowflake: "444444444444444444", DisplayName: &displayBob, AvatarHash: &avatarBob, Relation: "recipient"},
		},
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var count int
	if err := store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM command_usage_targets`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("want 2 target rows, got %d", count)
	}
}
```

If `newTestStoreWithUser` doesn't exist, look for the existing helper that returns a store + a known `appUserID` and use that name instead.

- [ ] **Step 4: Run the test to verify it fails**

Run: `cd apps/backend && go test ./internal/store/ -run TestInsertCommandUsage_WithTargets -count=1`
Expected: FAIL — `Targets` field unknown, OR test runs but 0 rows inserted because the implementation hasn't been wired yet.

- [ ] **Step 5: Rewrite `InsertCommandUsage` to wrap in a tx and write targets**

Replace the body of `InsertCommandUsage` (currently lines 66-86 of `command_usage.go`) with:

```go
func (p *PostgresStore) InsertCommandUsage(ctx context.Context, row CommandUsageInsert) error {
	if len(row.Options) == 0 || string(row.Options) == "null" {
		row.Options = []byte("{}")
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin command_usage tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var usageID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO command_usage (
			app_user_id, discord_id, guild_id, channel_id,
			interaction_type, command, subcommand, target,
			options, outcome, error_code, latency_ms
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12)
		RETURNING id
	`,
		row.AppUserID, row.DiscordID, row.GuildID, row.ChannelID,
		row.InteractionType, row.Command, row.Subcommand, row.Target,
		string(row.Options), row.Outcome, row.ErrorCode, row.LatencyMs,
	).Scan(&usageID); err != nil {
		return fmt.Errorf("insert command_usage: %w", err)
	}

	targets := row.Targets
	if len(targets) > MaxTargetsPerUsage {
		targets = targets[:MaxTargetsPerUsage]
	}
	for _, t := range targets {
		if _, err := tx.Exec(ctx, `
			INSERT INTO command_usage_targets (
				command_usage_id, kind, snowflake, display_name, avatar_hash, relation
			) VALUES ($1,$2,$3,$4,$5,$6)
		`, usageID, t.Kind, t.Snowflake, t.DisplayName, t.AvatarHash, t.Relation); err != nil {
			return fmt.Errorf("insert command_usage_target: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit command_usage: %w", err)
	}
	return nil
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `cd apps/backend && go test ./internal/store/ -run TestInsertCommandUsage_WithTargets -count=1`
Expected: PASS.

- [ ] **Step 7: Run the full store test suite to confirm no regressions**

Run: `cd apps/backend && go test ./internal/store/ -count=1`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add apps/backend/internal/store/command_usage.go apps/backend/internal/store/command_usage_test.go
git commit -m "feat(backend): persist command_usage_targets alongside command_usage"
```

---

### Task A3: Add cap test for `Targets`

**Files:**
- Test: `apps/backend/internal/store/command_usage_test.go`

- [ ] **Step 1: Add the cap test**

```go
func TestInsertCommandUsage_TargetsCappedAt500(t *testing.T) {
	ctx, store, appUserID := newTestStoreWithUser(t)
	targets := make([]CommandUsageTargetInsert, 0, 600)
	for i := 0; i < 600; i++ {
		targets = append(targets, CommandUsageTargetInsert{
			Kind: "user", Snowflake: fmt.Sprintf("%018d", i), Relation: "recipient",
		})
	}

	err := store.InsertCommandUsage(ctx, CommandUsageInsert{
		AppUserID:       appUserID,
		DiscordID:       "222222222222222222",
		InteractionType: "command",
		Command:         "announce",
		Outcome:         "success",
		Options:         []byte(`{}`),
		Targets:         targets,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var count int
	if err := store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM command_usage_targets`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != MaxTargetsPerUsage {
		t.Fatalf("want %d, got %d", MaxTargetsPerUsage, count)
	}
}
```

If `fmt` is not yet imported in the test file, add `"fmt"` to the import block.

- [ ] **Step 2: Run the test**

Run: `cd apps/backend && go test ./internal/store/ -run TestInsertCommandUsage_TargetsCappedAt500 -count=1`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/store/command_usage_test.go
git commit -m "test(backend): cap command_usage targets at 500 per record"
```

---

### Task A4: Wire `Targets` through the bot ingest handler

**Files:**
- Modify: `apps/backend/internal/http/bot_command_usage_handlers.go`
- Test: `apps/backend/internal/http/bot_command_usage_handlers_test.go`

- [ ] **Step 1: Extend the request struct + validation**

In `bot_command_usage_handlers.go`, add to the top of the file (with other constants near line 14):

```go
const maxDisplayNameLen = 256
const maxTargetsInRequest = 500

var validTargetKinds = map[string]struct{}{
	"user":    {},
	"role":    {},
	"channel": {},
	"guild":   {},
	"steam":   {},
}

var validTargetRelations = map[string]struct{}{
	"recipient": {},
	"scope":     {},
	"subject":   {},
}

type recordCommandUsageTarget struct {
	Kind        string  `json:"kind"`
	Snowflake   string  `json:"snowflake"`
	DisplayName *string `json:"displayName,omitempty"`
	AvatarHash  *string `json:"avatarHash,omitempty"`
	Relation    string  `json:"relation"`
}
```

Extend `recordCommandUsageRequest` (lines 31-43) by adding:

```go
	LatencyMs int                        `json:"latencyMs"`
	Targets   []recordCommandUsageTarget `json:"targets,omitempty"`
```

- [ ] **Step 2: Add the target conversion + validation block in the handler**

In `RecordCommandUsage`, immediately before the `InsertCommandUsage` call (around line 112), add:

```go
	// Validate and convert targets. Drop any invalid entries silently —
	// fire-and-forget posture: never block the bot reply.
	storeTargets := make([]store.CommandUsageTargetInsert, 0, len(req.Targets))
	if len(req.Targets) > maxTargetsInRequest {
		req.Targets = req.Targets[:maxTargetsInRequest]
	}
	for _, t := range req.Targets {
		if _, ok := validTargetKinds[t.Kind]; !ok {
			continue
		}
		if _, ok := validTargetRelations[t.Relation]; !ok {
			continue
		}
		// Snowflake validation: Discord snowflake for user|role|channel|guild,
		// 17-digit numeric for steam.
		if t.Kind == "steam" {
			if !isSteam64(t.Snowflake) {
				continue
			}
		} else if !validateSnowflakeValue(t.Snowflake) {
			continue
		}
		if t.DisplayName != nil && utf8.RuneCountInString(*t.DisplayName) > maxDisplayNameLen {
			runes := []rune(*t.DisplayName)
			trimmed := string(runes[:maxDisplayNameLen])
			t.DisplayName = &trimmed
		}
		storeTargets = append(storeTargets, store.CommandUsageTargetInsert{
			Kind:        t.Kind,
			Snowflake:   t.Snowflake,
			DisplayName: t.DisplayName,
			AvatarHash:  t.AvatarHash,
			Relation:    t.Relation,
		})
	}
```

Then update the `InsertCommandUsage` call to include `Targets: storeTargets`:

```go
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
		Targets:         storeTargets,
	}); err != nil {
```

- [ ] **Step 3: Add `isSteam64` helper if not present**

Run: `cd apps/backend && grep -rn "func isSteam64" internal/`
If it doesn't exist, add to `bot_command_usage_handlers.go` near the snowflake helper:

```go
func isSteam64(s string) bool {
	if len(s) != 17 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Add test for happy-path target ingest**

In `bot_command_usage_handlers_test.go`, add:

```go
func TestRecordCommandUsage_PersistsTargets(t *testing.T) {
	fx := setupTestHandler(t)
	defer fx.cleanup()

	body := `{
		"discordId":"222222222222222222",
		"guildId":"111111111111111111",
		"interactionType":"command",
		"command":"announce",
		"outcome":"success",
		"latencyMs":42,
		"options":{},
		"targets":[
			{"kind":"role","snowflake":"333333333333333333","relation":"scope"},
			{"kind":"user","snowflake":"444444444444444444","displayName":"bob","relation":"recipient"}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/internal/bot/setup/command-usage", strings.NewReader(body))
	rr := httptest.NewRecorder()
	fx.h.RecordCommandUsage(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d body=%s", rr.Code, rr.Body.String())
	}

	var count int
	if err := fx.db.QueryRow(fx.ctx, `SELECT COUNT(*) FROM command_usage_targets`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("want 2 target rows, got %d", count)
	}
}
```

If the fixture's field is `fx.h` and `fx.db` and `fx.ctx`, use those; otherwise adapt to the existing fixture shape used in the file (look at `TestRecordCommandUsage_HappyPath` for the canonical pattern).

- [ ] **Step 5: Add test for invalid-target silent drop**

```go
func TestRecordCommandUsage_DropsInvalidTargetsSilently(t *testing.T) {
	fx := setupTestHandler(t)
	defer fx.cleanup()

	body := `{
		"discordId":"222222222222222222",
		"interactionType":"command",
		"command":"announce",
		"outcome":"success",
		"latencyMs":1,
		"options":{},
		"targets":[
			{"kind":"banana","snowflake":"333333333333333333","relation":"scope"},
			{"kind":"role","snowflake":"not-a-snowflake","relation":"scope"},
			{"kind":"user","snowflake":"444444444444444444","relation":"recipient"}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/internal/bot/setup/command-usage", strings.NewReader(body))
	rr := httptest.NewRecorder()
	fx.h.RecordCommandUsage(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", rr.Code)
	}
	var count int
	if err := fx.db.QueryRow(fx.ctx, `SELECT COUNT(*) FROM command_usage_targets`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1 valid target row, got %d", count)
	}
}
```

- [ ] **Step 6: Run the tests**

Run: `cd apps/backend && go test ./internal/http/ -run TestRecordCommandUsage -count=1`
Expected: PASS for all three tests (existing happy-path + two new).

- [ ] **Step 7: Commit**

```bash
git add apps/backend/internal/http/bot_command_usage_handlers.go apps/backend/internal/http/bot_command_usage_handlers_test.go
git commit -m "feat(backend): accept targets in command-usage bot ingest"
```

---

## Phase B — Bot Capture

### Task B1: Extend the bot CommandUsagePayload + recorder

**Files:**
- Modify: `apps/bot/src/backend-client.ts:507-519`
- Modify: `apps/bot/src/command-usage.ts`
- Test: `apps/bot/src/command-usage.test.ts`

- [ ] **Step 1: Add the `Target` type and field to `CommandUsagePayload`**

In `backend-client.ts`, replace the `CommandUsagePayload` type (lines 507-519) with:

```ts
export type CommandUsageTarget = {
  kind:         "user" | "role" | "channel" | "guild" | "steam";
  snowflake:    string;
  displayName?: string | null;
  avatarHash?:  string | null;
  relation:     "recipient" | "scope" | "subject";
};

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
  targets?:        CommandUsageTarget[];
};
```

- [ ] **Step 2: Update `recordCommandUsage` signature to accept optional targets**

In `command-usage.ts`, modify the export to allow a `targets` parameter:

```ts
export function recordCommandUsage(
  interaction: Interaction,
  outcome: Outcome,
  errorCode: string | null,
  startedAt: number,
  targets: CommandUsageTarget[] | undefined = undefined,
): void {
  const payload = buildCommandUsagePayload(
    interaction,
    outcome,
    errorCode,
    Date.now() - startedAt,
  );
  if (!payload) return;
  if (!payload.discordId) return;
  if (targets && targets.length > 0) payload.targets = targets;

  getBackend()
    .recordCommandUsage(getWorkerID(), payload)
    .catch((err: unknown) => {
      console.warn(JSON.stringify({
        level:   "warn",
        message: "[bot] recordCommandUsage failed",
        userId:  payload.discordId,
        command: payload.command,
        error:   String(err),
      }));
    });
}
```

Add `import type { CommandUsageTarget } from "./backend-client.js";` to the import block at the top.

- [ ] **Step 3: Write failing test**

In `command-usage.test.ts`, add:

```ts
import { recordCommandUsage, __setBackendForTests, __resetForTests } from "./command-usage.js";
import type { CommandUsagePayload } from "./backend-client.js";

it("forwards targets in the payload when provided", () => {
  const captured: CommandUsagePayload[] = [];
  __setBackendForTests({
    recordCommandUsage: async (_w, p) => { captured.push(p); },
  });
  // Use the same chat-input interaction mock pattern the file already uses.
  const interaction = makeChatInputInteractionMock({ commandName: "announce" });
  recordCommandUsage(interaction, "success", null, Date.now(), [
    { kind: "role", snowflake: "333333333333333333", relation: "scope" },
    { kind: "user", snowflake: "444444444444444444", displayName: "bob", relation: "recipient" },
  ]);
  expect(captured.length).toBe(1);
  expect(captured[0].targets).toHaveLength(2);
  expect(captured[0].targets?.[0].kind).toBe("role");
  __resetForTests();
});
```

If `makeChatInputInteractionMock` doesn't exist by that name, copy the inline mock factory used in the file's existing chat-input test (around line 95-105 — search for `commandName: "track"`).

- [ ] **Step 4: Run the test**

Run: `pnpm --filter @rustlens/bot test --run command-usage`
Expected: PASS.

- [ ] **Step 5: Scaffold the dispatcher to forward targets**

Open `apps/bot/src/shard-worker.ts`. Locate the `interactionCreate` handler (starts at line 335).

5a. At line 339, after `const startedAt = Date.now();` and before `let outcome: Outcome = "success";`, add:

```ts
  let recordTargets: import("./backend-client.js").CommandUsageTarget[] | undefined;
```

5b. Update the `finally` block at line 610-613 to pass `recordTargets`:

```ts
  } finally {
    if (trackUsage) {
      recordCommandUsage(interaction, outcome, errorCode, startedAt, recordTargets);
    }
  }
```

- [ ] **Step 6: Run bot tests + commit**

Run: `pnpm --filter @rustlens/bot test --run`
Expected: PASS.

```bash
git add apps/bot/src/backend-client.ts apps/bot/src/command-usage.ts apps/bot/src/command-usage.test.ts apps/bot/src/shard-worker.ts
git commit -m "feat(bot): pipe targets[] through recordCommandUsage"
```

---

### Task B2: Capture targets in `/announce`

**Files:**
- Modify: `apps/bot/src/commands/announce.ts`

- [ ] **Step 1: Locate the dispatcher to confirm how startedAt + recordCommandUsage gets called**

Run: `grep -n "recordCommandUsage" apps/bot/src/shard-worker.ts`
Expected: a single call site near the end of the dispatcher that wraps the whole dispatch in try/finally. The dispatcher passes `interaction`, outcome, errorCode, and `startedAt`. The capture for `/announce` happens **inside** `handleAnnounceChatInput` because that's where the recipient list exists.

The pattern: we return the targets from `handleAnnounceChatInput` via a side-channel — the simplest is to expose a per-interaction map keyed by `interaction.id`. To avoid global state, switch the contract so `handleAnnounceChatInput` returns `{ handled: boolean; targets?: CommandUsageTarget[] }` and the shard worker reads it.

Check the current return type:

Run: `grep -n "handleAnnounceChatInput" apps/bot/src/shard-worker.ts apps/bot/src/commands/announce.ts`

If `handleAnnounceChatInput` currently returns `Promise<boolean>`, we'll change the return shape to `Promise<HandlerResult>` where `HandlerResult = boolean | { handled: true; targets: CommandUsageTarget[] }`. Use the union so we don't have to rewrite the other 20 handlers in this task.

- [ ] **Step 2: Add a small helper type at top of `announce.ts`**

After the existing imports:

```ts
import type { CommandUsageTarget } from "../backend-client.js";

export type AnnounceHandlerResult = {
  handled: true;
  targets: CommandUsageTarget[];
};
```

- [ ] **Step 3: Change `handleAnnounceChatInput` return type and capture targets**

Replace the return type and the success path of `handleAnnounceChatInput`:

Change the signature line `export async function handleAnnounceChatInput(interaction: ChatInputCommandInteraction)` to:

```ts
export async function handleAnnounceChatInput(
  interaction: ChatInputCommandInteraction,
): Promise<true | AnnounceHandlerResult> {
```

For every early-return that currently does `return true;`, leave it as-is (those paths recorded no fan-out).

At the bottom of the function, **after** the existing `await interaction.editReply(...)` call (line 144), replace `return true;` with:

```ts
  const sentTargets: CommandUsageTarget[] = [];
  // Note: we collect targets inline during the send loop above. Refactor needed.
```

That comment marks the issue — we actually need to collect during the Promise.all loop. Replace the existing `await Promise.all(...)` block (lines 129-142) with:

```ts
  const successful: { id: string; username: string; avatar: string | null }[] = [];

  await Promise.all(
    targets.map(async (target) => {
      try {
        await target.send({ embeds: [embed] });
        sent++;
        successful.push({
          id:       target.id,
          username: target.user.globalName ?? target.user.username,
          avatar:   target.user.avatar ?? null,
        });
      } catch (error) {
        logDiscordError("/announce send DM", error, {
          guildId: interaction.guildId,
          discordId: target.id,
        });
        failed++;
      }
    }),
  );
```

Then replace the final `return true;` with:

```ts
  const usageTargets: CommandUsageTarget[] = [
    { kind: "role", snowflake: role.id, displayName: role.name, relation: "scope" },
    ...successful.map<CommandUsageTarget>((s) => ({
      kind:        "user",
      snowflake:   s.id,
      displayName: s.username,
      avatarHash:  s.avatar,
      relation:    "recipient",
    })),
  ];
  return { handled: true, targets: usageTargets };
```

- [ ] **Step 4: Update the shard worker dispatcher call site**

In `apps/bot/src/shard-worker.ts`, replace the existing call site (currently lines 414-416):

```ts
      if (await handleAnnounceChatInput(interaction)) {
        return;
      }
```

with:

```ts
      {
        const result = await handleAnnounceChatInput(interaction);
        if (result) {
          if (result !== true) recordTargets = result.targets;
          return;
        }
      }
```

The `recordTargets` variable was declared in Task B1 step 5a; this assigns to it, and the `finally` block forwards it to `recordCommandUsage`.

- [ ] **Step 5: Run the full bot test suite to confirm no regressions**

Run: `pnpm --filter @rustlens/bot test --run`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/bot/src/commands/announce.ts apps/bot/src/shard-worker.ts
git commit -m "feat(bot): capture announce recipients as command_usage targets"
```

---

### Task B3: Capture targets in `/summon`

**Files:**
- Modify: `apps/bot/src/commands/summon.ts`

- [ ] **Step 1: Add the result type + capture moved members**

Open `summon.ts` and locate the handler entry point (likely `handleSummonChatInput`). At the top of the file with other imports:

```ts
import type { CommandUsageTarget } from "../backend-client.js";

export type SummonHandlerResult = {
  handled: true;
  targets: CommandUsageTarget[];
};
```

Change the handler's return type from `Promise<boolean>` to `Promise<true | SummonHandlerResult>`.

Inside the loop where each member is moved into the invoker's voice channel, collect successful moves:

```ts
const moved: { id: string; username: string; avatar: string | null }[] = [];
// inside the per-member loop, after the successful voice.setChannel:
moved.push({
  id:       member.id,
  username: member.user.globalName ?? member.user.username,
  avatar:   member.user.avatar ?? null,
});
```

At the final return:

```ts
const usageTargets: CommandUsageTarget[] = [
  { kind: "role", snowflake: role.id, displayName: role.name, relation: "scope" },
  ...moved.map<CommandUsageTarget>((m) => ({
    kind:        "user",
    snowflake:   m.id,
    displayName: m.username,
    avatarHash:  m.avatar,
    relation:    "recipient",
  })),
];
return { handled: true, targets: usageTargets };
```

If the existing handler doesn't have a `role` variable in scope at return time, derive it from the option earlier in the handler.

- [ ] **Step 2: Update the dispatcher branch for summon**

In `apps/bot/src/shard-worker.ts`, replace the existing call site (currently lines 410-412):

```ts
      if (await handleSummonChatInput(interaction)) {
        return;
      }
```

with:

```ts
      {
        const result = await handleSummonChatInput(interaction);
        if (result) {
          if (result !== true) recordTargets = result.targets;
          return;
        }
      }
```

- [ ] **Step 3: Run tests**

Run: `pnpm --filter @rustlens/bot test --run`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/bot/src/commands/summon.ts apps/bot/src/shard-worker.ts
git commit -m "feat(bot): capture summon recipients as command_usage targets"
```

---

### Task B4: Capture targets in `/nickname`

**Files:**
- Modify: `apps/bot/src/commands/nickname.ts`

- [ ] **Step 1: Add result type + capture each successful nick edit**

Mirror Task B3 — at the top of `nickname.ts`:

```ts
import type { CommandUsageTarget } from "../backend-client.js";

export type NicknameHandlerResult = {
  handled: true;
  targets: CommandUsageTarget[];
};
```

For each member whose nickname is successfully edited, push a `recipient` target:

```ts
const updated: { id: string; username: string; avatar: string | null }[] = [];
// after successful setNickname call:
updated.push({
  id:       member.id,
  username: member.user.globalName ?? member.user.username,
  avatar:   member.user.avatar ?? null,
});
```

Return at the bottom:

```ts
return {
  handled: true,
  targets: updated.map<CommandUsageTarget>((m) => ({
    kind:        "user",
    snowflake:   m.id,
    displayName: m.username,
    avatarHash:  m.avatar,
    relation:    "recipient",
  })),
};
```

- [ ] **Step 2: Update dispatcher branch**

In `apps/bot/src/shard-worker.ts`, replace the existing call site (currently lines 450-452):

```ts
      if (await handleNicknameChatInput(interaction)) {
        return;
      }
```

with:

```ts
      {
        const result = await handleNicknameChatInput(interaction);
        if (result) {
          if (result !== true) recordTargets = result.targets;
          return;
        }
      }
```

- [ ] **Step 3: Run tests + commit**

Run: `pnpm --filter @rustlens/bot test --run`

```bash
git add apps/bot/src/commands/nickname.ts apps/bot/src/shard-worker.ts
git commit -m "feat(bot): capture nickname edits as command_usage targets"
```

---

### Task B5: Capture targets in `/admin setsteam` and `/admin removesteam`

**Files:**
- Modify: `apps/bot/src/commands/admin.ts`

- [ ] **Step 1: Add result type + capture subject(s)**

```ts
import type { CommandUsageTarget } from "../backend-client.js";

export type AdminHandlerResult = {
  handled: true;
  targets: CommandUsageTarget[];
};
```

In the `/admin setsteam` subcommand handler, after the successful link write, return:

```ts
return {
  handled: true,
  targets: [
    {
      kind:        "user",
      snowflake:   targetUser.id,
      displayName: targetUser.globalName ?? targetUser.username,
      avatarHash:  targetUser.avatar ?? null,
      relation:    "subject",
    },
    { kind: "steam", snowflake: steamId, relation: "subject" },
  ],
};
```

In the `/admin removesteam` subcommand handler, after the successful delete:

```ts
return {
  handled: true,
  targets: [
    {
      kind:        "user",
      snowflake:   targetUser.id,
      displayName: targetUser.globalName ?? targetUser.username,
      avatarHash:  targetUser.avatar ?? null,
      relation:    "subject",
    },
  ],
};
```

Use the exact variable names the existing handlers use; the field names above (`targetUser`) are illustrative — check the file.

- [ ] **Step 2: Update dispatcher branch**

In `apps/bot/src/shard-worker.ts`, locate the `handleAdminChatInput` call site (currently lines 406-408) and wrap it the same way:

```ts
      {
        const result = await handleAdminChatInput(interaction);
        if (result) {
          if (result !== true) recordTargets = result.targets;
          return;
        }
      }
```

- [ ] **Step 3: Run tests + commit**

Run: `pnpm --filter @rustlens/bot test --run`

```bash
git add apps/bot/src/commands/admin.ts apps/bot/src/shard-worker.ts
git commit -m "feat(bot): capture /admin setsteam|removesteam targets"
```

---

### Task B6: Capture targets in `/manage ban` modal submit + `/setsteam`

**Files:**
- Modify: `apps/bot/src/commands/manage.ts` (or wherever the `/manage ban` modal submit handler lives)
- Modify: `apps/bot/src/commands/setsteam.ts`

- [ ] **Step 1: `/manage ban` modal submit — return banned user as subject**

In the ban modal-submit handler, after the successful backend PUT:

```ts
return {
  handled: true,
  targets: [
    {
      kind:        "user",
      snowflake:   bannedUser.id,
      displayName: bannedUser.globalName ?? bannedUser.username,
      avatarHash:  bannedUser.avatar ?? null,
      relation:    "subject",
    },
  ],
};
```

- [ ] **Step 2: `/setsteam` — return steam as subject**

In `setsteam.ts`, after the successful link create:

```ts
return {
  handled: true,
  targets: [
    { kind: "steam", snowflake: steamId, relation: "subject" },
  ],
};
```

- [ ] **Step 3: Update dispatcher branches**

In `apps/bot/src/shard-worker.ts`, locate the `handleManageChatInput` and `handleSetSteamChatInput` (or equivalent) call sites and wrap each:

```ts
      {
        const result = await handleManageChatInput(interaction);
        if (result) {
          if (result !== true) recordTargets = result.targets;
          return;
        }
      }
```

For `/manage ban`, the modal-submit handler is dispatched separately from the chat-input. Find the modal-submit dispatch (search for `isModalSubmit` in `shard-worker.ts`) and apply the same wrapping pattern to the manage ban modal handler call site.

- [ ] **Step 4: Run tests + commit**

Run: `pnpm --filter @rustlens/bot test --run`

```bash
git add apps/bot/src/commands/manage.ts apps/bot/src/commands/setsteam.ts apps/bot/src/shard-worker.ts
git commit -m "feat(bot): capture /manage ban and /setsteam targets"
```

---

### Task B7: Capture targets in `/transfer`, `/create invite`, `/track add|remove`

**Files:**
- Modify: `apps/bot/src/commands/transfer.ts`
- Modify: `apps/bot/src/commands/create.ts`
- Modify: `apps/bot/src/commands/track.ts`

- [ ] **Step 1: `/transfer` — each user a link was created for is a recipient**

After the bulk link-creation block, push a target per Discord ID that was actually linked (skip the ones already in the guild or skipped due to missing membership):

```ts
const recipients: CommandUsageTarget[] = createdLinks.map((u) => ({
  kind:        "user",
  snowflake:   u.discordId,
  displayName: u.displayName ?? null,
  avatarHash:  u.avatarHash ?? null,
  relation:    "recipient",
}));
return { handled: true, targets: recipients };
```

The variable name `createdLinks` is illustrative — adapt to whatever the existing handler builds.

- [ ] **Step 2: `/create invite` accept button — inviter is recipient, requester is subject**

In the accept-button handler, after the `/clan invite` message is posted and the requester DM is sent:

```ts
return {
  handled: true,
  targets: [
    {
      kind:        "user",
      snowflake:   requester.id,
      displayName: requester.globalName ?? requester.username,
      avatarHash:  requester.avatar ?? null,
      relation:    "subject",
    },
    {
      kind:        "user",
      snowflake:   inviter.id,
      displayName: inviter.globalName ?? inviter.username,
      avatarHash:  inviter.avatar ?? null,
      relation:    "recipient",
    },
  ],
};
```

- [ ] **Step 3: `/track add` and `/track remove` — each resolved steam ID is a subject**

For each successfully added/removed steam ID:

```ts
const subjects: CommandUsageTarget[] = resolvedSteamIds.map((s) => ({
  kind:      "steam",
  snowflake: s,
  relation:  "subject",
}));
return { handled: true, targets: subjects };
```

- [ ] **Step 4: Update dispatcher branches**

In `apps/bot/src/shard-worker.ts`, locate `handleTransferChatInput`, `handleCreateChatInput` (or the create invite button handler in the `isButton` branch), and `handleTrackChatInput` call sites. Wrap each:

```ts
      {
        const result = await handleTransferChatInput(interaction);
        if (result) {
          if (result !== true) recordTargets = result.targets;
          return;
        }
      }
```

Apply the identical pattern to the create and track call sites (changing only the handler name).

- [ ] **Step 5: Run tests + commit**

Run: `pnpm --filter @rustlens/bot test --run`

```bash
git add apps/bot/src/commands/transfer.ts apps/bot/src/commands/create.ts apps/bot/src/commands/track.ts apps/bot/src/shard-worker.ts
git commit -m "feat(bot): capture /transfer, /create invite, /track add|remove targets"
```

---

## Phase C — Backend Read API

### Task C1: Store-level filtered list with embedded targets

**Files:**
- Modify: `apps/backend/internal/store/command_usage.go`
- Test: `apps/backend/internal/store/command_usage_test.go`

- [ ] **Step 1: Add the filter struct + new list method**

In `command_usage.go`, append:

```go
// CommandUsageListFilters narrows ListUserCommandUsage. All fields optional.
type CommandUsageListFilters struct {
	Q                string   // ILIKE %q% across command/subcommand/target + targets.display_name/snowflake
	Outcomes         []string // success|error|banned|denied
	InteractionTypes []string // command|button|modal|string_select
	Command          string   // exact match
	GuildID          string   // exact match
	WindowDays       int      // 0 = no extra window filter; 1/7/30/90 supported
}

// CommandUsageRowWithTargets is a row plus its inline target snapshot list.
type CommandUsageRowWithTargets struct {
	CommandUsageRow
	Targets     []CommandUsageTargetRow `json:"targets"`
	TargetTotal *int                    `json:"targetTotal,omitempty"`
}

type CommandUsagePageWithTargets struct {
	Items []CommandUsageRowWithTargets
	Total int64
}

const InlineTargetCap = 100

func (p *PostgresStore) ListUserCommandUsageFiltered(
	ctx context.Context,
	appUserID string,
	filters CommandUsageListFilters,
	limit, offset int,
) (CommandUsagePageWithTargets, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	args := []any{appUserID}
	where := []string{"cu.app_user_id = $1"}

	if filters.WindowDays > 0 {
		args = append(args, time.Now().Add(-time.Duration(filters.WindowDays)*24*time.Hour))
		where = append(where, fmt.Sprintf("cu.created_at >= $%d", len(args)))
	}
	if filters.Command != "" {
		args = append(args, filters.Command)
		where = append(where, fmt.Sprintf("cu.command = $%d", len(args)))
	}
	if filters.GuildID != "" {
		args = append(args, filters.GuildID)
		where = append(where, fmt.Sprintf("cu.guild_id = $%d", len(args)))
	}
	if len(filters.Outcomes) > 0 {
		args = append(args, filters.Outcomes)
		where = append(where, fmt.Sprintf("cu.outcome = ANY($%d)", len(args)))
	}
	if len(filters.InteractionTypes) > 0 {
		args = append(args, filters.InteractionTypes)
		where = append(where, fmt.Sprintf("cu.interaction_type = ANY($%d)", len(args)))
	}
	if filters.Q != "" {
		args = append(args, "%"+filters.Q+"%")
		qIdx := len(args)
		where = append(where, fmt.Sprintf(`(
			cu.command ILIKE $%d
			OR cu.subcommand ILIKE $%d
			OR cu.target ILIKE $%d
			OR EXISTS (
				SELECT 1 FROM command_usage_targets t
				WHERE t.command_usage_id = cu.id
				AND (t.display_name ILIKE $%d OR t.snowflake ILIKE $%d)
			)
		)`, qIdx, qIdx, qIdx, qIdx, qIdx))
	}

	whereSQL := strings.Join(where, " AND ")

	var page CommandUsagePageWithTargets
	countSQL := "SELECT COUNT(*) FROM command_usage cu WHERE " + whereSQL
	if err := p.pool.QueryRow(ctx, countSQL, args...).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count filtered command_usage: %w", err)
	}
	if page.Total == 0 {
		page.Items = []CommandUsageRowWithTargets{}
		return page, nil
	}

	args = append(args, limit, offset)
	limitIdx := len(args) - 1
	offsetIdx := len(args)
	listSQL := fmt.Sprintf(`
		SELECT
			cu.id, cu.app_user_id, cu.discord_id, cu.guild_id, cu.channel_id,
			cu.interaction_type, cu.command, cu.subcommand, cu.target,
			cu.options, cu.outcome, cu.error_code, cu.latency_ms, cu.created_at,
			COALESCE(
				(SELECT jsonb_agg(jsonb_build_object(
					'kind', t.kind,
					'snowflake', t.snowflake,
					'displayName', t.display_name,
					'avatarHash', t.avatar_hash,
					'relation', t.relation
				) ORDER BY t.id)
				 FROM (
					SELECT id, kind, snowflake, display_name, avatar_hash, relation
					FROM command_usage_targets
					WHERE command_usage_id = cu.id
					ORDER BY id ASC
					LIMIT %d
				 ) t),
				'[]'::jsonb
			) AS targets_json,
			(SELECT COUNT(*) FROM command_usage_targets WHERE command_usage_id = cu.id) AS target_total
		FROM command_usage cu
		WHERE %s
		ORDER BY cu.created_at DESC, cu.id DESC
		LIMIT $%d OFFSET $%d
	`, InlineTargetCap, whereSQL, limitIdx, offsetIdx)

	rows, err := p.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return page, fmt.Errorf("list filtered command_usage: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var r CommandUsageRowWithTargets
		var targetsJSON []byte
		var targetTotal int64
		if err := rows.Scan(
			&r.ID, &r.AppUserID, &r.DiscordID, &r.GuildID, &r.ChannelID,
			&r.InteractionType, &r.Command, &r.Subcommand, &r.Target,
			&r.Options, &r.Outcome, &r.ErrorCode, &r.LatencyMs, &r.CreatedAt,
			&targetsJSON, &targetTotal,
		); err != nil {
			return page, fmt.Errorf("scan filtered command_usage: %w", err)
		}
		if err := json.Unmarshal(targetsJSON, &r.Targets); err != nil {
			return page, fmt.Errorf("unmarshal targets: %w", err)
		}
		if r.Targets == nil {
			r.Targets = []CommandUsageTargetRow{}
		}
		if targetTotal > int64(InlineTargetCap) {
			total := int(targetTotal)
			r.TargetTotal = &total
		}
		page.Items = append(page.Items, r)
	}
	return page, rows.Err()
}
```

Add `"strings"` and `"time"` to the import block if not already present (they should be).

- [ ] **Step 2: Write failing filter test**

```go
func TestListUserCommandUsageFiltered_OutcomeFilter(t *testing.T) {
	ctx, store, appUserID := newTestStoreWithUser(t)
	// insert one success + one error
	for _, outcome := range []string{"success", "error"} {
		if err := store.InsertCommandUsage(ctx, CommandUsageInsert{
			AppUserID:       appUserID,
			DiscordID:       "222222222222222222",
			InteractionType: "command",
			Command:         "ping",
			Outcome:         outcome,
			Options:         []byte(`{}`),
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	page, err := store.ListUserCommandUsageFiltered(ctx, appUserID,
		CommandUsageListFilters{Outcomes: []string{"error"}}, 25, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Outcome != "error" {
		t.Fatalf("want one error row, got total=%d items=%d", page.Total, len(page.Items))
	}
}
```

- [ ] **Step 3: Run tests + commit**

Run: `cd apps/backend && go test ./internal/store/ -run TestListUserCommandUsageFiltered -count=1`

```bash
git add apps/backend/internal/store/command_usage.go apps/backend/internal/store/command_usage_test.go
git commit -m "feat(backend): filtered command_usage list with embedded targets"
```

---

### Task C2: Filter-options + targets sub-list store methods

**Files:**
- Modify: `apps/backend/internal/store/command_usage.go`
- Test: `apps/backend/internal/store/command_usage_test.go`

- [ ] **Step 1: Add filter-options method**

```go
type CommandUsageFilterGuild struct {
	GuildID  string  `json:"guildId"`
	Name     string  `json:"name"`
	IconHash *string `json:"iconHash"`
}

type CommandUsageFilterOptions struct {
	Commands []string                  `json:"commands"`
	Guilds   []CommandUsageFilterGuild `json:"guilds"`
}

func (p *PostgresStore) CommandUsageFilterOptions(
	ctx context.Context,
	appUserID string,
	windowDays int,
) (CommandUsageFilterOptions, error) {
	out := CommandUsageFilterOptions{
		Commands: []string{},
		Guilds:   []CommandUsageFilterGuild{},
	}
	if windowDays <= 0 {
		windowDays = 90
	}
	cutoff := time.Now().Add(-time.Duration(windowDays) * 24 * time.Hour)

	rows, err := p.pool.Query(ctx, `
		SELECT DISTINCT command
		FROM command_usage
		WHERE app_user_id = $1 AND created_at >= $2
		ORDER BY command ASC
	`, appUserID, cutoff)
	if err != nil {
		return out, fmt.Errorf("filter commands: %w", err)
	}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return out, fmt.Errorf("scan filter command: %w", err)
		}
		out.Commands = append(out.Commands, c)
	}
	rows.Close()

	guildRows, err := p.pool.Query(ctx, `
		SELECT DISTINCT cu.guild_id, COALESCE(bg.name, '') AS name, bg.icon_hash
		FROM command_usage cu
		LEFT JOIN bot_guilds bg ON bg.guild_id = cu.guild_id
		WHERE cu.app_user_id = $1 AND cu.created_at >= $2 AND cu.guild_id IS NOT NULL
		ORDER BY name ASC, cu.guild_id ASC
	`, appUserID, cutoff)
	if err != nil {
		return out, fmt.Errorf("filter guilds: %w", err)
	}
	defer guildRows.Close()
	for guildRows.Next() {
		var g CommandUsageFilterGuild
		if err := guildRows.Scan(&g.GuildID, &g.Name, &g.IconHash); err != nil {
			return out, fmt.Errorf("scan filter guild: %w", err)
		}
		out.Guilds = append(out.Guilds, g)
	}
	return out, guildRows.Err()
}
```

- [ ] **Step 2: Add `ListCommandUsageTargets` for the full-list sub-endpoint**

```go
func (p *PostgresStore) ListCommandUsageTargets(
	ctx context.Context,
	usageID int64,
) ([]CommandUsageTargetRow, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT kind, snowflake, display_name, avatar_hash, relation
		FROM command_usage_targets
		WHERE command_usage_id = $1
		ORDER BY id ASC
	`, usageID)
	if err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}
	defer rows.Close()
	out := []CommandUsageTargetRow{}
	for rows.Next() {
		var t CommandUsageTargetRow
		if err := rows.Scan(&t.Kind, &t.Snowflake, &t.DisplayName, &t.AvatarHash, &t.Relation); err != nil {
			return nil, fmt.Errorf("scan target: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
```

- [ ] **Step 3: Write filter-options test**

```go
func TestCommandUsageFilterOptions_DistinctCommandsAndGuilds(t *testing.T) {
	ctx, store, appUserID := newTestStoreWithUser(t)
	gid := "111111111111111111"
	for _, c := range []string{"track", "track", "announce"} {
		if err := store.InsertCommandUsage(ctx, CommandUsageInsert{
			AppUserID:       appUserID,
			DiscordID:       "222222222222222222",
			GuildID:         &gid,
			InteractionType: "command",
			Command:         c,
			Outcome:         "success",
			Options:         []byte(`{}`),
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	opts, err := store.CommandUsageFilterOptions(ctx, appUserID, 90)
	if err != nil {
		t.Fatalf("filter opts: %v", err)
	}
	if len(opts.Commands) != 2 {
		t.Fatalf("want 2 distinct commands, got %d", len(opts.Commands))
	}
	if len(opts.Guilds) != 1 || opts.Guilds[0].GuildID != gid {
		t.Fatalf("want 1 guild %s, got %+v", gid, opts.Guilds)
	}
}
```

- [ ] **Step 4: Run + commit**

```bash
git add apps/backend/internal/store/command_usage.go apps/backend/internal/store/command_usage_test.go
git commit -m "feat(backend): command_usage filter options + targets sub-list"
```

---

### Task C3: Read-side enrichment join (app_users + bot_guilds)

**Files:**
- Modify: `apps/backend/internal/store/command_usage.go`

- [ ] **Step 1: Add an enrichment pass**

After the existing query in `ListUserCommandUsageFiltered`, before `return page, rows.Err()`, add enrichment:

```go
	// Collect all distinct user-target snowflakes and guild snowflakes across the page.
	userSnowflakes := map[string]struct{}{}
	guildSnowflakes := map[string]struct{}{}
	for _, item := range page.Items {
		for _, t := range item.Targets {
			switch t.Kind {
			case "user":
				userSnowflakes[t.Snowflake] = struct{}{}
			case "guild":
				guildSnowflakes[t.Snowflake] = struct{}{}
			}
		}
	}

	type userEnrichment struct {
		AppUserID   string
		DisplayName *string
		AvatarHash  *string
	}
	userMap := map[string]userEnrichment{}
	if len(userSnowflakes) > 0 {
		ids := make([]string, 0, len(userSnowflakes))
		for s := range userSnowflakes {
			ids = append(ids, s)
		}
		uRows, err := p.pool.Query(ctx, `
			SELECT discord_id, id, COALESCE(discord_global_name, discord_username), discord_avatar_hash
			FROM app_users
			WHERE discord_id = ANY($1)
		`, ids)
		if err != nil {
			return page, fmt.Errorf("enrich users: %w", err)
		}
		for uRows.Next() {
			var discordID, appUserID string
			var displayName string
			var avatar *string
			if err := uRows.Scan(&discordID, &appUserID, &displayName, &avatar); err != nil {
				uRows.Close()
				return page, fmt.Errorf("scan enrich user: %w", err)
			}
			dn := displayName
			userMap[discordID] = userEnrichment{AppUserID: appUserID, DisplayName: &dn, AvatarHash: avatar}
		}
		uRows.Close()
	}

	type guildEnrichment struct {
		Name     string
		IconHash *string
	}
	guildMap := map[string]guildEnrichment{}
	if len(guildSnowflakes) > 0 {
		ids := make([]string, 0, len(guildSnowflakes))
		for s := range guildSnowflakes {
			ids = append(ids, s)
		}
		gRows, err := p.pool.Query(ctx, `
			SELECT guild_id, name, icon_hash
			FROM bot_guilds
			WHERE guild_id = ANY($1)
		`, ids)
		if err != nil {
			return page, fmt.Errorf("enrich guilds: %w", err)
		}
		for gRows.Next() {
			var gid, name string
			var icon *string
			if err := gRows.Scan(&gid, &name, &icon); err != nil {
				gRows.Close()
				return page, fmt.Errorf("scan enrich guild: %w", err)
			}
			guildMap[gid] = guildEnrichment{Name: name, IconHash: icon}
		}
		gRows.Close()
	}

	// Apply enrichment: live values win over snapshot.
	for i := range page.Items {
		for j := range page.Items[i].Targets {
			t := &page.Items[i].Targets[j]
			switch t.Kind {
			case "user":
				if u, ok := userMap[t.Snowflake]; ok {
					if u.DisplayName != nil {
						t.DisplayName = u.DisplayName
					}
					if u.AvatarHash != nil {
						t.AvatarHash = u.AvatarHash
					}
					t.AppUserID = &u.AppUserID
				}
			case "guild":
				if g, ok := guildMap[t.Snowflake]; ok {
					name := g.Name
					t.DisplayName = &name
					if g.IconHash != nil {
						t.AvatarHash = g.IconHash
					}
				}
			}
		}
	}
```

Add `AppUserID` field to `CommandUsageTargetRow`:

```go
type CommandUsageTargetRow struct {
	Kind        string  `json:"kind"`
	Snowflake   string  `json:"snowflake"`
	DisplayName *string `json:"displayName"`
	AvatarHash  *string `json:"avatarHash"`
	Relation    string  `json:"relation"`
	AppUserID   *string `json:"appUserId,omitempty"`
}
```

- [ ] **Step 2: Write enrichment test**

```go
func TestListUserCommandUsageFiltered_EnrichesUserTargets(t *testing.T) {
	ctx, store, appUserID := newTestStoreWithUser(t)
	// create a second app_user that we'll use as the target
	otherDiscordID := "555555555555555555"
	otherAppUserID := insertAppUser(t, ctx, store, otherDiscordID, "alice", "globalAlice", "icon123")

	if err := store.InsertCommandUsage(ctx, CommandUsageInsert{
		AppUserID:       appUserID,
		DiscordID:       "222222222222222222",
		InteractionType: "command",
		Command:         "announce",
		Outcome:         "success",
		Options:         []byte(`{}`),
		Targets: []CommandUsageTargetInsert{
			{Kind: "user", Snowflake: otherDiscordID, Relation: "recipient"},
		},
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	page, err := store.ListUserCommandUsageFiltered(ctx, appUserID, CommandUsageListFilters{}, 25, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 || len(page.Items[0].Targets) != 1 {
		t.Fatalf("want one row with one target, got %+v", page)
	}
	tgt := page.Items[0].Targets[0]
	if tgt.AppUserID == nil || *tgt.AppUserID != otherAppUserID {
		t.Fatalf("want appUserId=%s, got %+v", otherAppUserID, tgt.AppUserID)
	}
	if tgt.DisplayName == nil || *tgt.DisplayName != "globalAlice" {
		t.Fatalf("want enriched displayName=globalAlice, got %+v", tgt.DisplayName)
	}
}
```

The helper `insertAppUser` is illustrative — if no such helper exists, write the SQL inline:

```go
func insertAppUser(t *testing.T, ctx context.Context, s *PostgresStore, discordID, username, globalName, avatar string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO app_users (discord_id, discord_username, discord_global_name, discord_avatar_hash)
		VALUES ($1,$2,$3,$4) RETURNING id
	`, discordID, username, globalName, avatar).Scan(&id); err != nil {
		t.Fatalf("insert app_user: %v", err)
	}
	return id
}
```

- [ ] **Step 3: Run + commit**

```bash
git add apps/backend/internal/store/command_usage.go apps/backend/internal/store/command_usage_test.go
git commit -m "feat(backend): enrich command_usage targets with live app_users/bot_guilds joins"
```

---

### Task C4: HTTP handler accepts filters + sub-endpoints

**Files:**
- Modify: `apps/backend/internal/http/admin_command_usage_handlers.go`
- Modify: `apps/backend/internal/http/router.go:315`
- Test: `apps/backend/internal/http/admin_command_usage_handlers_test.go` (create if missing)

- [ ] **Step 1: Rewrite the handler to use the filtered store method**

Replace the body of `AdminGetUserCommandUsage` in `admin_command_usage_handlers.go` with:

```go
func (h *Handler) AdminGetUserCommandUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}

	q := r.URL.Query()

	page := 1
	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	pageSize := 25
	if v := q.Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}
	offset := (page - 1) * pageSize

	filters := store.CommandUsageListFilters{
		Q:       q.Get("q"),
		Command: q.Get("command"),
		GuildID: q.Get("guildId"),
	}
	if v := q.Get("outcome"); v != "" {
		filters.Outcomes = splitAndValidate(v, map[string]struct{}{
			"success": {}, "error": {}, "banned": {}, "denied": {},
		})
	}
	if v := q.Get("interactionType"); v != "" {
		filters.InteractionTypes = splitAndValidate(v, map[string]struct{}{
			"command": {}, "button": {}, "modal": {}, "string_select": {},
		})
	}
	if v := q.Get("windowDays"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			switch n {
			case 1, 7, 30, 90:
				filters.WindowDays = n
			}
		}
	}

	listed, err := h.postgres.ListUserCommandUsageFiltered(ctx, id, filters, pageSize, offset)
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

	items := listed.Items
	if items == nil {
		items = []store.CommandUsageRowWithTargets{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
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

func splitAndValidate(csv string, allowed map[string]struct{}) []string {
	out := make([]string, 0, 4)
	for _, p := range strings.Split(csv, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := allowed[p]; ok {
			out = append(out, p)
		}
	}
	return out
}
```

Add `"strings"` to the import block if not present.

- [ ] **Step 2: Add the two sub-endpoint handlers**

Append to the same file:

```go
// AdminGetUserCommandUsageFilterOptions: GET /api/app/admin/users/{id}/command-usage/filter-options
func (h *Handler) AdminGetUserCommandUsageFilterOptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	windowDays := 90
	if v := r.URL.Query().Get("windowDays"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			switch n {
			case 1, 7, 30, 90:
				windowDays = n
			}
		}
	}
	opts, err := h.postgres.CommandUsageFilterOptions(ctx, id, windowDays)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load filter options")
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

// AdminGetCommandUsageTargets: GET /api/app/admin/users/{id}/command-usage/{usageId}/targets
func (h *Handler) AdminGetCommandUsageTargets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "usageId")
	usageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || usageID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid usageId")
		return
	}
	targets, err := h.postgres.ListCommandUsageTargets(ctx, usageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load targets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": targets})
}
```

- [ ] **Step 3: Register the new routes**

In `router.go` immediately after line 315 (the existing `command-usage` route), add:

```go
			r.Get("/users/{id}/command-usage/filter-options", handler.AdminGetUserCommandUsageFilterOptions)
			r.Get("/users/{id}/command-usage/{usageId}/targets", handler.AdminGetCommandUsageTargets)
```

- [ ] **Step 4: Add at least one HTTP test for the filter path**

If `admin_command_usage_handlers_test.go` doesn't exist, create it. Otherwise append:

```go
func TestAdminGetUserCommandUsage_OutcomeFilter(t *testing.T) {
	fx := setupTestHandler(t)
	defer fx.cleanup()
	appUserID := fx.seedAppUser("222222222222222222")
	fx.seedCommandUsage(appUserID, "ping", "success")
	fx.seedCommandUsage(appUserID, "ping", "error")

	req := httptest.NewRequest(http.MethodGet, "/api/app/admin/users/"+appUserID+"/command-usage?outcome=error", nil)
	req = withURLParam(req, "id", appUserID)
	rr := httptest.NewRecorder()
	fx.h.AdminGetUserCommandUsage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Total != 1 {
		t.Fatalf("want total=1, got %d", body.Total)
	}
}
```

The helpers (`seedAppUser`, `seedCommandUsage`, `withURLParam`) are illustrative; use the equivalents in the existing test fixtures. If they don't exist, model after `TestRecordCommandUsage_HappyPath` for fixture wiring.

- [ ] **Step 5: Run + commit**

```bash
cd apps/backend && go test ./internal/http/ -count=1
```

```bash
git add apps/backend/internal/http/admin_command_usage_handlers.go apps/backend/internal/http/admin_command_usage_handlers_test.go apps/backend/internal/http/router.go
git commit -m "feat(backend): filters and sub-endpoints for admin command-usage"
```

---

## Phase D — Frontend Rebuild

### Task D1: Extend the TypeScript API surface

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts:528-573`

- [ ] **Step 1: Extend types + add new fetchers**

Replace the existing `AdminCommandUsageItem` / `AdminCommandUsageSummary` / `AdminCommandUsagePage` / `getAdminUserCommandUsage` block (lines 528-573) with:

```ts
export type AdminCommandUsageTarget = {
	kind:        "user" | "role" | "channel" | "guild" | "steam";
	snowflake:   string;
	displayName: string | null;
	avatarHash:  string | null;
	relation:    "recipient" | "scope" | "subject";
	appUserId?:  string | null;
};

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
	targets:         AdminCommandUsageTarget[];
	targetTotal?:    number;
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

export type AdminCommandUsageFilters = {
	page?:             number;
	pageSize?:         number;
	q?:                string;
	outcome?:          string[];
	interactionType?:  string[];
	command?:          string;
	guildId?:          string;
	windowDays?:       1 | 7 | 30 | 90;
};

export async function getAdminUserCommandUsage(
	userId: string,
	opts: AdminCommandUsageFilters = {},
): Promise<AdminCommandUsagePage> {
	const sp = new URLSearchParams();
	if (opts.page)            sp.set("page",            String(opts.page));
	if (opts.pageSize)        sp.set("pageSize",        String(opts.pageSize));
	if (opts.q)               sp.set("q",               opts.q);
	if (opts.outcome?.length) sp.set("outcome",         opts.outcome.join(","));
	if (opts.interactionType?.length) sp.set("interactionType", opts.interactionType.join(","));
	if (opts.command)         sp.set("command",         opts.command);
	if (opts.guildId)         sp.set("guildId",         opts.guildId);
	if (opts.windowDays)      sp.set("windowDays",      String(opts.windowDays));
	const qs = sp.toString();
	return backendFetch<AdminCommandUsagePage>(
		`/api/app/admin/users/${encodeURIComponent(userId)}/command-usage${qs ? `?${qs}` : ""}`,
		undefined,
		{ requireSession: true },
	);
}

export type AdminCommandUsageFilterOptions = {
	commands: string[];
	guilds:   Array<{ guildId: string; name: string; iconHash: string | null }>;
};

export async function getAdminUserCommandUsageFilterOptions(
	userId: string,
	windowDays: 1 | 7 | 30 | 90 = 90,
): Promise<AdminCommandUsageFilterOptions> {
	return backendFetch<AdminCommandUsageFilterOptions>(
		`/api/app/admin/users/${encodeURIComponent(userId)}/command-usage/filter-options?windowDays=${windowDays}`,
		undefined,
		{ requireSession: true },
	);
}

export async function getAdminUserCommandUsageTargets(
	userId: string,
	usageId: number,
): Promise<{ targets: AdminCommandUsageTarget[] }> {
	return backendFetch<{ targets: AdminCommandUsageTarget[] }>(
		`/api/app/admin/users/${encodeURIComponent(userId)}/command-usage/${usageId}/targets`,
		undefined,
		{ requireSession: true },
	);
}
```

- [ ] **Step 2: Commit (no test — this is a thin API surface)**

```bash
git add apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): typed API surface for command-usage filters + targets"
```

---

### Task D2: Thread filters through the page route

**Files:**
- Modify: `apps/frontend/app/admin/users/[userId]/page.tsx`

- [ ] **Step 1: Parse the filter querystring and pass to the API**

Replace the `usagePage` block (line 28) and the `getAdminUserCommandUsage` invocation (line 47) with:

```tsx
const usagePage = Number((sp.usagePage as string | undefined) ?? "1") || 1;
const usagePageSize = 25;

const usageFilters = {
    page: usagePage,
    pageSize: usagePageSize,
    q: (sp.usageQ as string | undefined) || undefined,
    outcome: typeof sp.usageOutcome === "string"
        ? sp.usageOutcome.split(",").filter(Boolean)
        : undefined,
    interactionType: typeof sp.usageType === "string"
        ? sp.usageType.split(",").filter(Boolean)
        : undefined,
    command: (sp.usageCommand as string | undefined) || undefined,
    guildId: (sp.usageGuildId as string | undefined) || undefined,
    windowDays: ((): 1 | 7 | 30 | 90 | undefined => {
        const v = Number(sp.usageWindowDays as string | undefined);
        return v === 1 || v === 7 || v === 30 || v === 90 ? v : undefined;
    })(),
} as const;
```

And replace the `getAdminUserCommandUsage(...)` call inside `Promise.all` with:

```tsx
		getAdminUserCommandUsage(userId, usageFilters),
```

The `UsageSection` will also need the user's filter options. Load them in parallel:

```tsx
import {
    getAdminRoles,
    getAdminUser,
    getAdminUserAudit,
    getAdminUserCommandUsage,
    getAdminUserCommandUsageFilterOptions,
    getCurrentUserSafe,
} from "@/lib/rustlens-api";
```

Add to the `Promise.all`:

```tsx
const [csrfToken, currentUser, roles, audit, commandUsage, commandUsageFilterOptions] = await Promise.all([
    getCsrfToken(),
    getCurrentUserSafe(),
    getAdminRoles(),
    getAdminUserAudit(userId, { limit: 30 }),
    getAdminUserCommandUsage(userId, usageFilters),
    getAdminUserCommandUsageFilterOptions(userId, usageFilters.windowDays ?? 90),
]);
```

And pass to `UserDetailPage`:

```tsx
<UserDetailPage
    detail={userDetail}
    roles={roles}
    audit={audit.items}
    commandUsage={commandUsage}
    commandUsageFilterOptions={commandUsageFilterOptions}
    permissions={session.permissions ?? []}
/>
```

- [ ] **Step 2: Update `user-detail-page.tsx` to accept + forward the prop**

In `apps/frontend/app/admin/users/_components/user-detail-page.tsx`:

```tsx
import type {
    AdminCommandUsagePage,
    AdminCommandUsageFilterOptions,
    AdminRoleItem,
    AdminUserAuditItem,
    AdminUserDetail,
} from "@/lib/rustlens-api";

type Props = {
    detail: AdminUserDetail;
    roles: AdminRoleItem[];
    audit: AdminUserAuditItem[];
    commandUsage: AdminCommandUsagePage;
    commandUsageFilterOptions: AdminCommandUsageFilterOptions;
    permissions: string[];
};

export function UserDetailPage({
    detail,
    roles,
    audit,
    commandUsage,
    commandUsageFilterOptions,
    permissions,
}: Props) {
    // ...existing body...
    // Replace the UsageSection line with:
    // <UsageSection data={commandUsage} userId={detail.user.id} filterOptions={commandUsageFilterOptions} />
}
```

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/app/admin/users/[userId]/page.tsx apps/frontend/app/admin/users/_components/user-detail-page.tsx
git commit -m "feat(frontend): thread usage filters through the user detail route"
```

---

### Task D3: Extract a shared `SortHeader`

**Files:**
- Create: `apps/frontend/components/admin/sort-header.tsx`
- Modify: `apps/frontend/app/admin/users/_components/servers-section.tsx`

- [ ] **Step 1: Move the SortHeader component out of servers-section**

Create `sort-header.tsx`:

```tsx
"use client";

import { ArrowDownIcon, ArrowUpDownIcon, ArrowUpIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { TableHead } from "@/components/ui/table";

export type SortDir = "asc" | "desc";

type SortHeaderProps<K extends string> = {
	label:       string;
	sortKey:     K;
	currentKey:  K;
	currentDir:  SortDir;
	onSort:      (key: K) => void;
	className?:  string;
	align?:      "left" | "right";
};

export function SortHeader<K extends string>({
	label,
	sortKey,
	currentKey,
	currentDir,
	onSort,
	className,
	align = "left",
}: SortHeaderProps<K>) {
	const active = currentKey === sortKey;
	const Icon = active
		? currentDir === "asc"
			? ArrowUpIcon
			: ArrowDownIcon
		: ArrowUpDownIcon;
	return (
		<TableHead className={className}>
			<Button
				variant="ghost"
				size="sm"
				onClick={() => onSort(sortKey)}
				className={`-mx-2 h-auto px-2 py-1 font-medium ${
					align === "right" ? "ml-auto" : ""
				}`}
			>
				{label}
				<Icon
					className={`ml-1 size-3.5 ${active ? "" : "text-muted-foreground/60"}`}
				/>
			</Button>
		</TableHead>
	);
}
```

- [ ] **Step 2: Update `servers-section.tsx` to import the shared component**

In `servers-section.tsx`:
1. Remove the local `SortHeader` definition (lines 92-134).
2. Add `import { SortHeader, type SortDir } from "@/components/admin/sort-header";` at the top.
3. Remove the local `type SortDir = "asc" | "desc";` declaration.

- [ ] **Step 3: Run frontend type-check + commit**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`
Expected: PASS.

```bash
git add apps/frontend/components/admin/sort-header.tsx apps/frontend/app/admin/users/_components/servers-section.tsx
git commit -m "refactor(frontend): extract SortHeader for shared admin tables"
```

---

### Task D4: Build the recipient stack (avatars + hover-card)

**Files:**
- Create: `apps/frontend/app/admin/users/_components/usage-recipient-stack.tsx`

- [ ] **Step 1: Create the component**

```tsx
"use client";

import * as React from "react";
import Link from "next/link";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { userAvatarUrl } from "@/lib/discord-cdn";
import type { AdminCommandUsageTarget } from "@/lib/rustlens-api";

type Props = {
	recipients: AdminCommandUsageTarget[];
	max?:       number;
};

function recipientLabel(t: AdminCommandUsageTarget): string {
	if (t.displayName && t.displayName.trim()) return t.displayName;
	return t.snowflake;
}

function recipientInitial(t: AdminCommandUsageTarget): string {
	const l = recipientLabel(t);
	return l.charAt(0).toUpperCase() || "?";
}

function recipientAvatarSrc(t: AdminCommandUsageTarget): string | null {
	if (t.kind !== "user") return null;
	return userAvatarUrl(t.snowflake, t.avatarHash, 64);
}

function RecipientLine({ t }: { t: AdminCommandUsageTarget }) {
	const label = recipientLabel(t);
	const src = recipientAvatarSrc(t);
	const body = (
		<div className="flex items-center gap-2">
			<Avatar className="size-6">
				{src && <AvatarImage src={src} alt={label} />}
				<AvatarFallback className="text-[10px]">{recipientInitial(t)}</AvatarFallback>
			</Avatar>
			<span className="truncate text-sm">{label}</span>
		</div>
	);
	if (t.kind === "user" && t.appUserId) {
		return (
			<Link href={`/admin/users/${t.appUserId}`} className="hover:underline">
				{body}
			</Link>
		);
	}
	return body;
}

export function UsageRecipientStack({ recipients, max = 5 }: Props) {
	if (recipients.length === 0) return null;
	const visible = recipients.slice(0, max);
	const overflow = Math.max(0, recipients.length - visible.length);
	return (
		<HoverCard openDelay={120}>
			<HoverCardTrigger asChild>
				<div className="flex items-center">
					{visible.map((t) => {
						const src = recipientAvatarSrc(t);
						const label = recipientLabel(t);
						return (
							<Avatar key={t.snowflake} className="size-7 -ml-2 ring-2 ring-background first:ml-0">
								{src && <AvatarImage src={src} alt={label} />}
								<AvatarFallback className="text-[10px]">
									{recipientInitial(t)}
								</AvatarFallback>
							</Avatar>
						);
					})}
					{overflow > 0 && (
						<div className="size-7 -ml-2 flex items-center justify-center rounded-full bg-muted text-[10px] font-medium ring-2 ring-background">
							+{overflow}
						</div>
					)}
				</div>
			</HoverCardTrigger>
			<HoverCardContent className="w-72 max-h-80 overflow-auto p-2">
				<div className="flex flex-col gap-1">
					{recipients.map((t) => (
						<RecipientLine key={t.snowflake} t={t} />
					))}
				</div>
			</HoverCardContent>
		</HoverCard>
	);
}
```

- [ ] **Step 2: Type-check + commit**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`

```bash
git add apps/frontend/app/admin/users/_components/usage-recipient-stack.tsx
git commit -m "feat(frontend): stacked avatar HoverCard for usage recipients"
```

---

### Task D5: Build the filter bar

**Files:**
- Create: `apps/frontend/app/admin/users/_components/usage-filter-bar.tsx`

- [ ] **Step 1: Create the component**

```tsx
"use client";

import * as React from "react";
import { useRouter, useSearchParams, usePathname } from "next/navigation";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
	Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { AdminCommandUsageFilterOptions } from "@/lib/rustlens-api";

type Props = {
	filterOptions: AdminCommandUsageFilterOptions;
};

const OUTCOMES = ["success", "error", "banned", "denied"] as const;
const INTERACTION_TYPES = ["command", "button", "modal", "string_select"] as const;
const WINDOWS = ["1", "7", "30", "90"] as const;

function setOrDelete(sp: URLSearchParams, key: string, value: string | undefined) {
	if (value && value.length > 0) sp.set(key, value);
	else sp.delete(key);
	// Reset pagination on filter change.
	sp.delete("usagePage");
}

export function UsageFilterBar({ filterOptions }: Props) {
	const router = useRouter();
	const pathname = usePathname();
	const params = useSearchParams();
	const [draftQ, setDraftQ] = React.useState(params.get("usageQ") ?? "");

	const apply = React.useCallback(
		(updater: (sp: URLSearchParams) => void) => {
			const sp = new URLSearchParams(params.toString());
			updater(sp);
			router.replace(`${pathname}?${sp.toString()}#usage`, { scroll: false });
		},
		[params, pathname, router],
	);

	React.useEffect(() => {
		const t = setTimeout(() => {
			if ((params.get("usageQ") ?? "") === draftQ) return;
			apply((sp) => setOrDelete(sp, "usageQ", draftQ || undefined));
		}, 250);
		return () => clearTimeout(t);
	}, [draftQ, apply, params]);

	const outcomeRaw = params.get("usageOutcome");
	const outcomes = outcomeRaw ? outcomeRaw.split(",") : [];
	const interactionRaw = params.get("usageType");
	const interactionTypes = interactionRaw ? interactionRaw.split(",") : [];
	const command = params.get("usageCommand") ?? "";
	const guildId = params.get("usageGuildId") ?? "";
	const windowDays = params.get("usageWindowDays") ?? "90";

	const activeChips: { key: string; label: string; clear: () => void }[] = [];
	if (outcomes.length > 0) {
		activeChips.push({
			key: "usageOutcome",
			label: `outcome: ${outcomes.join("|")}`,
			clear: () => apply((sp) => setOrDelete(sp, "usageOutcome", undefined)),
		});
	}
	if (interactionTypes.length > 0) {
		activeChips.push({
			key: "usageType",
			label: `type: ${interactionTypes.join("|")}`,
			clear: () => apply((sp) => setOrDelete(sp, "usageType", undefined)),
		});
	}
	if (command) activeChips.push({
		key: "usageCommand",
		label: `command: ${command}`,
		clear: () => apply((sp) => setOrDelete(sp, "usageCommand", undefined)),
	});
	if (guildId) {
		const g = filterOptions.guilds.find((x) => x.guildId === guildId);
		activeChips.push({
			key: "usageGuildId",
			label: `guild: ${g?.name || guildId}`,
			clear: () => apply((sp) => setOrDelete(sp, "usageGuildId", undefined)),
		});
	}
	if (windowDays !== "90") activeChips.push({
		key: "usageWindowDays",
		label: `last ${windowDays}d`,
		clear: () => apply((sp) => setOrDelete(sp, "usageWindowDays", undefined)),
	});

	return (
		<div className="flex flex-col gap-2">
			<div className="flex flex-wrap items-center gap-2">
				<Input
					value={draftQ}
					onChange={(e) => setDraftQ(e.target.value)}
					placeholder="Search command, target, recipient…"
					className="max-w-md"
					aria-label="Search usage"
				/>

				<ToggleGroup
					type="multiple"
					value={outcomes}
					onValueChange={(v) => apply((sp) => setOrDelete(sp, "usageOutcome", v.join(",") || undefined))}
					aria-label="Outcome filter"
				>
					{OUTCOMES.map((o) => (
						<ToggleGroupItem key={o} value={o} className="capitalize">
							{o}
						</ToggleGroupItem>
					))}
				</ToggleGroup>

				<Select
					value={interactionTypes[0] ?? "all"}
					onValueChange={(v) => apply((sp) => setOrDelete(sp, "usageType", v === "all" ? undefined : v))}
				>
					<SelectTrigger className="w-[160px]" aria-label="Interaction type">
						<SelectValue placeholder="Type" />
					</SelectTrigger>
					<SelectContent>
						<SelectItem value="all">All types</SelectItem>
						{INTERACTION_TYPES.map((t) => (
							<SelectItem key={t} value={t}>{t}</SelectItem>
						))}
					</SelectContent>
				</Select>

				<Select
					value={command || "all"}
					onValueChange={(v) => apply((sp) => setOrDelete(sp, "usageCommand", v === "all" ? undefined : v))}
				>
					<SelectTrigger className="w-[200px]" aria-label="Command">
						<SelectValue placeholder="Command" />
					</SelectTrigger>
					<SelectContent>
						<SelectItem value="all">All commands</SelectItem>
						{filterOptions.commands.map((c) => (
							<SelectItem key={c} value={c}>/{c}</SelectItem>
						))}
					</SelectContent>
				</Select>

				<Select
					value={guildId || "all"}
					onValueChange={(v) => apply((sp) => setOrDelete(sp, "usageGuildId", v === "all" ? undefined : v))}
				>
					<SelectTrigger className="w-[220px]" aria-label="Guild">
						<SelectValue placeholder="Guild" />
					</SelectTrigger>
					<SelectContent>
						<SelectItem value="all">All guilds</SelectItem>
						{filterOptions.guilds.map((g) => (
							<SelectItem key={g.guildId} value={g.guildId}>
								{g.name || g.guildId}
							</SelectItem>
						))}
					</SelectContent>
				</Select>

				<Select
					value={windowDays}
					onValueChange={(v) => apply((sp) => setOrDelete(sp, "usageWindowDays", v === "90" ? undefined : v))}
				>
					<SelectTrigger className="w-[120px]" aria-label="Window">
						<SelectValue />
					</SelectTrigger>
					<SelectContent>
						{WINDOWS.map((w) => (
							<SelectItem key={w} value={w}>Last {w}d</SelectItem>
						))}
					</SelectContent>
				</Select>
			</div>

			{activeChips.length > 0 && (
				<div className="flex flex-wrap items-center gap-2 text-xs">
					{activeChips.map((c) => (
						<button
							key={c.key}
							onClick={c.clear}
							className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 hover:bg-muted/80"
							type="button"
						>
							{c.label} <span aria-hidden>×</span>
						</button>
					))}
					<Button
						variant="link"
						size="sm"
						className="h-auto px-1 text-xs"
						onClick={() => apply((sp) => {
							for (const k of ["usageQ","usageOutcome","usageType","usageCommand","usageGuildId","usageWindowDays"]) {
								sp.delete(k);
							}
							sp.delete("usagePage");
						})}
					>
						Clear all
					</Button>
				</div>
			)}
		</div>
	);
}
```

- [ ] **Step 2: Type-check + commit**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`

```bash
git add apps/frontend/app/admin/users/_components/usage-filter-bar.tsx
git commit -m "feat(frontend): URL-driven usage filter bar"
```

---

### Task D6: Rebuild `UsageSection` as a table

**Files:**
- Modify: `apps/frontend/app/admin/users/_components/usage-section.tsx` (full rewrite)

- [ ] **Step 1: Rewrite to the table-based layout**

Replace the entire file with:

```tsx
"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { RelativeTime } from "@/components/admin/relative-time";
import { SortHeader, type SortDir } from "@/components/admin/sort-header";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import {
	Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import { guildIconUrl, userAvatarUrl } from "@/lib/discord-cdn";
import type {
	AdminCommandUsageFilterOptions,
	AdminCommandUsageItem,
	AdminCommandUsagePage,
	AdminCommandUsageTarget,
} from "@/lib/rustlens-api";
import { getAdminUserCommandUsageTargets } from "@/lib/rustlens-api";
import { UsageFilterBar } from "./usage-filter-bar";
import { UsageRecipientStack } from "./usage-recipient-stack";

type Props = {
	userId:        string;
	data:          AdminCommandUsagePage;
	filterOptions: AdminCommandUsageFilterOptions;
};

type SortKey = "when" | "command" | "outcome" | "latency";

function outcomeBadgeVariant(o: AdminCommandUsageItem["outcome"]): "secondary" | "destructive" | "outline" {
	switch (o) {
		case "success": return "secondary";
		case "error":   return "destructive";
		case "banned":  return "destructive";
		case "denied":  return "outline";
	}
}

function commandLabel(item: AdminCommandUsageItem): string {
	if (item.subcommand) return `/${item.command} ${item.subcommand}`;
	return `/${item.command}`;
}

function compareItems(a: AdminCommandUsageItem, b: AdminCommandUsageItem, key: SortKey): number {
	switch (key) {
		case "when":     return a.createdAt.localeCompare(b.createdAt);
		case "command":  return commandLabel(a).localeCompare(commandLabel(b));
		case "outcome":  return a.outcome.localeCompare(b.outcome);
		case "latency":  return a.latencyMs - b.latencyMs;
	}
}

function relationBucket(targets: AdminCommandUsageTarget[]) {
	return {
		subjects:   targets.filter((t) => t.relation === "subject"),
		scopes:     targets.filter((t) => t.relation === "scope"),
		recipients: targets.filter((t) => t.relation === "recipient"),
	};
}

export function UsageSection({ userId, data, filterOptions }: Props) {
	const [expandedId, setExpandedId] = React.useState<number | null>(null);
	const [sortKey, setSortKey] = React.useState<SortKey>("when");
	const [sortDir, setSortDir] = React.useState<SortDir>("desc");
	const [fullTargetsCache, setFullTargetsCache] = React.useState<Record<number, AdminCommandUsageTarget[]>>({});

	const { items, total, page, pageSize, summary } = data;
	const topCommands = summary.topCommands ?? [];

	const toggleSort = (key: SortKey) => {
		if (key === sortKey) setSortDir((d) => (d === "asc" ? "desc" : "asc"));
		else { setSortKey(key); setSortDir(key === "latency" ? "desc" : "asc"); }
	};

	const sorted = React.useMemo(() => {
		const copy = [...items];
		copy.sort((a, b) => {
			const cmp = compareItems(a, b, sortKey);
			return sortDir === "asc" ? cmp : -cmp;
		});
		return copy;
	}, [items, sortKey, sortDir]);

	const fetchFullTargets = async (item: AdminCommandUsageItem) => {
		if (!item.targetTotal || fullTargetsCache[item.id]) return;
		try {
			const res = await getAdminUserCommandUsageTargets(userId, item.id);
			setFullTargetsCache((m) => ({ ...m, [item.id]: res.targets }));
		} catch (err) {
			console.warn("[usage] failed to load full targets", err);
		}
	};

	return (
		<section id="usage" className="flex flex-col gap-4 rounded-lg border bg-card p-4">
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
						{topCommands.length === 0 ? (
							<span className="text-sm text-muted-foreground">—</span>
						) : (
							topCommands.map((tc) => (
								<Badge key={tc.command} variant="secondary">
									{tc.command} · {tc.count}
								</Badge>
							))
						)}
					</div>
				</div>
			</div>

			<UsageFilterBar filterOptions={filterOptions} />

			<div className="text-sm text-muted-foreground">
				{total === 0 ? "No bot usage matches the filters." : `Showing ${items.length} of ${total}`}
			</div>

			{total === 0 ? null : (
				<div className="overflow-hidden rounded-lg border">
					<Table>
						<TableHeader>
							<TableRow>
								<SortHeader<SortKey> label="When"     sortKey="when"    currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} />
								<TableHead>Type</TableHead>
								<SortHeader<SortKey> label="Command"  sortKey="command" currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} />
								<TableHead>Target / Recipients</TableHead>
								<SortHeader<SortKey> label="Outcome"  sortKey="outcome" currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} />
								<SortHeader<SortKey> label="Latency"  sortKey="latency" currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} className="text-right" align="right" />
							</TableRow>
						</TableHeader>
						<TableBody>
							{sorted.map((item) => {
								const expanded = expandedId === item.id;
								const buckets = relationBucket(item.targets);
								return (
									<React.Fragment key={item.id}>
										<TableRow
											className="cursor-pointer"
											onClick={() => {
												setExpandedId(expanded ? null : item.id);
												if (!expanded) void fetchFullTargets(item);
											}}
										>
											<TableCell className="text-xs text-muted-foreground">
												<RelativeTime iso={item.createdAt} />
											</TableCell>
											<TableCell>
												<Badge variant="outline">{item.interactionType}</Badge>
											</TableCell>
											<TableCell>
												<code className="rounded bg-muted px-1 py-0.5 text-xs">{commandLabel(item)}</code>
											</TableCell>
											<TableCell>
												{buckets.recipients.length > 0 ? (
													<UsageRecipientStack recipients={buckets.recipients} />
												) : buckets.subjects[0] ? (
													<TargetChip t={buckets.subjects[0]} />
												) : buckets.scopes[0] ? (
													<TargetChip t={buckets.scopes[0]} />
												) : item.target ? (
													<code className="text-xs text-muted-foreground">{item.target}</code>
												) : (
													<span className="text-xs text-muted-foreground">—</span>
												)}
											</TableCell>
											<TableCell>
												<Badge variant={outcomeBadgeVariant(item.outcome)}>{item.outcome}</Badge>
											</TableCell>
											<TableCell className="text-right tabular-nums text-xs">{item.latencyMs}ms</TableCell>
										</TableRow>
										{expanded && (
											<TableRow>
												<TableCell colSpan={6} className="bg-muted/30">
													<ExpandedRow
														item={item}
														fullTargets={fullTargetsCache[item.id]}
														guildId={item.guildId}
													/>
												</TableCell>
											</TableRow>
										)}
									</React.Fragment>
								);
							})}
						</TableBody>
					</Table>
				</div>
			)}

			{total > 0 && (
				<UsagePagination userId={userId} page={page} pageSize={pageSize} total={total} />
			)}
		</section>
	);
}

function TargetChip({ t }: { t: AdminCommandUsageTarget }) {
	const label = t.displayName?.trim() || t.snowflake;
	if (t.kind === "user") {
		const src = userAvatarUrl(t.snowflake, t.avatarHash, 64);
		return (
			<div className="flex items-center gap-2">
				<Avatar className="size-5">
					{src && <AvatarImage src={src} alt={label} />}
					<AvatarFallback className="text-[10px]">{label.charAt(0).toUpperCase()}</AvatarFallback>
				</Avatar>
				<span className="text-sm">{label}</span>
			</div>
		);
	}
	if (t.kind === "guild") {
		const src = guildIconUrl(t.snowflake, t.avatarHash, 64);
		return (
			<div className="flex items-center gap-2">
				<Avatar className="size-5">
					{src && <AvatarImage src={src} alt={label} />}
					<AvatarFallback className="text-[10px]">{label.charAt(0).toUpperCase()}</AvatarFallback>
				</Avatar>
				<span className="text-sm">{label}</span>
			</div>
		);
	}
	return <Badge variant="outline">{t.kind}: {label}</Badge>;
}

function ExpandedRow({
	item,
	fullTargets,
	guildId,
}: {
	item: AdminCommandUsageItem;
	fullTargets: AdminCommandUsageTarget[] | undefined;
	guildId: string | null;
}) {
	const targets = fullTargets ?? item.targets;
	const buckets = relationBucket(targets);
	return (
		<div className="grid grid-cols-1 gap-3 p-3 text-xs">
			{guildId && (
				<div className="text-muted-foreground">guild: <code>{guildId}</code></div>
			)}
			<div>
				<div className="font-semibold text-muted-foreground">What they put in</div>
				<pre className="mt-1 whitespace-pre-wrap break-words">
					{JSON.stringify(item.options ?? {}, null, 2)}
				</pre>
			</div>
			{(buckets.subjects.length + buckets.scopes.length + buckets.recipients.length > 0) && (
				<div className="flex flex-col gap-2">
					{buckets.subjects.length > 0 && (
						<TargetGroup title="Subject" targets={buckets.subjects} />
					)}
					{buckets.scopes.length > 0 && (
						<TargetGroup title="Scope" targets={buckets.scopes} />
					)}
					{buckets.recipients.length > 0 && (
						<TargetGroup
							title={item.targetTotal && item.targetTotal > item.targets.length
								? `Recipients (${item.targetTotal})`
								: `Recipients (${buckets.recipients.length})`}
							targets={buckets.recipients}
						/>
					)}
				</div>
			)}
			<div className="text-muted-foreground">
				{item.outcome}{item.errorCode ? ` — ${item.errorCode}` : ""} · {item.latencyMs}ms
			</div>
		</div>
	);
}

function TargetGroup({ title, targets }: { title: string; targets: AdminCommandUsageTarget[] }) {
	return (
		<div>
			<div className="font-semibold text-muted-foreground">{title}</div>
			<div className="mt-1 flex flex-wrap gap-2">
				{targets.map((t) => (
					<TargetChip key={`${t.kind}:${t.snowflake}`} t={t} />
				))}
			</div>
		</div>
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
		if (typeof window === "undefined") {
			return `?usagePage=${p}#usage`;
		}
		const sp = new URLSearchParams(window.location.search);
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

- [ ] **Step 2: Type-check**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`
Expected: PASS.

- [ ] **Step 3: Lint**

Run: `cd apps/frontend && pnpm lint`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/app/admin/users/_components/usage-section.tsx
git commit -m "feat(frontend): rebuild usage tab as sortable table with filters + target capture"
```

---

### Task D7: Smoke-test the page in the browser

**Files:** none (manual check)

- [ ] **Step 1: Start the dev stack**

Run: `make dev`
Wait for backend, frontend, and infra to come up.

- [ ] **Step 2: Open the user detail page**

Navigate to `http://localhost/admin/users/<some-known-user-id>` and click the **Usage** tab.

Verify (with empty state OK if the user has zero usage):
- Summary cards still render unchanged.
- Filter bar renders all six controls without overlap; wraps on a narrow window.
- Searching `announce` in the search input narrows the table after ~250ms.
- Selecting **Error** in the outcome toggle filters the table.
- Clicking the **Clear all** chip resets the URL and re-renders.

- [ ] **Step 3: If data exists, expand a row**

- Verify the expanded row shows the guild ID, options JSON, and (when targets present) the Subject/Scope/Recipients groups.
- Verify the recipient stack hover-card opens and shows the full list.

- [ ] **Step 4: No commit** — UI smoke check only.

---

## Phase E — Docs

### Task E1: Update AGENTS.md

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Update the `command_usage` paragraph**

Find the paragraph that starts with `` `command_usage` stores every Discord bot interaction `` (around line 266).

Replace its final sentence (`Surfaced on /admin/users/{id} Usage tab via GET /api/app/admin/users/{id}/command-usage.`) with:

```
Surfaced on `/admin/users/{id}` Usage tab via `GET /api/app/admin/users/{id}/command-usage` (filterable by `q`, `outcome`, `interactionType`, `command`, `guildId`, `windowDays`). Fan-out commands (`/announce`, `/summon`, `/nickname`, `/admin setsteam|removesteam`, `/manage ban`, `/setsteam`, `/transfer`, `/create invite`, `/track add|remove`) additionally write rows into `command_usage_targets` (FK CASCADE off `command_usage`, snapshotted `display_name` + `avatar_hash` at capture time, `relation` of `recipient|scope|subject`, capped at 500 per record). The admin list response embeds up to 100 targets per row inline and exposes the rest via `GET /api/app/admin/users/{id}/command-usage/{usageId}/targets`. Filter autocomplete is served by `GET /api/app/admin/users/{id}/command-usage/filter-options`.
```

- [ ] **Step 2: Update the admin route bullet**

Find the bullet that starts with `` `The admin `GET /api/app/admin/users/{id}/command-usage?page=&pageSize=` route` `` (around line 253) and replace it with:

```
- The admin `GET /api/app/admin/users/{id}/command-usage` route returns a paginated list of bot interactions with embedded `targets[]` (up to 100 per row; full list via `…/{usageId}/targets`) plus a 90-day summary. Query params: `q` (free-text search across command/subcommand/target and command_usage_targets.display_name|snowflake), `outcome` (CSV: success|error|banned|denied), `interactionType` (CSV: command|button|modal|string_select), `command` (exact match), `guildId` (exact match), `windowDays` (1|7|30|90; default 90), `page`, `pageSize` (default 25, cap 100). Companion routes: `GET /api/app/admin/users/{id}/command-usage/filter-options` returns distinct commands + guilds seen in the user's window for the UI's autocomplete; `GET /api/app/admin/users/{id}/command-usage/{usageId}/targets` returns the full target list for a single record. User and guild targets are enriched at read time with live data from `app_users` and `bot_guilds` (snapshot is the fallback). Backs the Usage tab on `/admin/users/{id}`.
```

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document command_usage filters, targets, and new sub-endpoints"
```

---

## Final verification

- [ ] **Step 1: Run the full backend test suite**

Run: `cd apps/backend && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 2: Run the full bot test suite**

Run: `pnpm --filter @rustlens/bot test --run`
Expected: PASS.

- [ ] **Step 3: Run the frontend type-check + lint + tests**

Run: `cd apps/frontend && pnpm exec tsc --noEmit && pnpm lint && pnpm test --run`
Expected: PASS.

- [ ] **Step 4: Confirm no `?` or `TODO` placeholders remain**

Run: `grep -rn "TODO\|TBD\|FIXME" apps/backend/internal/store/command_usage.go apps/backend/internal/http/admin_command_usage_handlers.go apps/frontend/app/admin/users/_components/usage-section.tsx apps/frontend/app/admin/users/_components/usage-filter-bar.tsx apps/frontend/app/admin/users/_components/usage-recipient-stack.tsx`
Expected: no matches in any of those files.
