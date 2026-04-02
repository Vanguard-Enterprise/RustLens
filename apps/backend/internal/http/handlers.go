package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/auth"
	"rustlens/backend/internal/config"
	"rustlens/backend/internal/shards"
	"rustlens/backend/internal/steamapi"
	"rustlens/backend/internal/store"
)

var snowflakePattern = regexp.MustCompile(`^\d{17,20}$`)
var steamIDPattern = regexp.MustCompile(`^\d{17}$`)

const maxRequestBodyBytes = 1 << 20 // 1 MB

type Handler struct {
	cfg          config.Config
	postgres     *store.PostgresStore
	redis        *store.RedisStore
	shardService *shards.Service
	jwtValidator *auth.Validator
	steamClient  *steamapi.Client
}

func NewHandler(cfg config.Config, postgres *store.PostgresStore, redis *store.RedisStore, shardService *shards.Service, steamClient *steamapi.Client) *Handler {
	return &Handler{
		cfg:          cfg,
		postgres:     postgres,
		redis:        redis,
		shardService: shardService,
		jwtValidator: auth.NewValidator(cfg.BotServiceJWT, cfg.AllowedJWTIssuer, cfg.AllowedJWTAudience),
		steamClient:  steamClient,
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

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
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

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
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

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
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

func writeGuildSettings(w http.ResponseWriter, settings store.GuildSettings) {
	writeJSON(w, http.StatusOK, map[string]any{
		"guildId":                   settings.GuildID,
		"wipeReactLogChannelId":     settings.WipeReactLogChannelID,
		"linkingLogChannelId":       settings.LinkingLogChannelID,
		"trackedServerLogChannelId": settings.TrackedServerLogChannelID,
		"alertsChannelId":           settings.AlertsChannelID,
		"adminRoleId":               settings.AdminRoleID,
	})
}

func trackedServerToMap(item store.TrackedServer) map[string]any {
	result := map[string]any{
		"bmId": item.BmID,
		"name": item.Name,
	}
	if item.LastPolledAt != nil {
		result["lastPolledAt"] = *item.LastPolledAt
	} else {
		result["lastPolledAt"] = nil
	}
	result["lastPollError"] = item.LastPollError
	return result
}

func guildTrackedServerToMap(item store.GuildTrackedServer) map[string]any {
	return map[string]any{
		"guildId": item.GuildID,
		"bmId":    item.BmID,
		"name":    item.Name,
	}
}

func writeTrackedServerList(w http.ResponseWriter, items []store.TrackedServer) {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, trackedServerToMap(item))
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func writeGuildTrackedServerList(w http.ResponseWriter, items []store.GuildTrackedServer) {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, guildTrackedServerToMap(item))
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func playerStateToMap(item store.PlayerState) map[string]any {
	result := map[string]any{
		"serverBmId": item.ServerBmID,
		"serverName": item.ServerName,
		"playerBmId": item.PlayerBmID,
		"isOnline":   item.IsOnline,
		"updatedAt":  item.UpdatedAt,
	}
	if item.OnlineSince != nil {
		result["onlineSince"] = *item.OnlineSince
	} else {
		result["onlineSince"] = nil
	}
	if item.OfflineSince != nil {
		result["offlineSince"] = *item.OfflineSince
	} else {
		result["offlineSince"] = nil
	}
	return result
}

func writePlayerStateList(w http.ResponseWriter, items []store.PlayerState) {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, playerStateToMap(item))
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func wipeReactToMap(record store.WipeReact) map[string]any {
	return map[string]any{
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
	}
}

func writeWipeReact(w http.ResponseWriter, record store.WipeReact) {
	writeJSON(w, http.StatusOK, wipeReactToMap(record))
}

func writeWipeReactList(w http.ResponseWriter, records []store.WipeReact) {
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, wipeReactToMap(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func writeWipeReactReactionUpdate(w http.ResponseWriter, update store.WipeReactReactionUpdate) {
	response := map[string]any{
		"changed":   update.Changed,
		"wipeReact": wipeReactToMap(update.WipeReact),
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
		"changed":   removal.Changed,
		"wipeReact": wipeReactToMap(removal.WipeReact),
	}
	if removal.RemovedType != nil {
		response["removedType"] = string(*removal.RemovedType)
	} else {
		response["removedType"] = nil
	}
	writeJSON(w, http.StatusOK, response)
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

type updateGuildSettingsRequest struct {
	WipeReactLogChannelID     *string `json:"wipeReactLogChannelId"`
	LinkingLogChannelID       *string `json:"linkingLogChannelId"`
	TrackedServerLogChannelID *string `json:"trackedServerLogChannelId"`
	AlertsChannelID           *string `json:"alertsChannelId"`
	AdminRoleID               *string `json:"adminRoleId"`
}

func validateSnowflakeOptionalField(w http.ResponseWriter, value *string, fieldName string) bool {
	if value == nil {
		return true
	}

	if *value == "" {
		return true
	}

	if !snowflakePattern.MatchString(*value) {
		writeError(w, http.StatusBadRequest, fieldName+" must be a valid Discord snowflake")
		return false
	}

	return true
}

func (h *Handler) UpdateGuildSettings(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request updateGuildSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !validateSnowflakeOptionalField(w, request.WipeReactLogChannelID, "wipeReactLogChannelId") {
		return
	}
	if !validateSnowflakeOptionalField(w, request.LinkingLogChannelID, "linkingLogChannelId") {
		return
	}
	if !validateSnowflakeOptionalField(w, request.TrackedServerLogChannelID, "trackedServerLogChannelId") {
		return
	}
	if !validateSnowflakeOptionalField(w, request.AlertsChannelID, "alertsChannelId") {
		return
	}
	if !validateSnowflakeOptionalField(w, request.AdminRoleID, "adminRoleId") {
		return
	}

	settings, err := h.postgres.UpdateGuildSettings(r.Context(), guildID, store.GuildSettingsPatch{
		WipeReactLogChannelID:     request.WipeReactLogChannelID,
		LinkingLogChannelID:       request.LinkingLogChannelID,
		TrackedServerLogChannelID: request.TrackedServerLogChannelID,
		AlertsChannelID:           request.AlertsChannelID,
		AdminRoleID:               request.AdminRoleID,
	})
	if err != nil {
		log.Printf("[backend] UpdateGuildSettings error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to save guild settings")
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

	const maxReactionUserIDs = 10000

	totalReactionUserIDs := len(payload.AcceptedUserIDs) + len(payload.DeclinedUserIDs) + len(payload.LateUserIDs)
	if totalReactionUserIDs > maxReactionUserIDs {
		writeError(w, http.StatusBadRequest, "reaction user id lists exceed maximum allowed size")
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

func (h *Handler) ListWipeReacts(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	records, err := h.postgres.ListWipeReacts(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListWipeReacts error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to list wipe reacts")
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

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
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

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
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

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
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

func wipeReactScheduleToMap(record store.WipeReactSchedule) map[string]any {
	return map[string]any{
		"id":                record.ID,
		"guildId":           record.GuildID,
		"days":              record.Days,
		"includesForceWipe": record.IncludesForceWipe,
		"time":              record.Time,
		"timeZone":          record.TimeZone,
		"repeat":            record.Repeat,
		"postBefore":        record.PostBefore,
		"name":              record.Name,
		"description":       record.Description,
		"askLateReason":     record.AskLateReason,
		"askDeclinedReason": record.AskDeclinedReason,
		"channelId":         record.ChannelID,
		"lastPostedForUnix": record.LastPostedForUnix,
		"createdBy":         record.CreatedBy,
	}
}

func validateScheduleIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "scheduleID")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "scheduleID must be a positive integer")
		return 0, false
	}
	return id, true
}

var timePattern = regexp.MustCompile(`^\d{2}:\d{2}$`)

type wipeReactSchedulePayload struct {
	Days              []string `json:"days"`
	IncludesForceWipe bool     `json:"includesForceWipe"`
	Time              string   `json:"time"`
	TimeZone          string   `json:"timeZone"`
	Repeat            string   `json:"repeat"`
	PostBefore        string   `json:"postBefore"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	AskLateReason     bool     `json:"askLateReason"`
	AskDeclinedReason bool     `json:"askDeclinedReason"`
	ChannelID         string   `json:"channelId"`
	CreatedBy         string   `json:"createdBy"`
}

var validDays = map[string]bool{
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
	"friday": true, "saturday": true, "sunday": true,
}

var validPostBefore = map[string]bool{
	"1d": true, "2d": true, "3d": true, "4d": true, "1w": true, "2w": true,
}

var validRepeat = map[string]bool{
	"weekly": true, "biweekly": true, "monthly": true,
}

func validateWipeReactSchedulePayload(w http.ResponseWriter, payload wipeReactSchedulePayload) bool {
	for _, day := range payload.Days {
		if !validDays[day] {
			writeError(w, http.StatusBadRequest, "each day must be one of monday, tuesday, wednesday, thursday, friday, saturday, sunday")
			return false
		}
	}

	if payload.TimeZone == "" {
		writeError(w, http.StatusBadRequest, "timeZone is required")
		return false
	}

	if !validPostBefore[payload.PostBefore] {
		writeError(w, http.StatusBadRequest, "postBefore must be one of 1d, 2d, 3d, 4d, 1w, 2w")
		return false
	}

	if !validateSnowflakeValue(payload.CreatedBy) {
		writeError(w, http.StatusBadRequest, "createdBy must be a valid Discord snowflake")
		return false
	}

	if !payload.IncludesForceWipe && len(payload.Days) == 0 {
		writeError(w, http.StatusBadRequest, "days must not be empty when includesForceWipe is false")
		return false
	}

	if len(payload.Days) > 0 {
		if !timePattern.MatchString(payload.Time) {
			writeError(w, http.StatusBadRequest, "time must match HH:MM format")
			return false
		}
		if !validRepeat[payload.Repeat] {
			writeError(w, http.StatusBadRequest, "repeat must be one of weekly, biweekly, monthly")
			return false
		}
	}

	return true
}

func wipeReactScheduleFromPayload(guildID string, payload wipeReactSchedulePayload) store.WipeReactSchedule {
	days := payload.Days
	if days == nil {
		days = []string{}
	}
	return store.WipeReactSchedule{
		GuildID:           guildID,
		Days:              days,
		IncludesForceWipe: payload.IncludesForceWipe,
		Time:              payload.Time,
		TimeZone:          payload.TimeZone,
		Repeat:            payload.Repeat,
		PostBefore:        payload.PostBefore,
		Name:              payload.Name,
		Description:       payload.Description,
		AskLateReason:     payload.AskLateReason,
		AskDeclinedReason: payload.AskDeclinedReason,
		ChannelID:         payload.ChannelID,
		CreatedBy:         payload.CreatedBy,
	}
}

func (h *Handler) ListWipeReactSchedules(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	records, err := h.postgres.ListWipeReactSchedules(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListWipeReactSchedules error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to list wipe react schedules")
		return
	}

	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, wipeReactScheduleToMap(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) GetWipeReactSchedule(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	scheduleID, ok := validateScheduleIDParam(w, r)
	if !ok {
		return
	}

	record, err := h.postgres.GetWipeReactSchedule(r.Context(), guildID, scheduleID)
	if errors.Is(err, store.ErrWipeReactScheduleNotFound) {
		writeError(w, http.StatusNotFound, "wipe react schedule not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetWipeReactSchedule error for guild %s schedule %d: %v", guildID, scheduleID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch wipe react schedule")
		return
	}

	writeJSON(w, http.StatusOK, wipeReactScheduleToMap(record))
}

func (h *Handler) CreateWipeReactSchedule(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var payload wipeReactSchedulePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateWipeReactSchedulePayload(w, payload) {
		return
	}

	record, err := h.postgres.CreateWipeReactSchedule(r.Context(), wipeReactScheduleFromPayload(guildID, payload))
	if errors.Is(err, store.ErrWipeReactScheduleLimitReached) {
		writeError(w, http.StatusConflict, "wipe react schedule limit reached")
		return
	}
	if err != nil {
		log.Printf("[backend] CreateWipeReactSchedule error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to create wipe react schedule")
		return
	}

	writeJSON(w, http.StatusOK, wipeReactScheduleToMap(record))
}

func (h *Handler) UpdateWipeReactSchedule(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	scheduleID, ok := validateScheduleIDParam(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var payload wipeReactSchedulePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateWipeReactSchedulePayload(w, payload) {
		return
	}

	record, err := h.postgres.UpdateWipeReactSchedule(r.Context(), guildID, scheduleID, wipeReactScheduleFromPayload(guildID, payload))
	if errors.Is(err, store.ErrWipeReactScheduleNotFound) {
		writeError(w, http.StatusNotFound, "wipe react schedule not found")
		return
	}
	if err != nil {
		log.Printf("[backend] UpdateWipeReactSchedule error for guild %s schedule %d: %v", guildID, scheduleID, err)
		writeError(w, http.StatusInternalServerError, "failed to update wipe react schedule")
		return
	}

	writeJSON(w, http.StatusOK, wipeReactScheduleToMap(record))
}

func (h *Handler) DeleteWipeReactSchedule(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	scheduleID, ok := validateScheduleIDParam(w, r)
	if !ok {
		return
	}

	if err := h.postgres.DeleteWipeReactSchedule(r.Context(), guildID, scheduleID); errors.Is(err, store.ErrWipeReactScheduleNotFound) {
		writeError(w, http.StatusNotFound, "wipe react schedule not found")
		return
	} else if err != nil {
		log.Printf("[backend] DeleteWipeReactSchedule error for guild %s schedule %d: %v", guildID, scheduleID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete wipe react schedule")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) MarkWipeReactSchedulePosted(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	scheduleID, ok := validateScheduleIDParam(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var payload struct {
		LastPostedForUnix int64 `json:"lastPostedForUnix"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if payload.LastPostedForUnix <= 0 {
		writeError(w, http.StatusBadRequest, "lastPostedForUnix must be a positive integer")
		return
	}

	if err := h.postgres.MarkWipeReactSchedulePosted(r.Context(), guildID, scheduleID, payload.LastPostedForUnix); errors.Is(err, store.ErrWipeReactScheduleNotFound) {
		writeError(w, http.StatusNotFound, "wipe react schedule not found")
		return
	} else if err != nil {
		log.Printf("[backend] MarkWipeReactSchedulePosted error for guild %s schedule %d: %v", guildID, scheduleID, err)
		writeError(w, http.StatusInternalServerError, "failed to mark schedule as posted")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type createTrackedServerRequest struct {
	BmID string `json:"bmId"`
	Name string `json:"name"`
}

func (h *Handler) ListTrackedServers(w http.ResponseWriter, r *http.Request) {
	items, err := h.postgres.ListTrackedServers(r.Context())
	if err != nil {
		log.Printf("[backend] ListTrackedServers error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list tracked servers")
		return
	}

	writeTrackedServerList(w, items)
}

func (h *Handler) CreateTrackedServer(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request createTrackedServerRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	request.BmID = strings.TrimSpace(request.BmID)
	request.Name = strings.Join(strings.Fields(request.Name), " ")

	if request.BmID == "" {
		writeError(w, http.StatusBadRequest, "bmId is required")
		return
	}
	if request.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	item, err := h.postgres.CreateTrackedServer(r.Context(), request.BmID, request.Name)
	if errors.Is(err, store.ErrTrackedServerAlreadyExists) {
		writeError(w, http.StatusConflict, "tracked server already exists")
		return
	}
	if err != nil {
		log.Printf("[backend] CreateTrackedServer error for %s: %v", request.BmID, err)
		writeError(w, http.StatusInternalServerError, "failed to create tracked server")
		return
	}

	writeJSON(w, http.StatusCreated, trackedServerToMap(item))
}

func (h *Handler) DeleteTrackedServer(w http.ResponseWriter, r *http.Request) {
	bmID := strings.TrimSpace(chi.URLParam(r, "bmID"))
	if bmID == "" {
		writeError(w, http.StatusBadRequest, "bmID is required")
		return
	}

	if err := h.postgres.DeleteTrackedServer(r.Context(), bmID); errors.Is(err, store.ErrTrackedServerNotFound) {
		writeError(w, http.StatusNotFound, "tracked server not found")
		return
	} else if err != nil {
		log.Printf("[backend] DeleteTrackedServer error for %s: %v", bmID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete tracked server")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type replaceGuildTrackedServersRequest struct {
	BmIDs []string `json:"bmIds"`
}

func (h *Handler) ListGuildTrackedServers(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	items, err := h.postgres.ListGuildTrackedServers(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListGuildTrackedServers error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to list guild tracked servers")
		return
	}

	writeGuildTrackedServerList(w, items)
}

func (h *Handler) ReplaceGuildTrackedServers(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request replaceGuildTrackedServersRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(request.BmIDs) > 100 {
		writeError(w, http.StatusBadRequest, "bmIds exceeds maximum allowed size")
		return
	}

	seen := make(map[string]struct{}, len(request.BmIDs))
	bmIDs := make([]string, 0, len(request.BmIDs))
	for _, bmID := range request.BmIDs {
		trimmed := strings.TrimSpace(bmID)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "bmIds must not contain empty values")
			return
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		bmIDs = append(bmIDs, trimmed)
	}

	if len(bmIDs) > 0 {
		trackedServers, err := h.postgres.ListTrackedServers(r.Context())
		if err != nil {
			log.Printf("[backend] ListTrackedServers validation error for guild %s: %v", guildID, err)
			writeError(w, http.StatusInternalServerError, "failed to validate tracked servers")
			return
		}

		available := make(map[string]struct{}, len(trackedServers))
		for _, item := range trackedServers {
			available[item.BmID] = struct{}{}
		}

		for _, bmID := range bmIDs {
			if _, ok := available[bmID]; !ok {
				writeError(w, http.StatusBadRequest, "bmIds must reference existing tracked servers")
				return
			}
		}
	}

	if err := h.postgres.ReplaceGuildTrackedServers(r.Context(), guildID, bmIDs); err != nil {
		log.Printf("[backend] ReplaceGuildTrackedServers error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to save guild tracked servers")
		return
	}

	items, err := h.postgres.ListGuildTrackedServers(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListGuildTrackedServers (post-update) error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to list guild tracked servers")
		return
	}

	writeGuildTrackedServerList(w, items)
}

func (h *Handler) ListOnlinePlayerStatesByServer(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	if serverID == "" {
		writeError(w, http.StatusBadRequest, "serverID is required")
		return
	}

	items, err := h.postgres.ListOnlinePlayerStatesByServer(r.Context(), serverID)
	if err != nil {
		log.Printf("[backend] ListOnlinePlayerStatesByServer error for %s: %v", serverID, err)
		writeError(w, http.StatusInternalServerError, "failed to list player states")
		return
	}

	writePlayerStateList(w, items)
}

func (h *Handler) ListPlayerStatesByPlayer(w http.ResponseWriter, r *http.Request) {
	playerID := strings.TrimSpace(chi.URLParam(r, "playerID"))
	if playerID == "" {
		writeError(w, http.StatusBadRequest, "playerID is required")
		return
	}

	items, err := h.postgres.ListPlayerStatesByPlayer(r.Context(), playerID)
	if err != nil {
		log.Printf("[backend] ListPlayerStatesByPlayer error for %s: %v", playerID, err)
		writeError(w, http.StatusInternalServerError, "failed to list player states")
		return
	}

	writePlayerStateList(w, items)
}

func (h *Handler) GetPlayerState(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	if serverID == "" {
		writeError(w, http.StatusBadRequest, "serverID is required")
		return
	}

	playerID := strings.TrimSpace(chi.URLParam(r, "playerID"))
	if playerID == "" {
		writeError(w, http.StatusBadRequest, "playerID is required")
		return
	}

	item, err := h.postgres.GetPlayerState(r.Context(), serverID, playerID)
	if errors.Is(err, store.ErrPlayerStateNotFound) {
		writeError(w, http.StatusNotFound, "player state not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetPlayerState error for %s/%s: %v", serverID, playerID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch player state")
		return
	}

	writeJSON(w, http.StatusOK, playerStateToMap(item))
}

func steamAccountToMap(record store.SteamAccount) map[string]any {
	result := map[string]any{
		"steamId":     record.SteamID,
		"bmName":      record.BmName,
		"avatarImage": record.AvatarImage,
		"createdAt":   record.CreatedAt,
	}
	if record.BmID != nil {
		result["bmId"] = *record.BmID
	} else {
		result["bmId"] = nil
	}
	return result
}

func steamDiscordLinkToMap(link store.SteamDiscordLink) map[string]any {
	return map[string]any{
		"steamId":   link.SteamID,
		"discordId": link.DiscordID,
		"type":      link.Type,
		"source":    link.Source,
		"createdAt": link.CreatedAt,
	}
}

func writeSteamAccount(w http.ResponseWriter, record store.SteamAccount) {
	writeJSON(w, http.StatusOK, steamAccountToMap(record))
}

func writeSteamDiscordLinkList(w http.ResponseWriter, links []store.SteamDiscordLink) {
	items := make([]map[string]any, 0, len(links))
	for _, link := range links {
		items = append(items, steamDiscordLinkToMap(link))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) GetSteamAccount(w http.ResponseWriter, r *http.Request) {
	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	account, err := h.postgres.GetSteamAccountBySteamID(r.Context(), steamID)
	if errors.Is(err, store.ErrSteamAccountNotFound) {
		writeError(w, http.StatusNotFound, "steam account not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetSteamAccount error for %s: %v", steamID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch steam account")
		return
	}

	writeSteamAccount(w, account)
}

func (h *Handler) GetSteamAccountByBmID(w http.ResponseWriter, r *http.Request) {
	bmID := chi.URLParam(r, "bmID")
	if bmID == "" {
		writeError(w, http.StatusBadRequest, "bmID is required")
		return
	}

	account, err := h.postgres.GetSteamAccountByBmID(r.Context(), bmID)
	if errors.Is(err, store.ErrSteamAccountNotFound) {
		writeError(w, http.StatusNotFound, "steam account not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetSteamAccountByBmID error for %s: %v", bmID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch steam account")
		return
	}

	writeSteamAccount(w, account)
}

func (h *Handler) ListLinksBySteamID(w http.ResponseWriter, r *http.Request) {
	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	links, err := h.postgres.ListLinksBySteamID(r.Context(), steamID)
	if err != nil {
		log.Printf("[backend] ListLinksBySteamID error for %s: %v", steamID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch links")
		return
	}

	writeSteamDiscordLinkList(w, links)
}

func (h *Handler) ListLinksByDiscordID(w http.ResponseWriter, r *http.Request) {
	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	links, err := h.postgres.ListLinksByDiscordID(r.Context(), discordID)
	if err != nil {
		log.Printf("[backend] ListLinksByDiscordID error for %s: %v", discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch links")
		return
	}

	writeSteamDiscordLinkList(w, links)
}

type createSoftLinkRequest struct {
	SteamID   string `json:"steamId"`
	DiscordID string `json:"discordId"`
	Source    string `json:"source"`
}

type resolveSteamIDRequest struct {
	Input string `json:"input"`
}

func (h *Handler) ResolveSteamID(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request resolveSteamIDRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	steamID, err := h.steamClient.ResolveSteamID(r.Context(), request.Input)
	if errors.Is(err, steamapi.ErrUnsupportedProfileInput) || errors.Is(err, steamapi.ErrVanityNotFound) || errors.Is(err, steamapi.ErrSteamAPIKeyRequired) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		log.Printf("[backend] ResolveSteamID error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to resolve steam id")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"steamId": steamID})
}

func (h *Handler) CreateSoftLink(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request createSoftLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.SteamID == "" || !steamIDPattern.MatchString(request.SteamID) {
		writeError(w, http.StatusBadRequest, "steamId must be a 17-digit number")
		return
	}
	if !validateSnowflakeValue(request.DiscordID) {
		writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
		return
	}
	if request.Source == "" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
	}

	link, err := h.postgres.CreateSteamDiscordLink(r.Context(), store.SteamDiscordLink{
		SteamID:   request.SteamID,
		DiscordID: request.DiscordID,
		Type:      "soft",
		Source:    request.Source,
	})
	if errors.Is(err, store.ErrLinkAlreadyExists) {
		writeError(w, http.StatusConflict, "soft link already exists for this steam and discord pair")
		return
	}
	if err != nil {
		log.Printf("[backend] CreateSoftLink error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create soft link")
		return
	}

	writeJSON(w, http.StatusCreated, steamDiscordLinkToMap(link))
}

type bulkHardLinkEntry struct {
	SteamID   string  `json:"steamId"`
	BmID      *string `json:"bmId"`
	DiscordID string  `json:"discordId"`
	Source    string  `json:"source"`
}

type bulkHardLinksRequest struct {
	Links []bulkHardLinkEntry `json:"links"`
}

func (h *Handler) BulkUpsertHardLinks(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request bulkHardLinksRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(request.Links) == 0 {
		writeError(w, http.StatusBadRequest, "links array must not be empty")
		return
	}

	storeLinks := make([]store.BulkHardLink, 0, len(request.Links))
	for i, entry := range request.Links {
		if entry.SteamID == "" || !steamIDPattern.MatchString(entry.SteamID) {
			writeError(w, http.StatusBadRequest, "links["+strconv.Itoa(i)+"].steamId must be a 17-digit number")
			return
		}
		if !validateSnowflakeValue(entry.DiscordID) {
			writeError(w, http.StatusBadRequest, "links["+strconv.Itoa(i)+"].discordId must be a valid Discord snowflake")
			return
		}
		if entry.Source == "" {
			writeError(w, http.StatusBadRequest, "links["+strconv.Itoa(i)+"].source is required")
			return
		}
		storeLinks = append(storeLinks, store.BulkHardLink{
			SteamID:   entry.SteamID,
			BmID:      entry.BmID,
			DiscordID: entry.DiscordID,
			Source:    entry.Source,
		})
	}

	if err := h.postgres.BulkUpsertHardLinks(r.Context(), storeLinks); err != nil {
		log.Printf("[backend] BulkUpsertHardLinks error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to upsert hard links")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "count": len(storeLinks)})
}

func (h *Handler) DeleteSoftLink(w http.ResponseWriter, r *http.Request) {
	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	err := h.postgres.DeleteSoftLink(r.Context(), steamID, discordID)
	if errors.Is(err, store.ErrSteamAccountNotFound) {
		writeError(w, http.StatusNotFound, "soft link not found")
		return
	}
	if err != nil {
		log.Printf("[backend] DeleteSoftLink error for %s/%s: %v", steamID, discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete soft link")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func guildSteamDiscordLinkToMap(link store.GuildSteamDiscordLink) map[string]any {
	return map[string]any{
		"guildId":   link.GuildID,
		"discordId": link.DiscordID,
		"steamId":   link.SteamID,
		"createdAt": link.CreatedAt,
	}
}

func writeGuildSteamDiscordLinkList(w http.ResponseWriter, links []store.GuildSteamDiscordLink) {
	items := make([]map[string]any, 0, len(links))
	for _, link := range links {
		items = append(items, guildSteamDiscordLinkToMap(link))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func guildOnlineSummaryItemToMap(item store.GuildOnlineSummaryItem) map[string]any {
	return map[string]any{
		"guildId":    item.GuildID,
		"discordId":  item.DiscordID,
		"steamId":    item.SteamID,
		"playerBmId": item.PlayerBmID,
		"bmName":     item.BmName,
		"serverBmId": item.ServerBmID,
		"serverName": item.ServerName,
		"onlineUnix": item.OnlineUnix,
	}
}

func writeGuildOnlineSummaryList(w http.ResponseWriter, items []store.GuildOnlineSummaryItem) {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, guildOnlineSummaryItemToMap(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

type createGuildLinkRequest struct {
	DiscordID string `json:"discordId"`
	SteamID   string `json:"steamId"`
}

func (h *Handler) CreateGuildSteamDiscordLink(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request createGuildLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !validateSnowflakeValue(request.DiscordID) {
		writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
		return
	}
	if request.SteamID == "" || !steamIDPattern.MatchString(request.SteamID) {
		writeError(w, http.StatusBadRequest, "steamId must be a 17-digit number")
		return
	}

	link, err := h.postgres.CreateGuildSteamDiscordLink(r.Context(), guildID, request.DiscordID, request.SteamID)
	if errors.Is(err, store.ErrLinkAlreadyExists) {
		writeError(w, http.StatusConflict, "guild link already exists")
		return
	}
	if err != nil {
		log.Printf("[backend] CreateGuildSteamDiscordLink error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create guild link")
		return
	}

	writeJSON(w, http.StatusCreated, guildSteamDiscordLinkToMap(link))
}

func (h *Handler) ListGuildLinks(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	links, err := h.postgres.ListGuildLinks(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListGuildLinks error for %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild links")
		return
	}

	writeGuildSteamDiscordLinkList(w, links)
}

func (h *Handler) ListGuildOnlineSummary(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	items, err := h.postgres.ListGuildOnlineSummary(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListGuildOnlineSummary error for %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild online summary")
		return
	}

	writeGuildOnlineSummaryList(w, items)
}

func (h *Handler) ListGuildLinksByDiscord(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	links, err := h.postgres.ListGuildLinksByGuildAndDiscord(r.Context(), guildID, discordID)
	if err != nil {
		log.Printf("[backend] ListGuildLinksByDiscord error for %s/%s: %v", guildID, discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild links")
		return
	}

	writeGuildSteamDiscordLinkList(w, links)
}

func (h *Handler) ListGuildLinksBySteam(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	links, err := h.postgres.ListGuildLinksByGuildAndSteam(r.Context(), guildID, steamID)
	if err != nil {
		log.Printf("[backend] ListGuildLinksBySteam error for %s/%s: %v", guildID, steamID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild links")
		return
	}

	writeGuildSteamDiscordLinkList(w, links)
}

func (h *Handler) DeleteGuildSteamDiscordLink(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	err := h.postgres.DeleteGuildSteamDiscordLink(r.Context(), guildID, discordID, steamID)
	if errors.Is(err, store.ErrGuildLinkNotFound) {
		writeError(w, http.StatusNotFound, "guild link not found")
		return
	}
	if err != nil {
		log.Printf("[backend] DeleteGuildSteamDiscordLink error for %s/%s/%s: %v", guildID, discordID, steamID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete guild link")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
