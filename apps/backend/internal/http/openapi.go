package httpapi

import (
	"encoding/json"
	"net/http"
	"sync"
)

var (
	openAPIDocOnce sync.Once
	openAPIDocJSON []byte
)

func buildOpenAPIDocument() map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "RustLens Backend API",
			"version":     "1.0.0",
			"description": "Backend API for health checks and bot shard orchestration.",
		},
		"servers": []map[string]any{
			{"url": "http://localhost:8080"},
		},
		"paths": map[string]any{
			"/api/healthz": map[string]any{
				"get": map[string]any{
					"summary":     "Health check",
					"description": "Returns backend health and dependency status.",
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Healthy response",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/HealthResponse"},
								},
							},
						},
						"503": map[string]any{
							"description": "Degraded response",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/HealthResponse"},
								},
							},
						},
					},
				},
			},
			"/api/internal/bot/shards/register": map[string]any{
				"post": map[string]any{
					"summary":     "Register a bot worker",
					"description": "Registers a bot worker and returns shard target metadata.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/RegisterRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Worker registered",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/RegisterResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/shards/heartbeat": map[string]any{
				"post": map[string]any{
					"summary":     "Heartbeat a bot worker",
					"description": "Refreshes worker liveness and shard leases.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/HeartbeatRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Heartbeat accepted",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/StatusResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/shards/release": map[string]any{
				"post": map[string]any{
					"summary":     "Release a bot worker",
					"description": "Releases worker leases on shutdown.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/ReleaseRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Worker released",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/StatusResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/shards/assignments": map[string]any{
				"get": map[string]any{
					"summary":     "Get shard assignments",
					"description": "Returns shard assignments for the caller or a specific worker.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "workerId",
							"in":          "query",
							"required":    false,
							"description": "Optional worker ID; defaults to JWT claim worker_id.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Assignments returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/AssignmentsResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/settings": map[string]any{
				"get": map[string]any{
					"summary":     "Get guild settings",
					"description": "Returns persisted guild bot settings such as log channels and the admin role.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Guild settings returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/GuildSettings"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
				"put": map[string]any{
					"summary":     "Update guild settings",
					"description": "Updates persisted guild bot settings such as log channels and the admin role.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/UpdateGuildSettingsRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Guild settings updated",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/GuildSettings"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/active": map[string]any{
				"get": map[string]any{
					"summary":     "List active wipe reacts",
					"description": "Returns active persisted wipe reacts for a guild.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Active wipe reacts returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/WipeReactListResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/wipe-reacts": map[string]any{
				"get": map[string]any{
					"summary":     "List wipe reacts",
					"description": "Returns persisted wipe reacts for a guild, newest first.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Wipe reacts returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/WipeReactListResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
				"post": map[string]any{
					"summary":     "Create wipe react",
					"description": "Persists a wipe react message after Discord message creation.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/WipeReact"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Wipe react created",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/WipeReact"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/{messageID}": map[string]any{
				"get": map[string]any{
					"summary":     "Get wipe react",
					"description": "Returns one persisted wipe react by Discord message ID.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "messageID",
							"in":          "path",
							"required":    true,
							"description": "Discord message ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Wipe react returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/WipeReact"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
				"put": map[string]any{
					"summary":     "Update wipe react",
					"description": "Updates the persisted config of a wipe react.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "messageID",
							"in":          "path",
							"required":    true,
							"description": "Discord message ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/WipeReact"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Wipe react updated",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/WipeReact"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
				"delete": map[string]any{
					"summary":     "Delete wipe react",
					"description": "Deletes a persisted wipe react entry.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "messageID",
							"in":          "path",
							"required":    true,
							"description": "Discord message ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Wipe react deleted",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/StatusResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/{messageID}/reactions": map[string]any{
				"post": map[string]any{
					"summary":     "Set wipe react reaction",
					"description": "Sets one user's wipe react status, removing any previous status.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "messageID",
							"in":          "path",
							"required":    true,
							"description": "Discord message ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/SetWipeReactReactionRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Reaction updated",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/WipeReactReactionUpdate"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/{messageID}/reactions/{userID}": map[string]any{
				"delete": map[string]any{
					"summary":     "Remove wipe react reaction",
					"description": "Removes one user's wipe react status entirely.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "messageID",
							"in":          "path",
							"required":    true,
							"description": "Discord message ID.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "userID",
							"in":          "path",
							"required":    true,
							"description": "Discord user ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Reaction removed",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/WipeReactReactionRemoval"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/steam/{steamID}": map[string]any{
				"get": map[string]any{
					"summary":     "Get steam account",
					"description": "Returns a steam account by Steam ID.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "steamID",
							"in":          "path",
							"required":    true,
							"description": "17-digit Steam ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Steam account returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/SteamAccount"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"404": map[string]any{"$ref": "#/components/responses/NotFound"},
					},
				},
			},
			"/api/internal/bot/setup/steam/{steamID}/links": map[string]any{
				"get": map[string]any{
					"summary":     "List links by Steam ID",
					"description": "Returns all discord links for a steam account.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "steamID",
							"in":          "path",
							"required":    true,
							"description": "17-digit Steam ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Links returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/SteamDiscordLinkList"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/steam/by-bm/{bmID}": map[string]any{
				"get": map[string]any{
					"summary":     "Get steam account by BattleMetrics ID",
					"description": "Returns a steam account looked up by its BattleMetrics player ID.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "bmID",
							"in":          "path",
							"required":    true,
							"description": "BattleMetrics player ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Steam account returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/SteamAccount"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"404": map[string]any{"$ref": "#/components/responses/NotFound"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/tracked-servers": map[string]any{
				"get": map[string]any{
					"summary":     "List guild tracked servers",
					"description": "Returns the tracked BattleMetrics servers selected for one guild.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Guild tracked servers returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/GuildTrackedServerListResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
				"put": map[string]any{
					"summary":     "Replace guild tracked servers",
					"description": "Replaces the tracked BattleMetrics servers selected for one guild.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/ReplaceGuildTrackedServersRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Guild tracked servers updated",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/GuildTrackedServerListResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/tracked-servers": map[string]any{
				"get": map[string]any{
					"summary":     "List tracked servers",
					"description": "Returns all tracked BattleMetrics servers configured for the bot.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Tracked servers returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/TrackedServerListResponse"},
								},
							},
						},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
				"post": map[string]any{
					"summary":     "Create tracked server",
					"description": "Adds a BattleMetrics server to the bot's tracked server list.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/CreateTrackedServerRequest"},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{
							"description": "Tracked server created",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/TrackedServer"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"409": map[string]any{
							"description": "Tracked server already exists",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
								},
							},
						},
					},
				},
			},
			"/api/internal/bot/setup/tracked-servers/{bmID}": map[string]any{
				"delete": map[string]any{
					"summary":     "Delete tracked server",
					"description": "Deletes a tracked BattleMetrics server by ID.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "bmID",
							"in":          "path",
							"required":    true,
							"description": "BattleMetrics server ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Tracked server deleted",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/StatusResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"404": map[string]any{"$ref": "#/components/responses/NotFound"},
					},
				},
			},
			"/api/internal/bot/setup/player-states/servers/{serverID}": map[string]any{
				"get": map[string]any{
					"summary":     "List online player states by server",
					"description": "Returns current online player states for a tracked BattleMetrics server.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "serverID",
							"in":          "path",
							"required":    true,
							"description": "BattleMetrics server ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Player states returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/PlayerStateListResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/player-states/servers/{serverID}/players/{playerID}": map[string]any{
				"get": map[string]any{
					"summary":     "Get player state by server and player",
					"description": "Returns the current stored presence state for one player on one tracked server.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "serverID",
							"in":          "path",
							"required":    true,
							"description": "BattleMetrics server ID.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "playerID",
							"in":          "path",
							"required":    true,
							"description": "BattleMetrics player ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Player state returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/PlayerState"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"404": map[string]any{"$ref": "#/components/responses/NotFound"},
					},
				},
			},
			"/api/internal/bot/setup/player-states/players/{playerID}": map[string]any{
				"get": map[string]any{
					"summary":     "List player states by player",
					"description": "Returns stored presence states for one BattleMetrics player across all tracked servers.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "playerID",
							"in":          "path",
							"required":    true,
							"description": "BattleMetrics player ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Player states returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/PlayerStateListResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/online-summary": map[string]any{
				"get": map[string]any{
					"summary":     "List guild online summary",
					"description": "Returns guild-linked users who are currently online on tracked BattleMetrics servers.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Online summary returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/GuildOnlineSummaryListResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/steam/resolve": map[string]any{
				"post": map[string]any{
					"summary":     "Resolve Steam profile input",
					"description": "Resolves a 17-digit Steam ID or a supported steamcommunity.com profile URL into a 17-digit Steam ID.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/ResolveSteamIDRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Steam ID resolved",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/ResolveSteamIDResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/steam/links": map[string]any{
				"post": map[string]any{
					"summary":     "Create soft link",
					"description": "Creates a soft steam-discord link. Auto-creates the steam account if needed.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/CreateSoftLinkRequest"},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{
							"description": "Soft link created",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/SteamDiscordLink"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"409": map[string]any{
							"description": "Soft link already exists",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
								},
							},
						},
					},
				},
			},
			"/api/internal/bot/setup/steam/bulk-hard-links": map[string]any{
				"post": map[string]any{
					"summary":     "Bulk upsert hard links",
					"description": "Creates or updates multiple hard steam-discord links in a single transaction.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/BulkHardLinksRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Hard links upserted",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/BulkHardLinksResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/steam/{steamID}/links/{discordID}/soft": map[string]any{
				"delete": map[string]any{
					"summary":     "Delete soft link",
					"description": "Deletes a soft link between a steam account and a discord user.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "steamID",
							"in":          "path",
							"required":    true,
							"description": "17-digit Steam ID.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "discordID",
							"in":          "path",
							"required":    true,
							"description": "Discord user ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Soft link deleted",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/StatusResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"404": map[string]any{"$ref": "#/components/responses/NotFound"},
					},
				},
			},
			"/api/internal/bot/setup/discord/{discordID}/links": map[string]any{
				"get": map[string]any{
					"summary":     "List links by Discord ID",
					"description": "Returns all steam links for a discord user.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "discordID",
							"in":          "path",
							"required":    true,
							"description": "Discord user ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Links returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/SteamDiscordLinkList"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/discord/{discordID}/guilds": map[string]any{
				"get": map[string]any{
					"summary":     "List user guild links by Discord ID",
					"description": "Returns all guild links recorded for a Discord user in user_guilds.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "discordID",
							"in":          "path",
							"required":    true,
							"description": "Discord user ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "User guild links returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/UserGuildLinkList"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/user-guilds": map[string]any{
				"post": map[string]any{
					"summary":     "Create user guild link",
					"description": "Creates or upserts a user_guilds entry for a Discord user and guild.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/CreateUserGuildLinkRequest"},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{
							"description": "User guild link created",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/UserGuildLink"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/links": map[string]any{
				"get": map[string]any{
					"summary":     "List all guild steam-discord links",
					"description": "Returns all guild-scoped steam-discord links for a guild.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Guild links returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/GuildSteamDiscordLinkList"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
				"post": map[string]any{
					"summary":     "Create guild steam-discord link",
					"description": "Creates a guild-scoped link between a Discord user and a Steam account.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/CreateGuildLinkRequest"},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{
							"description": "Guild link created",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/GuildSteamDiscordLink"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"409": map[string]any{
							"description": "Link already exists",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
								},
							},
						},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/discord/{discordID}/links": map[string]any{
				"get": map[string]any{
					"summary":     "List guild links by Discord ID",
					"description": "Returns all guild-scoped steam links for a Discord user.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "discordID",
							"in":          "path",
							"required":    true,
							"description": "Discord user ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Guild links returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/GuildSteamDiscordLinkList"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/steam/{steamID}/links": map[string]any{
				"get": map[string]any{
					"summary":     "List guild links by Steam ID",
					"description": "Returns all guild-scoped discord links for a Steam account.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "steamID",
							"in":          "path",
							"required":    true,
							"description": "17-digit Steam ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Guild links returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/GuildSteamDiscordLinkList"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/discord/{discordID}/steam/{steamID}": map[string]any{
				"delete": map[string]any{
					"summary":     "Delete guild steam-discord link",
					"description": "Deletes a guild-scoped link between a Discord user and a Steam account.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "guildID",
							"in":          "path",
							"required":    true,
							"description": "Discord guild ID.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "discordID",
							"in":          "path",
							"required":    true,
							"description": "Discord user ID.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "steamID",
							"in":          "path",
							"required":    true,
							"description": "17-digit Steam ID.",
							"schema":      map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Guild link deleted",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/StatusResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"404": map[string]any{"$ref": "#/components/responses/NotFound"},
					},
				},
			},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
				},
			},
			"responses": map[string]any{
				"BadRequest": map[string]any{
					"description": "Bad request",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
						},
					},
				},
				"Unauthorized": map[string]any{
					"description": "Unauthorized",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
						},
					},
				},
				"NotFound": map[string]any{
					"description": "Not found",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
						},
					},
				},
			},
			"schemas": map[string]any{
				"HealthResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"status": map[string]any{"type": "string"},
						"db":     map[string]any{"type": "string"},
						"redis":  map[string]any{"type": "string"},
					},
					"required": []string{"status", "db", "redis"},
				},
				"RegisterRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"workerId": map[string]any{"type": "string"},
					},
				},
				"RegisterResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"workerId":            map[string]any{"type": "string"},
						"targetShards":        map[string]any{"type": "integer"},
						"heartbeatTtlSeconds": map[string]any{"type": "integer"},
					},
					"required": []string{"workerId", "targetShards", "heartbeatTtlSeconds"},
				},
				"HeartbeatRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"workerId": map[string]any{"type": "string"},
						"shardIds": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "integer"},
						},
					},
				},
				"ReleaseRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"workerId": map[string]any{"type": "string"},
					},
				},
				"StatusResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"status": map[string]any{"type": "string"},
					},
					"required": []string{"status"},
				},
				"AssignmentsResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"workerId":     map[string]any{"type": "string"},
						"targetShards": map[string]any{"type": "integer"},
						"shardIds": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "integer"},
						},
					},
					"required": []string{"workerId", "targetShards", "shardIds"},
				},
				"GuildSettings": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"guildId":                   map[string]any{"type": "string"},
						"wipeReactLogChannelId":     map[string]any{"type": "string"},
						"linkingLogChannelId":       map[string]any{"type": "string"},
						"trackedServerLogChannelId": map[string]any{"type": "string"},
						"alertsChannelId":           map[string]any{"type": "string"},
						"adminRoleId":               map[string]any{"type": "string"},
					},
					"required": []string{
						"guildId",
						"wipeReactLogChannelId",
						"linkingLogChannelId",
						"trackedServerLogChannelId",
						"alertsChannelId",
						"adminRoleId",
					},
				},
				"UpdateGuildSettingsRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"wipeReactLogChannelId":     map[string]any{"type": "string"},
						"linkingLogChannelId":       map[string]any{"type": "string"},
						"trackedServerLogChannelId": map[string]any{"type": "string"},
						"alertsChannelId":           map[string]any{"type": "string"},
						"adminRoleId":               map[string]any{"type": "string"},
					},
				},
				"GuildTrackedServer": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"guildId": map[string]any{"type": "string"},
						"bmId":    map[string]any{"type": "string"},
						"name":    map[string]any{"type": "string"},
					},
					"required": []string{"guildId", "bmId", "name"},
				},
				"GuildTrackedServerListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/GuildTrackedServer"},
						},
					},
					"required": []string{"items"},
				},
				"ReplaceGuildTrackedServersRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"bmIds": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required": []string{"bmIds"},
				},
				"TrackedServer": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"bmId":          map[string]any{"type": "string"},
						"name":          map[string]any{"type": "string"},
						"lastPolledAt":  map[string]any{"type": "string", "format": "date-time", "nullable": true},
						"lastPollError": map[string]any{"type": "string"},
					},
					"required": []string{"bmId", "name", "lastPolledAt", "lastPollError"},
				},
				"TrackedServerListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/TrackedServer"},
						},
					},
					"required": []string{"items"},
				},
				"CreateTrackedServerRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"bmId": map[string]any{"type": "string"},
						"name": map[string]any{"type": "string"},
					},
					"required": []string{"bmId", "name"},
				},
				"PlayerState": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"serverBmId":   map[string]any{"type": "string"},
						"serverName":   map[string]any{"type": "string"},
						"playerBmId":   map[string]any{"type": "string"},
						"isOnline":     map[string]any{"type": "boolean"},
						"onlineSince":  map[string]any{"type": "string", "format": "date-time", "nullable": true},
						"offlineSince": map[string]any{"type": "string", "format": "date-time", "nullable": true},
						"updatedAt":    map[string]any{"type": "string", "format": "date-time"},
					},
					"required": []string{"serverBmId", "serverName", "playerBmId", "isOnline", "onlineSince", "offlineSince", "updatedAt"},
				},
				"PlayerStateListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/PlayerState"},
						},
					},
					"required": []string{"items"},
				},
				"GuildOnlineSummaryItem": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"guildId":    map[string]any{"type": "string"},
						"discordId":  map[string]any{"type": "string"},
						"steamId":    map[string]any{"type": "string"},
						"playerBmId": map[string]any{"type": "string"},
						"bmName":     map[string]any{"type": "string"},
						"serverBmId": map[string]any{"type": "string"},
						"serverName": map[string]any{"type": "string"},
						"onlineUnix": map[string]any{"type": "integer"},
					},
					"required": []string{"guildId", "discordId", "steamId", "playerBmId", "bmName", "serverBmId", "serverName", "onlineUnix"},
				},
				"GuildOnlineSummaryListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/GuildOnlineSummaryItem"},
						},
					},
					"required": []string{"items"},
				},
				"WipeReact": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"guildId":           map[string]any{"type": "string"},
						"channelId":         map[string]any{"type": "string"},
						"messageId":         map[string]any{"type": "string"},
						"creatorId":         map[string]any{"type": "string"},
						"name":              map[string]any{"type": "string"},
						"description":       map[string]any{"type": "string"},
						"timeZone":          map[string]any{"type": "string"},
						"scheduledUnix":     map[string]any{"type": "integer"},
						"askLateReason":     map[string]any{"type": "boolean"},
						"askDeclinedReason": map[string]any{"type": "boolean"},
						"acceptedUserIds": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"declinedUserIds": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"lateUserIds": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"embedTimestamp": map[string]any{"type": "string"},
					},
					"required": []string{
						"guildId", "channelId", "messageId", "creatorId", "name", "description",
						"timeZone", "scheduledUnix", "askLateReason", "askDeclinedReason",
						"acceptedUserIds", "declinedUserIds", "lateUserIds", "embedTimestamp",
					},
				},
				"WipeReactListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/WipeReact"},
						},
					},
					"required": []string{"items"},
				},
				"SetWipeReactReactionRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"userId": map[string]any{"type": "string"},
						"type":   map[string]any{"type": "string"},
					},
					"required": []string{"userId", "type"},
				},
				"WipeReactReactionUpdate": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"changed":      map[string]any{"type": "boolean"},
						"previousType": map[string]any{"type": "string", "nullable": true},
						"wipeReact":    map[string]any{"$ref": "#/components/schemas/WipeReact"},
					},
					"required": []string{"changed", "wipeReact"},
				},
				"WipeReactReactionRemoval": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"changed":     map[string]any{"type": "boolean"},
						"removedType": map[string]any{"type": "string", "nullable": true},
						"wipeReact":   map[string]any{"$ref": "#/components/schemas/WipeReact"},
					},
					"required": []string{"changed", "wipeReact"},
				},
				"SteamAccount": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"steamId":     map[string]any{"type": "string"},
						"bmId":        map[string]any{"type": "string", "nullable": true},
						"bmName":      map[string]any{"type": "string"},
						"avatarImage": map[string]any{"type": "string"},
						"createdAt":   map[string]any{"type": "string", "format": "date-time"},
					},
					"required": []string{"steamId", "bmId", "bmName", "avatarImage", "createdAt"},
				},
				"SteamDiscordLink": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"steamId":   map[string]any{"type": "string"},
						"discordId": map[string]any{"type": "string"},
						"type":      map[string]any{"type": "string", "enum": []string{"soft", "hard"}},
						"source":    map[string]any{"type": "string"},
						"createdAt": map[string]any{"type": "string", "format": "date-time"},
					},
					"required": []string{"steamId", "discordId", "type", "source", "createdAt"},
				},
				"SteamDiscordLinkList": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/SteamDiscordLink"},
						},
					},
					"required": []string{"items"},
				},
				"CreateSoftLinkRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"steamId":   map[string]any{"type": "string"},
						"discordId": map[string]any{"type": "string"},
						"source":    map[string]any{"type": "string"},
					},
					"required": []string{"steamId", "discordId", "source"},
				},
				"ResolveSteamIDRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"input": map[string]any{"type": "string"},
					},
					"required": []string{"input"},
				},
				"ResolveSteamIDResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"steamId": map[string]any{"type": "string"},
					},
					"required": []string{"steamId"},
				},
				"BulkHardLinksRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"links": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"steamId":   map[string]any{"type": "string"},
									"bmId":      map[string]any{"type": "string", "nullable": true},
									"discordId": map[string]any{"type": "string"},
									"source":    map[string]any{"type": "string"},
								},
								"required": []string{"steamId", "discordId", "source"},
							},
						},
					},
					"required": []string{"links"},
				},
				"BulkHardLinksResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"status": map[string]any{"type": "string"},
						"count":  map[string]any{"type": "integer"},
					},
					"required": []string{"status", "count"},
				},
				"GuildSteamDiscordLink": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"guildId":   map[string]any{"type": "string"},
						"discordId": map[string]any{"type": "string"},
						"steamId":   map[string]any{"type": "string"},
						"createdAt": map[string]any{"type": "string", "format": "date-time"},
					},
					"required": []string{"guildId", "discordId", "steamId", "createdAt"},
				},
				"GuildSteamDiscordLinkList": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/GuildSteamDiscordLink"},
						},
					},
					"required": []string{"items"},
				},
				"CreateGuildLinkRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"discordId": map[string]any{"type": "string"},
						"steamId":   map[string]any{"type": "string"},
					},
					"required": []string{"discordId", "steamId"},
				},
				"UserGuildLink": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"discordId": map[string]any{"type": "string"},
						"guildId":   map[string]any{"type": "string"},
						"createdAt": map[string]any{"type": "string", "format": "date-time"},
					},
					"required": []string{"discordId", "guildId", "createdAt"},
				},
				"UserGuildLinkList": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/UserGuildLink"},
						},
					},
					"required": []string{"items"},
				},
				"CreateUserGuildLinkRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"discordId": map[string]any{"type": "string"},
						"guildId":   map[string]any{"type": "string"},
					},
					"required": []string{"discordId", "guildId"},
				},
				"ErrorResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"error": map[string]any{"type": "string"},
					},
					"required": []string{"error"},
				},
			},
		},
	}
}

func openAPISpecJSON() []byte {
	openAPIDocOnce.Do(func() {
		document := buildOpenAPIDocument()
		encoded, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			openAPIDocJSON = []byte(`{"openapi":"3.0.3","info":{"title":"RustLens Backend API","version":"1.0.0"}}`)
			return
		}
		openAPIDocJSON = encoded
	})

	return openAPIDocJSON
}

func (h *Handler) OpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPISpecJSON())
}

func (h *Handler) DocsUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width,initial-scale=1" />
    <title>RustLens API Docs</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
    <style>
      body { margin: 0; background: #fafafa; }
      #swagger-ui { max-width: 1200px; margin: 0 auto; }
    </style>
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
      window.ui = SwaggerUIBundle({
        url: "/api/docs/openapi.json",
        dom_id: "#swagger-ui",
        deepLinking: true,
        displayRequestDuration: true
      });
    </script>
  </body>
</html>`))
}
