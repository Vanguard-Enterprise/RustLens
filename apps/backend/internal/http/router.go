package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/auth"
	"rustlens/backend/internal/battlemetrics"
	"rustlens/backend/internal/bmmatch"
	"rustlens/backend/internal/clientip"
	"rustlens/backend/internal/config"
	"rustlens/backend/internal/events"
	"rustlens/backend/internal/oauth/discord"
	"rustlens/backend/internal/outboundhttp"
	"rustlens/backend/internal/platformsync"
	"rustlens/backend/internal/proxycrypto"
	"rustlens/backend/internal/shards"
	"rustlens/backend/internal/steamapi"
	"rustlens/backend/internal/store"
)

type contextKey string

const claimsContextKey = contextKey("claims")
const appSessionContextKey = contextKey("app-session")

func claimsFromContext(ctx context.Context) *auth.Claims {
	claims, ok := ctx.Value(claimsContextKey).(*auth.Claims)
	if !ok {
		return nil
	}
	return claims
}

func appSessionFromContext(ctx context.Context) *auth.AppSessionClaims {
	claims, ok := ctx.Value(appSessionContextKey).(*auth.AppSessionClaims)
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
	bmMatch *bmmatch.Client,
	platformSync *platformsync.Client,
	outboundProvider *outboundhttp.Provider,
	proxyCrypto *proxycrypto.Crypto,
	eventsHub *events.Hub,
	publisher events.Publisher,
) http.Handler {
	if publisher == nil {
		publisher = events.NopPublisher{}
	}
	appAuthRepo := appauth.NewRepo(postgres.Pool())
	handler := NewHandler(cfg, postgres, redis, shardService, steamClient, steamSummaryClient, battleMetricsClient, bmMatch, platformSync, outboundProvider, proxyCrypto, publisher, appAuthRepo)

	apiKeyStore := store.NewAPIKeyStore(postgres.Pool())
	headers := clientip.Headers{
		Trust:         cfg.TrustProxyHeaders,
		IPHeader:      cfg.TrustProxyIPHeader,
		CountryHeader: cfg.TrustProxyGeoHeader,
	}
	apiKeyAuthMW := appauth.NewAPIKeyAuth(apiKeyStore, &apiKeyUserLoader{pool: postgres.Pool()}, headers)

	// bridgeAPIKeyPerms copies the key's snapshot permissions from the
	// actions context onto the rbac resolver context so HasPermission /
	// PermissionsForUser intersect correctly.
	bridgeAPIKeyPerms := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if k := actions.APIKeyAuthFromContext(r.Context()); k != nil {
				ctx := rbac.WithAPIKeyPermissions(r.Context(), k.KeyPermissions)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}

	// requireAdminPerm gates a route on a single permission. Works for both
	// API-key and session callers — it resolves the actor's user ID from
	// whichever auth context is present and intersects against the rbac
	// resolver (which itself respects WithAPIKeyPermissions).
	requireAdminPerm := func(perm string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var userID string
				if k := actions.APIKeyAuthFromContext(r.Context()); k != nil {
					userID = k.UserID
				} else if s := appauth.SessionFromContext(r.Context()); s != nil {
					userID = s.UserID
				}
				if userID == "" {
					writeError(w, http.StatusUnauthorized, "unauthenticated")
					return
				}
				if handler.rbacResolver == nil {
					writeError(w, http.StatusInternalServerError, "rbac unavailable")
					return
				}
				ok, err := handler.rbacResolver.HasPermission(r.Context(), userID, perm)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "permission_check_failed")
					return
				}
				if !ok {
					writeError(w, http.StatusForbidden, "forbidden")
					return
				}
				next.ServeHTTP(w, r)
			})
		}
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	// Run API-key auth globally so it fires before subrouter-level guards
	// (including requireAppSecret on /api/app). Non-API-key requests are
	// a no-op pass-through.
	r.Use(apiKeyAuthMW)

	r.Get("/api/docs", handler.DocsUI)
	r.Get("/api/docs/", handler.DocsUI)
	r.Get("/api/docs/openapi.json", handler.OpenAPISpec)
	r.Get("/api/healthz", handler.Healthz)

	// Discord Webhook Events (Dev Portal). Signature-authed inside the handler.
	r.Post("/api/discord/webhook-events", handler.DiscordWebhookEvents)

	r.Route("/api/internal/bot/shards", func(r chi.Router) {
		r.Use(bridgeAPIKeyPerms)
		r.Use(requireBotRouteScope(handler.rbacResolver, handler.auditSink, headers))
		r.Use(botJWTMiddleware(handler.jwtValidator))
		r.Post("/register", handler.RegisterShardWorker)
		r.Post("/heartbeat", handler.HeartbeatShardWorker)
		r.Post("/release", handler.ReleaseShardWorker)
		r.Get("/assignments", handler.GetShardAssignments)
	})

	r.Route("/api/internal/bot/owner", func(r chi.Router) {
		r.Use(bridgeAPIKeyPerms)
		r.Use(requireBotRouteScope(handler.rbacResolver, handler.auditSink, headers))
		r.Use(botJWTMiddleware(handler.jwtValidator))
		r.Get("/stats", handler.GetOwnerStats)
		r.Get("/proxies", handler.ListOwnerProxies)
		r.Post("/proxies", handler.CreateOwnerProxy)
		r.Patch("/proxies/{proxyID}", handler.UpdateOwnerProxy)
		r.Delete("/proxies/{proxyID}", handler.DeleteOwnerProxy)
	})

	r.Route("/api/internal/bot/setup", func(r chi.Router) {
		r.Use(bridgeAPIKeyPerms)
		r.Use(requireBotRouteScope(handler.rbacResolver, handler.auditSink, headers))
		r.Use(botJWTMiddleware(handler.jwtValidator))
		r.Get("/guilds/{guildID}/settings", handler.GetGuildSettings)
		r.Put("/guilds/{guildID}/settings", handler.UpdateGuildSettings)
		r.Get("/guilds/{guildID}/tracked-servers", handler.ListGuildTrackedServers)
		r.Put("/guilds/{guildID}/tracked-servers", handler.ReplaceGuildTrackedServers)
		r.Post("/guilds/{guildID}/tracked-servers/{bmID}", handler.AddGuildTrackedServer)
		r.Delete("/guilds/{guildID}/tracked-servers/{bmID}", handler.RemoveGuildTrackedServer)
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
		r.Post("/guilds/{guildID}/wipe-react-schedule-posts/claim", handler.ClaimNextWipeReactSchedulePost)
		r.Post("/guilds/{guildID}/wipe-react-schedule-posts/{scheduleID}/complete", handler.CompleteWipeReactSchedulePost)
		r.Post("/guilds/{guildID}/wipe-react-schedule-posts/{scheduleID}/release", handler.ReleaseWipeReactSchedulePost)
		r.Get("/tracked-servers", handler.ListTrackedServers)
		r.Post("/tracked-servers", handler.CreateTrackedServer)
		r.Get("/tracked-servers/{bmID}/player-count-history", handler.GetServerPlayerCountAnalytics)
		r.Put("/tracked-servers/{bmID}", handler.UpdateTrackedServer)
		r.Delete("/tracked-servers/{bmID}", handler.DeleteTrackedServer)
		r.Get("/player-states/servers/{serverID}", handler.ListOnlinePlayerStatesByServer)
		r.Get("/player-states/servers/{serverID}/search", handler.SearchPlayerStatesOnServer)
		r.Get("/player-states/servers/{serverID}/players/{playerID}", handler.GetPlayerState)
		r.Get("/player-states/players/{playerID}", handler.ListPlayerStatesByPlayer)

		// Steam & Discord linking endpoints
		r.Get("/steam/by-bm/{bmID}", handler.GetSteamAccountByBmID)
		r.Get("/players/{bmID}/bm-profile", handler.GetBmPlayerProfile)
		r.Get("/players/{bmID}/bm-analytics", handler.GetBmPlayerAnalytics)
		r.Get("/players/{bmID}/offline-analytics", handler.GetPlayerOfflineAnalytics)
		r.Get("/steam/{steamID}/profile", handler.GetSteamProfile)
		r.Get("/steam/{steamID}/analytics", handler.GetSteamAnalytics)
		r.Post("/steam/resolve", handler.ResolveSteamID)
		r.Post("/steam/links", handler.CreateSoftLink)
		r.Post("/steam/bulk-hard-links", handler.BulkUpsertHardLinks)
		r.Post("/platform-sync/{platform}/{id}", handler.SyncPlatformIdentity)
		r.Get("/steam-accounts/count", handler.GetSteamAccountCount)
		r.Post("/user-guilds", handler.CreateUserGuildLink)
		r.Post("/user-guilds/leave", handler.MarkUserGuildLeft)
		r.Post("/bot-guilds/upsert", handler.UpsertBotGuild)
		r.Post("/bot-guilds/leave", handler.MarkBotGuildLeft)
		r.Post("/bot-guilds/reconcile", handler.ReconcileBotGuilds)
		r.Post("/bot-user-installs/seen", handler.TouchBotUserInstallSeen)
		r.Post("/users/ensure", handler.EnsureAppUser)
		r.Post("/command-usage", handler.RecordCommandUsage)
		r.Get("/steam/{steamID}", handler.GetSteamAccount)
		r.Get("/steam/{steamID}/friends", handler.GetSteamFriends)
		r.Get("/steam/{steamID}/links", handler.ListLinksBySteamID)
		r.Delete("/steam/{steamID}/links/{discordID}/soft", handler.DeleteSoftLink)
		r.Get("/discord/{discordID}/links", handler.ListLinksByDiscordID)
		r.Get("/discord/{discordID}/setsteam-candidates", handler.ListSetSteamCandidates)
		r.Put("/discord/{discordID}/link-visibility-hidden", handler.HideDiscordLinkIdentity)
		r.Delete("/discord/{discordID}/link-visibility-hidden", handler.UnhideDiscordLinkIdentity)
		r.Get("/discord/{discordID}/guilds", handler.ListUserGuildLinksByDiscord)
		r.Get("/discord/{discordID}/is-website-admin", handler.IsDiscordWebsiteAdmin)
		r.Get("/discord/{discordID}/has-pro-role", handler.HasDiscordPRORole)

		// User ban registry (owner-only /ban command)
		r.Get("/bans/{discordID}", handler.GetUserBan)
		r.Put("/bans/{discordID}", handler.UpdateUserBan)

		// Shared bot-command rate limiter + low-guild fraud detector
		r.Post("/rate-limit/consume", handler.ConsumeRateLimit)

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
		r.Post("/guilds/{guildID}/track-groups/{groupID}/notifications/claim", handler.ClaimTrackGroupNotification)
		r.Get("/guilds/{guildID}/track-groups/{groupID}/analytics", handler.GetTrackGroupAnalytics)
	})

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
		GuildSyncer:   discordGuildSyncerAdapter{s: postgres},
	})

	r.Route("/api/auth/discord", func(r chi.Router) {
		r.Get("/", discordHandler.Initiate)
		r.Get("/callback", discordHandler.Callback)
	})

	r.Route("/api/events", func(r chi.Router) {
		if eventsHub != nil {
			r.Get("/stream", NewEventsHandler(cfg.AppSessionSecret, eventsHub, postgres.Pool()).ServeHTTP)
		}
	})

	r.Route("/api/app", func(r chi.Router) {
		// requireAppSecret guards the /api/app surface from non-frontend callers.
		// API-key-authenticated requests bypass it; they are already authenticated
		// and scoped through the global apiKeyAuthMW above.
		r.Use(func(next http.Handler) http.Handler {
			secretMW := requireAppSecret(next)
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if actions.APIKeyAuthFromContext(r.Context()) != nil {
					next.ServeHTTP(w, r)
					return
				}
				secretMW.ServeHTTP(w, r)
			})
		})

		r.Post("/auth/logout", appAuthHandler.Logout)

		r.Group(func(r chi.Router) {
			r.Use(requireSession)
			r.Get("/me", appAuthHandler.Me)
			r.Get("/me/preferences", appAuthHandler.Preferences)
			r.Patch("/me/preferences", appAuthHandler.UpdatePreferences)
		})

		r.Group(func(r chi.Router) {
			// requireSession validates the session epoch + suspended_at +
			// deleted_at on every request. The previous appSessionMiddleware
			// only checked the HMAC + expiry, so a force-logged-out, suspended,
			// or deleted user retained access for up to 14 days.
			r.Use(requireSession)
			r.Get("/me/connections", handler.GetCurrentAppUserConnections)
			r.Get("/me/guilds", handler.GetCurrentAppUserGuilds)
			r.Get("/me/guilds/installed", handler.GetCurrentAppUserInstalledGuilds)
			r.Get("/me/guilds/{guildID}/links", handler.GetCurrentAppUserGuildLinks)
		})

		r.Group(func(r chi.Router) {
			r.Use(requireSession)
			r.Get("/players/search", handler.SearchPlayers)
			r.Get("/players/{identifier}/profile", handler.GetPlayerProfileApp)
		})

		r.Route("/admin", func(r chi.Router) {
			r.Use(bridgeAPIKeyPerms)
			// requireSession gates non-API-key requests; API-key requests skip it
			// because the key middleware has already authenticated the caller.
			r.Use(func(next http.Handler) http.Handler {
				sessionMW := requireSession(next)
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if actions.APIKeyAuthFromContext(r.Context()) != nil {
						next.ServeHTTP(w, r)
						return
					}
					sessionMW.ServeHTTP(w, r)
				})
			})
			r.Post("/actions/{type}", handler.actionDispatcher.ServeHTTP)
			r.With(requireAdminPerm(rbac.PermRolesRead)).Get("/roles", handler.ListAdminRoles)
			r.With(requireAdminPerm(rbac.PermRolesRead)).Get("/permissions", handler.ListAdminPermissions)
			r.With(requireAdminPerm(rbac.PermUsersRead)).Get("/users", handler.ListAdminUsers)
			r.With(requireAdminPerm(rbac.PermUsersRead)).Get("/administrators", handler.ListAdministrators)
			r.With(requireAdminPerm(rbac.PermUsersRead)).Get("/users/{id}", handler.GetAdminUser)
			r.With(requireAdminPerm(rbac.PermUsersAuditRead)).Get("/users/{id}/audit", handler.ListAdminUserAudit)
			r.With(requireAdminPerm(rbac.PermUsersRead)).Get("/users/{id}/command-usage", handler.AdminGetUserCommandUsage)
			r.With(requireAdminPerm(rbac.PermUsersRead)).Get("/users/{id}/command-usage/filter-options", handler.AdminGetUserCommandUsageFilterOptions)
			r.With(requireAdminPerm(rbac.PermUsersRead)).Get("/users/{id}/command-usage/{usageId}/targets", handler.AdminGetCommandUsageTargets)
			r.With(requireAdminPerm(rbac.PermUsersRead)).Get("/users/{id}/command-usage/{usageId}/children", handler.AdminGetCommandUsageChildren)
			r.With(requireAdminPerm(rbac.PermGuildsRead)).Get("/guilds", handler.ListAdminGuilds)
			r.With(requireAdminPerm(rbac.PermUsersRead)).Get("/user-installs", handler.ListAdminUserInstalls)
			r.With(requireAdminPerm(rbac.PermBmServersRead)).Get("/servers", handler.ListAdminServers)
			r.With(requireAdminPerm(rbac.PermAuditRead)).Get("/audit", handler.ListAdminAudit)
			r.With(requireAdminPerm(rbac.PermAuditRead)).Get("/audit/event-types", handler.ListAdminAuditEventTypes)
			r.With(requireAdminPerm(rbac.PermAuditRead)).Get("/audit/{id}", handler.GetAdminAuditEvent)
			r.Get("/proxies", handler.ListAdminProxies)
			r.Post("/proxies", handler.CreateAdminProxy)
			r.Post("/proxies/bulk", handler.BulkAdminProxies)
			r.Patch("/proxies/{proxyID}", handler.UpdateAdminProxy)
			r.Delete("/proxies/{proxyID}", handler.DeleteAdminProxy)
			r.Get("/api-keys", handler.ListAdminAPIKeys)
			r.Get("/bm-servers", handler.ListAdminBmServers)
			r.Post("/bm-servers", handler.CreateAdminBmServer)
			r.Post("/bm-servers/bulk", handler.BulkAdminBmServers)
			r.Put("/bm-servers/{bmID}", handler.UpdateAdminBmServer)
			r.Delete("/bm-servers/{bmID}", handler.DeleteAdminBmServer)
		})
	})

	return r
}

func botJWTMiddleware(validator *auth.Validator) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// API-key-authenticated requests bypass bot JWT validation; they are
			// already authenticated and scoped through requireBotRouteScope.
			if actions.APIKeyAuthFromContext(r.Context()) != nil {
				next.ServeHTTP(w, r)
				return
			}

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

func appSessionMiddleware(validator *auth.AppSessionValidator) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawToken := strings.TrimSpace(r.Header.Get("X-RustLens-Session"))
			if rawToken == "" {
				writeError(w, http.StatusUnauthorized, "missing app session")
				return
			}

			claims, err := validator.Parse(rawToken)
			if err != nil {
				status := http.StatusUnauthorized
				if errors.Is(err, auth.ErrExpiredAppSession) {
					status = http.StatusUnauthorized
				}
				writeError(w, status, "invalid app session")
				return
			}

			ctx := context.WithValue(r.Context(), appSessionContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// discordGuildSyncerAdapter bridges the discord OAuth handler's GuildSyncer
// interface (which speaks discord.UserGuild) to the store's SyncUserGuildInput,
// keeping the store and oauth/discord packages decoupled.
type discordGuildSyncerAdapter struct {
	s *store.PostgresStore
}

func (a discordGuildSyncerAdapter) SyncUserGuildsForUser(ctx context.Context, discordID string, guilds []discord.UserGuild) error {
	in := make([]store.SyncUserGuildInput, len(guilds))
	for i, g := range guilds {
		in[i] = store.SyncUserGuildInput{
			GuildID:                g.ID,
			Name:                   g.Name,
			IconHash:               g.IconHash,
			ApproximateMemberCount: g.ApproximateMemberCount,
			IsOwner:                g.Owner,
		}
	}
	return a.s.SyncUserGuildsForUser(ctx, discordID, in)
}
