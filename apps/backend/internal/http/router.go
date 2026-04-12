package httpapi

import (
	"context"
	"net/http"
	"strings"

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
		r.Get("/guilds/{guildID}/tracked-servers", handler.ListGuildTrackedServers)
		r.Put("/guilds/{guildID}/tracked-servers", handler.ReplaceGuildTrackedServers)
		r.Get("/guilds/{guildID}/wipe-reacts/active", handler.ListActiveWipeReacts)
		r.Get("/guilds/{guildID}/wipe-reacts", handler.ListWipeReacts)
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
		r.Post("/user-guilds", handler.CreateUserGuildLink)
		r.Post("/user-guilds/leave", handler.MarkUserGuildLeft)
		r.Get("/steam/{steamID}", handler.GetSteamAccount)
		r.Get("/steam/{steamID}/links", handler.ListLinksBySteamID)
		r.Delete("/steam/{steamID}/links/{discordID}/soft", handler.DeleteSoftLink)
		r.Get("/discord/{discordID}/links", handler.ListLinksByDiscordID)
		r.Get("/discord/{discordID}/guilds", handler.ListUserGuildLinksByDiscord)

		// Guild-scoped steam-discord links
		r.Get("/guilds/{guildID}/online-summary", handler.ListGuildOnlineSummary)
		r.Get("/guilds/{guildID}/links", handler.ListGuildLinks)
		r.Get("/guilds/{guildID}/nickname-targets", handler.ListGuildNicknameTargets)
		r.Post("/guilds/{guildID}/links", handler.CreateGuildSteamDiscordLink)
		r.Get("/guilds/{guildID}/discord/{discordID}/links", handler.ListGuildLinksByDiscord)
		r.Get("/guilds/{guildID}/steam/{steamID}/links", handler.ListGuildLinksBySteam)
		r.Delete("/guilds/{guildID}/discord/{discordID}/steam/{steamID}", handler.DeleteGuildSteamDiscordLink)
	})

	r.Route("/api/app", func(r chi.Router) {
		r.Use(appInternalMiddleware(cfg.AppAPIInternalSecret))
		r.Post("/auth/discord/complete", handler.CompleteDiscordAuth)
		r.Get("/auth/email/status", handler.GetEmailAuthStatus)
		r.Post("/auth/register", handler.RegisterPasswordUser)
		r.Post("/auth/login", handler.LoginPasswordUser)
		r.Post("/auth/password/reset/request", handler.RequestPasswordReset)
		r.Post("/auth/password/reset/consume", handler.ConsumePasswordReset)
		r.Post("/auth/email/verify/request", handler.RequestEmailVerification)
		r.Post("/auth/email/verify/consume", handler.ConsumeEmailVerification)
		r.Get("/me", handler.GetCurrentAppUser)
		r.Get("/me/connections", handler.GetCurrentAppUserConnections)
		r.Get("/me/guilds", handler.GetCurrentAppUserGuilds)
		r.Get("/admin/users", handler.ListAppAdminUsers)
		r.Get("/admin/guilds", handler.ListAppAdminGuilds)
		r.Get("/admin/servers", handler.ListAppAdminServers)
		r.Get("/admin/audit", handler.ListAppAuditEvents)
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

func appInternalMiddleware(expectedSecret string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.TrimSpace(r.Header.Get("X-RustLens-App-Secret")) != expectedSecret {
				writeError(w, http.StatusUnauthorized, "invalid app secret")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
