package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/players"
	"rustlens/backend/internal/store"
)

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
//   - Free-text queries require the caller to have at least one row in
//     admin_user_roles (i.e. session.AdminRoles is non-empty) and run a
//     name-substring search across all globally tracked servers.
func (h *Handler) SearchPlayers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeError(w, http.StatusBadRequest, "q must be at least 2 characters")
		return
	}
	includeOffline := r.URL.Query().Get("includeOffline") == "true"

	kind := players.ClassifyQuery(q)

	if kind == players.KindName {
		session := appauth.SessionFromContext(r.Context())
		if session == nil || len(session.AdminRoles) == 0 {
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
