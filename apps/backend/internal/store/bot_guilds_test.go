package store

import (
	"context"
	"testing"
	"time"
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

func strPtr(v string) *string { return &v }
func int32Ptr(v int32) *int32 { return &v }
