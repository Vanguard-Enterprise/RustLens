# Dashboard Installed Guilds Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show every Discord guild the signed-in user shares with the RustLens bot on `/dashboard`, with metadata pulled from a backend-cached `bot_guilds` table that the bot keeps fresh via `guildCreate` / `guildDelete` / `guildUpdate` / `clientReady` reconcile.

**Architecture:** The bot is the only writer to the new `bot_guilds` table and the existing `user_guilds` table. On `guildCreate` it bulk-upserts current members in 1000-row chunks; `clientReady` reconciles per-shard ownership using `(guild_id >> 22) % shard_count`. A new public endpoint `GET /api/app/me/guilds/installed` joins both tables for the dashboard. Frontend builds Discord CDN URLs from the cached hashes via a shared util.

**Tech Stack:** Go (chi, pgx, standard `testing`); TypeScript / Node (discord.js v14, vitest); Next.js 16 / React 19; PostgreSQL.

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `apps/backend/internal/store/postgres.go` | modify | Add `bot_guilds` table to schema-init block. |
| `apps/backend/internal/store/bot_guilds.go` | create | Store layer: upsert metadata, bulk-upsert members in batches, mark left, reconcile, dashboard query. |
| `apps/backend/internal/store/bot_guilds_test.go` | create | Store-layer tests (skip when `TEST_POSTGRES_URL` unset). |
| `apps/backend/internal/http/bot_guilds_handlers.go` | create | Three internal bot-side handlers: `UpsertBotGuild`, `MarkBotGuildLeft`, `ReconcileBotGuilds`. |
| `apps/backend/internal/http/bot_guilds_handlers_test.go` | create | Handler tests (request validation, payload shape, status codes). |
| `apps/backend/internal/http/app_handlers.go` | modify | Add `GetCurrentAppUserInstalledGuilds` handler. |
| `apps/backend/internal/http/router.go` | modify | Register the 4 new routes. |
| `apps/bot/src/backend-client.ts` | modify | Add `upsertBotGuild`, `markBotGuildLeft`, `reconcileBotGuilds` client methods. |
| `apps/bot/src/backend-client.test.ts` | modify | Add tests for the 3 new methods. |
| `apps/bot/src/bot-guilds.ts` | create | `registerBotGuildSync(client, shardId, shardCount)` — lifecycle handlers + chunked member sync. |
| `apps/bot/src/bot-guilds.test.ts` | create | Vitest tests for payload-building + chunking. |
| `apps/bot/src/shard-worker.ts` | modify | Call `registerBotGuildSync` next to `registerUserGuildSync`. |
| `apps/frontend/lib/discord-cdn.ts` | create | `guildIconUrl`, `guildBannerUrl`, `userAvatarUrl` helpers. |
| `apps/frontend/lib/discord-cdn.test.ts` | create | Vitest tests for the helpers. |
| `apps/frontend/lib/rustlens-api.ts` | modify | Reshape `DashboardGuild` types; activate `getMyInstalledGuilds`. |
| `apps/frontend/app/dashboard/_components/guild-list.tsx` | modify | Use new field names + CDN URL helpers. |

---

## Task 1: Add `bot_guilds` schema

**Files:**
- Modify: `apps/backend/internal/store/postgres.go` (inside the schema-init `EnsureSchema` block, near the existing `user_guilds` definition)
- Test: `apps/backend/internal/store/bot_guilds_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/store/bot_guilds_test.go`:

```go
package store

import (
	"context"
	"testing"
)

func TestEnsureSchema_CreatesBotGuildsTable(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	expectedColumns := map[string]string{
		"guild_id":                 "text",
		"name":                     "text",
		"icon_hash":                "text",
		"banner_hash":              "text",
		"owner_discord_id":         "text",
		"owner_username":           "text",
		"owner_global_name":        "text",
		"owner_avatar_hash":        "text",
		"approximate_member_count": "integer",
		"is_installed":             "boolean",
		"installed_at":             "timestamp with time zone",
		"left_at":                  "timestamp with time zone",
		"metadata_updated_at":      "timestamp with time zone",
		"members_synced_at":        "timestamp with time zone",
	}

	for column, dataType := range expectedColumns {
		var actual string
		err := s.pool.QueryRow(ctx, `
			SELECT data_type
			FROM information_schema.columns
			WHERE table_name = 'bot_guilds' AND column_name = $1
		`, column).Scan(&actual)
		if err != nil {
			t.Fatalf("query column %s: %v", column, err)
		}
		if actual != dataType {
			t.Errorf("bot_guilds.%s data_type = %q, want %q", column, actual, dataType)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/store -run TestEnsureSchema_CreatesBotGuildsTable -v`
Expected: FAIL — `bot_guilds` table does not exist yet (test will skip if `TEST_POSTGRES_URL` not set; that's also acceptable, but locally the dev DB should be available).

- [ ] **Step 3: Add schema**

Open `apps/backend/internal/store/postgres.go`. Find the existing `CREATE TABLE IF NOT EXISTS user_guilds` block (around line 335). After the `CREATE INDEX IF NOT EXISTS user_guilds_guild_idx` block (around line 369–370), add:

```go
		CREATE TABLE IF NOT EXISTS bot_guilds (
			guild_id                 TEXT PRIMARY KEY,
			name                     TEXT NOT NULL DEFAULT '',
			icon_hash                TEXT,
			banner_hash              TEXT,
			owner_discord_id         TEXT,
			owner_username           TEXT,
			owner_global_name        TEXT,
			owner_avatar_hash        TEXT,
			approximate_member_count INTEGER,
			is_installed             BOOLEAN NOT NULL DEFAULT TRUE,
			installed_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			left_at                  TIMESTAMPTZ,
			metadata_updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			members_synced_at        TIMESTAMPTZ
		);

		CREATE INDEX IF NOT EXISTS bot_guilds_installed_idx
		ON bot_guilds (is_installed) WHERE is_installed = TRUE;
```

(Place it as a new `CREATE TABLE` block inside the same SQL string the other `CREATE TABLE IF NOT EXISTS` calls live in.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd apps/backend && go test ./internal/store -run TestEnsureSchema_CreatesBotGuildsTable -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/store/postgres.go apps/backend/internal/store/bot_guilds_test.go
git commit -m "feat(backend): add bot_guilds schema for installed-guilds dashboard"
```

---

## Task 2: Store layer — upsert metadata + mark left

**Files:**
- Create: `apps/backend/internal/store/bot_guilds.go`
- Modify: `apps/backend/internal/store/bot_guilds_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/store/bot_guilds_test.go`:

```go
import "time" // add to existing imports if not present

func TestUpsertBotGuildMetadata_InsertsAndUpdates(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	input := UpsertBotGuildInput{
		GuildID:                "100000000000000001",
		Name:                   "Test Guild",
		IconHash:               strPtr("abc"),
		BannerHash:             nil,
		OwnerDiscordID:         strPtr("200000000000000002"),
		OwnerUsername:          strPtr("ownername"),
		OwnerGlobalName:        strPtr("Owner Display"),
		OwnerAvatarHash:        strPtr("def"),
		ApproximateMemberCount: int32Ptr(42),
	}

	if err := s.UpsertBotGuildMetadata(ctx, input); err != nil {
		t.Fatalf("first UpsertBotGuildMetadata: %v", err)
	}

	// Update path
	input.Name = "Renamed"
	input.IconHash = strPtr("zzz")
	if err := s.UpsertBotGuildMetadata(ctx, input); err != nil {
		t.Fatalf("second UpsertBotGuildMetadata: %v", err)
	}

	var name, iconHash string
	var isInstalled bool
	var leftAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT name, icon_hash, is_installed, left_at FROM bot_guilds WHERE guild_id = $1
	`, input.GuildID).Scan(&name, &iconHash, &isInstalled, &leftAt)
	if err != nil {
		t.Fatalf("query bot_guilds: %v", err)
	}
	if name != "Renamed" || iconHash != "zzz" || !isInstalled || leftAt != nil {
		t.Fatalf("unexpected row: name=%q iconHash=%q isInstalled=%v leftAt=%v", name, iconHash, isInstalled, leftAt)
	}
}

func TestMarkBotGuildLeft_SetsFlagAndTimestamp(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	guildID := "100000000000000003"
	if err := s.UpsertBotGuildMetadata(ctx, UpsertBotGuildInput{GuildID: guildID, Name: "g"}); err != nil {
		t.Fatalf("seed UpsertBotGuildMetadata: %v", err)
	}
	if err := s.MarkBotGuildLeft(ctx, guildID); err != nil {
		t.Fatalf("MarkBotGuildLeft: %v", err)
	}

	var isInstalled bool
	var leftAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT is_installed, left_at FROM bot_guilds WHERE guild_id = $1
	`, guildID).Scan(&isInstalled, &leftAt)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if isInstalled || leftAt == nil {
		t.Fatalf("want is_installed=false leftAt non-nil, got %v / %v", isInstalled, leftAt)
	}

	// Re-upserting must reset the flag (bot rejoined).
	if err := s.UpsertBotGuildMetadata(ctx, UpsertBotGuildInput{GuildID: guildID, Name: "g"}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	err = s.pool.QueryRow(ctx, `
		SELECT is_installed, left_at FROM bot_guilds WHERE guild_id = $1
	`, guildID).Scan(&isInstalled, &leftAt)
	if err != nil {
		t.Fatalf("query 2: %v", err)
	}
	if !isInstalled || leftAt != nil {
		t.Fatalf("want is_installed=true leftAt nil, got %v / %v", isInstalled, leftAt)
	}
}

func strPtr(v string) *string { return &v }
func int32Ptr(v int32) *int32 { return &v }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/store -run "TestUpsertBotGuildMetadata|TestMarkBotGuildLeft" -v`
Expected: FAIL — undefined: `UpsertBotGuildInput`, `UpsertBotGuildMetadata`, `MarkBotGuildLeft`.

- [ ] **Step 3: Implement the store methods**

Create `apps/backend/internal/store/bot_guilds.go`:

```go
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type UpsertBotGuildInput struct {
	GuildID                string
	Name                   string
	IconHash               *string
	BannerHash             *string
	OwnerDiscordID         *string
	OwnerUsername          *string
	OwnerGlobalName        *string
	OwnerAvatarHash        *string
	ApproximateMemberCount *int32
}

// UpsertBotGuildMetadata inserts or updates a row in bot_guilds and clears any
// prior "left" state. Member sync timestamp is left untouched.
func (p *PostgresStore) UpsertBotGuildMetadata(ctx context.Context, in UpsertBotGuildInput) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO bot_guilds (
			guild_id, name, icon_hash, banner_hash,
			owner_discord_id, owner_username, owner_global_name, owner_avatar_hash,
			approximate_member_count, is_installed, installed_at, left_at, metadata_updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, TRUE, NOW(), NULL, NOW())
		ON CONFLICT (guild_id) DO UPDATE SET
			name                     = EXCLUDED.name,
			icon_hash                = EXCLUDED.icon_hash,
			banner_hash              = EXCLUDED.banner_hash,
			owner_discord_id         = EXCLUDED.owner_discord_id,
			owner_username           = EXCLUDED.owner_username,
			owner_global_name        = EXCLUDED.owner_global_name,
			owner_avatar_hash        = EXCLUDED.owner_avatar_hash,
			approximate_member_count = EXCLUDED.approximate_member_count,
			is_installed             = TRUE,
			left_at                  = NULL,
			metadata_updated_at      = NOW()
	`,
		in.GuildID, in.Name, in.IconHash, in.BannerHash,
		in.OwnerDiscordID, in.OwnerUsername, in.OwnerGlobalName, in.OwnerAvatarHash,
		in.ApproximateMemberCount,
	)
	return err
}

// MarkBotGuildLeft flags the row as no longer installed. No-op if absent.
func (p *PostgresStore) MarkBotGuildLeft(ctx context.Context, guildID string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE bot_guilds
		SET is_installed = FALSE,
		    left_at = NOW()
		WHERE guild_id = $1
	`, guildID)
	return err
}

// MembersSyncedAt returns the bot_guilds.members_synced_at timestamp for a
// guild, or (nil, false) when the row does not exist.
func (p *PostgresStore) MembersSyncedAt(ctx context.Context, guildID string) (*time.Time, bool, error) {
	var ts *time.Time
	err := p.pool.QueryRow(ctx, `
		SELECT members_synced_at FROM bot_guilds WHERE guild_id = $1
	`, guildID).Scan(&ts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return ts, true, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd apps/backend && go test ./internal/store -run "TestUpsertBotGuildMetadata|TestMarkBotGuildLeft" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/store/bot_guilds.go apps/backend/internal/store/bot_guilds_test.go
git commit -m "feat(backend): add bot_guilds upsert/mark-left store methods"
```

---

## Task 3: Store layer — bulk member upsert + members_synced_at stamp

**Files:**
- Modify: `apps/backend/internal/store/bot_guilds.go`
- Modify: `apps/backend/internal/store/bot_guilds_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/store/bot_guilds_test.go`:

```go
func TestBulkUpsertUserGuilds_ActivatesMembers(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	guildID := "100000000000000010"
	if err := s.UpsertBotGuildMetadata(ctx, UpsertBotGuildInput{GuildID: guildID, Name: "g"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	discordIDs := []string{"300000000000000001", "300000000000000002", "300000000000000003"}
	if err := s.BulkUpsertUserGuilds(ctx, guildID, discordIDs); err != nil {
		t.Fatalf("BulkUpsertUserGuilds: %v", err)
	}

	for _, id := range discordIDs {
		var isActive bool
		err := s.pool.QueryRow(ctx, `SELECT is_active FROM user_guilds WHERE discord_id = $1 AND guild_id = $2`, id, guildID).Scan(&isActive)
		if err != nil {
			t.Fatalf("query %s: %v", id, err)
		}
		if !isActive {
			t.Errorf("expected is_active=true for %s", id)
		}
	}
}

func TestStampMembersSynced_SetsTimestamp(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	guildID := "100000000000000011"
	if err := s.UpsertBotGuildMetadata(ctx, UpsertBotGuildInput{GuildID: guildID, Name: "g"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	before, _, err := s.MembersSyncedAt(ctx, guildID)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	if before != nil {
		t.Fatalf("expected nil before stamp, got %v", before)
	}

	if err := s.StampMembersSynced(ctx, guildID); err != nil {
		t.Fatalf("StampMembersSynced: %v", err)
	}

	after, ok, err := s.MembersSyncedAt(ctx, guildID)
	if err != nil || !ok || after == nil {
		t.Fatalf("read after: ok=%v err=%v ts=%v", ok, err, after)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/store -run "TestBulkUpsertUserGuilds|TestStampMembersSynced" -v`
Expected: FAIL — undefined: `BulkUpsertUserGuilds`, `StampMembersSynced`.

- [ ] **Step 3: Implement**

Append to `apps/backend/internal/store/bot_guilds.go`:

```go
// BulkUpsertUserGuilds inserts or reactivates user_guilds rows for the given
// discord IDs in a single statement. Caller is responsible for chunking input
// to a reasonable size (the HTTP layer caps incoming batches at 1000).
func (p *PostgresStore) BulkUpsertUserGuilds(ctx context.Context, guildID string, discordIDs []string) error {
	if len(discordIDs) == 0 {
		return nil
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO user_guilds (discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at)
		SELECT did, $1, TRUE, NOW(), NOW(), NULL FROM unnest($2::text[]) AS did
		ON CONFLICT (discord_id, guild_id) DO UPDATE
		SET is_active = TRUE,
		    last_seen_at = NOW(),
		    left_at = NULL
	`, guildID, discordIDs)
	return err
}

// StampMembersSynced marks bot_guilds.members_synced_at = NOW(). Used after a
// guildCreate / clientReady member sync completes.
func (p *PostgresStore) StampMembersSynced(ctx context.Context, guildID string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE bot_guilds SET members_synced_at = NOW() WHERE guild_id = $1
	`, guildID)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd apps/backend && go test ./internal/store -run "TestBulkUpsertUserGuilds|TestStampMembersSynced" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/store/bot_guilds.go apps/backend/internal/store/bot_guilds_test.go
git commit -m "feat(backend): add bulk user_guilds upsert + members_synced_at stamp"
```

---

## Task 4: Store layer — sharded reconcile

**Files:**
- Modify: `apps/backend/internal/store/bot_guilds.go`
- Modify: `apps/backend/internal/store/bot_guilds_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/store/bot_guilds_test.go`:

```go
func TestReconcileShardBotGuilds_OnlyTouchesOwnShard(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	// Pick guild IDs whose shard buckets we know.
	// Discord shard formula: (guild_id >> 22) % shard_count.
	// With shard_count=2, guild_id where bit 22 is 0 -> shard 0; bit 22 is 1 -> shard 1.
	shard0Existing := "1"                 // (1 >> 22) % 2 = 0
	shard0Stale    := "2"                 // (2 >> 22) % 2 = 0
	shard1Other    := "4194304"           // (4194304 >> 22) % 2 = 1

	for _, id := range []string{shard0Existing, shard0Stale, shard1Other} {
		if err := s.UpsertBotGuildMetadata(ctx, UpsertBotGuildInput{GuildID: id, Name: "g"}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	// Reconcile shard 0: only shard0Existing is still present.
	if err := s.ReconcileShardBotGuilds(ctx, 0, 2, []string{shard0Existing}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	checkInstalled := func(id string, want bool) {
		t.Helper()
		var installed bool
		if err := s.pool.QueryRow(ctx, `SELECT is_installed FROM bot_guilds WHERE guild_id = $1`, id).Scan(&installed); err != nil {
			t.Fatalf("query %s: %v", id, err)
		}
		if installed != want {
			t.Errorf("guild %s: is_installed = %v, want %v", id, installed, want)
		}
	}
	checkInstalled(shard0Existing, true)
	checkInstalled(shard0Stale, false)
	checkInstalled(shard1Other, true) // shard 1's row must be untouched
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/store -run TestReconcileShardBotGuilds -v`
Expected: FAIL — undefined: `ReconcileShardBotGuilds`.

- [ ] **Step 3: Implement**

Append to `apps/backend/internal/store/bot_guilds.go`:

```go
// ReconcileShardBotGuilds marks any currently-installed bot_guilds row in the
// given shard's range as left, unless its guild_id appears in presentGuildIDs.
// Shard membership uses Discord's standard formula: (guild_id >> 22) % shard_count.
func (p *PostgresStore) ReconcileShardBotGuilds(ctx context.Context, shardID int, shardCount int, presentGuildIDs []string) error {
	if shardCount <= 0 {
		return errors.New("shardCount must be > 0")
	}
	if shardID < 0 || shardID >= shardCount {
		return errors.New("shardID out of range")
	}
	_, err := p.pool.Exec(ctx, `
		UPDATE bot_guilds
		SET is_installed = FALSE, left_at = NOW()
		WHERE is_installed = TRUE
		  AND ((guild_id::bigint >> 22) % $1::bigint) = $2::bigint
		  AND guild_id <> ALL($3::text[])
	`, shardCount, shardID, presentGuildIDs)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd apps/backend && go test ./internal/store -run TestReconcileShardBotGuilds -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/store/bot_guilds.go apps/backend/internal/store/bot_guilds_test.go
git commit -m "feat(backend): add sharded bot_guilds reconcile"
```

---

## Task 5: Store layer — dashboard query

**Files:**
- Modify: `apps/backend/internal/store/bot_guilds.go`
- Modify: `apps/backend/internal/store/bot_guilds_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/store/bot_guilds_test.go`:

```go
func TestListInstalledGuildsForUser_FiltersAndJoins(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	discordID := "300000000000000099"
	installed := "100000000000000100"
	uninstalled := "100000000000000101"
	notMember := "100000000000000102"

	for _, id := range []string{installed, uninstalled, notMember} {
		if err := s.UpsertBotGuildMetadata(ctx, UpsertBotGuildInput{
			GuildID:                id,
			Name:                   "Guild " + id,
			ApproximateMemberCount: int32Ptr(7),
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	if err := s.MarkBotGuildLeft(ctx, uninstalled); err != nil {
		t.Fatalf("seed mark left: %v", err)
	}

	// User is in `installed` and `uninstalled` (active rows in user_guilds);
	// not in `notMember`.
	if err := s.BulkUpsertUserGuilds(ctx, installed, []string{discordID}); err != nil {
		t.Fatalf("seed user_guilds installed: %v", err)
	}
	if err := s.BulkUpsertUserGuilds(ctx, uninstalled, []string{discordID}); err != nil {
		t.Fatalf("seed user_guilds uninstalled: %v", err)
	}

	rows, err := s.ListInstalledGuildsForUser(ctx, discordID)
	if err != nil {
		t.Fatalf("ListInstalledGuildsForUser: %v", err)
	}
	if len(rows) != 1 || rows[0].GuildID != installed {
		t.Fatalf("got %#v, want exactly the installed guild", rows)
	}
	if rows[0].Name != "Guild "+installed {
		t.Errorf("Name = %q", rows[0].Name)
	}
	if rows[0].ApproximateMemberCount == nil || *rows[0].ApproximateMemberCount != 7 {
		t.Errorf("ApproximateMemberCount = %v, want 7", rows[0].ApproximateMemberCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/store -run TestListInstalledGuildsForUser -v`
Expected: FAIL — undefined: `ListInstalledGuildsForUser`.

- [ ] **Step 3: Implement**

Append to `apps/backend/internal/store/bot_guilds.go`:

```go
type DashboardGuildRow struct {
	GuildID                string
	Name                   string
	IconHash               *string
	BannerHash             *string
	OwnerDiscordID         *string
	OwnerUsername          *string
	OwnerGlobalName        *string
	OwnerAvatarHash        *string
	ApproximateMemberCount *int32
	TrackedServerCount     int32
}

func (p *PostgresStore) ListInstalledGuildsForUser(ctx context.Context, discordID string) ([]DashboardGuildRow, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT bg.guild_id,
		       bg.name,
		       bg.icon_hash,
		       bg.banner_hash,
		       bg.owner_discord_id,
		       bg.owner_username,
		       bg.owner_global_name,
		       bg.owner_avatar_hash,
		       bg.approximate_member_count,
		       (SELECT COUNT(*) FROM guild_tracked_servers gts WHERE gts.guild_id = bg.guild_id)::int AS tracked_server_count
		FROM user_guilds ug
		JOIN bot_guilds bg ON bg.guild_id = ug.guild_id
		WHERE ug.discord_id = $1
		  AND ug.is_active = TRUE
		  AND bg.is_installed = TRUE
		ORDER BY bg.name ASC, bg.guild_id ASC
	`, discordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]DashboardGuildRow, 0)
	for rows.Next() {
		var r DashboardGuildRow
		if err := rows.Scan(
			&r.GuildID, &r.Name, &r.IconHash, &r.BannerHash,
			&r.OwnerDiscordID, &r.OwnerUsername, &r.OwnerGlobalName, &r.OwnerAvatarHash,
			&r.ApproximateMemberCount, &r.TrackedServerCount,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd apps/backend && go test ./internal/store -run TestListInstalledGuildsForUser -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/store/bot_guilds.go apps/backend/internal/store/bot_guilds_test.go
git commit -m "feat(backend): add ListInstalledGuildsForUser dashboard query"
```

---

## Task 6: Internal HTTP handlers — upsert / mark-left / reconcile

**Files:**
- Create: `apps/backend/internal/http/bot_guilds_handlers.go`
- Create: `apps/backend/internal/http/bot_guilds_handlers_test.go`

- [ ] **Step 1: Write the failing test (validation only — Postgres-free)**

Create `apps/backend/internal/http/bot_guilds_handlers_test.go`:

```go
package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpsertBotGuild_RejectsInvalidGuildID(t *testing.T) {
	h := &Handler{}
	body, _ := json.Marshal(map[string]any{"guildId": "not-a-snowflake", "name": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/internal/bot/setup/bot-guilds/upsert", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.UpsertBotGuild(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestMarkBotGuildLeft_RejectsInvalidGuildID(t *testing.T) {
	h := &Handler{}
	body, _ := json.Marshal(map[string]any{"guildId": "abc"})
	req := httptest.NewRequest(http.MethodPost, "/api/internal/bot/setup/bot-guilds/leave", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.MarkBotGuildLeft(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestReconcileBotGuilds_RejectsBadShardCount(t *testing.T) {
	h := &Handler{}
	body, _ := json.Marshal(map[string]any{"shardId": 0, "shardCount": 0, "guildIds": []string{}})
	req := httptest.NewRequest(http.MethodPost, "/api/internal/bot/setup/bot-guilds/reconcile", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ReconcileBotGuilds(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/http -run "TestUpsertBotGuild|TestMarkBotGuildLeft|TestReconcileBotGuilds" -v`
Expected: FAIL — undefined: `UpsertBotGuild`, `MarkBotGuildLeft`, `ReconcileBotGuilds` methods on `Handler`.

- [ ] **Step 3: Implement the handlers**

Create `apps/backend/internal/http/bot_guilds_handlers.go`:

```go
package http

import (
	"encoding/json"
	"log"
	"net/http"

	"rustlens/backend/internal/store"
)

const maxMembersPerUpsertBatch = 1000

type upsertBotGuildRequest struct {
	GuildID                string   `json:"guildId"`
	Name                   string   `json:"name"`
	IconHash               *string  `json:"iconHash,omitempty"`
	BannerHash             *string  `json:"bannerHash,omitempty"`
	OwnerDiscordID         *string  `json:"ownerDiscordId,omitempty"`
	OwnerUsername          *string  `json:"ownerUsername,omitempty"`
	OwnerGlobalName        *string  `json:"ownerGlobalName,omitempty"`
	OwnerAvatarHash        *string  `json:"ownerAvatarHash,omitempty"`
	ApproximateMemberCount *int32   `json:"approximateMemberCount,omitempty"`
	Members                []string `json:"members,omitempty"`
	MembersSyncComplete    bool     `json:"membersSyncComplete,omitempty"`
}

type markBotGuildLeftRequest struct {
	GuildID string `json:"guildId"`
}

type reconcileBotGuildsRequest struct {
	ShardID    int      `json:"shardId"`
	ShardCount int      `json:"shardCount"`
	GuildIDs   []string `json:"guildIds"`
}

func (h *Handler) UpsertBotGuild(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req upsertBotGuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !validateSnowflakeValue(req.GuildID) {
		writeError(w, http.StatusBadRequest, "guildId must be a valid Discord snowflake")
		return
	}
	if len(req.Members) > maxMembersPerUpsertBatch {
		writeError(w, http.StatusBadRequest, "members batch exceeds maximum size")
		return
	}
	for _, m := range req.Members {
		if !validateSnowflakeValue(m) {
			writeError(w, http.StatusBadRequest, "members must contain valid Discord snowflakes")
			return
		}
	}

	if err := h.postgres.UpsertBotGuildMetadata(r.Context(), store.UpsertBotGuildInput{
		GuildID:                req.GuildID,
		Name:                   req.Name,
		IconHash:               req.IconHash,
		BannerHash:             req.BannerHash,
		OwnerDiscordID:         req.OwnerDiscordID,
		OwnerUsername:          req.OwnerUsername,
		OwnerGlobalName:        req.OwnerGlobalName,
		OwnerAvatarHash:        req.OwnerAvatarHash,
		ApproximateMemberCount: req.ApproximateMemberCount,
	}); err != nil {
		log.Printf("[backend] UpsertBotGuildMetadata error for %s: %v", req.GuildID, err)
		writeError(w, http.StatusInternalServerError, "failed to upsert bot guild")
		return
	}

	if len(req.Members) > 0 {
		if err := h.postgres.BulkUpsertUserGuilds(r.Context(), req.GuildID, req.Members); err != nil {
			log.Printf("[backend] BulkUpsertUserGuilds error for %s: %v", req.GuildID, err)
			writeError(w, http.StatusInternalServerError, "failed to upsert members")
			return
		}
	}

	if req.MembersSyncComplete {
		if err := h.postgres.StampMembersSynced(r.Context(), req.GuildID); err != nil {
			log.Printf("[backend] StampMembersSynced error for %s: %v", req.GuildID, err)
			writeError(w, http.StatusInternalServerError, "failed to stamp members_synced_at")
			return
		}
	}

	syncedAt, _, err := h.postgres.MembersSyncedAt(r.Context(), req.GuildID)
	if err != nil {
		log.Printf("[backend] MembersSyncedAt error for %s: %v", req.GuildID, err)
		writeError(w, http.StatusInternalServerError, "failed to read members_synced_at")
		return
	}

	resp := map[string]any{"status": "ok", "membersSyncedAt": nil}
	if syncedAt != nil {
		resp["membersSyncedAt"] = syncedAt.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) MarkBotGuildLeft(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req markBotGuildLeftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateSnowflakeValue(req.GuildID) {
		writeError(w, http.StatusBadRequest, "guildId must be a valid Discord snowflake")
		return
	}
	if err := h.postgres.MarkBotGuildLeft(r.Context(), req.GuildID); err != nil {
		log.Printf("[backend] MarkBotGuildLeft error for %s: %v", req.GuildID, err)
		writeError(w, http.StatusInternalServerError, "failed to mark guild left")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ReconcileBotGuilds(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req reconcileBotGuildsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ShardCount <= 0 || req.ShardID < 0 || req.ShardID >= req.ShardCount {
		writeError(w, http.StatusBadRequest, "invalid shardId/shardCount")
		return
	}
	for _, id := range req.GuildIDs {
		if !validateSnowflakeValue(id) {
			writeError(w, http.StatusBadRequest, "guildIds must contain valid Discord snowflakes")
			return
		}
	}
	if err := h.postgres.ReconcileShardBotGuilds(r.Context(), req.ShardID, req.ShardCount, req.GuildIDs); err != nil {
		log.Printf("[backend] ReconcileShardBotGuilds error shard=%d/%d: %v", req.ShardID, req.ShardCount, err)
		writeError(w, http.StatusInternalServerError, "failed to reconcile bot guilds")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd apps/backend && go test ./internal/http -run "TestUpsertBotGuild|TestMarkBotGuildLeft|TestReconcileBotGuilds" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/http/bot_guilds_handlers.go apps/backend/internal/http/bot_guilds_handlers_test.go
git commit -m "feat(backend): add internal bot-guilds upsert/leave/reconcile handlers"
```

---

## Task 7: Public dashboard handler

**Files:**
- Modify: `apps/backend/internal/http/app_handlers.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/http/bot_guilds_handlers_test.go`:

```go
func TestGetCurrentAppUserInstalledGuilds_RequiresSession(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/app/me/guilds/installed", nil)
	w := httptest.NewRecorder()
	h.GetCurrentAppUserInstalledGuilds(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/http -run TestGetCurrentAppUserInstalledGuilds -v`
Expected: FAIL — undefined `GetCurrentAppUserInstalledGuilds`.

- [ ] **Step 3: Implement the handler**

In `apps/backend/internal/http/app_handlers.go`, immediately after `GetCurrentAppUserGuilds` (the function that returns at the end with `writeJSON(w, http.StatusOK, map[string]any{"items": items})` — should currently end around line 476), append:

```go
func (h *Handler) GetCurrentAppUserInstalledGuilds(w http.ResponseWriter, r *http.Request) {
	actor, ok, status, message := requireAppActor(r, h.postgres)
	if !ok {
		writeError(w, status, message)
		return
	}

	currentUser, err := h.postgres.GetCurrentUserByID(r.Context(), actor.ID)
	if err != nil {
		log.Printf("[backend] GetCurrentAppUserInstalledGuilds error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load installed guilds")
		return
	}

	if currentUser.DiscordAccount == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []map[string]any{}})
		return
	}

	rows, err := h.postgres.ListInstalledGuildsForUser(r.Context(), currentUser.DiscordAccount.ProviderAccountID)
	if err != nil {
		log.Printf("[backend] ListInstalledGuildsForUser error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load installed guilds")
		return
	}

	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		var owner map[string]any
		if row.OwnerDiscordID != nil {
			owner = map[string]any{
				"id":          *row.OwnerDiscordID,
				"username":    derefString(row.OwnerUsername),
				"globalName":  row.OwnerGlobalName,
				"avatarHash":  row.OwnerAvatarHash,
			}
		}
		items = append(items, map[string]any{
			"id":                     row.GuildID,
			"name":                   row.Name,
			"iconHash":               row.IconHash,
			"bannerHash":             row.BannerHash,
			"approximateMemberCount": row.ApproximateMemberCount,
			"trackedServerCount":     row.TrackedServerCount,
			"owner":                  owner,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd apps/backend && go test ./internal/http -run TestGetCurrentAppUserInstalledGuilds -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/http/app_handlers.go apps/backend/internal/http/bot_guilds_handlers_test.go
git commit -m "feat(backend): add /api/app/me/guilds/installed handler"
```

---

## Task 8: Wire all 4 routes

**Files:**
- Modify: `apps/backend/internal/http/router.go`

- [ ] **Step 1: Register internal routes**

In `apps/backend/internal/http/router.go`, find the `r.Route("/api/internal/bot/setup", func(r chi.Router) {` block. Locate the existing pair `r.Post("/user-guilds", handler.CreateUserGuildLink)` and `r.Post("/user-guilds/leave", handler.MarkUserGuildLeft)` (around lines 118–119). Immediately after the second of those, add:

```go
		r.Post("/bot-guilds/upsert", handler.UpsertBotGuild)
		r.Post("/bot-guilds/leave", handler.MarkBotGuildLeft)
		r.Post("/bot-guilds/reconcile", handler.ReconcileBotGuilds)
```

- [ ] **Step 2: Register the public route**

In the same file, find the block:

```go
		r.Group(func(r chi.Router) {
			r.Use(appSessionMiddleware(handler.appSessions))
			r.Post("/auth/email/verify/request", handler.RequestEmailVerification)
			r.Get("/me/connections", handler.GetCurrentAppUserConnections)
			r.Get("/me/guilds", handler.GetCurrentAppUserGuilds)
		})
```

Add a new line directly under `r.Get("/me/guilds", handler.GetCurrentAppUserGuilds)`:

```go
			r.Get("/me/guilds/installed", handler.GetCurrentAppUserInstalledGuilds)
```

- [ ] **Step 3: Build the backend to verify it compiles**

Run: `cd apps/backend && go build ./...`
Expected: clean exit (no errors).

- [ ] **Step 4: Run all backend tests**

Run: `cd apps/backend && go test ./...`
Expected: PASS for all tests (skipped tests OK if `TEST_POSTGRES_URL` unset).

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/http/router.go
git commit -m "feat(backend): wire bot-guilds + me/guilds/installed routes"
```

---

## Task 9: Bot — backend-client methods

**Files:**
- Modify: `apps/bot/src/backend-client.ts`
- Modify: `apps/bot/src/backend-client.test.ts`

- [ ] **Step 1: Write the failing test**

In `apps/bot/src/backend-client.test.ts`, append a new `describe` block (or add to the existing one):

```ts
it("upserts a bot guild with members and sync flag", async () => {
  const mockResponse = {
    ok: true,
    json: async () => ({ status: "ok", membersSyncedAt: "2026-04-27T12:00:00.000Z" }),
  } as Response;
  const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

  const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
  await client.upsertBotGuild("worker-a", {
    guildId: "1",
    name: "G",
    iconHash: null,
    bannerHash: null,
    ownerDiscordId: null,
    ownerUsername: null,
    ownerGlobalName: null,
    ownerAvatarHash: null,
    approximateMemberCount: null,
    members: ["10", "11"],
    membersSyncComplete: true,
  });

  const [url, init] = fetchSpy.mock.calls[0];
  expect(url).toBe("http://localhost:8080/api/internal/bot/setup/bot-guilds/upsert");
  expect(init?.method).toBe("POST");
  const body = JSON.parse(init?.body as string);
  expect(body.guildId).toBe("1");
  expect(body.members).toEqual(["10", "11"]);
  expect(body.membersSyncComplete).toBe(true);
});

it("marks a bot guild left", async () => {
  const mockResponse = { ok: true, json: async () => ({ status: "ok" }) } as Response;
  const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

  const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
  await client.markBotGuildLeft("worker-a", "1");

  const [url, init] = fetchSpy.mock.calls[0];
  expect(url).toBe("http://localhost:8080/api/internal/bot/setup/bot-guilds/leave");
  expect(init?.method).toBe("POST");
  expect(JSON.parse(init?.body as string)).toEqual({ guildId: "1" });
});

it("reconciles bot guilds with shard info", async () => {
  const mockResponse = { ok: true, json: async () => ({ status: "ok" }) } as Response;
  const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

  const client = new BackendClient("http://localhost:8080", "secret", "rustlens-bot", "rustlens-backend");
  await client.reconcileBotGuilds("worker-a", { shardId: 1, shardCount: 4, guildIds: ["1", "2"] });

  const [url, init] = fetchSpy.mock.calls[0];
  expect(url).toBe("http://localhost:8080/api/internal/bot/setup/bot-guilds/reconcile");
  expect(JSON.parse(init?.body as string)).toEqual({ shardId: 1, shardCount: 4, guildIds: ["1", "2"] });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd apps/bot && pnpm vitest run backend-client.test.ts`
Expected: FAIL — `upsertBotGuild`, `markBotGuildLeft`, `reconcileBotGuilds` do not exist on `BackendClient`.

- [ ] **Step 3: Implement the methods**

In `apps/bot/src/backend-client.ts`, add the following type and methods. Find the end of the `BackendClient` class (after `markUserGuildLeft` near line 863) and add inside the class:

```ts
type UpsertBotGuildPayload = {
  guildId: string;
  name: string;
  iconHash: string | null;
  bannerHash: string | null;
  ownerDiscordId: string | null;
  ownerUsername: string | null;
  ownerGlobalName: string | null;
  ownerAvatarHash: string | null;
  approximateMemberCount: number | null;
  members?: string[];
  membersSyncComplete?: boolean;
};

type UpsertBotGuildResponse = {
  status: "ok";
  membersSyncedAt: string | null;
};

// ...inside class BackendClient:

  upsertBotGuild(workerId: string, payload: UpsertBotGuildPayload) {
    return this.call<UpsertBotGuildResponse>(
      workerId,
      `/api/internal/bot/setup/bot-guilds/upsert`,
      "POST",
      payload,
    );
  }

  markBotGuildLeft(workerId: string, guildId: string) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/setup/bot-guilds/leave`,
      "POST",
      { guildId },
    );
  }

  reconcileBotGuilds(
    workerId: string,
    payload: { shardId: number; shardCount: number; guildIds: string[] },
  ) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/setup/bot-guilds/reconcile`,
      "POST",
      payload,
    );
  }
```

The `UpsertBotGuildPayload` and `UpsertBotGuildResponse` types should be placed in the type-declarations section near the top of the file (next to the other `type Foo = { ... }` blocks). Then `export` them at the bottom of the file so the new module can import them:

```ts
export type { UpsertBotGuildPayload, UpsertBotGuildResponse };
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd apps/bot && pnpm vitest run backend-client.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/bot/src/backend-client.ts apps/bot/src/backend-client.test.ts
git commit -m "feat(bot): add bot-guilds backend-client methods"
```

---

## Task 10: Bot — `bot-guilds.ts` lifecycle module

**Files:**
- Create: `apps/bot/src/bot-guilds.ts`
- Create: `apps/bot/src/bot-guilds.test.ts`

- [ ] **Step 1: Write the failing test**

Create `apps/bot/src/bot-guilds.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { __testables } from "./bot-guilds.js";

const { buildUpsertPayload, chunk, MEMBERS_RESYNC_INTERVAL_MS } = __testables;

describe("bot-guilds buildUpsertPayload", () => {
  it("maps a Discord guild to the upsert payload shape", () => {
    const guild = {
      id: "1",
      name: "Test",
      icon: "abc",
      banner: null,
      ownerId: "200",
      memberCount: 10,
    };
    const owner = {
      id: "200",
      username: "owner",
      globalName: "Owner Display",
      avatar: "def",
    };
    const payload = buildUpsertPayload(guild as any, owner as any);
    expect(payload).toEqual({
      guildId: "1",
      name: "Test",
      iconHash: "abc",
      bannerHash: null,
      ownerDiscordId: "200",
      ownerUsername: "owner",
      ownerGlobalName: "Owner Display",
      ownerAvatarHash: "def",
      approximateMemberCount: 10,
    });
  });

  it("nulls out missing owner data", () => {
    const guild = { id: "1", name: "Test", icon: null, banner: null, ownerId: "200", memberCount: null };
    const payload = buildUpsertPayload(guild as any, null);
    expect(payload.ownerDiscordId).toBe("200");
    expect(payload.ownerUsername).toBe(null);
    expect(payload.ownerGlobalName).toBe(null);
    expect(payload.ownerAvatarHash).toBe(null);
    expect(payload.approximateMemberCount).toBe(null);
  });
});

describe("bot-guilds chunk", () => {
  it("splits arrays into fixed-size chunks", () => {
    expect(chunk(["a", "b", "c", "d", "e"], 2)).toEqual([["a", "b"], ["c", "d"], ["e"]]);
  });
  it("returns a single empty array on empty input", () => {
    expect(chunk([], 1000)).toEqual([]);
  });
});

describe("bot-guilds MEMBERS_RESYNC_INTERVAL_MS", () => {
  it("is at least 1 day", () => {
    expect(MEMBERS_RESYNC_INTERVAL_MS).toBeGreaterThanOrEqual(86_400_000);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd apps/bot && pnpm vitest run bot-guilds.test.ts`
Expected: FAIL — `./bot-guilds.js` cannot be resolved.

- [ ] **Step 3: Implement the module**

Create `apps/bot/src/bot-guilds.ts`:

```ts
import type { Client, Guild, User } from "discord.js";
import { BackendClient, type UpsertBotGuildPayload } from "./backend-client.js";
import { loadConfig } from "./config.js";

const MEMBERS_RESYNC_INTERVAL_MS = 7 * 24 * 60 * 60 * 1000;
const MEMBER_BATCH_SIZE = 1000;

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(
      config.backendUrl,
      config.jwtSecret,
      config.jwtIssuer,
      config.jwtAudience,
    );
    _workerID = `${config.workerId}-bot-guilds`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function buildUpsertPayload(guild: Guild, owner: User | null): Omit<UpsertBotGuildPayload, "members" | "membersSyncComplete"> {
  return {
    guildId: guild.id,
    name: guild.name,
    iconHash: guild.icon ?? null,
    bannerHash: guild.banner ?? null,
    ownerDiscordId: guild.ownerId ?? null,
    ownerUsername: owner?.username ?? null,
    ownerGlobalName: owner?.globalName ?? null,
    ownerAvatarHash: owner?.avatar ?? null,
    approximateMemberCount: guild.memberCount ?? null,
  };
}

function chunk<T>(items: T[], size: number): T[][] {
  if (items.length === 0) return [];
  const out: T[][] = [];
  for (let i = 0; i < items.length; i += size) {
    out.push(items.slice(i, i + size));
  }
  return out;
}

async function resolveOwner(guild: Guild): Promise<User | null> {
  try {
    return await guild.fetchOwner().then((m) => m.user);
  } catch {
    return null;
  }
}

async function listHumanMemberIds(guild: Guild): Promise<string[]> {
  const members = await guild.members.fetch();
  return members.filter((m) => !m.user.bot).map((m) => m.id);
}

async function syncMembers(guild: Guild) {
  let memberIds: string[];
  try {
    memberIds = await listHumanMemberIds(guild);
  } catch (error) {
    console.error("[bot] bot-guilds: failed to fetch members", { guildId: guild.id, error });
    return;
  }

  const batches = chunk(memberIds, MEMBER_BATCH_SIZE);
  if (batches.length === 0) {
    // No members? Stamp as synced anyway with an empty payload.
    await getBackend().upsertBotGuild(getWorkerID(), {
      ...buildUpsertPayload(guild, await resolveOwner(guild)),
      members: [],
      membersSyncComplete: true,
    });
    return;
  }

  const owner = await resolveOwner(guild);
  for (let i = 0; i < batches.length; i++) {
    const isLast = i === batches.length - 1;
    try {
      await getBackend().upsertBotGuild(getWorkerID(), {
        ...buildUpsertPayload(guild, owner),
        members: batches[i],
        membersSyncComplete: isLast,
      });
    } catch (error) {
      console.error("[bot] bot-guilds: member batch failed", { guildId: guild.id, batchIndex: i, error });
      return; // bail; clientReady will retry on next startup
    }
  }
}

async function upsertMetadataOnly(guild: Guild) {
  try {
    const owner = await resolveOwner(guild);
    await getBackend().upsertBotGuild(getWorkerID(), buildUpsertPayload(guild, owner));
  } catch (error) {
    console.error("[bot] bot-guilds: metadata upsert failed", { guildId: guild.id, error });
  }
}

async function handleGuildCreate(guild: Guild) {
  await upsertMetadataOnly(guild);
  // Fire-and-forget so we don't block the gateway.
  void syncMembers(guild);
}

async function handleGuildDelete(guild: Guild) {
  try {
    await getBackend().markBotGuildLeft(getWorkerID(), guild.id);
  } catch (error) {
    console.error("[bot] bot-guilds: mark left failed", { guildId: guild.id, error });
  }
}

async function handleGuildUpdate(guild: Guild) {
  await upsertMetadataOnly(guild);
}

async function handleClientReady(client: Client, shardId: number, shardCount: number) {
  const guilds = [...client.guilds.cache.values()];
  const guildIds = guilds.map((g) => g.id);

  try {
    await getBackend().reconcileBotGuilds(getWorkerID(), { shardId, shardCount, guildIds });
  } catch (error) {
    console.error("[bot] bot-guilds: reconcile failed", { shardId, error });
  }

  for (const guild of guilds) {
    let response;
    try {
      const owner = await resolveOwner(guild);
      response = await getBackend().upsertBotGuild(getWorkerID(), buildUpsertPayload(guild, owner));
    } catch (error) {
      console.error("[bot] bot-guilds: ready metadata upsert failed", { guildId: guild.id, error });
      continue;
    }

    const stale =
      !response.membersSyncedAt ||
      Date.now() - new Date(response.membersSyncedAt).getTime() > MEMBERS_RESYNC_INTERVAL_MS;
    if (stale) {
      void syncMembers(guild);
    }
  }
}

export function registerBotGuildSync(client: Client, shardId: number, shardCount: number) {
  client.on("guildCreate", (guild) => {
    handleGuildCreate(guild).catch((error) => {
      console.error("[bot] bot-guilds: guildCreate handler failed", { guildId: guild.id, error });
    });
  });
  client.on("guildDelete", (guild) => {
    handleGuildDelete(guild).catch((error) => {
      console.error("[bot] bot-guilds: guildDelete handler failed", { guildId: guild.id, error });
    });
  });
  client.on("guildUpdate", (_oldGuild, guild) => {
    handleGuildUpdate(guild).catch((error) => {
      console.error("[bot] bot-guilds: guildUpdate handler failed", { guildId: guild.id, error });
    });
  });
  client.once("clientReady", () => {
    handleClientReady(client, shardId, shardCount).catch((error) => {
      console.error("[bot] bot-guilds: clientReady handler failed", { shardId, error });
    });
  });
}

export const __testables = {
  buildUpsertPayload,
  chunk,
  MEMBERS_RESYNC_INTERVAL_MS,
};
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd apps/bot && pnpm vitest run bot-guilds.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/bot/src/bot-guilds.ts apps/bot/src/bot-guilds.test.ts
git commit -m "feat(bot): add bot-guilds lifecycle handlers + chunked member sync"
```

---

## Task 11: Bot — wire `registerBotGuildSync` into shard worker

**Files:**
- Modify: `apps/bot/src/shard-worker.ts`

- [ ] **Step 1: Add the import and the call**

In `apps/bot/src/shard-worker.ts`, find:

```ts
import { registerUserGuildSync } from "./user-guilds.js";
```

Add directly under it:

```ts
import { registerBotGuildSync } from "./bot-guilds.js";
```

Find:

```ts
registerUserGuildSync(client);
```

Add directly under it:

```ts
registerBotGuildSync(client, shardID, shardCount);
```

(`shardID` and `shardCount` are already in scope — they are read from `process.env.SHARD_ID` / `process.env.SHARD_COUNT` near the top of the file. Verify they exist: `grep -n "shardID\|shardCount" apps/bot/src/shard-worker.ts` should show declarations near the top.)

- [ ] **Step 2: Build the bot to verify it compiles**

Run: `cd apps/bot && pnpm tsc --noEmit`
Expected: clean exit (no errors).

- [ ] **Step 3: Run the bot test suite**

Run: `cd apps/bot && pnpm vitest run`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/bot/src/shard-worker.ts
git commit -m "feat(bot): register bot-guilds sync in shard worker"
```

---

## Task 12: Frontend — `discord-cdn.ts` URL helpers

**Files:**
- Create: `apps/frontend/lib/discord-cdn.ts`
- Create: `apps/frontend/lib/discord-cdn.test.ts`

- [ ] **Step 1: Write the failing test**

Create `apps/frontend/lib/discord-cdn.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { guildBannerUrl, guildIconUrl, userAvatarUrl } from "./discord-cdn";

describe("guildIconUrl", () => {
  it("returns null when hash is null", () => {
    expect(guildIconUrl("1", null)).toBe(null);
  });
  it("uses .png for non-animated hashes", () => {
    expect(guildIconUrl("1", "abc")).toBe("https://cdn.discordapp.com/icons/1/abc.png?size=256");
  });
  it("uses .gif for animated hashes (a_ prefix)", () => {
    expect(guildIconUrl("1", "a_abc")).toBe("https://cdn.discordapp.com/icons/1/a_abc.gif?size=256");
  });
  it("honours custom size", () => {
    expect(guildIconUrl("1", "abc", 1024)).toBe("https://cdn.discordapp.com/icons/1/abc.png?size=1024");
  });
});

describe("guildBannerUrl", () => {
  it("uses banners path", () => {
    expect(guildBannerUrl("1", "abc")).toBe("https://cdn.discordapp.com/banners/1/abc.png?size=1024");
  });
});

describe("userAvatarUrl", () => {
  it("uses avatars path", () => {
    expect(userAvatarUrl("1", "abc")).toBe("https://cdn.discordapp.com/avatars/1/abc.png?size=128");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/frontend && pnpm vitest run lib/discord-cdn.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement the helpers**

Create `apps/frontend/lib/discord-cdn.ts`:

```ts
const CDN_BASE = "https://cdn.discordapp.com";

function extFromHash(hash: string): "gif" | "png" {
  return hash.startsWith("a_") ? "gif" : "png";
}

export function guildIconUrl(guildId: string, hash: string | null, size = 256): string | null {
  if (!hash) return null;
  return `${CDN_BASE}/icons/${guildId}/${hash}.${extFromHash(hash)}?size=${size}`;
}

export function guildBannerUrl(guildId: string, hash: string | null, size = 1024): string | null {
  if (!hash) return null;
  return `${CDN_BASE}/banners/${guildId}/${hash}.${extFromHash(hash)}?size=${size}`;
}

export function userAvatarUrl(userId: string, hash: string | null, size = 128): string | null {
  if (!hash) return null;
  return `${CDN_BASE}/avatars/${userId}/${hash}.${extFromHash(hash)}?size=${size}`;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd apps/frontend && pnpm vitest run lib/discord-cdn.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/lib/discord-cdn.ts apps/frontend/lib/discord-cdn.test.ts
git commit -m "feat(frontend): add Discord CDN URL helpers"
```

---

## Task 13: Frontend — reshape `DashboardGuild` types and activate fetch

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 1: Update the type definitions**

In `apps/frontend/lib/rustlens-api.ts`, replace the existing `DashboardGuildOwner` and `DashboardGuild` blocks with:

```ts
export type DashboardGuildOwner = {
	id: string;
	username: string;
	globalName: string | null;
	avatarHash: string | null;
};

export type DashboardGuild = {
	id: string;
	name: string;
	iconHash: string | null;
	bannerHash: string | null;
	approximateMemberCount: number | null;
	trackedServerCount: number;
	owner: DashboardGuildOwner | null;
};
```

- [ ] **Step 2: Activate the live fetch**

In the same file, replace the entire `getMyInstalledGuilds` function (currently a stub returning `[]` with a TODO comment) with:

```ts
/**
 * Returns the guilds the current user is a member of and that have RustLens
 * installed, with metadata (name, owner, icon, banner, member count) suitable
 * for rendering on the user dashboard.
 */
export async function getMyInstalledGuilds(): Promise<DashboardGuild[]> {
	const payload = await backendFetch<{ items: DashboardGuild[] }>(
		"/api/app/me/guilds/installed",
		undefined,
		{ requireSession: true },
	);
	return payload.items;
}
```

- [ ] **Step 3: Verify it typechecks**

Run: `cd apps/frontend && npx tsc --noEmit`
Expected: errors only in `app/dashboard/_components/guild-list.tsx` (it still references the old field names — Task 14 fixes it).

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): reshape DashboardGuild types and call /me/guilds/installed"
```

---

## Task 14: Frontend — update guild-list rendering

**Files:**
- Modify: `apps/frontend/app/dashboard/_components/guild-list.tsx`

- [ ] **Step 1: Replace the file body**

Replace the contents of `apps/frontend/app/dashboard/_components/guild-list.tsx` with:

```tsx
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Skeleton } from "@/components/ui/skeleton";
import { guildBannerUrl, guildIconUrl, userAvatarUrl } from "@/lib/discord-cdn";
import { getMyInstalledGuilds, type DashboardGuild } from "@/lib/rustlens-api";

function initials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]!.toUpperCase())
    .join("");
}

function GuildBanner({ guild }: { guild: DashboardGuild }) {
  const src =
    guildBannerUrl(guild.id, guild.bannerHash) ??
    guildIconUrl(guild.id, guild.iconHash, 1024);
  if (!src) {
    return (
      <div
        aria-hidden
        className="h-32 w-full bg-gradient-to-br from-muted to-muted-foreground/20"
      />
    );
  }
  return (
    <img
      src={src}
      alt=""
      className="h-32 w-full object-cover"
      loading="lazy"
    />
  );
}

function GuildCard({ guild }: { guild: DashboardGuild }) {
  const iconSrc = guildIconUrl(guild.id, guild.iconHash) ?? undefined;
  const ownerName = guild.owner?.globalName ?? guild.owner?.username ?? null;
  const ownerAvatarSrc =
    guild.owner ? userAvatarUrl(guild.owner.id, guild.owner.avatarHash) ?? undefined : undefined;

  return (
    <Card className="overflow-hidden">
      <GuildBanner guild={guild} />
      <CardHeader className="flex-row items-center gap-3">
        <Avatar size="lg" className="ring-2 ring-background">
          {iconSrc ? <AvatarImage src={iconSrc} alt="" /> : null}
          <AvatarFallback>{initials(guild.name)}</AvatarFallback>
        </Avatar>
        <CardTitle className="truncate">{guild.name}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {guild.owner && ownerName ? (
          <div className="flex items-center gap-2 text-muted-foreground">
            <Avatar size="sm">
              {ownerAvatarSrc ? <AvatarImage src={ownerAvatarSrc} alt="" /> : null}
              <AvatarFallback>{initials(ownerName)}</AvatarFallback>
            </Avatar>
            <span className="truncate text-xs">Owner: {ownerName}</span>
          </div>
        ) : null}
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          {guild.approximateMemberCount != null ? (
            <span>{guild.approximateMemberCount.toLocaleString()} members</span>
          ) : null}
          <span>
            {guild.trackedServerCount} tracked server
            {guild.trackedServerCount === 1 ? "" : "s"}
          </span>
        </div>
      </CardContent>
    </Card>
  );
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed py-16 text-center">
      <div className="font-heading text-base">No RustLens guilds yet</div>
      <p className="max-w-md text-sm text-muted-foreground">
        You aren&apos;t in any Discord servers that have RustLens installed.
        Once you join one—or invite the bot to your own server—it will show up
        here.
      </p>
    </div>
  );
}

export async function GuildList() {
  const guilds = await getMyInstalledGuilds();

  if (guilds.length === 0) {
    return <EmptyState />;
  }

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
      {guilds.map((guild) => (
        <GuildCard key={guild.id} guild={guild} />
      ))}
    </div>
  );
}

export function GuildListSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
      {Array.from({ length: 6 }).map((_, i) => (
        <Card key={i} className="overflow-hidden">
          <Skeleton className="h-32 w-full rounded-none" />
          <CardHeader className="flex-row items-center gap-3">
            <Skeleton className="size-10 rounded-full" />
            <Skeleton className="h-4 w-32" />
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            <Skeleton className="h-3 w-40" />
            <Skeleton className="h-3 w-24" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
```

- [ ] **Step 2: Verify it typechecks**

Run: `cd apps/frontend && npx tsc --noEmit`
Expected: clean exit.

- [ ] **Step 3: Run frontend test suite**

Run: `cd apps/frontend && pnpm vitest run`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/app/dashboard/_components/guild-list.tsx
git commit -m "feat(frontend): render dashboard guild cards from new shape via CDN helpers"
```

---

## Task 15: Manual acceptance test

**Files:** none (manual verification)

- [ ] **Step 1: Start the stack**

Run the local dev stack as documented (whatever `make dev` / `pnpm dev` workflow exists for backend + bot + frontend). Ensure the database migration has run (the new schema-init block will have created `bot_guilds` on backend startup).

- [ ] **Step 2: Sign in as a test user**

Open `http://localhost:3000/login`, authenticate via Discord OAuth using a test account. Confirm `/dashboard` loads.

- [ ] **Step 3: Add the bot to a server the test user is already in**

Use the bot's normal invite URL to add it to a Discord server where the test user is already a member.

- [ ] **Step 4: Confirm the guild appears on `/dashboard`**

Without logging out, refresh `/dashboard`. The newly-bot-installed server must appear within seconds (the time it takes the bot's bulk-upsert to finish — for a small server, near-instant; for a large server, up to a few seconds for the first batch to land).

If it does not appear:
1. Check backend logs for `UpsertBotGuildMetadata error` or `BulkUpsertUserGuilds error`.
2. Check bot logs for `[bot] bot-guilds: ...` errors.
3. Confirm `SELECT * FROM bot_guilds WHERE guild_id = '<id>'` returns a row with `is_installed = TRUE`.
4. Confirm `SELECT * FROM user_guilds WHERE discord_id = '<test-user-discord-id>' AND guild_id = '<id>'` returns a row with `is_active = TRUE`.

- [ ] **Step 5: Remove the bot and confirm the guild disappears**

Kick the bot from the server. Refresh `/dashboard`. The guild should disappear (the `guildDelete` event fires `markBotGuildLeft`, which sets `is_installed = FALSE`, and the dashboard query filters on `is_installed = TRUE`).

- [ ] **Step 6: Final commit (none required if all earlier commits are clean)**

If any small fixups were needed during manual testing, commit them with descriptive messages.

---

## Self-review notes

- **Spec coverage:**
  - `bot_guilds` schema → Task 1.
  - Upsert/mark-left/bulk-members/reconcile/dashboard-query store layer → Tasks 2–5.
  - 3 internal handlers + 1 public handler → Tasks 6–7.
  - Route wiring → Task 8.
  - Bot client + lifecycle module + wiring → Tasks 9–11.
  - CDN helpers + type/shape changes + UI → Tasks 12–14.
  - Acceptance test ("join a new server, see it without re-login") → Task 15.
  - Existing-installation backfill via `members_synced_at IS NULL` triggering bulk-upsert on first `clientReady` → covered by Task 10's `handleClientReady` (`stale` is true when `membersSyncedAt` is null).
  - `membersSyncComplete: true` flag on the final batch → covered in Task 6 (handler stamps when flag is true) and Task 10 (`isLast` controls the flag).
  - Sharded reconcile uses `(guild_id >> 22) % shard_count` → Task 4 implementation, Task 6 handler validates shard inputs.
- **Type consistency:** `UpsertBotGuildPayload` (TS) ↔ `upsertBotGuildRequest` (Go) field names match camelCase: `guildId`, `iconHash`, `bannerHash`, `ownerDiscordId`, `ownerUsername`, `ownerGlobalName`, `ownerAvatarHash`, `approximateMemberCount`, `members`, `membersSyncComplete`. `DashboardGuild` (frontend) ↔ JSON returned by `GetCurrentAppUserInstalledGuilds` (Go) match: `id`, `name`, `iconHash`, `bannerHash`, `approximateMemberCount`, `trackedServerCount`, `owner.{id,username,globalName,avatarHash}`.
- **No placeholders:** All code blocks contain working code; no TODOs in the plan body.
