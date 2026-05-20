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

func TestListUserCommandUsageFiltered_OutcomeFilter(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	appUserID := "usr_filter_outcome"

	// Clean up any leftover rows from prior runs (non-transactional test DB).
	_, _ = s.pool.Exec(ctx, `DELETE FROM app_users WHERE id = 'usr_filter_outcome'`)

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_filter_outcome', '555555555555555555', 'filter_tester', 'filter_tester')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// insert one success + one error
	for _, outcome := range []string{"success", "error"} {
		if err := s.InsertCommandUsage(ctx, CommandUsageInsert{
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

	page, err := s.ListUserCommandUsageFiltered(ctx, appUserID,
		CommandUsageListFilters{Outcomes: []string{"error"}}, 25, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Outcome != "error" {
		t.Fatalf("want one error row, got total=%d items=%d", page.Total, len(page.Items))
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

func TestListUserCommandUsageFiltered_EnrichesUserTargets(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	actorID := "usr_enrich_actor"
	targetDiscordID := "666666666666666666"

	// Clean up any leftover rows from prior runs.
	_, _ = s.pool.Exec(ctx, `DELETE FROM app_users WHERE id = $1 OR discord_id = $2`, actorID, targetDiscordID)

	// Seed the actor (the user who ran the command).
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_enrich_actor', '600000000000000000', 'enrich_actor', 'enrich_actor')
	`); err != nil {
		t.Fatalf("seed actor: %v", err)
	}

	// Seed the target user — this is whose profile we expect to be enriched.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, discord_global_name, discord_avatar)
		VALUES ('usr_enrich_target', '666666666666666666', 'alice', 'globalAlice', 'avatar_abc')
	`); err != nil {
		t.Fatalf("seed target user: %v", err)
	}

	// Snapshot name at write time is stale — enrichment should replace it with live value.
	staleDisplayName := "stale_alice"
	err := s.InsertCommandUsage(ctx, CommandUsageInsert{
		AppUserID:       actorID,
		DiscordID:       "600000000000000000",
		InteractionType: "command",
		Command:         "announce",
		Outcome:         "success",
		Options:         []byte(`{}`),
		Targets: []CommandUsageTargetInsert{
			{Kind: "user", Snowflake: targetDiscordID, DisplayName: &staleDisplayName, Relation: "recipient"},
		},
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	page, err := s.ListUserCommandUsageFiltered(ctx, actorID, CommandUsageListFilters{}, 25, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("expected 1 item, got total=%d items=%d", page.Total, len(page.Items))
	}

	targets := page.Items[0].Targets
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	target := targets[0]

	// AppUserID must be populated by enrichment.
	if target.AppUserID == nil || *target.AppUserID != "usr_enrich_target" {
		t.Fatalf("expected AppUserID='usr_enrich_target', got %v", target.AppUserID)
	}
	// DisplayName must be the live global name, not the stale snapshot.
	if target.DisplayName == nil || *target.DisplayName != "globalAlice" {
		t.Fatalf("expected DisplayName='globalAlice', got %v", target.DisplayName)
	}
	// AvatarHash must be populated from discord_avatar.
	if target.AvatarHash == nil || *target.AvatarHash != "avatar_abc" {
		t.Fatalf("expected AvatarHash='avatar_abc', got %v", target.AvatarHash)
	}
}

func TestCommandUsageFilterOptions_DistinctCommandsAndGuilds(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	appUserID := "usr_filter_opts"

	// Clean up any leftover rows from prior runs (non-transactional test DB).
	_, _ = s.pool.Exec(ctx, `DELETE FROM app_users WHERE id = 'usr_filter_opts'`)

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_filter_opts', '777777777777777777', 'filter_opts_tester', 'filter_opts_tester')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	gid := "111111111111111111"
	for _, c := range []string{"track", "track", "announce"} {
		if err := s.InsertCommandUsage(ctx, CommandUsageInsert{
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

	opts, err := s.CommandUsageFilterOptions(ctx, appUserID, 90)
	if err != nil {
		t.Fatalf("filter opts: %v", err)
	}
	if len(opts.Commands) != 2 {
		t.Fatalf("want 2 distinct commands, got %d: %v", len(opts.Commands), opts.Commands)
	}
	if len(opts.Guilds) != 1 || opts.Guilds[0].GuildID != gid {
		t.Fatalf("want 1 guild %s, got %+v", gid, opts.Guilds)
	}
}

func TestCommandUsage_CorrelationLinkingAndChildren(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	appUserID := "usr_corr_a"
	otherUserID := "usr_corr_b"

	_, _ = s.pool.Exec(ctx, `DELETE FROM app_users WHERE id = ANY($1)`,
		[]string{appUserID, otherUserID})
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ($1, '888888888888888881', 'corr_a', 'corr_a'),
		       ($2, '888888888888888882', 'corr_b', 'corr_b')
	`, appUserID, otherUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	corr := "abc123"
	parent := CommandUsageInsert{
		AppUserID:       appUserID,
		DiscordID:       "888888888888888881",
		InteractionType: "command",
		Command:         "bind",
		Subcommand:      strPtr("c4"),
		Options:         []byte("{}"),
		Outcome:         "success",
		LatencyMs:       50,
		CorrelationID:   &corr,
	}
	if err := s.InsertCommandUsage(ctx, parent); err != nil {
		t.Fatalf("insert parent: %v", err)
	}

	// Child references the parent via parentCorrelationId — should be linked.
	child := CommandUsageInsert{
		AppUserID:           appUserID,
		DiscordID:           "888888888888888881",
		InteractionType:     "modal",
		Command:             "bind_c4_modal",
		Options:             []byte("{}"),
		Outcome:             "success",
		LatencyMs:           20,
		ParentCorrelationID: &corr,
	}
	if err := s.InsertCommandUsage(ctx, child); err != nil {
		t.Fatalf("insert child: %v", err)
	}

	// Same correlation id from a different user must NOT match the first user's
	// parent — scoping protects against cross-user link forgery.
	stray := CommandUsageInsert{
		AppUserID:           otherUserID,
		DiscordID:           "888888888888888882",
		InteractionType:     "modal",
		Command:             "bind_c4_modal",
		Options:             []byte("{}"),
		Outcome:             "success",
		ParentCorrelationID: &corr,
	}
	if err := s.InsertCommandUsage(ctx, stray); err != nil {
		t.Fatalf("insert stray: %v", err)
	}

	// Top-level list omits the child.
	page, err := s.ListUserCommandUsageFiltered(ctx, appUserID, CommandUsageListFilters{}, 25, 0)
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("want top-level total=1 (parent only), got %d", page.Total)
	}
	if page.Items[0].Command != "bind" {
		t.Fatalf("want top-level command=bind, got %s", page.Items[0].Command)
	}
	if page.Items[0].ChildCount != 1 {
		t.Fatalf("want childCount=1, got %d", page.Items[0].ChildCount)
	}
	parentID := page.Items[0].ID

	// Children listing returns the child only.
	children, err := s.ListUserCommandUsageChildren(ctx, appUserID, parentID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	if len(children) != 1 || children[0].Command != "bind_c4_modal" {
		t.Fatalf("unexpected children: %+v", children)
	}
	if children[0].ParentUsageID == nil || *children[0].ParentUsageID != parentID {
		t.Fatalf("child not linked to parent: %+v", children[0].ParentUsageID)
	}

	// The other user's stray modal must show up at the top level (no parent in
	// their scope) and have no children.
	otherPage, err := s.ListUserCommandUsageFiltered(ctx, otherUserID, CommandUsageListFilters{}, 25, 0)
	if err != nil {
		t.Fatalf("list filtered other: %v", err)
	}
	if otherPage.Total != 1 || otherPage.Items[0].ParentUsageID != nil {
		t.Fatalf("stray should be unlinked top-level row, got %+v", otherPage.Items)
	}
}

func TestCommandUsage_ChildrenScopedToUser(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	owner := "usr_kids_owner"
	other := "usr_kids_other"

	_, _ = s.pool.Exec(ctx, `DELETE FROM app_users WHERE id = ANY($1)`,
		[]string{owner, other})
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ($1, '999999999999999991', 'kids_o', 'kids_o'),
		       ($2, '999999999999999992', 'kids_x', 'kids_x')
	`, owner, other); err != nil {
		t.Fatalf("seed: %v", err)
	}

	corr := "deadbeef"
	if err := s.InsertCommandUsage(ctx, CommandUsageInsert{
		AppUserID: owner, DiscordID: "999999999999999991", InteractionType: "command",
		Command: "bind", Options: []byte("{}"), Outcome: "success", CorrelationID: &corr,
	}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := s.InsertCommandUsage(ctx, CommandUsageInsert{
		AppUserID: owner, DiscordID: "999999999999999991", InteractionType: "modal",
		Command: "bind_modal", Options: []byte("{}"), Outcome: "success", ParentCorrelationID: &corr,
	}); err != nil {
		t.Fatalf("seed child: %v", err)
	}

	page, err := s.ListUserCommandUsageFiltered(ctx, owner, CommandUsageListFilters{}, 25, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	parentID := page.Items[0].ID

	// Asking for the other user's view of the same parent id returns nothing.
	children, err := s.ListUserCommandUsageChildren(ctx, other, parentID)
	if err != nil {
		t.Fatalf("children other: %v", err)
	}
	if len(children) != 0 {
		t.Fatalf("expected zero children for other user, got %d", len(children))
	}
}
