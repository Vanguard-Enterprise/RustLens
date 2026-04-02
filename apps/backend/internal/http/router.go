package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"rustlens/backend/internal/auth"
	"rustlens/backend/internal/config"
	"rustlens/backend/internal/shards"
	"rustlens/backend/internal/steamapi"
	"rustlens/backend/internal/store"
)

type contextKey string

const claimsContextKey = contextKey("claims")

func claimsFromContext(ctx context.Context) *auth.Claims {
	claims, ok := ctx.Value(claimsContextKey).(*auth.Claims)
	if !ok {
		return nil
	}
	return claims
}

func NewRouter(cfg config.Config, postgres *store.PostgresStore, redis *store.RedisStore, shardService *shards.Service, steamClient *steamapi.Client) http.Handler {
	handler := NewHandler(cfg, postgres, redis, shardService, steamClient)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/api/docs", handler.DocsUI)
	r.Get("/api/docs/", handler.DocsUI)
	r.Get("/api/docs/openapi.json", handler.OpenAPISpec)
	r.Get("/api/healthz", handler.Healthz)

	r.Route("/api/internal/bot/shards", func(r chi.Router) {
		r.Use(botJWTMiddleware(handler.jwtValidator))
		r.Post("/register", handler.RegisterShardWorker)
		r.Post("/heartbeat", handler.HeartbeatShardWorker)
		r.Post("/release", handler.ReleaseShardWorker)
		r.Get("/assignments", handler.GetShardAssignments)
	})

	r.Route("/api/internal/bot/setup", func(r chi.Router) {
		r.Use(botJWTMiddleware(handler.jwtValidator))
		r.Get("/guilds/{guildID}/settings", handler.GetGuildSettings)
		r.Put("/guilds/{guildID}/settings", handler.UpdateGuildSettings)
		r.Get("/guilds/{guildID}/wipe-reacts/active", handler.ListActiveWipeReacts)
		r.Post("/guilds/{guildID}/wipe-reacts", handler.CreateWipeReact)
		r.Get("/guilds/{guildID}/wipe-reacts/{messageID}", handler.GetWipeReact)
		r.Put("/guilds/{guildID}/wipe-reacts/{messageID}", handler.UpdateWipeReact)
		r.Delete("/guilds/{guildID}/wipe-reacts/{messageID}", handler.DeleteWipeReact)
		r.Post("/guilds/{guildID}/wipe-reacts/{messageID}/reactions", handler.SetWipeReactReaction)
		r.Delete("/guilds/{guildID}/wipe-reacts/{messageID}/reactions/{userID}", handler.RemoveWipeReactReaction)
		r.Get("/guilds/{guildID}/wipe-react-schedules", handler.ListWipeReactSchedules)
		r.Post("/guilds/{guildID}/wipe-react-schedules", handler.CreateWipeReactSchedule)
		r.Get("/guilds/{guildID}/wipe-react-schedules/{scheduleID}", handler.GetWipeReactSchedule)
		r.Put("/guilds/{guildID}/wipe-react-schedules/{scheduleID}", handler.UpdateWipeReactSchedule)
		r.Delete("/guilds/{guildID}/wipe-react-schedules/{scheduleID}", handler.DeleteWipeReactSchedule)
		r.Post("/guilds/{guildID}/wipe-react-schedules/{scheduleID}/mark-posted", handler.MarkWipeReactSchedulePosted)
		r.Get("/tracked-servers", handler.ListTrackedServers)
		r.Post("/tracked-servers", handler.CreateTrackedServer)
		r.Delete("/tracked-servers/{bmID}", handler.DeleteTrackedServer)
		r.Get("/player-states/servers/{serverID}", handler.ListOnlinePlayerStatesByServer)
		r.Get("/player-states/servers/{serverID}/players/{playerID}", handler.GetPlayerState)
		r.Get("/player-states/players/{playerID}", handler.ListPlayerStatesByPlayer)

		// Steam & Discord linking endpoints
		r.Get("/steam/by-bm/{bmID}", handler.GetSteamAccountByBmID)
		r.Post("/steam/resolve", handler.ResolveSteamID)
		r.Post("/steam/links", handler.CreateSoftLink)
		r.Post("/steam/bulk-hard-links", handler.BulkUpsertHardLinks)
		r.Get("/steam/{steamID}", handler.GetSteamAccount)
		r.Get("/steam/{steamID}/links", handler.ListLinksBySteamID)
		r.Delete("/steam/{steamID}/links/{discordID}/soft", handler.DeleteSoftLink)
		r.Get("/discord/{discordID}/links", handler.ListLinksByDiscordID)

		// Guild-scoped steam-discord links
		r.Get("/guilds/{guildID}/online-summary", handler.ListGuildOnlineSummary)
		r.Get("/guilds/{guildID}/links", handler.ListGuildLinks)
		r.Post("/guilds/{guildID}/links", handler.CreateGuildSteamDiscordLink)
		r.Get("/guilds/{guildID}/discord/{discordID}/links", handler.ListGuildLinksByDiscord)
		r.Get("/guilds/{guildID}/steam/{steamID}/links", handler.ListGuildLinksBySteam)
		r.Delete("/guilds/{guildID}/discord/{discordID}/steam/{steamID}", handler.DeleteGuildSteamDiscordLink)
	})

	return r
}

func botJWTMiddleware(validator *auth.Validator) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawToken, err := auth.ExtractBearerToken(r.Header.Get("Authorization"))
			if err != nil {
				writeError(w, http.StatusUnauthorized, err.Error())
				return
			}

			claims, err := validator.Parse(rawToken)
			if err != nil {
				writeError(w, http.StatusUnauthorized, err.Error())
				return
			}

			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
