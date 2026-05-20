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
				"patch": map[string]any{
					"summary":     "Update outbound proxy",
					"description": "Enables or disables an owner-managed outbound proxy and reloads the runtime proxy provider.",
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
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/UpdateOutboundProxyRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Proxy updated",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/OutboundProxy"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"404": map[string]any{"$ref": "#/components/responses/NotFound"},
					},
				},
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
			"/api/internal/bot/setup/tracked-servers/{bmID}/player-count-history": map[string]any{
				"get": map[string]any{
					"summary":     "Get tracked server player count analytics",
					"description": "Returns chart-ready BattleMetrics player-count history for a tracked server. The selected timeframe covers completed UTC days ending yesterday; weekday distribution is computed from cached history refreshed daily at 00:01 UTC.",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "bmID",
							"in":          "path",
							"required":    true,
							"description": "BattleMetrics server ID.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "timeframe",
							"in":          "query",
							"required":    true,
							"description": "Timeframe for the top time-series chart.",
							"schema":      map[string]any{"type": "string", "enum": []string{"7d", "14d", "30d", "60d", "90d"}},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Server analytics returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/ServerAnalyticsResponse"},
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
			"/api/internal/bot/setup/discord/{discordID}/setsteam-candidates": map[string]any{
				"get": map[string]any{
					"summary":     "List /setsteam picker candidates",
					"description": "Returns Steam accounts to surface in the /setsteam modal: all hard links, plus the most recent five soft links from the last 90 days. Each Steam ID appears at most once (hard supersedes soft). The total result is capped at 25 (Discord's StringSelectMenu limit), preferring hard then most-recent soft. Hidden discord identities return an empty list.",
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
							"description": "Candidates returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/SetSteamCandidateList"},
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
			"/api/internal/bot/setup/discord/{discordID}/has-pro-role": map[string]any{
				"get": map[string]any{
					"summary":     "Check Discord user's PRO role",
					"description": "Returns whether the Discord user is linked to an app user with the seeded PRO admin role.",
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
							"description": "PRO role status returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type":     "object",
										"required": []string{"hasRole"},
										"properties": map[string]any{
											"hasRole": map[string]any{"type": "boolean"},
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
					"description": "Refreshes stale Steam game-ban cache entries using a 6 hour TTL and returns only game bans from the last 90 days. method=light checks visible guild-linked Steam IDs. method=deep expands to visible global hard links for guild-linked Discord users after optionally refreshing up to 100 eligible Discord IDs through platformsync.",
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
							"name":        "method",
							"in":          "query",
							"required":    false,
							"description": "Scan method. Defaults to light.",
							"schema":      map[string]any{"type": "string", "enum": []string{"light", "deep"}},
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
			"/api/app/me/preferences": map[string]any{
				"get": map[string]any{
					"summary":     "Get the current user's notification preferences",
					"description": "Returns the signed-in user's notification preferences. Lazily creates a default row (both flags true) if none exists.",
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Preferences returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/NotificationPreferences"},
								},
							},
						},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
				"patch": map[string]any{
					"summary":     "Update the current user's notification preferences",
					"description": "Applies a partial update. Absent fields are unchanged. Unknown fields and non-boolean values return 400.",
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/NotificationPreferencesPatch"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Updated preferences returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/NotificationPreferences"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
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
					"description": "Returns paginated bot_guilds rows with name/icon/owner metadata, plus join-derived isConfigured (guild_settings) and trackedServerCount (guild_tracked_servers). Supports free-text search across name, guild_id prefix, and owner username.",
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "q",
							"in":          "query",
							"required":    false,
							"description": "Substring/prefix search across bot_guilds.name, bot_guilds.guild_id (prefix), and bot_guilds.owner_username.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "page",
							"in":          "query",
							"required":    false,
							"description": "1-based page number. Default 1.",
							"schema":      map[string]any{"type": "integer", "minimum": 1},
						},
						{
							"name":        "pageSize",
							"in":          "query",
							"required":    false,
							"description": "Page size, default 20, capped at 100.",
							"schema":      map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Admin guilds returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/AdminGuildListResponse"},
								},
							},
						},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"403": map[string]any{"$ref": "#/components/responses/Forbidden"},
					},
				},
			},
			"/api/app/admin/user-installs": map[string]any{
				"get": map[string]any{
					"summary":     "List admin user-app installs",
					"description": "Returns paginated bot_user_installs rows — Discord users who installed RustLens as a user app (integration_type=1). Supports free-text search across username/global_name (substring) and discord_id (prefix).",
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "q",
							"in":          "query",
							"required":    false,
							"description": "Substring search across username and global_name; prefix match on discord_id.",
							"schema":      map[string]any{"type": "string"},
						},
						{
							"name":        "page",
							"in":          "query",
							"required":    false,
							"description": "1-based page number. Default 1.",
							"schema":      map[string]any{"type": "integer", "minimum": 1},
						},
						{
							"name":        "pageSize",
							"in":          "query",
							"required":    false,
							"description": "Page size, default 20, capped at 100.",
							"schema":      map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Admin user-app installs returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/AdminUserInstallListResponse"},
								},
							},
						},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"403": map[string]any{"$ref": "#/components/responses/Forbidden"},
					},
				},
			},
			"/api/discord/webhook-events": map[string]any{
				"post": map[string]any{
					"summary":     "Discord Webhook Events sink",
					"description": "Public endpoint registered in the Discord Developer Portal under 'Webhook Events'. Authenticated via ed25519 signature over (X-Signature-Timestamp || rawBody) using the application's public key. Handles the PING handshake (type=0) and event payloads (type=1) for APPLICATION_AUTHORIZED (integration_type=1 user installs only) and APPLICATION_DEAUTHORIZED. Idempotent: returns 204 on success.",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/DiscordWebhookPayload"},
							},
						},
					},
					"parameters": []map[string]any{
						{
							"name":     "X-Signature-Ed25519",
							"in":       "header",
							"required": true,
							"schema":   map[string]any{"type": "string"},
						},
						{
							"name":     "X-Signature-Timestamp",
							"in":       "header",
							"required": true,
							"schema":   map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"204": map[string]any{"description": "Event accepted (PING or processed event)"},
						"400": map[string]any{"description": "Malformed body"},
						"401": map[string]any{"description": "Missing or invalid ed25519 signature"},
						"503": map[string]any{"description": "DISCORD_APP_PUBLIC_KEY not configured"},
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
			"/api/app/admin/proxies": map[string]any{
				"get": map[string]any{
					"summary":     "List outbound proxies (admin web)",
					"description": "Returns owner-managed outbound proxies with credentials redacted. Requires proxies.read.",
					"tags":        []string{"admin"},
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
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
						"403": map[string]any{"$ref": "#/components/responses/Forbidden"},
					},
				},
				"post": map[string]any{
					"summary":     "Create outbound proxy (admin web)",
					"description": "Stores a full proxy URL encrypted at rest and returns only a credential-free endpoint. Requires proxies.edit.",
					"tags":        []string{"admin"},
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
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
						"403": map[string]any{"$ref": "#/components/responses/Forbidden"},
						"409": map[string]any{"$ref": "#/components/responses/Conflict"},
					},
				},
			},
			"/api/app/admin/proxies/bulk": map[string]any{
				"post": map[string]any{
					"summary":     "Bulk update outbound proxies (admin web)",
					"description": "Applies enable/disable/delete to up to 200 proxy IDs. Requires proxies.edit.",
					"tags":        []string{"admin"},
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Bulk applied"},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"403": map[string]any{"$ref": "#/components/responses/Forbidden"},
					},
				},
			},
			"/api/app/admin/proxies/{proxyID}": map[string]any{
				"patch": map[string]any{
					"summary":     "Update outbound proxy (admin web)",
					"description": "Enables or disables a proxy and reloads the runtime provider. Requires proxies.edit.",
					"tags":        []string{"admin"},
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
					"parameters": []map[string]any{
						{
							"name":        "proxyID",
							"in":          "path",
							"required":    true,
							"description": "Outbound proxy ID.",
							"schema":      map[string]any{"type": "integer"},
						},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/UpdateOutboundProxyRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Proxy updated",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/OutboundProxy"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
						"403": map[string]any{"$ref": "#/components/responses/Forbidden"},
						"404": map[string]any{"$ref": "#/components/responses/NotFound"},
					},
				},
				"delete": map[string]any{
					"summary":     "Delete outbound proxy (admin web)",
					"description": "Deletes a proxy and reloads the runtime provider. Requires proxies.edit.",
					"tags":        []string{"admin"},
					"security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
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
						"403": map[string]any{"$ref": "#/components/responses/Forbidden"},
						"404": map[string]any{"$ref": "#/components/responses/NotFound"},
					},
				},
			},
			"/api/app/admin/permissions": map[string]any{
				"get": map[string]any{
					"summary":     "List the canonical admin permission catalog",
					"description": "Returns every permission key registered in rbac.AllPermissions(), with its human-readable description. Static catalog; no DB read.",
					"tags":        []string{"admin"},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Permission catalog",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"items": map[string]any{
												"type": "array",
												"items": map[string]any{
													"type":     "object",
													"required": []string{"key", "description"},
													"properties": map[string]any{
														"key":         map[string]any{"type": "string"},
														"description": map[string]any{"type": "string"},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			"/api/app/admin/roles": map[string]any{
				"get": map[string]any{
					"summary": "List admin roles with permission and member counts",
					"tags":    []string{"admin"},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Roles",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"items": map[string]any{
												"type": "array",
												"items": map[string]any{
													"type":     "object",
													"required": []string{"id", "key", "name", "isSystem", "permissions", "memberCount"},
													"properties": map[string]any{
														"id":          map[string]any{"type": "string"},
														"key":         map[string]any{"type": "string"},
														"name":        map[string]any{"type": "string"},
														"description": map[string]any{"type": "string"},
														"isSystem":    map[string]any{"type": "boolean"},
														"permissions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
														"memberCount": map[string]any{"type": "integer"},
													},
												},
											},
										},
									},
								},
							},
						},
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
				"UpdateOutboundProxyRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"enabled": map[string]any{"type": "boolean"},
					},
					"required": []string{"enabled"},
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
						"alertRoleId":               map[string]any{"type": "string"},
					},
					"required": []string{
						"guildId",
						"wipeReactLogChannelId",
						"linkingLogChannelId",
						"trackedServerLogChannelId",
						"trackedUsersCategoryId",
						"adminRoleId",
						"alertRoleId",
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
						"alertRoleId":               map[string]any{"type": "string"},
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
						"highFrequency": map[string]any{"type": "boolean"},
						"lastPolledAt":  map[string]any{"type": "string", "format": "date-time", "nullable": true},
						"lastPollError": map[string]any{"type": "string"},
					},
					"required": []string{"bmId", "name", "highFrequency", "lastPolledAt", "lastPollError"},
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
				"ServerAnalyticsPoint": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"timestampUnix": map[string]any{"type": "integer"},
						"timestamp":     map[string]any{"type": "string", "format": "date-time"},
						"min":           map[string]any{"type": "integer"},
						"value":         map[string]any{"type": "number"},
						"max":           map[string]any{"type": "integer"},
					},
					"required": []string{"timestampUnix", "timestamp", "min", "value", "max"},
				},
				"ServerAnalyticsWeekday": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"weekday": map[string]any{"type": "string"},
						"min":     map[string]any{"type": "number"},
						"value":   map[string]any{"type": "number"},
						"max":     map[string]any{"type": "number"},
					},
					"required": []string{"weekday", "min", "value", "max"},
				},
				"ServerAnalyticsResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"serverBmId":  map[string]any{"type": "string"},
						"serverName":  map[string]any{"type": "string"},
						"timeframe":   map[string]any{"type": "string"},
						"start":       map[string]any{"type": "string", "format": "date-time"},
						"stop":        map[string]any{"type": "string", "format": "date-time"},
						"cachedAt":    map[string]any{"type": "string", "format": "date-time"},
						"cacheWindow": map[string]any{"type": "string"},
						"points": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/ServerAnalyticsPoint"},
						},
						"weekdays": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/ServerAnalyticsWeekday"},
						},
					},
					"required": []string{"serverBmId", "serverName", "timeframe", "start", "stop", "cachedAt", "cacheWindow", "points", "weekdays"},
				},
				"CreateTrackedServerRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"bmId":          map[string]any{"type": "string"},
						"name":          map[string]any{"type": "string"},
						"highFrequency": map[string]any{"type": "boolean"},
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
				"SetSteamCandidate": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"steamId":     map[string]any{"type": "string"},
						"type":        map[string]any{"type": "string", "enum": []string{"soft", "hard"}},
						"displayName": map[string]any{"type": "string"},
						"createdAt":   map[string]any{"type": "string", "format": "date-time"},
					},
					"required": []string{"steamId", "type", "displayName", "createdAt"},
				},
				"SetSteamCandidateList": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/SetSteamCandidate"},
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
					},
					"required": []string{"user", "steamAccounts", "guilds", "discordAccount"},
				},
				"CurrentUserConnectionsResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"steamAccounts": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/LinkedSteamAccount"},
						},
						"discordAccount": map[string]any{"$ref": "#/components/schemas/ConnectedAccount"},
					},
					"required": []string{"steamAccounts"},
				},
				"NotificationPreferences": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"notifyInApp":                map[string]any{"type": "boolean"},
						"notifyDiscordAnnouncements": map[string]any{"type": "boolean"},
					},
					"required": []string{"notifyInApp", "notifyDiscordAnnouncements"},
				},
				"NotificationPreferencesPatch": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"notifyInApp":                map[string]any{"type": "boolean"},
						"notifyDiscordAnnouncements": map[string]any{"type": "boolean"},
					},
					"additionalProperties": false,
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
						"guildCount":           map[string]any{"type": "integer"},
						"steamConnectionCount": map[string]any{"type": "integer"},
					},
					"required": []string{"user", "discordId", "discordUsername", "guildCount", "steamConnectionCount"},
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
				"AdminGuildItem": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"guildId":                map[string]any{"type": "string"},
						"name":                   map[string]any{"type": "string"},
						"iconHash":               map[string]any{"type": "string", "nullable": true},
						"ownerDiscordId":         map[string]any{"type": "string", "nullable": true},
						"ownerUsername":          map[string]any{"type": "string", "nullable": true},
						"ownerGlobalName":        map[string]any{"type": "string", "nullable": true},
						"ownerAvatarHash":        map[string]any{"type": "string", "nullable": true},
						"approximateMemberCount": map[string]any{"type": "integer", "nullable": true},
						"isInstalled":            map[string]any{"type": "boolean"},
						"leftAt":                 map[string]any{"type": "string", "format": "date-time", "nullable": true},
						"isConfigured":           map[string]any{"type": "boolean"},
						"trackedServerCount":     map[string]any{"type": "integer"},
					},
					"required": []string{
						"guildId", "name", "iconHash",
						"ownerDiscordId", "ownerUsername", "ownerGlobalName", "ownerAvatarHash",
						"approximateMemberCount", "isInstalled", "leftAt",
						"isConfigured", "trackedServerCount",
					},
				},
				"AdminGuildListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/AdminGuildItem"},
						},
						"total":    map[string]any{"type": "integer"},
						"page":     map[string]any{"type": "integer"},
						"pageSize": map[string]any{"type": "integer"},
					},
					"required": []string{"items", "total", "page", "pageSize"},
				},
				"AdminUserInstallItem": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"discordId":   map[string]any{"type": "string"},
						"username":    map[string]any{"type": "string"},
						"globalName":  map[string]any{"type": "string", "nullable": true},
						"avatarHash":  map[string]any{"type": "string", "nullable": true},
						"scopes":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"isInstalled": map[string]any{"type": "boolean"},
						"installedAt": map[string]any{"type": "string", "format": "date-time"},
						"leftAt":      map[string]any{"type": "string", "format": "date-time", "nullable": true},
						"lastSeenAt":  map[string]any{"type": "string", "format": "date-time", "nullable": true},
					},
					"required": []string{
						"discordId", "username", "globalName", "avatarHash", "scopes",
						"isInstalled", "installedAt", "leftAt", "lastSeenAt",
					},
				},
				"AdminUserInstallListResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/AdminUserInstallItem"},
						},
						"total":    map[string]any{"type": "integer"},
						"page":     map[string]any{"type": "integer"},
						"pageSize": map[string]any{"type": "integer"},
					},
					"required": []string{"items", "total", "page", "pageSize"},
				},
				"DiscordWebhookPayload": map[string]any{
					"type":        "object",
					"description": "Envelope for Discord Webhook Events. type=0 is the PING handshake; type=1 carries an event object.",
					"properties": map[string]any{
						"version":        map[string]any{"type": "integer"},
						"application_id": map[string]any{"type": "string"},
						"type":           map[string]any{"type": "integer", "enum": []int{0, 1}},
						"event": map[string]any{
							"type":     "object",
							"nullable": true,
							"properties": map[string]any{
								"type":      map[string]any{"type": "string", "enum": []string{"APPLICATION_AUTHORIZED", "APPLICATION_DEAUTHORIZED"}},
								"timestamp": map[string]any{"type": "string", "format": "date-time"},
								"data":      map[string]any{"type": "object"},
							},
						},
					},
					"required": []string{"version", "type"},
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
