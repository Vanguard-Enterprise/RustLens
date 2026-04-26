package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/store"
)

type trackGroupCreateRequest struct {
	Name        string `json:"name"`
	ServerBmID  string `json:"serverBmId"`
	ChannelID   string `json:"channelId"`
	MessageID   string `json:"messageId"`
	SendUpdates bool   `json:"sendUpdates"`
	OnlineCount int    `json:"onlineCount"`
	CreatedBy   string `json:"createdBy"`
}

type trackGroupUpdateRequest struct {
	ChannelID   *string `json:"channelId"`
	MessageID   *string `json:"messageId"`
	SendUpdates *bool   `json:"sendUpdates"`
	OnlineCount *int    `json:"onlineCount"`
}

type trackGroupAddMembersRequest struct {
	Members []trackGroupMemberPayload `json:"members"`
}

type trackGroupMemberPayload struct {
	SteamID string `json:"steamId"`
	BmID    string `json:"bmId"`
}

type trackGroupRemoveMembersRequest struct {
	Identifiers []string `json:"identifiers"`
}

func trackGroupToMap(group store.TrackGroup) map[string]any {
	return map[string]any{
		"id":          group.ID,
		"guildId":     group.GuildID,
		"name":        group.Name,
		"serverBmId":  group.ServerBmID,
		"channelId":   group.ChannelID,
		"messageId":   group.MessageID,
		"sendUpdates": group.SendUpdates,
		"onlineCount": group.OnlineCount,
		"createdBy":   group.CreatedBy,
		"createdAt":   group.CreatedAt,
		"updatedAt":   group.UpdatedAt,
	}
}

func trackGroupMemberToMap(member store.TrackGroupMember) map[string]any {
	return map[string]any{
		"trackGroupId": member.TrackGroupID,
		"steamId":      member.SteamID,
		"bmId":         member.BmID,
		"createdAt":    member.CreatedAt,
	}
}

func trackGroupOnlineRowToMap(row store.TrackGroupOnlineRow) map[string]any {
	return map[string]any{
		"steamId":     row.SteamID,
		"bmId":        row.BmID,
		"bmName":      row.BmName,
		"discordId":   row.DiscordID,
		"avatarImage": row.AvatarImage,
		"isOnline":    row.IsOnline,
		"onlineUnix":  row.OnlineUnix,
	}
}

func parseTrackGroupID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "groupID")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "groupID must be a positive integer")
		return 0, false
	}
	return id, true
}

func (h *Handler) CreateTrackGroup(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req trackGroupCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.ServerBmID == "" {
		writeError(w, http.StatusBadRequest, "serverBmId is required")
		return
	}

	saved, err := h.postgres.CreateTrackGroup(r.Context(), store.TrackGroup{
		GuildID:     guildID,
		Name:        req.Name,
		ServerBmID:  req.ServerBmID,
		ChannelID:   req.ChannelID,
		MessageID:   req.MessageID,
		SendUpdates: req.SendUpdates,
		OnlineCount: req.OnlineCount,
		CreatedBy:   req.CreatedBy,
	})
	if errors.Is(err, store.ErrTrackGroupNameTaken) {
		writeError(w, http.StatusConflict, "track group with that name already exists")
		return
	}
	if err != nil {
		log.Printf("[backend] CreateTrackGroup error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to create track group")
		return
	}

	writeJSON(w, http.StatusOK, trackGroupToMap(saved))
}

func (h *Handler) ListTrackGroups(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	items, err := h.postgres.ListTrackGroups(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListTrackGroups error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to list track groups")
		return
	}

	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, trackGroupToMap(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func (h *Handler) GetTrackGroup(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	id, ok := parseTrackGroupID(w, r)
	if !ok {
		return
	}

	group, err := h.postgres.GetTrackGroup(r.Context(), guildID, id)
	if errors.Is(err, store.ErrTrackGroupNotFound) {
		writeError(w, http.StatusNotFound, "track group not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetTrackGroup error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to load track group")
		return
	}

	writeJSON(w, http.StatusOK, trackGroupToMap(group))
}

func (h *Handler) UpdateTrackGroup(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	id, ok := parseTrackGroupID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req trackGroupUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	group, err := h.postgres.UpdateTrackGroup(r.Context(), guildID, id, store.TrackGroupPatch{
		ChannelID:   req.ChannelID,
		MessageID:   req.MessageID,
		SendUpdates: req.SendUpdates,
		OnlineCount: req.OnlineCount,
	})
	if errors.Is(err, store.ErrTrackGroupNotFound) {
		writeError(w, http.StatusNotFound, "track group not found")
		return
	}
	if err != nil {
		log.Printf("[backend] UpdateTrackGroup error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to update track group")
		return
	}

	writeJSON(w, http.StatusOK, trackGroupToMap(group))
}

func (h *Handler) DeleteTrackGroup(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	id, ok := parseTrackGroupID(w, r)
	if !ok {
		return
	}

	if err := h.postgres.DeleteTrackGroup(r.Context(), guildID, id); errors.Is(err, store.ErrTrackGroupNotFound) {
		writeError(w, http.StatusNotFound, "track group not found")
		return
	} else if err != nil {
		log.Printf("[backend] DeleteTrackGroup error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete track group")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) ListTrackGroupMembers(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	id, ok := parseTrackGroupID(w, r)
	if !ok {
		return
	}

	if _, err := h.postgres.GetTrackGroup(r.Context(), guildID, id); errors.Is(err, store.ErrTrackGroupNotFound) {
		writeError(w, http.StatusNotFound, "track group not found")
		return
	} else if err != nil {
		log.Printf("[backend] GetTrackGroup error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to load track group")
		return
	}

	members, err := h.postgres.ListTrackGroupMembers(r.Context(), id)
	if err != nil {
		log.Printf("[backend] ListTrackGroupMembers error for group %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}

	response := make([]map[string]any, 0, len(members))
	for _, member := range members {
		response = append(response, trackGroupMemberToMap(member))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func (h *Handler) AddTrackGroupMembers(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	id, ok := parseTrackGroupID(w, r)
	if !ok {
		return
	}

	if _, err := h.postgres.GetTrackGroup(r.Context(), guildID, id); errors.Is(err, store.ErrTrackGroupNotFound) {
		writeError(w, http.StatusNotFound, "track group not found")
		return
	} else if err != nil {
		log.Printf("[backend] GetTrackGroup error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to load track group")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req trackGroupAddMembersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	members := make([]store.TrackGroupMember, 0, len(req.Members))
	for _, member := range req.Members {
		members = append(members, store.TrackGroupMember{
			TrackGroupID: id,
			SteamID:      member.SteamID,
			BmID:         member.BmID,
		})
	}

	added, err := h.postgres.AddTrackGroupMembers(r.Context(), id, members)
	if err != nil {
		log.Printf("[backend] AddTrackGroupMembers error for group %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to add members")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"added": added,
		"count": len(added),
	})
}

func (h *Handler) RemoveTrackGroupMembers(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	id, ok := parseTrackGroupID(w, r)
	if !ok {
		return
	}

	if _, err := h.postgres.GetTrackGroup(r.Context(), guildID, id); errors.Is(err, store.ErrTrackGroupNotFound) {
		writeError(w, http.StatusNotFound, "track group not found")
		return
	} else if err != nil {
		log.Printf("[backend] GetTrackGroup error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to load track group")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req trackGroupRemoveMembersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	removed, err := h.postgres.RemoveTrackGroupMembers(r.Context(), id, req.Identifiers)
	if err != nil {
		log.Printf("[backend] RemoveTrackGroupMembers error for group %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to remove members")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"count": removed})
}

func (h *Handler) ListTrackGroupOnlineState(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	id, ok := parseTrackGroupID(w, r)
	if !ok {
		return
	}

	if _, err := h.postgres.GetTrackGroup(r.Context(), guildID, id); errors.Is(err, store.ErrTrackGroupNotFound) {
		writeError(w, http.StatusNotFound, "track group not found")
		return
	} else if err != nil {
		log.Printf("[backend] GetTrackGroup error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to load track group")
		return
	}

	rows, err := h.postgres.ListTrackGroupOnlineState(r.Context(), id)
	if err != nil {
		log.Printf("[backend] ListTrackGroupOnlineState error for group %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to load online state")
		return
	}

	response := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		response = append(response, trackGroupOnlineRowToMap(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}
