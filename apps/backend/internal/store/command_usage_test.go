package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestCommandUsage_InsertAndList(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	// seed an app_users row (FK target)
	if _, err := s.pool.Exec(ctx, `
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
	s := newTestPostgres(t)

	if _, err := s.pool.Exec(ctx, `
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
	s := newTestPostgres(t)

	if _, err := s.pool.Exec(ctx, `
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
		if err := s.InsertCommandUsage(ctx, mk("player", "success")); err != nil {
			t.Fatalf("InsertCommandUsage: %v", err)
		}
	}
	if err := s.InsertCommandUsage(ctx, mk("track", "success")); err != nil {
		t.Fatalf("InsertCommandUsage: %v", err)
	}
	if err := s.InsertCommandUsage(ctx, mk("track", "error")); err != nil {
		t.Fatalf("InsertCommandUsage: %v", err)
	}

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

func TestInsertCommandUsage_WithTargets(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	appUserID := "usr_targets1"
	guildID := "111111111111111111"

	// Clean up any leftover rows from prior runs (non-transactional test DB).
	_, _ = s.pool.Exec(ctx, `DELETE FROM app_users WHERE id = 'usr_targets1'`)

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_targets1', '999999999999999999', 'targets_tester', 'targets_tester')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	displayBob := "bob"
	avatarBob := "abcde"

	err := s.InsertCommandUsage(ctx, CommandUsageInsert{
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
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM command_usage_targets ct
		JOIN command_usage cu ON cu.id = ct.command_usage_id
		WHERE cu.app_user_id = $1
	`, appUserID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("want 2 target rows, got %d", count)
	}
}

func TestCommandUsage_CascadeOnUserDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_test4', '444', 'tester4', 'tester4')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.InsertCommandUsage(ctx, CommandUsageInsert{
		AppUserID: "usr_test4", DiscordID: "444",
		InteractionType: "command", Command: "player",
		Options: []byte("{}"), Outcome: "success", LatencyMs: 1,
	}); err != nil {
		t.Fatalf("InsertCommandUsage: %v", err)
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM app_users WHERE id='usr_test4'`); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var count int
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM command_usage WHERE app_user_id='usr_test4'`).Scan(&count)
	if count != 0 {
		t.Fatalf("expected cascade delete, got %d rows", count)
	}
}

func TestInsertCommandUsage_TargetsCappedAt500(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	appUserID := "usr_cap_test"

	// Clean up any leftover rows from prior runs (non-transactional test DB).
	_, _ = s.pool.Exec(ctx, `DELETE FROM app_users WHERE id = 'usr_cap_test'`)

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_cap_test', '888888888888888888', 'cap_tester', 'cap_tester')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	targets := make([]CommandUsageTargetInsert, 0, 600)
	for i := range 600 {
		targets = append(targets, CommandUsageTargetInsert{
			Kind: "user", Snowflake: fmt.Sprintf("%018d", i), Relation: "recipient",
		})
	}

	err := s.InsertCommandUsage(ctx, CommandUsageInsert{
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
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM command_usage_targets t
		JOIN command_usage cu ON cu.id = t.command_usage_id
		WHERE cu.app_user_id = $1
	`, appUserID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != MaxTargetsPerUsage {
		t.Fatalf("want %d, got %d", MaxTargetsPerUsage, count)
	}
}
