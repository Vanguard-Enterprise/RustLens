package httpapi

import (
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"
	"time"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/store"
)

func appUserToMap(user store.AppUser) map[string]any {
	return map[string]any{
		"id":          user.ID,
		"displayName": user.DisplayName,
		"avatarUrl":   user.AvatarURL,
		"roles":       user.Roles,
		"createdAt":   user.CreatedAt,
		"updatedAt":   user.UpdatedAt,
		"lastLoginAt": user.LastLoginAt,
	}
}

func connectedAccountToMap(account store.ConnectedAccount) map[string]any {
	return map[string]any{
		"provider":          account.Provider,
		"providerAccountId": account.ProviderAccountID,
		"username":          account.Username,
		"displayName":       account.DisplayName,
		"avatarUrl":         account.AvatarURL,
		"profileEmail":      account.ProfileEmail,
		"createdAt":         account.CreatedAt,
		"updatedAt":         account.UpdatedAt,
	}
}

func linkedSteamAccountToMap(account store.LinkedSteamAccount) map[string]any {
	return map[string]any{
		"steamId":     account.SteamID,
		"bmId":        account.BmID,
		"bmName":      account.BmName,
		"avatarImage": account.AvatarImage,
		"type":        account.LinkType,
		"source":      account.Source,
	}
}

func userGuildMembershipToMap(guild store.UserGuildMembership) map[string]any {
	return map[string]any{
		"guildId":     guild.GuildID,
		"isActive":    guild.IsActive,
		"firstSeenAt": guild.FirstSeenAt,
		"lastSeenAt":  guild.LastSeenAt,
		"leftAt":      guild.LeftAt,
	}
}

func currentUserToMap(current store.CurrentUser) map[string]any {
	steamAccounts := make([]map[string]any, 0, len(current.SteamAccounts))
	for _, account := range current.SteamAccounts {
		steamAccounts = append(steamAccounts, linkedSteamAccountToMap(account))
	}

	guilds := make([]map[string]any, 0, len(current.Guilds))
	for _, guild := range current.Guilds {
		guilds = append(guilds, userGuildMembershipToMap(guild))
	}

	payload := map[string]any{
		"user":          appUserToMap(current.User),
		"steamAccounts": steamAccounts,
		"guilds":        guilds,
	}

	if current.DiscordAccount != nil {
		payload["discordAccount"] = connectedAccountToMap(*current.DiscordAccount)
	} else {
		payload["discordAccount"] = nil
	}

	return payload
}

func requireAppActor(r *http.Request, postgres *store.PostgresStore) (store.AppUser, bool, int, string) {
	s := appauth.SessionFromContext(r.Context())
	if s == nil {
		return store.AppUser{}, false, http.StatusUnauthorized, "missing app session"
	}
	userID := s.UserID

	user, err := postgres.GetAppUserByID(r.Context(), userID)
	if errors.Is(err, store.ErrAppUserNotFound) {
		// Session signature is valid but the underlying user was deleted (or
		// the DB was wiped). Treat this as an invalid session so the frontend
		// clears the cookie and redirects to /login instead of crashing.
		return store.AppUser{}, false, http.StatusUnauthorized, "invalid_session"
	}
	if err != nil {
		return store.AppUser{}, false, http.StatusInternalServerError, "failed to load app user"
	}

	return user, true, http.StatusOK, ""
}

func requireInternalAdmin(r *http.Request, postgres *store.PostgresStore) (store.AppUser, bool, int, string) {
	user, ok, status, message := requireAppActor(r, postgres)
	if !ok {
		return store.AppUser{}, false, status, message
	}
	if !slices.Contains(user.Roles, "internal_admin") {
		return store.AppUser{}, false, http.StatusForbidden, "internal admin access required"
	}
	return user, true, http.StatusOK, ""
}

func (h *Handler) GetCurrentAppUser(w http.ResponseWriter, r *http.Request) {
	actor, ok, status, message := requireAppActor(r, h.postgres)
	if !ok {
		writeError(w, status, message)
		return
	}

	currentUser, err := h.postgres.GetCurrentUserByID(r.Context(), actor.ID)
	if errors.Is(err, store.ErrAppUserNotFound) {
		writeError(w, http.StatusNotFound, "app user not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetCurrentAppUser error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load current user")
		return
	}

	writeJSON(w, http.StatusOK, currentUserToMap(currentUser))
}

func (h *Handler) GetCurrentAppUserConnections(w http.ResponseWriter, r *http.Request) {
	actor, ok, status, message := requireAppActor(r, h.postgres)
	if !ok {
		writeError(w, status, message)
		return
	}

	currentUser, err := h.postgres.GetCurrentUserByID(r.Context(), actor.ID)
	if err != nil {
		log.Printf("[backend] GetCurrentAppUserConnections error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load connections")
		return
	}

	steamAccounts := make([]map[string]any, 0, len(currentUser.SteamAccounts))
	for _, account := range currentUser.SteamAccounts {
		steamAccounts = append(steamAccounts, linkedSteamAccountToMap(account))
	}

	payload := map[string]any{
		"steamAccounts": steamAccounts,
	}
	if currentUser.DiscordAccount != nil {
		payload["discordAccount"] = connectedAccountToMap(*currentUser.DiscordAccount)
	}

	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) GetCurrentAppUserGuilds(w http.ResponseWriter, r *http.Request) {
	actor, ok, status, message := requireAppActor(r, h.postgres)
	if !ok {
		writeError(w, status, message)
		return
	}

	currentUser, err := h.postgres.GetCurrentUserByID(r.Context(), actor.ID)
	if err != nil {
		log.Printf("[backend] GetCurrentAppUserGuilds error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load guilds")
		return
	}

	items := make([]map[string]any, 0, len(currentUser.Guilds))
	for _, guild := range currentUser.Guilds {
		items = append(items, userGuildMembershipToMap(guild))
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) GetCurrentAppUserInstalledGuilds(w http.ResponseWriter, r *http.Request) {
	actor, ok, status, message := requireAppActor(r, h.postgres)
	if !ok {
		writeError(w, status, message)
		return
	}

	// Read discord_id directly from app_users — that's where the live auth
	// subsystem stores it. user_connected_accounts is unused in production.
	discordID, err := h.postgres.GetAppUserDiscordID(r.Context(), actor.ID)
	if err != nil {
		log.Printf("[backend] GetAppUserDiscordID error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load installed guilds")
		return
	}
	if discordID == "" {
		log.Printf("[backend] /me/guilds/installed: user %s has empty discord_id", actor.ID)
		writeJSON(w, http.StatusOK, map[string]any{"items": []map[string]any{}})
		return
	}

	rows, err := h.postgres.ListInstalledGuildsForUser(r.Context(), discordID)
	if err != nil {
		log.Printf("[backend] ListInstalledGuildsForUser error for %s (discord=%s): %v", actor.ID, discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to load installed guilds")
		return
	}
	log.Printf("[backend] /me/guilds/installed: user=%s discord=%s rows=%d", actor.ID, discordID, len(rows))

	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		var owner map[string]any
		if row.OwnerDiscordID != nil {
			owner = map[string]any{
				"id":         *row.OwnerDiscordID,
				"username":   derefString(row.OwnerUsername),
				"globalName": row.OwnerGlobalName,
				"avatarHash": row.OwnerAvatarHash,
			}
		}
		items = append(items, map[string]any{
			"id":                     row.GuildID,
			"name":                   row.Name,
			"iconHash":               row.IconHash,
			"bannerHash":             row.BannerHash,
			"approximateMemberCount": row.ApproximateMemberCount,
			"trackedServerCount":     row.TrackedServerCount,
			"owner":                  owner,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (h *Handler) ListAppAdminUsers(w http.ResponseWriter, r *http.Request) {
	actor, ok, status, message := requireInternalAdmin(r, h.postgres)
	if !ok {
		writeError(w, status, message)
		return
	}

	items, err := h.postgres.ListAdminUsers(r.Context())
	if err != nil {
		log.Printf("[backend] ListAppAdminUsers error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load admin users")
		return
	}

	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]any{
			"user":                 appUserToMap(item.User),
			"discordId":            item.DiscordID,
			"discordUsername":      item.DiscordUsername,
			"guildCount":           item.GuildCount,
			"steamConnectionCount": item.SteamConnectionCount,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": payload})
}

func (h *Handler) ListAppAdminServers(w http.ResponseWriter, r *http.Request) {
	actor, ok, status, message := requireInternalAdmin(r, h.postgres)
	if !ok {
		writeError(w, status, message)
		return
	}

	items, err := h.postgres.ListAdminServers(r.Context())
	if err != nil {
		log.Printf("[backend] ListAppAdminServers error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load admin servers")
		return
	}

	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]any{
			"serverBmId":    item.ServerBMID,
			"name":          item.Name,
			"lastPolledAt":  item.LastPolledAt,
			"lastPollError": item.LastPollError,
			"guildCount":    item.GuildCount,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": payload})
}

func (h *Handler) ListAppAuditEvents(w http.ResponseWriter, r *http.Request) {
	actor, ok, status, message := requireInternalAdmin(r, h.postgres)
	if !ok {
		writeError(w, status, message)
		return
	}

	limit := 50
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	items, err := h.postgres.ListAuditEvents(r.Context(), limit)
	if err != nil {
		log.Printf("[backend] ListAppAuditEvents error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load audit events")
		return
	}

	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]any{
			"id":           item.ID,
			"actorUserId":  item.ActorUserID,
			"targetUserId": item.TargetUserID,
			"eventType":    item.EventType,
			"payload":      item.Payload,
			"createdAt":    item.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": payload})
}

// GetCurrentAppUserGuildLinks returns the caller's Discord<->Steam links scoped
// to a single guild. Membership in the guild is required.
func (h *Handler) GetCurrentAppUserGuildLinks(w http.ResponseWriter, r *http.Request) {
	actor, ok, status, message := requireAppActor(r, h.postgres)
	if !ok {
		writeError(w, status, message)
		return
	}

	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	discordID, err := h.postgres.GetAppUserDiscordID(r.Context(), actor.ID)
	if err != nil {
		log.Printf("[backend] GetAppUserDiscordID error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load guild links")
		return
	}
	if discordID == "" {
		writeError(w, http.StatusForbidden, "not a member of this guild")
		return
	}

	isMember, err := h.postgres.IsGuildMember(r.Context(), guildID, discordID)
	if err != nil {
		log.Printf("[backend] IsGuildMember error for %s guild=%s: %v", actor.ID, guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to load guild links")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not a member of this guild")
		return
	}

	rows, err := h.postgres.ListGuildLinksByGuildAndDiscord(r.Context(), guildID, discordID)
	if err != nil {
		log.Printf("[backend] ListGuildLinksByGuildAndDiscord error for %s guild=%s: %v", actor.ID, guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to load guild links")
		return
	}

	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"steamId":  row.SteamID,
			"linkedAt": row.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
