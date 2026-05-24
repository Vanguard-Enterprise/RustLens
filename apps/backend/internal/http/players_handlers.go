package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/players"
	"rustlens/backend/internal/store"
)

// allowPlayerSearch is a minimal per-userID sliding-window counter using
// Redis INCR/EXPIRE. Fails open on Redis errors. scope is one of
// "search" or "profile".
func (h *Handler) allowPlayerSearch(ctx context.Context, userID, scope string, max int, window time.Duration) bool {
	if h.redis == nil || userID == "" {
		return true
	}
	key := "rustlens:players_" + scope + ":" + userID
	count, err := h.redis.Client().Incr(ctx, key).Result()
	if err != nil {
		return true // fail open on Redis hiccup
	}
	if count == 1 {
		_ = h.redis.Client().Expire(ctx, key, window).Err()
	}
	return count <= int64(max)
}

type playerSearchHit struct {
	SteamID       string                  `json:"steamId"`
	BmID          *string                 `json:"bmId"`
	BmName        *string                 `json:"bmName"`
	AvatarURL     *string                 `json:"avatarUrl"`
	CurrentServer *playerSearchServerInfo `json:"currentServer"`
	IsOnline      bool                    `json:"isOnline"`
	LastSeenAt    *string                 `json:"lastSeenAt"`
}

type playerSearchServerInfo struct {
	BmID string `json:"bmId"`
	Name string `json:"name"`
}

type playerSearchResponse struct {
	Items []playerSearchHit `json:"items"`
	Mode  string            `json:"mode"`
}

// SearchPlayers handles GET /api/app/players/search?q=&includeOffline=
//
//   - Identifier-shaped queries (Steam ID, BM ID, steamcommunity URL) are
//     open to any logged-in user and return 0 or 1 results.
//   - Free-text queries require the caller to have at least one admin
//     permission and run a
//     name-substring search across all globally tracked servers.
func (h *Handler) SearchPlayers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeError(w, http.StatusBadRequest, "q must be at least 2 characters")
		return
	}
	session := appauth.SessionFromContext(r.Context())
	userID := ""
	if session != nil {
		userID = session.UserID
	}
	if !h.allowPlayerSearch(r.Context(), userID, "search", 30, 60*time.Second) {
		writeError(w, http.StatusTooManyRequests, "rate limited")
		return
	}
	includeOffline := r.URL.Query().Get("includeOffline") == "true"

	kind := players.ClassifyQuery(q)
	// Resolve admin live from rbac rather than from session.AdminRoles, which
	// is baked into the cookie at sign-in and only refreshed when the
	// frontend's sliding-TTL kicks in (up to 7 days). A demoted user kept
	// name-search across all tracked servers until that refresh.
	isAdmin := false
	if userID != "" && h.rbacResolver != nil {
		perms, err := h.rbacResolver.PermissionsForUser(r.Context(), userID)
		if err != nil {
			log.Printf("[backend] SearchPlayers permission lookup error for %s: %v", userID, err)
			writeError(w, http.StatusInternalServerError, "permission_check_failed")
			return
		}
		isAdmin = len(perms) > 0
	}

	if kind == players.KindName {
		if !isAdmin {
			writeError(w, http.StatusForbidden, "name search requires admin")
			return
		}
		hits, err := h.players.SearchByName(r.Context(), q, includeOffline)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "search failed")
			return
		}
		writeJSON(w, http.StatusOK, playerSearchResponse{
			Items: toPlayerSearchHits(hits),
			Mode:  "name",
		})
		return
	}

	steamID, err := h.players.ResolveToSteamID(r.Context(), q)
	if err != nil {
		if errors.Is(err, players.ErrNotFound) || errors.Is(err, store.ErrSteamAccountNotFound) {
			if kind == players.KindSteamVanity && isAdmin {
				hits, ferr := h.players.SearchByName(r.Context(), q, includeOffline)
				if ferr != nil {
					writeError(w, http.StatusInternalServerError, "search failed")
					return
				}
				writeJSON(w, http.StatusOK, playerSearchResponse{
					Items: toPlayerSearchHits(hits),
					Mode:  "name",
				})
				return
			}
			writeJSON(w, http.StatusOK, playerSearchResponse{Items: []playerSearchHit{}, Mode: "identifier"})
			return
		}
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	hit := h.lookupSingleHit(r.Context(), steamID)
	resp := playerSearchResponse{Mode: "identifier", Items: []playerSearchHit{}}
	if hit != nil {
		resp.Items = []playerSearchHit{*hit}
	}
	writeJSON(w, http.StatusOK, resp)
}

// lookupSingleHit fetches a single steam_accounts + cache + player_state
// row for the resolved Steam ID. Returns nil when no row exists.
func (h *Handler) lookupSingleHit(ctx context.Context, steamID string) *playerSearchHit {
	acct, err := h.postgres.GetSteamAccountBySteamID(ctx, steamID)
	if err != nil {
		return nil
	}
	cache, _ := h.postgres.GetSteamAccountCache(ctx, steamID)
	hit := playerSearchHit{
		SteamID: acct.SteamID,
		BmID:    acct.BmID,
	}
	if cache.AvatarImage != "" {
		av := cache.AvatarImage
		hit.AvatarURL = &av
	}
	// Try bm_profile_cache for bm_name
	if acct.BmID != nil {
		bmCache, _ := h.postgres.GetBmProfileCache(ctx, *acct.BmID)
		if bmCache.BmName != "" {
			n := bmCache.BmName
			hit.BmName = &n
		}
		states, err := h.postgres.ListPlayerStatesByPlayer(ctx, *acct.BmID)
		if err == nil {
			for _, s := range states {
				if s.IsOnline {
					hit.CurrentServer = &playerSearchServerInfo{BmID: s.ServerBmID, Name: s.ServerName}
					hit.IsOnline = true
					break
				}
			}
		}
	}
	return &hit
}

// GetPlayerProfileApp handles GET /api/app/players/{identifier}/profile.
//
// Resolves {identifier} (Steam ID, BM ID, or steamcommunity URL) to a
// Steam64 ID, then delegates response shape to the existing
// GetSteamProfile pipeline so the bot-internal and app routes return the
// same JSON.
func (h *Handler) GetPlayerProfileApp(w http.ResponseWriter, r *http.Request) {
	session := appauth.SessionFromContext(r.Context())
	userID := ""
	if session != nil {
		userID = session.UserID
	}
	if !h.allowPlayerSearch(r.Context(), userID, "profile", 60, 60*time.Second) {
		writeError(w, http.StatusTooManyRequests, "rate limited")
		return
	}
	identifier := chi.URLParam(r, "identifier")
	steamID, err := h.players.ResolveToSteamID(r.Context(), identifier)
	if err != nil {
		if errors.Is(err, players.ErrNotFound) || errors.Is(err, store.ErrSteamAccountNotFound) {
			writeError(w, http.StatusNotFound, "player not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	// Re-issue the request through GetSteamProfile so the response matches
	// the bot-internal shape exactly. Rebuild the chi context with
	// {steamID} populated.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("steamID", steamID)
	r2 := r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	h.GetSteamProfile(w, r2)
}

func toPlayerSearchHits(rows []store.PlayerSearchHit) []playerSearchHit {
	out := make([]playerSearchHit, 0, len(rows))
	for _, r := range rows {
		hit := playerSearchHit{
			SteamID:    r.SteamID,
			BmID:       r.BmID,
			BmName:     r.BmName,
			AvatarURL:  r.AvatarImage,
			IsOnline:   r.IsOnline,
			LastSeenAt: r.LastSeenAt,
		}
		if r.CurrentServerBm != nil && r.CurrentServer != nil {
			hit.CurrentServer = &playerSearchServerInfo{BmID: *r.CurrentServerBm, Name: *r.CurrentServer}
		}
		out = append(out, hit)
	}
	return out
}
