package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/auth"
	"rustlens/backend/internal/config"
	"rustlens/backend/internal/shards"
	"rustlens/backend/internal/store"
)

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
		response["db"] = dbErr.Error()
	}

	if redisErr != nil {
		status = http.StatusServiceUnavailable
		response["status"] = "degraded"
		response["redis"] = redisErr.Error()
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

func (h *Handler) GetGuildSetupState(w http.ResponseWriter, r *http.Request) {
	guildID := chi.URLParam(r, "guildID")
	if guildID == "" {
		writeError(w, http.StatusBadRequest, "guildID is required")
		return
	}

	state, err := h.postgres.GetGuildSetupState(r.Context(), guildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"guildId":       state.GuildID,
		"status":        state.Status,
		"configuredBy":  state.ConfiguredBy,
		"selectedRoles": state.SelectedRoles,
	})
}

func (h *Handler) ClaimGuildSetup(w http.ResponseWriter, r *http.Request) {
	guildID := chi.URLParam(r, "guildID")
	if guildID == "" {
		writeError(w, http.StatusBadRequest, "guildID is required")
		return
	}

	claimed, state, err := h.postgres.ClaimGuildSetup(r.Context(), guildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"claimed":       claimed,
		"guildId":       state.GuildID,
		"status":        state.Status,
		"configuredBy":  state.ConfiguredBy,
		"selectedRoles": state.SelectedRoles,
	})
}

type completeGuildSetupRequest struct {
	ConfiguredBy  string   `json:"configuredBy"`
	SelectedRoles []string `json:"selectedRoles"`
}

func (h *Handler) CompleteGuildSetup(w http.ResponseWriter, r *http.Request) {
	guildID := chi.URLParam(r, "guildID")
	if guildID == "" {
		writeError(w, http.StatusBadRequest, "guildID is required")
		return
	}

	var request completeGuildSetupRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.postgres.CompleteGuildSetup(r.Context(), guildID, request.ConfiguredBy, request.SelectedRoles); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	state, err := h.postgres.GetGuildSetupState(r.Context(), guildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"guildId":       state.GuildID,
		"status":        state.Status,
		"configuredBy":  state.ConfiguredBy,
		"selectedRoles": state.SelectedRoles,
	})
}

func (h *Handler) ResetGuildSetup(w http.ResponseWriter, r *http.Request) {
	guildID := chi.URLParam(r, "guildID")
	if guildID == "" {
		writeError(w, http.StatusBadRequest, "guildID is required")
		return
	}

	if err := h.postgres.ResetGuildSetup(r.Context(), guildID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
