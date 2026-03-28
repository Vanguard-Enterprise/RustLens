package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/auth"
	"rustlens/backend/internal/config"
	"rustlens/backend/internal/shards"
	"rustlens/backend/internal/store"
)

var snowflakePattern = regexp.MustCompile(`^\d{17,20}$`)

type Handler struct {
	cfg          config.Config
	postgres     *store.PostgresStore
	redis        *store.RedisStore
	shardService *shards.Service
	jwtValidator *auth.Validator
}

func NewHandler(cfg config.Config, postgres *store.PostgresStore, redis *store.RedisStore, shardService *shards.Service) *Handler {
	return &Handler{
		cfg:          cfg,
		postgres:     postgres,
		redis:        redis,
		shardService: shardService,
		jwtValidator: auth.NewValidator(cfg.BotServiceJWT, cfg.AllowedJWTIssuer, cfg.AllowedJWTAudience),
	}
}

func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	dbErr := h.postgres.Ping(ctx)
	redisErr := h.redis.Ping(ctx)

	status := http.StatusOK
	response := map[string]any{
		"status": "ok",
		"db":     "ok",
		"redis":  "ok",
	}

	if dbErr != nil {
		status = http.StatusServiceUnavailable
		response["status"] = "degraded"
		response["db"] = "unavailable"
		log.Printf("[backend] healthz db ping failed: %v", dbErr)
	}

	if redisErr != nil {
		status = http.StatusServiceUnavailable
		response["status"] = "degraded"
		response["redis"] = "unavailable"
		log.Printf("[backend] healthz redis ping failed: %v", redisErr)
	}

	writeJSON(w, status, response)
}

type registerRequest struct {
	WorkerID string `json:"workerId"`
}

func (h *Handler) RegisterShardWorker(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}

	var request registerRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.WorkerID == "" {
		request.WorkerID = claims.WorkerID
	}

	response, err := h.shardService.Register(r.Context(), request.WorkerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, response)
}

type heartbeatRequest struct {
	WorkerID string `json:"workerId"`
	ShardIDs []int  `json:"shardIds"`
}

func (h *Handler) HeartbeatShardWorker(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}

	var request heartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.WorkerID == "" {
		request.WorkerID = claims.WorkerID
	}

	if err := h.shardService.Heartbeat(r.Context(), request.WorkerID, request.ShardIDs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type releaseRequest struct {
	WorkerID string `json:"workerId"`
}

func (h *Handler) ReleaseShardWorker(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}

	var request releaseRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.WorkerID == "" {
		request.WorkerID = claims.WorkerID
	}

	if err := h.shardService.Release(r.Context(), request.WorkerID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
}

func (h *Handler) GetShardAssignments(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}

	workerID := r.URL.Query().Get("workerId")
	if workerID == "" {
		workerID = claims.WorkerID
	}

	shards, targetShards, err := h.shardService.Assignments(r.Context(), workerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workerId":     workerID,
		"targetShards": targetShards,
		"shardIds":     shards,
	})
}

func validateGuildID(w http.ResponseWriter, r *http.Request) (string, bool) {
	guildID := chi.URLParam(r, "guildID")
	if guildID == "" {
		writeError(w, http.StatusBadRequest, "guildID is required")
		return "", false
	}
	if !snowflakePattern.MatchString(guildID) {
		writeError(w, http.StatusBadRequest, "guildID must be a valid Discord snowflake")
		return "", false
	}
	return guildID, true
}

func validateSnowflakePathParam(w http.ResponseWriter, r *http.Request, paramName string) (string, bool) {
	value := chi.URLParam(r, paramName)
	if value == "" {
		writeError(w, http.StatusBadRequest, paramName+" is required")
		return "", false
	}
	if !snowflakePattern.MatchString(value) {
		writeError(w, http.StatusBadRequest, paramName+" must be a valid Discord snowflake")
		return "", false
	}
	return value, true
}

func writeGuildSetupState(w http.ResponseWriter, state store.GuildSetupState) {
	writeJSON(w, http.StatusOK, map[string]any{
		"guildId":          state.GuildID,
		"status":           state.Status,
		"configuredBy":     state.ConfiguredBy,
		"selectedRoles":    state.SelectedRoles,
		"selectedChannels": state.SelectedChannels,
	})
}

func writeGuildSettings(w http.ResponseWriter, settings store.GuildSettings) {
	writeJSON(w, http.StatusOK, map[string]any{
		"guildId":      settings.GuildID,
		"logChannelId": settings.LogChannelID,
	})
}

func writeWipeReact(w http.ResponseWriter, record store.WipeReact) {
	writeJSON(w, http.StatusOK, map[string]any{
		"guildId":           record.GuildID,
		"channelId":         record.ChannelID,
		"messageId":         record.MessageID,
		"creatorId":         record.CreatorID,
		"name":              record.Name,
		"description":       record.Description,
		"timeZone":          record.TimeZone,
		"scheduledUnix":     record.ScheduledUnix,
		"askLateReason":     record.AskLateReason,
		"askDeclinedReason": record.AskDeclinedReason,
		"acceptedUserIds":   record.AcceptedUserIDs,
		"declinedUserIds":   record.DeclinedUserIDs,
		"lateUserIds":       record.LateUserIDs,
		"embedTimestamp":    record.EmbedTimestamp,
	})
}

func writeWipeReactList(w http.ResponseWriter, records []store.WipeReact) {
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, map[string]any{
			"guildId":           record.GuildID,
			"channelId":         record.ChannelID,
			"messageId":         record.MessageID,
			"creatorId":         record.CreatorID,
			"name":              record.Name,
			"description":       record.Description,
			"timeZone":          record.TimeZone,
			"scheduledUnix":     record.ScheduledUnix,
			"askLateReason":     record.AskLateReason,
			"askDeclinedReason": record.AskDeclinedReason,
			"acceptedUserIds":   record.AcceptedUserIDs,
			"declinedUserIds":   record.DeclinedUserIDs,
			"lateUserIds":       record.LateUserIDs,
			"embedTimestamp":    record.EmbedTimestamp,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func writeWipeReactReactionUpdate(w http.ResponseWriter, update store.WipeReactReactionUpdate) {
	response := map[string]any{
		"changed": update.Changed,
		"wipeReact": map[string]any{
			"guildId":           update.WipeReact.GuildID,
			"channelId":         update.WipeReact.ChannelID,
			"messageId":         update.WipeReact.MessageID,
			"creatorId":         update.WipeReact.CreatorID,
			"name":              update.WipeReact.Name,
			"description":       update.WipeReact.Description,
			"timeZone":          update.WipeReact.TimeZone,
			"scheduledUnix":     update.WipeReact.ScheduledUnix,
			"askLateReason":     update.WipeReact.AskLateReason,
			"askDeclinedReason": update.WipeReact.AskDeclinedReason,
			"acceptedUserIds":   update.WipeReact.AcceptedUserIDs,
			"declinedUserIds":   update.WipeReact.DeclinedUserIDs,
			"lateUserIds":       update.WipeReact.LateUserIDs,
			"embedTimestamp":    update.WipeReact.EmbedTimestamp,
		},
	}
	if update.PreviousType != nil {
		response["previousType"] = string(*update.PreviousType)
	} else {
		response["previousType"] = nil
	}
	writeJSON(w, http.StatusOK, response)
}

func writeWipeReactReactionRemoval(w http.ResponseWriter, removal store.WipeReactReactionRemoval) {
	response := map[string]any{
		"changed": removal.Changed,
		"wipeReact": map[string]any{
			"guildId":           removal.WipeReact.GuildID,
			"channelId":         removal.WipeReact.ChannelID,
			"messageId":         removal.WipeReact.MessageID,
			"creatorId":         removal.WipeReact.CreatorID,
			"name":              removal.WipeReact.Name,
			"description":       removal.WipeReact.Description,
			"timeZone":          removal.WipeReact.TimeZone,
			"scheduledUnix":     removal.WipeReact.ScheduledUnix,
			"askLateReason":     removal.WipeReact.AskLateReason,
			"askDeclinedReason": removal.WipeReact.AskDeclinedReason,
			"acceptedUserIds":   removal.WipeReact.AcceptedUserIDs,
			"declinedUserIds":   removal.WipeReact.DeclinedUserIDs,
			"lateUserIds":       removal.WipeReact.LateUserIDs,
			"embedTimestamp":    removal.WipeReact.EmbedTimestamp,
		},
	}
	if removal.RemovedType != nil {
		response["removedType"] = string(*removal.RemovedType)
	} else {
		response["removedType"] = nil
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) GetGuildSetupState(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	state, err := h.postgres.GetGuildSetupState(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] GetGuildSetupState error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild setup state")
		return
	}

	writeGuildSetupState(w, state)
}

func (h *Handler) ClaimGuildSetup(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	claimed, state, err := h.postgres.ClaimGuildSetup(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ClaimGuildSetup error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to claim guild setup")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"claimed":          claimed,
		"guildId":          state.GuildID,
		"status":           state.Status,
		"configuredBy":     state.ConfiguredBy,
		"selectedRoles":    state.SelectedRoles,
		"selectedChannels": state.SelectedChannels,
	})
}

type completeGuildSetupRequest struct {
	ConfiguredBy     string   `json:"configuredBy"`
	SelectedRoles    []string `json:"selectedRoles"`
	SelectedChannels []string `json:"selectedChannels"`
}

func (h *Handler) CompleteGuildSetup(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	var request completeGuildSetupRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.ConfiguredBy == "" {
		writeError(w, http.StatusBadRequest, "configuredBy is required")
		return
	}
	if len(request.SelectedRoles) == 0 {
		writeError(w, http.StatusBadRequest, "selectedRoles must not be empty")
		return
	}

	if err := h.postgres.CompleteGuildSetup(r.Context(), guildID, request.ConfiguredBy, request.SelectedRoles, request.SelectedChannels); err != nil {
		log.Printf("[backend] CompleteGuildSetup error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to complete guild setup")
		return
	}

	state, err := h.postgres.GetGuildSetupState(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] GetGuildSetupState (post-complete) error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild setup state")
		return
	}

	writeGuildSetupState(w, state)
}

func (h *Handler) ResetGuildSetup(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	if err := h.postgres.ResetGuildSetup(r.Context(), guildID); err != nil {
		log.Printf("[backend] ResetGuildSetup error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to reset guild setup")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

func (h *Handler) GetGuildSettings(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	settings, err := h.postgres.GetGuildSettings(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] GetGuildSettings error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild settings")
		return
	}

	writeGuildSettings(w, settings)
}

type setGuildLogChannelRequest struct {
	LogChannelID string `json:"logChannelId"`
}

func (h *Handler) SetGuildLogChannel(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	var request setGuildLogChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.LogChannelID == "" {
		writeError(w, http.StatusBadRequest, "logChannelId is required")
		return
	}
	if !snowflakePattern.MatchString(request.LogChannelID) {
		writeError(w, http.StatusBadRequest, "logChannelId must be a valid Discord snowflake")
		return
	}

	settings, err := h.postgres.SetGuildLogChannel(r.Context(), guildID, request.LogChannelID)
	if err != nil {
		log.Printf("[backend] SetGuildLogChannel error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to save guild log channel")
		return
	}

	writeGuildSettings(w, settings)
}

type wipeReactPayload struct {
	ChannelID         string   `json:"channelId"`
	MessageID         string   `json:"messageId"`
	CreatorID         string   `json:"creatorId"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	TimeZone          string   `json:"timeZone"`
	ScheduledUnix     int64    `json:"scheduledUnix"`
	AskLateReason     bool     `json:"askLateReason"`
	AskDeclinedReason bool     `json:"askDeclinedReason"`
	AcceptedUserIDs   []string `json:"acceptedUserIds"`
	DeclinedUserIDs   []string `json:"declinedUserIds"`
	LateUserIDs       []string `json:"lateUserIds"`
	EmbedTimestamp    string   `json:"embedTimestamp"`
}

func validateSnowflakeValue(value string) bool {
	return value != "" && snowflakePattern.MatchString(value)
}

func validateWipeReactPayload(w http.ResponseWriter, payload wipeReactPayload, requireMessageID bool) bool {
	if !validateSnowflakeValue(payload.ChannelID) {
		writeError(w, http.StatusBadRequest, "channelId must be a valid Discord snowflake")
		return false
	}
	if requireMessageID && !validateSnowflakeValue(payload.MessageID) {
		writeError(w, http.StatusBadRequest, "messageId must be a valid Discord snowflake")
		return false
	}
	if !validateSnowflakeValue(payload.CreatorID) {
		writeError(w, http.StatusBadRequest, "creatorId must be a valid Discord snowflake")
		return false
	}
	if payload.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return false
	}
	if payload.TimeZone == "" {
		writeError(w, http.StatusBadRequest, "timeZone is required")
		return false
	}
	if payload.ScheduledUnix <= 0 {
		writeError(w, http.StatusBadRequest, "scheduledUnix must be greater than zero")
		return false
	}
	if payload.EmbedTimestamp == "" {
		writeError(w, http.StatusBadRequest, "embedTimestamp is required")
		return false
	}
	reactionUserIDs := append([]string{}, payload.AcceptedUserIDs...)
	reactionUserIDs = append(reactionUserIDs, payload.DeclinedUserIDs...)
	reactionUserIDs = append(reactionUserIDs, payload.LateUserIDs...)
	for _, userID := range reactionUserIDs {
		if !validateSnowflakeValue(userID) {
			writeError(w, http.StatusBadRequest, "reaction user ids must be valid Discord snowflakes")
			return false
		}
	}
	return true
}

func wipeReactFromPayload(guildID string, payload wipeReactPayload) store.WipeReact {
	return store.WipeReact{
		GuildID:           guildID,
		ChannelID:         payload.ChannelID,
		MessageID:         payload.MessageID,
		CreatorID:         payload.CreatorID,
		Name:              payload.Name,
		Description:       payload.Description,
		TimeZone:          payload.TimeZone,
		ScheduledUnix:     payload.ScheduledUnix,
		AskLateReason:     payload.AskLateReason,
		AskDeclinedReason: payload.AskDeclinedReason,
		AcceptedUserIDs:   payload.AcceptedUserIDs,
		DeclinedUserIDs:   payload.DeclinedUserIDs,
		LateUserIDs:       payload.LateUserIDs,
		EmbedTimestamp:    payload.EmbedTimestamp,
	}
}

func (h *Handler) ListActiveWipeReacts(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	records, err := h.postgres.ListActiveWipeReacts(r.Context(), guildID, time.Now().Unix())
	if err != nil {
		log.Printf("[backend] ListActiveWipeReacts error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to list active wipe reacts")
		return
	}

	writeWipeReactList(w, records)
}

func (h *Handler) GetWipeReact(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	messageID, ok := validateSnowflakePathParam(w, r, "messageID")
	if !ok {
		return
	}

	record, err := h.postgres.GetWipeReact(r.Context(), guildID, messageID)
	if errors.Is(err, store.ErrWipeReactNotFound) {
		writeError(w, http.StatusNotFound, "wipe react not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetWipeReact error for guild %s message %s: %v", guildID, messageID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch wipe react")
		return
	}

	writeWipeReact(w, record)
}

func (h *Handler) CreateWipeReact(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	var payload wipeReactPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateWipeReactPayload(w, payload, true) {
		return
	}

	record, err := h.postgres.CreateWipeReact(r.Context(), wipeReactFromPayload(guildID, payload))
	if err != nil {
		log.Printf("[backend] CreateWipeReact error for guild %s message %s: %v", guildID, payload.MessageID, err)
		writeError(w, http.StatusInternalServerError, "failed to create wipe react")
		return
	}

	writeWipeReact(w, record)
}

func (h *Handler) UpdateWipeReact(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	messageID, ok := validateSnowflakePathParam(w, r, "messageID")
	if !ok {
		return
	}

	var payload wipeReactPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	payload.MessageID = messageID
	if !validateWipeReactPayload(w, payload, true) {
		return
	}

	record, err := h.postgres.UpdateWipeReact(r.Context(), guildID, messageID, wipeReactFromPayload(guildID, payload))
	if errors.Is(err, store.ErrWipeReactNotFound) {
		writeError(w, http.StatusNotFound, "wipe react not found")
		return
	}
	if err != nil {
		log.Printf("[backend] UpdateWipeReact error for guild %s message %s: %v", guildID, messageID, err)
		writeError(w, http.StatusInternalServerError, "failed to update wipe react")
		return
	}

	writeWipeReact(w, record)
}

func (h *Handler) DeleteWipeReact(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	messageID, ok := validateSnowflakePathParam(w, r, "messageID")
	if !ok {
		return
	}

	if err := h.postgres.DeleteWipeReact(r.Context(), guildID, messageID); errors.Is(err, store.ErrWipeReactNotFound) {
		writeError(w, http.StatusNotFound, "wipe react not found")
		return
	} else if err != nil {
		log.Printf("[backend] DeleteWipeReact error for guild %s message %s: %v", guildID, messageID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete wipe react")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type setWipeReactReactionRequest struct {
	UserID string `json:"userId"`
	Type   string `json:"type"`
}

func (h *Handler) SetWipeReactReaction(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	messageID, ok := validateSnowflakePathParam(w, r, "messageID")
	if !ok {
		return
	}

	var payload setWipeReactReactionRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateSnowflakeValue(payload.UserID) {
		writeError(w, http.StatusBadRequest, "userId must be a valid Discord snowflake")
		return
	}

	var reactionType store.WipeReactReactionType
	switch payload.Type {
	case string(store.WipeReactReactionAccepted):
		reactionType = store.WipeReactReactionAccepted
	case string(store.WipeReactReactionDeclined):
		reactionType = store.WipeReactReactionDeclined
	case string(store.WipeReactReactionLate):
		reactionType = store.WipeReactReactionLate
	default:
		writeError(w, http.StatusBadRequest, "type must be one of accepted, declined, or late")
		return
	}

	update, err := h.postgres.SetWipeReactReaction(r.Context(), guildID, messageID, payload.UserID, reactionType)
	if errors.Is(err, store.ErrWipeReactNotFound) {
		writeError(w, http.StatusNotFound, "wipe react not found")
		return
	}
	if err != nil {
		log.Printf("[backend] SetWipeReactReaction error for guild %s message %s: %v", guildID, messageID, err)
		writeError(w, http.StatusInternalServerError, "failed to update wipe react reaction")
		return
	}

	writeWipeReactReactionUpdate(w, update)
}

func (h *Handler) RemoveWipeReactReaction(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	messageID, ok := validateSnowflakePathParam(w, r, "messageID")
	if !ok {
		return
	}
	userID, ok := validateSnowflakePathParam(w, r, "userID")
	if !ok {
		return
	}

	removal, err := h.postgres.RemoveWipeReactReaction(r.Context(), guildID, messageID, userID)
	if errors.Is(err, store.ErrWipeReactNotFound) {
		writeError(w, http.StatusNotFound, "wipe react not found")
		return
	}
	if err != nil {
		log.Printf("[backend] RemoveWipeReactReaction error for guild %s message %s user %s: %v", guildID, messageID, userID, err)
		writeError(w, http.StatusInternalServerError, "failed to remove wipe react reaction")
		return
	}

	writeWipeReactReactionRemoval(w, removal)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
