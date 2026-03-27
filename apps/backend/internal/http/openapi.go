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
			"/api/internal/bot/setup/guilds/{guildID}": map[string]any{
				"get": map[string]any{
					"summary":     "Get guild setup state",
					"description": "Returns the persisted setup state for a guild.",
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
							"description": "Setup state returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/GuildSetupState"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/claim": map[string]any{
				"post": map[string]any{
					"summary":     "Claim guild setup",
					"description": "Attempts to claim setup for a guild before role creation begins.",
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
							"description": "Claim result returned",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/ClaimGuildSetupResponse"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/complete": map[string]any{
				"post": map[string]any{
					"summary":     "Complete guild setup",
					"description": "Marks a guild setup as configured and stores selected role keys.",
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
								"schema": map[string]any{"$ref": "#/components/schemas/CompleteGuildSetupRequest"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Setup marked configured",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/GuildSetupState"},
								},
							},
						},
						"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
						"401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
					},
				},
			},
			"/api/internal/bot/setup/guilds/{guildID}/reset": map[string]any{
				"post": map[string]any{
					"summary":     "Reset guild setup",
					"description": "Clears the persisted guild setup state.",
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
							"description": "Setup reset",
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
				"GuildSetupState": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"guildId": map[string]any{"type": "string"},
						"status":  map[string]any{"type": "string"},
						"configuredBy": map[string]any{
							"type": "string",
						},
						"selectedRoles": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required": []string{"guildId", "status", "configuredBy", "selectedRoles"},
				},
				"ClaimGuildSetupResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"claimed":      map[string]any{"type": "boolean"},
						"guildId":      map[string]any{"type": "string"},
						"status":       map[string]any{"type": "string"},
						"configuredBy": map[string]any{"type": "string"},
						"selectedRoles": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required": []string{"claimed", "guildId", "status", "configuredBy", "selectedRoles"},
				},
				"CompleteGuildSetupRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"configuredBy": map[string]any{"type": "string"},
						"selectedRoles": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required": []string{"configuredBy", "selectedRoles"},
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
