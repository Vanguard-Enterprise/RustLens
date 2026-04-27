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
