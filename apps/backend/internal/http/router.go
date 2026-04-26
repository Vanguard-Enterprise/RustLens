package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/auth"
	"rustlens/backend/internal/battlemetrics"
	"rustlens/backend/internal/config"
	"rustlens/backend/internal/oauth/discord"
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

func NewRouter(
	cfg config.Config,
	postgres *store.PostgresStore,
	redis *store.RedisStore,
	shardService *shards.Service,
	steamClient *steamapi.Client,
	steamSummaryClient *steamapi.Client,
	battleMetricsClient *battlemetrics.Client,
	authBattleMetricsClient *battlemetrics.Client,
) http.Handler {
	handler := NewHandler(cfg, postgres, redis, shardService, steamClient, steamSummaryClient, battleMetricsClient, authBattleMetricsClient)

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

	r.Route("/api/internal/bot/owner", func(r chi.Router) {
		r.Use(botJWTMiddleware(handler.jwtValidator))
		r.Get("/stats", handler.GetOwnerStats)
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
		r.Post("/guilds/{guildID}/wipe-react-schedule-posts/claim", handler.ClaimNextWipeReactSchedulePost)
		r.Post("/guilds/{guildID}/wipe-react-schedule-posts/{scheduleID}/complete", handler.CompleteWipeReactSchedulePost)
		r.Post("/guilds/{guildID}/wipe-react-schedule-posts/{scheduleID}/release", handler.ReleaseWipeReactSchedulePost)
		r.Get("/tracked-servers", handler.ListTrackedServers)
		r.Post("/tracked-servers", handler.CreateTrackedServer)
		r.Put("/tracked-servers/{bmID}", handler.UpdateTrackedServer)
		r.Delete("/tracked-servers/{bmID}", handler.DeleteTrackedServer)
		r.Get("/player-states/servers/{serverID}", handler.ListOnlinePlayerStatesByServer)
		r.Get("/player-states/servers/{serverID}/search", handler.SearchPlayerStatesOnServer)
		r.Get("/player-states/servers/{serverID}/players/{playerID}", handler.GetPlayerState)
		r.Get("/player-states/players/{playerID}", handler.ListPlayerStatesByPlayer)

		// Steam & Discord linking endpoints
		r.Get("/steam/by-bm/{bmID}", handler.GetSteamAccountByBmID)
		r.Get("/steam/{steamID}/profile", handler.GetSteamProfile)
		r.Get("/steam/{steamID}/analytics", handler.GetSteamAnalytics)
		r.Post("/steam/resolve", handler.ResolveSteamID)
		r.Post("/steam/links", handler.CreateSoftLink)
		r.Post("/steam/bulk-hard-links", handler.BulkUpsertHardLinks)
		r.Get("/steam-accounts/count", handler.GetSteamAccountCount)
		r.Post("/user-guilds", handler.CreateUserGuildLink)
		r.Post("/user-guilds/leave", handler.MarkUserGuildLeft)
		r.Get("/steam/{steamID}", handler.GetSteamAccount)
		r.Get("/steam/{steamID}/links", handler.ListLinksBySteamID)
		r.Delete("/steam/{steamID}/links/{discordID}/soft", handler.DeleteSoftLink)
		r.Get("/discord/{discordID}/links", handler.ListLinksByDiscordID)
		r.Get("/discord/{discordID}/guilds", handler.ListUserGuildLinksByDiscord)
		r.Get("/discord/{discordID}/is-website-admin", handler.IsDiscordWebsiteAdmin)

		// User ban registry (owner-only /ban command)
		r.Get("/bans/{discordID}", handler.GetUserBan)
		r.Put("/bans/{discordID}", handler.UpdateUserBan)

		// Guild-scoped steam-discord links
		r.Get("/guilds/{guildID}/online-summary", handler.ListGuildOnlineSummary)
		r.Get("/guilds/{guildID}/game-bans", handler.CheckGuildGameBans)
		r.Get("/guilds/{guildID}/tracked-server-log-events", handler.ListGuildTrackedServerLogEvents)
		r.Get("/guilds/{guildID}/links", handler.ListGuildLinks)
		r.Get("/guilds/{guildID}/nickname-targets", handler.ListGuildNicknameTargets)
		r.Post("/guilds/{guildID}/links", handler.CreateGuildSteamDiscordLink)
		r.Get("/guilds/{guildID}/discord/{discordID}/links", handler.ListGuildLinksByDiscord)
		r.Get("/guilds/{guildID}/steam/{steamID}/links", handler.ListGuildLinksBySteam)
		r.Delete("/guilds/{guildID}/discord/{discordID}/steam/{steamID}", handler.DeleteGuildSteamDiscordLink)

		// Track groups (admin /track command)
		r.Get("/guilds/{guildID}/track-groups", handler.ListTrackGroups)
		r.Post("/guilds/{guildID}/track-groups", handler.CreateTrackGroup)
		r.Get("/guilds/{guildID}/track-groups/{groupID}", handler.GetTrackGroup)
		r.Put("/guilds/{guildID}/track-groups/{groupID}", handler.UpdateTrackGroup)
		r.Delete("/guilds/{guildID}/track-groups/{groupID}", handler.DeleteTrackGroup)
		r.Get("/guilds/{guildID}/track-groups/{groupID}/members", handler.ListTrackGroupMembers)
		r.Post("/guilds/{guildID}/track-groups/{groupID}/members", handler.AddTrackGroupMembers)
		r.Post("/guilds/{guildID}/track-groups/{groupID}/members/remove", handler.RemoveTrackGroupMembers)
		r.Get("/guilds/{guildID}/track-groups/{groupID}/online-state", handler.ListTrackGroupOnlineState)
	})

	appAuthRepo := appauth.NewRepo(postgres.Pool())
	appAuthHandler := appauth.NewHandler(appAuthRepo)
	requireAppSecret := appauth.RequireAppSecret(cfg.AppAPIInternalSecret)
	requireSession := appauth.RequireSession(cfg.AppSessionSecret, postgres.Pool())

	discordHandler := discord.NewHandler(discord.HandlerConfig{
		ClientID:      cfg.DiscordClientID,
		ClientSecret:  cfg.DiscordClientSecret,
		AppOrigin:     cfg.AppOrigin,
		SessionSecret: cfg.AppSessionSecret,
		Repo:          appAuthRepo,
		Pool:          postgres.Pool(),
	})

	r.Route("/api/auth/discord", func(r chi.Router) {
		r.Get("/", discordHandler.Initiate)
		r.Get("/callback", discordHandler.Callback)
	})

	r.Route("/api/app", func(r chi.Router) {
		r.Use(requireAppSecret)

		r.Post("/auth/logout", appAuthHandler.Logout)

		r.Group(func(r chi.Router) {
			r.Use(requireSession)
			r.Get("/me", appAuthHandler.Me)
		})

		r.Route("/admin", func(r chi.Router) {
			r.Use(requireSession)
			r.Post("/actions/{type}", handler.actionDispatcher.ServeHTTP)
			r.Get("/roles", handler.ListAdminRoles)
			r.Get("/users", handler.ListAdminUsers)
			r.Get("/users/export.csv", handler.ExportAdminUsersCSV)
			r.Get("/users/{id}", handler.GetAdminUser)
			r.Get("/users/{id}/audit", handler.ListAdminUserAudit)
			r.Get("/guilds", handler.ListAdminGuilds)
			r.Get("/servers", handler.ListAdminServers)
			r.Get("/audit", handler.ListAdminAudit)
		})
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
