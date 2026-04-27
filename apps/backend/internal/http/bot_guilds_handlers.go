package httpapi

import (
	"encoding/json"
	"log"
	"net/http"

	"rustlens/backend/internal/store"
)

const maxMembersPerUpsertBatch = 1000

type upsertBotGuildRequest struct {
	GuildID                string   `json:"guildId"`
	Name                   string   `json:"name"`
	IconHash               *string  `json:"iconHash,omitempty"`
	BannerHash             *string  `json:"bannerHash,omitempty"`
	OwnerDiscordID         *string  `json:"ownerDiscordId,omitempty"`
	OwnerUsername          *string  `json:"ownerUsername,omitempty"`
	OwnerGlobalName        *string  `json:"ownerGlobalName,omitempty"`
	OwnerAvatarHash        *string  `json:"ownerAvatarHash,omitempty"`
	ApproximateMemberCount *int32   `json:"approximateMemberCount,omitempty"`
	Members                []string `json:"members,omitempty"`
	MembersSyncComplete    bool     `json:"membersSyncComplete,omitempty"`
}

type markBotGuildLeftRequest struct {
	GuildID string `json:"guildId"`
}

type reconcileBotGuildsRequest struct {
	ShardID    int      `json:"shardId"`
	ShardCount int      `json:"shardCount"`
	GuildIDs   []string `json:"guildIds"`
}

func (h *Handler) UpsertBotGuild(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req upsertBotGuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !validateSnowflakeValue(req.GuildID) {
		writeError(w, http.StatusBadRequest, "guildId must be a valid Discord snowflake")
		return
	}
	if len(req.Members) > maxMembersPerUpsertBatch {
		writeError(w, http.StatusBadRequest, "members batch exceeds maximum size")
		return
	}
	for _, m := range req.Members {
		if !validateSnowflakeValue(m) {
			writeError(w, http.StatusBadRequest, "members must contain valid Discord snowflakes")
			return
		}
	}

	if err := h.postgres.UpsertBotGuildMetadata(r.Context(), store.UpsertBotGuildInput{
		GuildID:                req.GuildID,
		Name:                   req.Name,
		IconHash:               req.IconHash,
		BannerHash:             req.BannerHash,
		OwnerDiscordID:         req.OwnerDiscordID,
		OwnerUsername:          req.OwnerUsername,
		OwnerGlobalName:        req.OwnerGlobalName,
		OwnerAvatarHash:        req.OwnerAvatarHash,
		ApproximateMemberCount: req.ApproximateMemberCount,
	}); err != nil {
		log.Printf("[backend] UpsertBotGuildMetadata error for %s: %v", req.GuildID, err)
		writeError(w, http.StatusInternalServerError, "failed to upsert bot guild")
		return
	}

	if len(req.Members) > 0 {
		if err := h.postgres.BulkUpsertUserGuilds(r.Context(), req.GuildID, req.Members); err != nil {
			log.Printf("[backend] BulkUpsertUserGuilds error for %s: %v", req.GuildID, err)
			writeError(w, http.StatusInternalServerError, "failed to upsert members")
			return
		}
	}

	if req.MembersSyncComplete {
		if err := h.postgres.StampMembersSynced(r.Context(), req.GuildID); err != nil {
			log.Printf("[backend] StampMembersSynced error for %s: %v", req.GuildID, err)
			writeError(w, http.StatusInternalServerError, "failed to stamp members_synced_at")
			return
		}
	}

	syncedAt, _, err := h.postgres.MembersSyncedAt(r.Context(), req.GuildID)
	if err != nil {
		log.Printf("[backend] MembersSyncedAt error for %s: %v", req.GuildID, err)
		writeError(w, http.StatusInternalServerError, "failed to read members_synced_at")
		return
	}

	resp := map[string]any{"status": "ok", "membersSyncedAt": nil}
	if syncedAt != nil {
		resp["membersSyncedAt"] = syncedAt.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) MarkBotGuildLeft(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req markBotGuildLeftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateSnowflakeValue(req.GuildID) {
		writeError(w, http.StatusBadRequest, "guildId must be a valid Discord snowflake")
		return
	}
	if err := h.postgres.MarkBotGuildLeft(r.Context(), req.GuildID); err != nil {
		log.Printf("[backend] MarkBotGuildLeft error for %s: %v", req.GuildID, err)
		writeError(w, http.StatusInternalServerError, "failed to mark guild left")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ReconcileBotGuilds(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req reconcileBotGuildsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ShardCount <= 0 || req.ShardID < 0 || req.ShardID >= req.ShardCount {
		writeError(w, http.StatusBadRequest, "invalid shardId/shardCount")
		return
	}
	for _, id := range req.GuildIDs {
		if !validateSnowflakeValue(id) {
			writeError(w, http.StatusBadRequest, "guildIds must contain valid Discord snowflakes")
			return
		}
	}
	if err := h.postgres.ReconcileShardBotGuilds(r.Context(), req.ShardID, req.ShardCount, req.GuildIDs); err != nil {
		log.Printf("[backend] ReconcileShardBotGuilds error shard=%d/%d: %v", req.ShardID, req.ShardCount, err)
		writeError(w, http.StatusInternalServerError, "failed to reconcile bot guilds")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
