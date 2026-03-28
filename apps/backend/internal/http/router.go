package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"rustlens/backend/internal/auth"
	"rustlens/backend/internal/config"
	"rustlens/backend/internal/shards"
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

func NewRouter(cfg config.Config, postgres *store.PostgresStore, redis *store.RedisStore, shardService *shards.Service) http.Handler {
	handler := NewHandler(cfg, postgres, redis, shardService)

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
		r.Get("/guilds/{guildID}", handler.GetGuildSetupState)
		r.Post("/guilds/{guildID}/claim", handler.ClaimGuildSetup)
		r.Post("/guilds/{guildID}/complete", handler.CompleteGuildSetup)
		r.Post("/guilds/{guildID}/reset", handler.ResetGuildSetup)
		r.Get("/guilds/{guildID}/settings", handler.GetGuildSettings)
		r.Post("/guilds/{guildID}/settings/log-channel", handler.SetGuildLogChannel)
		r.Get("/guilds/{guildID}/wipe-reacts/active", handler.ListActiveWipeReacts)
		r.Post("/guilds/{guildID}/wipe-reacts", handler.CreateWipeReact)
		r.Get("/guilds/{guildID}/wipe-reacts/{messageID}", handler.GetWipeReact)
		r.Put("/guilds/{guildID}/wipe-reacts/{messageID}", handler.UpdateWipeReact)
		r.Delete("/guilds/{guildID}/wipe-reacts/{messageID}", handler.DeleteWipeReact)
		r.Post("/guilds/{guildID}/wipe-reacts/{messageID}/reactions", handler.SetWipeReactReaction)
		r.Delete("/guilds/{guildID}/wipe-reacts/{messageID}/reactions/{userID}", handler.RemoveWipeReactReaction)
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
