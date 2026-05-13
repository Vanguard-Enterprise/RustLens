package store

import (
	"context"
	"testing"
	"time"
)

func TestEnsureSchema_CreatesBotUserInstallsTable(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	expectedColumns := map[string]string{
		"discord_id":          "text",
		"username":            "text",
		"global_name":         "text",
		"avatar_hash":         "text",
		"scopes":              "ARRAY",
		"is_installed":        "boolean",
		"installed_at":        "timestamp with time zone",
		"left_at":             "timestamp with time zone",
		"last_seen_at":        "timestamp with time zone",
		"metadata_updated_at": "timestamp with time zone",
	}

	for column, dataType := range expectedColumns {
		var actual string
		err := s.pool.QueryRow(ctx, `
			SELECT data_type
			FROM information_schema.columns
			WHERE table_name = 'bot_user_installs' AND column_name = $1
		`, column).Scan(&actual)
		if err != nil {
			t.Fatalf("query column %s: %v", column, err)
		}
		if actual != dataType {
			t.Errorf("bot_user_installs.%s data_type = %q, want %q", column, actual, dataType)
		}
	}
}

func TestUpsertBotUserInstall_InsertsAndUpdates(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	input := UpsertBotUserInstallInput{
		DiscordID:  "400000000000000001",
		Username:   "alice",
		GlobalName: strPtr("Alice"),
		AvatarHash: strPtr("abc"),
		Scopes:     []string{"applications.commands"},
	}

	if err := s.UpsertBotUserInstall(ctx, input); err != nil {
		t.Fatalf("first UpsertBotUserInstall: %v", err)
	}

	input.Username = "alice-renamed"
	input.AvatarHash = strPtr("zzz")
	if err := s.UpsertBotUserInstall(ctx, input); err != nil {
		t.Fatalf("second UpsertBotUserInstall: %v", err)
	}

	var username, avatarHash string
	var isInstalled bool
	var leftAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT username, avatar_hash, is_installed, left_at
		FROM bot_user_installs WHERE discord_id = $1
	`, input.DiscordID).Scan(&username, &avatarHash, &isInstalled, &leftAt)
	if err != nil {
		t.Fatalf("query bot_user_installs: %v", err)
	}
	if username != "alice-renamed" || avatarHash != "zzz" || !isInstalled || leftAt != nil {
		t.Fatalf("unexpected row: username=%q avatar=%q installed=%v leftAt=%v",
			username, avatarHash, isInstalled, leftAt)
	}
}

func TestMarkBotUserInstallRemoved_SetsFlagAndTimestamp(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	id := "400000000000000002"
	if err := s.UpsertBotUserInstall(ctx, UpsertBotUserInstallInput{DiscordID: id, Username: "u"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.MarkBotUserInstallRemoved(ctx, id); err != nil {
		t.Fatalf("MarkBotUserInstallRemoved: %v", err)
	}

	var installed bool
	var leftAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT is_installed, left_at FROM bot_user_installs WHERE discord_id = $1
	`, id).Scan(&installed, &leftAt)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if installed || leftAt == nil {
		t.Fatalf("want installed=false leftAt non-nil, got %v / %v", installed, leftAt)
	}

	// Re-upserting must reset the flag (user reauthorized).
	if err := s.UpsertBotUserInstall(ctx, UpsertBotUserInstallInput{DiscordID: id, Username: "u"}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	err = s.pool.QueryRow(ctx, `
		SELECT is_installed, left_at FROM bot_user_installs WHERE discord_id = $1
	`, id).Scan(&installed, &leftAt)
	if err != nil {
		t.Fatalf("query 2: %v", err)
	}
	if !installed || leftAt != nil {
		t.Fatalf("want installed=true leftAt nil, got %v / %v", installed, leftAt)
	}
}

func TestTouchBotUserInstallSeen_InsertsAndBumps(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	id := "400000000000000003"

	// First touch must insert the row.
	if err := s.TouchBotUserInstallSeen(ctx, id); err != nil {
		t.Fatalf("first touch: %v", err)
	}
	var first time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT last_seen_at FROM bot_user_installs WHERE discord_id = $1`,
		id).Scan(&first); err != nil {
		t.Fatalf("read first: %v", err)
	}

	// Second touch must bump last_seen_at strictly forward.
	time.Sleep(10 * time.Millisecond)
	if err := s.TouchBotUserInstallSeen(ctx, id); err != nil {
		t.Fatalf("second touch: %v", err)
	}
	var second time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT last_seen_at FROM bot_user_installs WHERE discord_id = $1`,
		id).Scan(&second); err != nil {
		t.Fatalf("read second: %v", err)
	}
	if !second.After(first) {
		t.Errorf("expected second(%v) > first(%v)", second, first)
	}
}

func TestListAdminUserInstalls_PaginatesAndFilters(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	// Seed a few rows with predictable usernames.
	seeds := []UpsertBotUserInstallInput{
		{DiscordID: "400000000000000010", Username: "alice"},
		{DiscordID: "400000000000000011", Username: "bob"},
		{DiscordID: "400000000000000012", Username: "carol"},
	}
	for _, in := range seeds {
		if err := s.UpsertBotUserInstall(ctx, in); err != nil {
			t.Fatalf("seed %s: %v", in.DiscordID, err)
		}
	}

	rows, total, err := s.ListAdminUserInstalls(ctx, ListAdminUserInstallsOpts{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("ListAdminUserInstalls: %v", err)
	}
	if total < len(seeds) {
		t.Fatalf("total = %d, want >= %d", total, len(seeds))
	}
	if len(rows) < len(seeds) {
		t.Fatalf("rows = %d, want >= %d", len(rows), len(seeds))
	}

	// Filter by username substring.
	rows, total, err = s.ListAdminUserInstalls(ctx, ListAdminUserInstallsOpts{Q: "ali", Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("ListAdminUserInstalls(q=ali): %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].Username != "alice" {
		t.Fatalf("q=ali got total=%d rows=%#v", total, rows)
	}

	// Filter by discord_id prefix.
	rows, total, err = s.ListAdminUserInstalls(ctx, ListAdminUserInstallsOpts{Q: "400000000000000011", Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("ListAdminUserInstalls(q=id): %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].DiscordID != "400000000000000011" {
		t.Fatalf("q=id got total=%d rows=%#v", total, rows)
	}
}
