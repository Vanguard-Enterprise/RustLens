package httpapi

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecContainsCoreRoutes(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal(openAPISpecJSON(), &spec); err != nil {
		t.Fatalf("failed to parse spec json: %v", err)
	}

	openapiVersion, ok := spec["openapi"].(string)
	if !ok || openapiVersion == "" {
		t.Fatalf("missing openapi version")
	}

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatalf("missing paths object")
	}

	requiredPaths := []string{
		"/api/healthz",
		"/api/internal/bot/shards/register",
		"/api/internal/bot/shards/heartbeat",
		"/api/internal/bot/shards/release",
		"/api/internal/bot/shards/assignments",
		"/api/internal/bot/setup/guilds/{guildID}/settings",
		"/api/internal/bot/setup/guilds/{guildID}/tracked-servers",
		"/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/active",
		"/api/internal/bot/setup/guilds/{guildID}/wipe-reacts",
		"/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/{messageID}",
		"/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/{messageID}/reactions",
		"/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/{messageID}/reactions/{userID}",
		"/api/internal/bot/setup/tracked-servers",
		"/api/internal/bot/setup/tracked-servers/{bmID}",
		"/api/internal/bot/setup/player-states/servers/{serverID}",
		"/api/internal/bot/setup/player-states/servers/{serverID}/players/{playerID}",
		"/api/internal/bot/setup/player-states/players/{playerID}",
		"/api/internal/bot/setup/user-guilds",
		"/api/internal/bot/setup/discord/{discordID}/guilds",
		"/api/internal/bot/setup/guilds/{guildID}/online-summary",
		"/api/internal/bot/setup/guilds/{guildID}/nickname-targets",
	}

	for _, path := range requiredPaths {
		if _, exists := paths[path]; !exists {
			t.Fatalf("missing path in openapi spec: %s", path)
		}
	}
}
