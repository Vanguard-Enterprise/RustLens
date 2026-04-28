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
			"/api/internal/bot/owner/proxies": map[string]any{
				"get": map[string]any{
					"summary":     "List outbound proxies",
					"description": "Returns owner-managed outbound proxies with credentials removed from endpoints.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Proxies returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/OutboundProxyListResponse"},
								},
							},
						},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
				"post": map[string]any{
					"summary":     "Create outbound proxy",
					"description": "Stores a full proxy URL encrypted at rest and returns only a credential-free endpoint.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/CreateOutboundProxyRequest"},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{
							"description": "Proxy created",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/OutboundProxy"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"409": map[string]any{"$ref": "#/components/responses/Conflict"},
					},
				},
			},
			"/api/internal/bot/owner/proxies/{proxyID}": map[string]any{
				"delete": map[string]any{
					"summary":     "Delete outbound proxy",
					"description": "Deletes an owner-managed outbound proxy and reloads the runtime proxy provider.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "proxyID",
							"in":          "path",
							"required":    true,
							"description": "Outbound proxy ID.",
							"schema":      map[string]any{"type": "integer"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{"$ref": "#/components/responses/OKStatus"},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"404": map[string]any{"$ref": "#/components/responses/NotFound"},
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
			"/api/internal/bot/setup/guilds/{guildID}/wipe-react-schedule-posts/claim": map[string]any{
				"post": map[string]any{
					"summary":     "Claim next scheduled wipe react post",
					"description": "Claims the next due scheduled wipe react post for a guild.",
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
								"schema": map[string]any{"$ref": "#/components/schemas/WipeReactSchedulePostWorkerRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Claim result returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/WipeReactSchedulePostClaimResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/wipe-react-schedule-posts/{scheduleID}/complete": map[string]any{
				"post": map[string]any{
					"summary":     "Complete scheduled wipe react post",
					"description": "Marks a claimed scheduled wipe react post as completed.",
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
							"name":        "scheduleID",
							"in":          "path",
							"required":    true,
							"description": "Scheduled post ID.",
							"schema":      map[string]any{"type": "integer"},
						},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/CompleteWipeReactSchedulePostRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Scheduled post completed",
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
			"/api/internal/bot/setup/guilds/{guildID}/wipe-react-schedule-posts/{scheduleID}/release": map[string]any{
				"post": map[string]any{
					"summary":     "Release scheduled wipe react post",
					"description": "Releases a claimed scheduled wipe react post back to the queue.",
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
							"name":        "scheduleID",
							"in":          "path",
							"required":    true,
							"description": "Scheduled post ID.",
							"schema":      map[string]any{"type": "integer"},
						},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/WipeReactSchedulePostWorkerRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Scheduled post released",
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
					"description": "Returns visible discord links for a steam account. Discord identities hidden from link visibility are omitted.",
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
			"/api/internal/bot/setup/players/{bmID}/bm-profile": map[string]any{
				"get": map[string]any{
					"summary":     "Get BattleMetrics-keyed player profile",
					"description": "Returns the player profile by BM ID using the BM-keyed cache, falling back to a fresh BattleMetrics fetch when the cache is missing or stale. The response uses the same shape as the steam-linked profile endpoint so /player can render a single profile card; Steam-only fields (rust hours, avatar, steamId) are populated only when a Steam linkage exists.",
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
							"description": "Player profile returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/SteamProfile"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"404": map[string]any{"$ref": "#/components/responses/NotFound"},
						"502": map[string]any{"description": "BattleMetrics upstream failure"},
					},
				},
			},
			"/api/internal/bot/setup/players/{bmID}/bm-analytics": map[string]any{
				"get": map[string]any{
					"summary":     "Get BattleMetrics-keyed player analytics",
					"description": "Returns analytics for a player by BM ID using the BM-keyed cache, with Steam-only enrichment (top games, total games) sourced from steam_account_cache when a Steam linkage exists.",
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
							"description": "Player analytics returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/SteamAnalytics"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"404": map[string]any{"$ref": "#/components/responses/NotFound"},
						"502": map[string]any{"description": "BattleMetrics upstream failure"},
					},
				},
			},
			"/api/internal/bot/setup/players/{bmID}/offline-analytics": map[string]any{
				"get": map[string]any{
					"summary":     "Get player offline analytics",
					"description": "Returns chart-ready offline timing analytics from the player's last BattleMetrics sessions across all servers.",
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
							"description": "Offline analytics returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/TrackAnalyticsResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/steam-accounts/count": map[string]any{
				"get": map[string]any{
					"summary":     "Get steam account count",
					"description": "Returns the number of rows currently stored in steam_accounts.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Steam account count returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/CountResponse"},
								},
							},
						},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
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
			"/api/internal/bot/setup/guilds/{guildID}/tracked-servers/{bmID}": map[string]any{
				"post": map[string]any{
					"summary":     "Enable a tracked server for a guild",
					"description": "Adds one tracked BattleMetrics server to the guild's enabled list.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{"name": "guildID", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
						{"name": "bmID", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Server enabled"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"404": map[string]any{"description": "Tracked server not found"},
						"409": map[string]any{"description": "Server already enabled"},
					},
				},
				"delete": map[string]any{
					"summary":     "Disable a tracked server for a guild",
					"description": "Removes one tracked BattleMetrics server from the guild's enabled list.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{"name": "guildID", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
						{"name": "bmID", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Server disabled"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"404": map[string]any{"description": "Server was not enabled"},
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
			"/api/internal/bot/setup/player-states/servers/{serverID}/search": map[string]any{
				"get": map[string]any{
					"summary":     "Search player states by name on a server",
					"description": "Returns matching player states for one tracked BattleMetrics server. By default only online players are returned; setting includeOffline=true also includes players whose offlineSince is within the last 24 hours.",
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
							"name":        "q",
							"in":          "query",
							"required":    true,
							"description": "Case-insensitive substring to match against steam_account_cache.bm_name.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "Maximum number of results to return. Defaults to 20 and is capped at 20.",
							"schema":      map[string]any{"type": "integer", "minimum": 1, "maximum": 20, "default": 20},
						},
						{
							"name":        "includeOffline",
							"in":          "query",
							"required":    false,
							"description": "When true, also includes players whose offlineSince is within the last 24 hours. Defaults to false.",
							"schema":      map[string]any{"type": "boolean", "default": false},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Matching player states returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type":     "object",
										"required": []string{"items"},
										"properties": map[string]any{
											"items": map[string]any{
												"type": "array",
												"items": map[string]any{
													"type":     "object",
													"required": []string{"serverBmId", "serverName", "playerBmId", "steamId", "bmName", "isOnline"},
													"properties": map[string]any{
														"serverBmId": map[string]any{"type": "string"},
														"serverName": map[string]any{"type": "string"},
														"playerBmId": map[string]any{"type": "string"},
														"steamId":    map[string]any{"type": "string"},
														"bmName":     map[string]any{"type": "string"},
														"isOnline":   map[string]any{"type": "boolean"},
													},
												},
											},
										},
									},
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
					"description": "Returns visible steam links for a discord user. Hidden discord link identities return an empty list.",
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
			"/api/internal/bot/setup/discord/{discordID}/link-visibility-hidden": map[string]any{
				"put": map[string]any{
					"summary":     "Hide Discord link identity",
					"description": "Suppresses link lookup disclosure for a Discord identity without deleting stored links.",
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
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/HideDiscordLinkIdentityRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Discord link identity hidden",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/HiddenDiscordLinkIdentity"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
				"delete": map[string]any{
					"summary":     "Unhide Discord link identity",
					"description": "Removes link lookup suppression for a Discord identity.",
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
							"description": "Discord link identity unhidden",
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
					"description": "Creates or upserts a user_guilds entry and marks the user active in the guild.",
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
				"delete": map[string]any{
					"summary":     "Mark user guild link inactive",
					"description": "Marks a user_guilds entry inactive for a Discord user and guild and sets leftAt.",
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
						"200": map[string]any{
							"description": "User guild link updated",
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
			"/api/internal/bot/setup/guilds/{guildID}/game-bans": map[string]any{
				"get": map[string]any{
					"summary":     "Check guild game bans",
					"description": "Refreshes stale Steam game-ban cache entries for guild-linked Steam IDs using a 6 hour TTL and returns only game bans from the last 90 days.",
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
							"description": "Recent guild game bans returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/GuildRecentGameBanListResponse"},
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
			"/api/internal/bot/setup/guilds/{guildID}/nickname-targets": map[string]any{
				"get": map[string]any{
					"summary":     "List guild nickname targets",
					"description": "Returns one guild-scoped steam-discord link per Discord user joined with steam_accounts bm_id and steam_account_cache bm_name for nickname updates.",
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
							"description": "Nickname targets returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/GuildNicknameTargetList"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
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
			"/api/auth/discord": map[string]any{
				"get": map[string]any{
					"summary":     "Begin Discord OAuth sign-in",
					"description": "Sets PKCE/state cookies and 302-redirects to Discord. Refuses with /login?error=discord-already-signed-in if the user already has a session cookie.",
					"responses": map[string]any{
						"302": map[string]any{
							"description": "Redirect to Discord authorize endpoint or back to /login on policy refusal.",
						},
					},
				},
			},
			"/api/auth/discord/link": map[string]any{
				"get": map[string]any{
					"summary":     "Begin Discord OAuth account-link flow",
					"description": "Requires an active session (rustlens_session cookie). 302s to /login?error=session when missing.",
					"responses": map[string]any{
						"302": map[string]any{"description": "Redirect to Discord authorize endpoint or /login on missing session."},
					},
				},
			},
			"/api/auth/discord/callback": map[string]any{
				"get": map[string]any{
					"summary":     "Discord OAuth callback (sign-in or link)",
					"description": "Validates state, exchanges code, fetches profile, branches on intent cookie. Sign-in path mints rustlens_session cookie and redirects to /dashboard. Link path redirects to /dashboard/settings with notice or error query string.",
					"parameters": []any{
						map[string]any{"name": "code", "in": "query", "required": true, "schema": map[string]any{"type": "string"}},
						map[string]any{"name": "state", "in": "query", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{
						"302": map[string]any{"description": "Redirect to /dashboard, /dashboard/settings, or /login depending on outcome."},
					},
				},
			},
			"/api/app/auth/register": map[string]any{
				"post": map[string]any{
					"summary":     "Register a password user",
					"description": "Creates a new email and password browser account and returns the signed-in user payload.",
					"security":    []map[string][]string{{"appSecretAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/PasswordRegisterRequest"},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{
							"description": "User registered",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/CurrentUserResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"409": map[string]any{"$ref": "#/components/responses/Conflict"},
					},
				},
			},
			"/api/app/auth/login": map[string]any{
				"post": map[string]any{
					"summary":     "Log in with email and password",
					"description": "Authenticates a browser app user with email and password and returns the current user payload.",
					"security":    []map[string][]string{{"appSecretAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/PasswordLoginRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "User authenticated",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/CurrentUserResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/app/auth/password/reset/request": map[string]any{
				"post": map[string]any{
					"summary":     "Request a password reset",
					"description": "Creates a password reset token for a linked email address and optionally returns a debug token in local debug mode.",
					"security":    []map[string][]string{{"appSecretAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/PasswordResetRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Password reset request handled",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/PasswordResetRequestResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/app/auth/password/reset/consume": map[string]any{
				"post": map[string]any{
					"summary":     "Consume a password reset token",
					"description": "Resets a password with a one-time token and returns the current user payload.",
					"security":    []map[string][]string{{"appSecretAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/ConsumePasswordResetRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Password reset consumed",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/CurrentUserResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/app/auth/email/verify/consume": map[string]any{
				"post": map[string]any{
					"summary":     "Consume an email verification token",
					"description": "Marks the primary email as verified using a one-time token and returns the current user payload.",
					"security":    []map[string][]string{{"appSecretAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/ConsumeEmailVerificationRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Email verified",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/CurrentUserResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/app/auth/email/verify/request": map[string]any{
				"post": map[string]any{
					"summary":     "Request email verification",
					"description": "Creates an email verification token for the signed-in app user and optionally returns a debug token in local debug mode.",
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Email verification request handled",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/EmailVerificationRequestResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"409": map[string]any{"$ref": "#/components/responses/Conflict"},
					},
				},
			},
			"/api/app/me": map[string]any{
				"get": map[string]any{
					"summary":     "Get the current app user",
					"description": "Returns the signed-in browser user's account, email, connection, and guild membership data.",
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Current user returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/CurrentUserResponse"},
								},
							},
						},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"404": map[string]any{"$ref": "#/components/responses/NotFound"},
					},
				},
			},
			"/api/app/me/connections": map[string]any{
				"get": map[string]any{
					"summary":     "Get current app user connections",
					"description": "Returns the signed-in browser user's email, Discord, and Steam connection data.",
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Connections returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/CurrentUserConnectionsResponse"},
								},
							},
						},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/app/me/guilds": map[string]any{
				"get": map[string]any{
					"summary":     "Get current app user guild memberships",
					"description": "Returns the signed-in browser user's guild membership records mirrored from Discord.",
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Guild memberships returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/UserGuildMembershipListResponse"},
								},
							},
						},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/app/admin/users": map[string]any{
				"get": map[string]any{
					"summary":     "List admin users",
					"description": "Returns internal admin overview data for browser app users.",
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Admin users returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/AdminUserListResponse"},
								},
							},
						},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"403": map[string]any{"$ref": "#/components/responses/Forbidden"},
					},
				},
			},
			"/api/app/admin/guilds": map[string]any{
				"get": map[string]any{
					"summary":     "List admin guilds",
					"description": "Returns internal admin overview data for guild activity, linked members, and tracked servers.",
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Admin guilds returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/AdminGuildOverviewListResponse"},
								},
							},
						},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"403": map[string]any{"$ref": "#/components/responses/Forbidden"},
					},
				},
			},
			"/api/app/admin/servers": map[string]any{
				"get": map[string]any{
					"summary":     "List admin servers",
					"description": "Returns internal admin overview data for owner-managed tracked BattleMetrics servers.",
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Admin servers returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/AdminServerOverviewListResponse"},
								},
							},
						},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"403": map[string]any{"$ref": "#/components/responses/Forbidden"},
					},
				},
			},
			"/api/app/admin/audit": map[string]any{
				"get": map[string]any{
					"summary":     "List admin audit events",
					"description": "Returns recent browser auth and internal admin audit events.",
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "Optional limit between 1 and 200. Defaults to 50.",
							"schema":      map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Audit events returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/AuditEventListResponse"},
								},
							},
						},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"403": map[string]any{"$ref": "#/components/responses/Forbidden"},
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
				"appSecretAuth": map[string]any{
					"type": "apiKey",
					"in":   "header",
					"name": "X-RustLens-App-Secret",
				},
				"appSessionAuth": map[string]any{
					"type": "apiKey",
					"in":   "header",
					"name": "X-RustLens-Session",
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
				"Forbidden": map[string]any{
					"description": "Forbidden",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
						},
					},
				},
				"Conflict": map[string]any{
					"description": "Conflict",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
						},
					},
				},
				"OKStatus": map[string]any{
					"description": "Status response",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/StatusResponse"},
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
				"CountResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"count": map[string]any{"type": "integer"},
					},
					"required": []string{"count"},
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
				"OutboundProxy": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":       map[string]any{"type": "integer"},
						"endpoint": map[string]any{"type": "string", "description": "Credential-free endpoint, e.g. http://1.2.3.4:8080."},
						"enabled":  map[string]any{"type": "boolean"},
					},
					"required": []string{"id", "endpoint", "enabled"},
				},
				"OutboundProxyListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/OutboundProxy"},
						},
					},
					"required": []string{"items"},
				},
				"CreateOutboundProxyRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"endpoint": map[string]any{"type": "string", "description": "Full proxy URL including username and password."},
					},
					"required": []string{"endpoint"},
				},
				"GuildSettings": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"guildId":                   map[string]any{"type": "string"},
						"wipeReactLogChannelId":     map[string]any{"type": "string"},
						"linkingLogChannelId":       map[string]any{"type": "string"},
						"trackedServerLogChannelId": map[string]any{"type": "string"},
						"trackedUsersCategoryId":    map[string]any{"type": "string"},
						"adminRoleId":               map[string]any{"type": "string"},
					},
					"required": []string{
						"guildId",
						"wipeReactLogChannelId",
						"linkingLogChannelId",
						"trackedServerLogChannelId",
						"trackedUsersCategoryId",
						"adminRoleId",
					},
				},
				"UpdateGuildSettingsRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"wipeReactLogChannelId":     map[string]any{"type": "string"},
						"linkingLogChannelId":       map[string]any{"type": "string"},
						"trackedServerLogChannelId": map[string]any{"type": "string"},
						"trackedUsersCategoryId":    map[string]any{"type": "string"},
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
				"TrackAnalyticsHour": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"hourUnix": map[string]any{"type": "integer"},
						"hour":     map[string]any{"type": "integer"},
						"avg":      map[string]any{"type": "number"},
						"minutes":  map[string]any{"type": "number"},
					},
					"required": []string{"hourUnix", "hour", "avg", "minutes"},
				},
				"TrackAnalyticsDay": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"weekday":   map[string]any{"type": "string"},
						"bestStart": map[string]any{"type": "integer"},
						"bestEnd":   map[string]any{"type": "integer"},
						"hours": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/TrackAnalyticsHour"},
						},
					},
					"required": []string{"weekday", "bestStart", "bestEnd", "hours"},
				},
				"TrackAnalyticsResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"groupName":  map[string]any{"type": "string"},
						"serverName": map[string]any{"type": "string"},
						"timeZone":   map[string]any{"type": "string"},
						"members":    map[string]any{"type": "integer"},
						"analyzed":   map[string]any{"type": "integer"},
						"days": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/TrackAnalyticsDay"},
						},
					},
					"required": []string{"groupName", "serverName", "timeZone", "members", "analyzed", "days"},
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
				"GuildRecentGameBan": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"guildId":        map[string]any{"type": "string"},
						"discordId":      map[string]any{"type": "string"},
						"steamId":        map[string]any{"type": "string"},
						"gameBannedAt":   map[string]any{"type": "string", "format": "date-time"},
						"gameBannedUnix": map[string]any{"type": "integer"},
					},
					"required": []string{"guildId", "discordId", "steamId", "gameBannedAt", "gameBannedUnix"},
				},
				"GuildRecentGameBanListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/GuildRecentGameBan"},
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
				"WipeReactSchedulePost": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":                map[string]any{"type": "integer"},
						"scheduleId":        map[string]any{"type": "integer"},
						"guildId":           map[string]any{"type": "string"},
						"channelId":         map[string]any{"type": "string"},
						"wipeUnix":          map[string]any{"type": "integer"},
						"postUnix":          map[string]any{"type": "integer"},
						"timeZone":          map[string]any{"type": "string"},
						"name":              map[string]any{"type": "string"},
						"description":       map[string]any{"type": "string"},
						"askLateReason":     map[string]any{"type": "boolean"},
						"askDeclinedReason": map[string]any{"type": "boolean"},
						"createdBy":         map[string]any{"type": "string"},
					},
					"required": []string{"id", "scheduleId", "guildId", "channelId", "wipeUnix", "postUnix", "timeZone", "name", "description", "askLateReason", "askDeclinedReason", "createdBy"},
				},
				"WipeReactSchedulePostClaimResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"item": map[string]any{"$ref": "#/components/schemas/WipeReactSchedulePost", "nullable": true},
					},
					"required": []string{"item"},
				},
				"WipeReactSchedulePostWorkerRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"workerId": map[string]any{"type": "string"},
					},
					"required": []string{"workerId"},
				},
				"CompleteWipeReactSchedulePostRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"workerId":  map[string]any{"type": "string"},
						"messageId": map[string]any{"type": "string"},
						"channelId": map[string]any{"type": "string"},
					},
					"required": []string{"workerId", "messageId", "channelId"},
				},
				"SteamAccount": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"steamId":     map[string]any{"type": "string"},
						"bmId":        map[string]any{"type": "string", "nullable": true},
						"bmName":      map[string]any{"type": "string"},
						"avatarImage": map[string]any{"type": "string"},
						"firstSeenAt": map[string]any{"type": "string", "format": "date-time", "nullable": true},
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
				"HiddenDiscordLinkIdentity": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"discordId": map[string]any{"type": "string"},
						"reason":    map[string]any{"type": "string"},
						"createdBy": map[string]any{"type": "string"},
						"createdAt": map[string]any{"type": "string", "format": "date-time"},
						"updatedAt": map[string]any{"type": "string", "format": "date-time"},
					},
					"required": []string{"discordId", "reason", "createdBy", "createdAt", "updatedAt"},
				},
				"HideDiscordLinkIdentityRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"reason":    map[string]any{"type": "string"},
						"createdBy": map[string]any{"type": "string"},
					},
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
				"GuildNicknameTarget": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"guildId":   map[string]any{"type": "string"},
						"discordId": map[string]any{"type": "string"},
						"steamId":   map[string]any{"type": "string"},
						"bmId":      map[string]any{"type": "string", "nullable": true},
						"bmName":    map[string]any{"type": "string", "nullable": true},
					},
					"required": []string{"guildId", "discordId", "steamId", "bmId", "bmName"},
				},
				"GuildNicknameTargetList": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/GuildNicknameTarget"},
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
						"discordId":   map[string]any{"type": "string"},
						"guildId":     map[string]any{"type": "string"},
						"isActive":    map[string]any{"type": "boolean"},
						"firstSeenAt": map[string]any{"type": "string", "format": "date-time"},
						"lastSeenAt":  map[string]any{"type": "string", "format": "date-time"},
						"leftAt":      map[string]any{"type": "string", "format": "date-time", "nullable": true},
					},
					"required": []string{"discordId", "guildId", "isActive", "firstSeenAt", "lastSeenAt", "leftAt"},
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
				"AppUser": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":          map[string]any{"type": "string"},
						"displayName": map[string]any{"type": "string"},
						"avatarUrl":   map[string]any{"type": "string"},
						"roles": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"createdAt":   map[string]any{"type": "string", "format": "date-time"},
						"updatedAt":   map[string]any{"type": "string", "format": "date-time"},
						"lastLoginAt": map[string]any{"type": "string", "format": "date-time"},
					},
					"required": []string{"id", "displayName", "avatarUrl", "roles", "createdAt", "updatedAt", "lastLoginAt"},
				},
				"ConnectedAccount": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"provider":          map[string]any{"type": "string"},
						"providerAccountId": map[string]any{"type": "string"},
						"username":          map[string]any{"type": "string"},
						"displayName":       map[string]any{"type": "string"},
						"avatarUrl":         map[string]any{"type": "string"},
						"profileEmail":      map[string]any{"type": "string", "format": "email"},
						"createdAt":         map[string]any{"type": "string", "format": "date-time"},
						"updatedAt":         map[string]any{"type": "string", "format": "date-time"},
					},
					"required": []string{"provider", "providerAccountId", "username", "displayName", "avatarUrl", "profileEmail", "createdAt", "updatedAt"},
				},
				"UserEmail": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"email":      map[string]any{"type": "string", "format": "email"},
						"isPrimary":  map[string]any{"type": "boolean"},
						"isVerified": map[string]any{"type": "boolean"},
						"createdAt":  map[string]any{"type": "string", "format": "date-time"},
						"updatedAt":  map[string]any{"type": "string", "format": "date-time"},
					},
					"required": []string{"email", "isPrimary", "isVerified", "createdAt", "updatedAt"},
				},
				"LinkedSteamAccount": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"steamId":     map[string]any{"type": "string"},
						"bmId":        map[string]any{"type": "string"},
						"bmName":      map[string]any{"type": "string"},
						"avatarImage": map[string]any{"type": "string"},
						"type":        map[string]any{"type": "string", "enum": []string{"soft", "hard"}},
						"source":      map[string]any{"type": "string"},
					},
					"required": []string{"steamId", "bmId", "bmName", "avatarImage", "type", "source"},
				},
				"UserGuildMembership": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"guildId":     map[string]any{"type": "string"},
						"isActive":    map[string]any{"type": "boolean"},
						"firstSeenAt": map[string]any{"type": "string", "format": "date-time"},
						"lastSeenAt":  map[string]any{"type": "string", "format": "date-time"},
						"leftAt":      map[string]any{"type": "string", "format": "date-time", "nullable": true},
					},
					"required": []string{"guildId", "isActive", "firstSeenAt", "lastSeenAt", "leftAt"},
				},
				"CurrentUserResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"user": map[string]any{"$ref": "#/components/schemas/AppUser"},
						"emails": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/UserEmail"},
						},
						"steamAccounts": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/LinkedSteamAccount"},
						},
						"guilds": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/UserGuildMembership"},
						},
						"discordAccount": map[string]any{
							"allOf":    []map[string]any{{"$ref": "#/components/schemas/ConnectedAccount"}},
							"nullable": true,
						},
						"primaryEmail": map[string]any{
							"allOf":    []map[string]any{{"$ref": "#/components/schemas/UserEmail"}},
							"nullable": true,
						},
					},
					"required": []string{"user", "emails", "steamAccounts", "guilds", "discordAccount", "primaryEmail"},
				},
				"PasswordRegisterRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"email":       map[string]any{"type": "string", "format": "email"},
						"password":    map[string]any{"type": "string"},
						"displayName": map[string]any{"type": "string"},
					},
					"required": []string{"email", "password"},
				},
				"PasswordLoginRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"email":    map[string]any{"type": "string", "format": "email"},
						"password": map[string]any{"type": "string"},
					},
					"required": []string{"email", "password"},
				},
				"PasswordResetRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"email": map[string]any{"type": "string", "format": "email"},
					},
					"required": []string{"email"},
				},
				"PasswordResetRequestResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"status":     map[string]any{"type": "string", "enum": []string{"sent", "unlinked"}},
						"linked":     map[string]any{"type": "boolean"},
						"debugToken": map[string]any{"type": "string"},
					},
					"required": []string{"status", "linked"},
				},
				"ConsumePasswordResetRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"token":       map[string]any{"type": "string"},
						"newPassword": map[string]any{"type": "string"},
					},
					"required": []string{"token", "newPassword"},
				},
				"EmailVerificationRequestResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"status":     map[string]any{"type": "string", "enum": []string{"sent"}},
						"debugToken": map[string]any{"type": "string"},
					},
					"required": []string{"status"},
				},
				"ConsumeEmailVerificationRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"token": map[string]any{"type": "string"},
					},
					"required": []string{"token"},
				},
				"CurrentUserConnectionsResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"emails": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/UserEmail"},
						},
						"steamAccounts": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/LinkedSteamAccount"},
						},
						"discordAccount": map[string]any{"$ref": "#/components/schemas/ConnectedAccount"},
					},
					"required": []string{"emails", "steamAccounts"},
				},
				"UserGuildMembershipListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/UserGuildMembership"},
						},
					},
					"required": []string{"items"},
				},
				"AdminUserListItem": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"user":                 map[string]any{"$ref": "#/components/schemas/AppUser"},
						"discordId":            map[string]any{"type": "string"},
						"discordUsername":      map[string]any{"type": "string"},
						"primaryEmail":         map[string]any{"type": "string", "format": "email"},
						"guildCount":           map[string]any{"type": "integer"},
						"steamConnectionCount": map[string]any{"type": "integer"},
					},
					"required": []string{"user", "discordId", "discordUsername", "primaryEmail", "guildCount", "steamConnectionCount"},
				},
				"AdminUserListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/AdminUserListItem"},
						},
					},
					"required": []string{"items"},
				},
				"AdminGuildOverview": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"guildId":        map[string]any{"type": "string"},
						"activeMembers":  map[string]any{"type": "integer"},
						"linkedMembers":  map[string]any{"type": "integer"},
						"trackedServers": map[string]any{"type": "integer"},
						"adminRoleId":    map[string]any{"type": "string"},
					},
					"required": []string{"guildId", "activeMembers", "linkedMembers", "trackedServers", "adminRoleId"},
				},
				"AdminGuildOverviewListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/AdminGuildOverview"},
						},
					},
					"required": []string{"items"},
				},
				"AdminServerOverview": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"serverBmId":    map[string]any{"type": "string"},
						"name":          map[string]any{"type": "string"},
						"lastPolledAt":  map[string]any{"type": "string", "format": "date-time", "nullable": true},
						"lastPollError": map[string]any{"type": "string"},
						"guildCount":    map[string]any{"type": "integer"},
					},
					"required": []string{"serverBmId", "name", "lastPolledAt", "lastPollError", "guildCount"},
				},
				"AdminServerOverviewListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/AdminServerOverview"},
						},
					},
					"required": []string{"items"},
				},
				"AuditEvent": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":           map[string]any{"type": "integer"},
						"actorUserId":  map[string]any{"type": "string"},
						"targetUserId": map[string]any{"type": "string"},
						"eventType":    map[string]any{"type": "string"},
						"payload":      map[string]any{"type": "object", "additionalProperties": true},
						"createdAt":    map[string]any{"type": "string", "format": "date-time"},
					},
					"required": []string{"id", "actorUserId", "targetUserId", "eventType", "payload", "createdAt"},
				},
				"AuditEventListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/AuditEvent"},
						},
					},
					"required": []string{"items"},
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
