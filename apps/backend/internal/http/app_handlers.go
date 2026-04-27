package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"
	"time"

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

func userEmailToMap(email store.UserEmail) map[string]any {
	return map[string]any{
		"email":      email.Email,
		"isPrimary":  email.IsPrimary,
		"isVerified": email.IsVerified,
		"createdAt":  email.CreatedAt,
		"updatedAt":  email.UpdatedAt,
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
	emails := make([]map[string]any, 0, len(current.Emails))
	for _, email := range current.Emails {
		emails = append(emails, userEmailToMap(email))
	}

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
		"emails":        emails,
		"steamAccounts": steamAccounts,
		"guilds":        guilds,
	}

	if current.DiscordAccount != nil {
		payload["discordAccount"] = connectedAccountToMap(*current.DiscordAccount)
	} else {
		payload["discordAccount"] = nil
	}

	if current.PrimaryEmail != nil {
		payload["primaryEmail"] = userEmailToMap(*current.PrimaryEmail)
	} else {
		payload["primaryEmail"] = nil
	}

	return payload
}

func requireAppActor(r *http.Request, postgres *store.PostgresStore) (store.AppUser, bool, int, string) {
	session := appSessionFromContext(r.Context())
	if session == nil || session.UserID == "" {
		return store.AppUser{}, false, http.StatusUnauthorized, "missing app session"
	}

	user, err := postgres.GetAppUserByID(r.Context(), session.UserID)
	if errors.Is(err, store.ErrAppUserNotFound) {
		return store.AppUser{}, false, http.StatusUnauthorized, "app user not found"
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

type completeDiscordAuthRequest struct {
	DiscordID     string `json:"discordId"`
	Username      string `json:"username"`
	DisplayName   string `json:"displayName"`
	AvatarURL     string `json:"avatarUrl"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
}

func (h *Handler) CompleteDiscordAuth(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request completeDiscordAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !validateSnowflakeValue(request.DiscordID) {
		writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
		return
	}

	currentUser, err := h.postgres.UpsertDiscordAccount(r.Context(), store.DiscordAuthInput{
		DiscordID:           request.DiscordID,
		Username:            request.Username,
		DisplayName:         request.DisplayName,
		AvatarURL:           request.AvatarURL,
		Email:               request.Email,
		EmailVerified:       request.EmailVerified,
		AssignInternalAdmin: slices.Contains(h.cfg.AppAdminDiscordIDs, request.DiscordID),
	})
	if err != nil {
		log.Printf("[backend] CompleteDiscordAuth error for %s: %v", request.DiscordID, err)
		writeError(w, http.StatusInternalServerError, "failed to complete discord auth")
		return
	}

	writeJSON(w, http.StatusOK, currentUserToMap(currentUser))
}

type passwordRegisterRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

func (h *Handler) RegisterPasswordUser(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request passwordRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if request.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if request.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required")
		return
	}

	currentUser, err := h.postgres.CreatePasswordUser(r.Context(), request.Email, request.Password, request.DisplayName)
	if errors.Is(err, store.ErrEmailAlreadyRegistered) {
		writeError(w, http.StatusConflict, "email is already registered")
		return
	}
	if errors.Is(err, store.ErrWeakPassword) {
		writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
		return
	}
	if err != nil {
		log.Printf("[backend] RegisterPasswordUser error for %s: %v", request.Email, err)
		writeError(w, http.StatusInternalServerError, "failed to register user")
		return
	}

	writeJSON(w, http.StatusCreated, currentUserToMap(currentUser))
}

type passwordLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) LoginPasswordUser(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request passwordLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if request.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required")
		return
	}

	currentUser, err := h.postgres.AuthenticatePasswordUser(r.Context(), request.Email, request.Password)
	if errors.Is(err, store.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		log.Printf("[backend] LoginPasswordUser error for %s: %v", request.Email, err)
		writeError(w, http.StatusInternalServerError, "failed to sign in")
		return
	}

	writeJSON(w, http.StatusOK, currentUserToMap(currentUser))
}

type passwordResetRequest struct {
	Email string `json:"email"`
}

func (h *Handler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request passwordResetRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if request.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	token, linked, err := h.postgres.CreatePasswordResetToken(r.Context(), request.Email, 30*time.Minute)
	if errors.Is(err, store.ErrEmailNotLinked) {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if err != nil {
		log.Printf("[backend] RequestPasswordReset error for %s: %v", request.Email, err)
		writeError(w, http.StatusInternalServerError, "failed to create password reset")
		return
	}

	response := map[string]any{
		"status": "unlinked",
		"linked": linked,
	}
	if linked {
		response["status"] = "sent"
		if h.cfg.AllowAuthDebugTokens {
			response["debugToken"] = token
		}
	} else {
		response["status"] = "unlinked"
	}

	writeJSON(w, http.StatusOK, response)
}

type consumePasswordResetRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

func (h *Handler) ConsumePasswordReset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request consumePasswordResetRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if request.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	if request.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "new password is required")
		return
	}

	currentUser, err := h.postgres.ResetPasswordWithToken(r.Context(), request.Token, request.NewPassword)
	if errors.Is(err, store.ErrPasswordResetTokenInvalid) {
		writeError(w, http.StatusUnauthorized, "password reset token is invalid or expired")
		return
	}
	if errors.Is(err, store.ErrWeakPassword) {
		writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
		return
	}
	if err != nil {
		log.Printf("[backend] ConsumePasswordReset error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to reset password")
		return
	}

	writeJSON(w, http.StatusOK, currentUserToMap(currentUser))
}

func (h *Handler) RequestEmailVerification(w http.ResponseWriter, r *http.Request) {
	actor, ok, status, message := requireAppActor(r, h.postgres)
	if !ok {
		writeError(w, status, message)
		return
	}

	token, err := h.postgres.CreateEmailVerificationToken(r.Context(), actor.ID, 24*time.Hour)
	if errors.Is(err, store.ErrEmailNotLinked) {
		writeError(w, http.StatusBadRequest, "no email is linked to this account")
		return
	}
	if errors.Is(err, store.ErrEmailAlreadyVerified) {
		writeError(w, http.StatusConflict, "email is already verified")
		return
	}
	if err != nil {
		log.Printf("[backend] RequestEmailVerification error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to create email verification")
		return
	}

	if h.cfg.AllowAuthDebugTokens {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "sent",
			"debugToken": token,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "sent",
	})
}

type consumeEmailVerificationRequest struct {
	Token string `json:"token"`
}

func (h *Handler) ConsumeEmailVerification(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request consumeEmailVerificationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if request.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	currentUser, err := h.postgres.VerifyEmailWithToken(r.Context(), request.Token)
	if errors.Is(err, store.ErrEmailVerificationInvalid) {
		writeError(w, http.StatusUnauthorized, "email verification token is invalid or expired")
		return
	}
	if err != nil {
		log.Printf("[backend] ConsumeEmailVerification error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to verify email")
		return
	}

	writeJSON(w, http.StatusOK, currentUserToMap(currentUser))
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

	emails := make([]map[string]any, 0, len(currentUser.Emails))
	for _, email := range currentUser.Emails {
		emails = append(emails, userEmailToMap(email))
	}

	steamAccounts := make([]map[string]any, 0, len(currentUser.SteamAccounts))
	for _, account := range currentUser.SteamAccounts {
		steamAccounts = append(steamAccounts, linkedSteamAccountToMap(account))
	}

	payload := map[string]any{
		"emails":        emails,
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

	currentUser, err := h.postgres.GetCurrentUserByID(r.Context(), actor.ID)
	if err != nil {
		log.Printf("[backend] GetCurrentAppUserInstalledGuilds error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load installed guilds")
		return
	}

	if currentUser.DiscordAccount == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []map[string]any{}})
		return
	}

	rows, err := h.postgres.ListInstalledGuildsForUser(r.Context(), currentUser.DiscordAccount.ProviderAccountID)
	if err != nil {
		log.Printf("[backend] ListInstalledGuildsForUser error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load installed guilds")
		return
	}

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
			"primaryEmail":         item.PrimaryEmail,
			"guildCount":           item.GuildCount,
			"steamConnectionCount": item.SteamConnectionCount,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": payload})
}

func (h *Handler) ListAppAdminGuilds(w http.ResponseWriter, r *http.Request) {
	actor, ok, status, message := requireInternalAdmin(r, h.postgres)
	if !ok {
		writeError(w, status, message)
		return
	}

	items, err := h.postgres.ListAdminGuilds(r.Context())
	if err != nil {
		log.Printf("[backend] ListAppAdminGuilds error for %s: %v", actor.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load admin guilds")
		return
	}

	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]any{
			"guildId":        item.GuildID,
			"activeMembers":  item.ActiveMembers,
			"linkedMembers":  item.LinkedMembers,
			"trackedServers": item.TrackedServers,
			"adminRoleId":    item.AdminRoleID,
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
