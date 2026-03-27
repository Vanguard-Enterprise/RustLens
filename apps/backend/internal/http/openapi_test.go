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
		"/api/internal/bot/setup/guilds/{guildID}",
		"/api/internal/bot/setup/guilds/{guildID}/claim",
		"/api/internal/bot/setup/guilds/{guildID}/complete",
		"/api/internal/bot/setup/guilds/{guildID}/reset",
	}

	for _, path := range requiredPaths {
		if _, exists := paths[path]; !exists {
			t.Fatalf("missing path in openapi spec: %s", path)
		}
	}
}
