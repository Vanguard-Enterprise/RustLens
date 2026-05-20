package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/platformsync"
	"rustlens/backend/internal/store"
)

const platformSyncTTL = 14 * 24 * time.Hour
const platformSyncSource = "platformsync"

type platformSyncResponse struct {
	Synced             bool      `json:"synced"`
	Reason             string    `json:"reason,omitempty"`
	LastSyncedAt       time.Time `json:"lastSyncedAt"`
	Platform           string    `json:"platform"`
	PlatformID         string    `json:"platformId"`
	Linked             bool      `json:"linked"`
	SteamID            string    `json:"steamId,omitempty"`
	DiscordID          string    `json:"discordId,omitempty"`
	PreviousSteamIDs   []string  `json:"previousSteamIds,omitempty"`
	PreviousDiscordIDs []string  `json:"previousDiscordIds,omitempty"`
	HardLinkCount      int       `json:"hardLinkCount"`
}

// SyncPlatformIdentity runs a platformsync.io lookup for the given
// (platform, id) pair if the last lookup is older than 14 days.
//
// Behaviour:
//   - platform=steam: every (steam_id, discord_id) tuple discovered — including
//     all `previous_discord_ids` — is upserted as a hard link in
//     `steam_discord_links`. Only the queried steam_id is marked as synced in
//     `platform_sync_state` so future searches by one of the discovered
//     discord_ids still hit platformsync.
//   - platform=discord: every (steam_id, discord_id) tuple discovered —
//     including all `previous_steam_ids` — is upserted as a hard link. Only
//     the queried discord_id is marked as synced.
//
// Returns synced=false (no upstream call) when the cached row is fresh.
func (h *Handler) SyncPlatformIdentity(w http.ResponseWriter, r *http.Request) {
	platform := chi.URLParam(r, "platform")
	id := chi.URLParam(r, "id")

	if platform != "steam" && platform != "discord" {
		writeError(w, http.StatusBadRequest, "platform must be 'steam' or 'discord'")
		return
	}

	if !validateSnowflakeValue(id) {
		writeError(w, http.StatusBadRequest, "id must be a 17-20 digit snowflake / steam id")
		return
	}
	if platform == "steam" && !steamIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "steam id must be a 17-digit number")
		return
	}

	resp, status, err := h.syncPlatformIdentity(r.Context(), platform, id)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) syncPlatformIdentity(ctx context.Context, platform string, id string) (platformSyncResponse, int, error) {
	resp := platformSyncResponse{
		Platform:   platform,
		PlatformID: id,
	}

	state, err := h.postgres.GetPlatformSyncState(ctx, platform, id)
	if err != nil && !errors.Is(err, store.ErrSteamAccountNotFound) {
		log.Printf("[backend] SyncPlatformIdentity GetPlatformSyncState error: %v", err)
		return resp, http.StatusInternalServerError, errors.New("failed to load platform sync state")
	}

	if err == nil && time.Since(state.LastPlatformSyncAt) < platformSyncTTL {
		resp.Synced = false
		resp.Reason = "fresh"
		resp.LastSyncedAt = state.LastPlatformSyncAt
		return resp, http.StatusOK, nil
	}

	lookup, lookupErr := h.platformSync.Lookup(ctx, id)
	if errors.Is(lookupErr, platformsync.ErrNotConfigured) {
		return resp, http.StatusServiceUnavailable, errors.New("platformsync not configured")
	}

	if errors.Is(lookupErr, platformsync.ErrNotLinked) {
		// Even when the lookup says the identity is not linked, we still record
		// that we asked so the bot doesn't keep hammering the upstream service.
		syncedAt, err := h.recordPlatformSyncState(ctx, platform, id)
		if err != nil {
			log.Printf("[backend] SyncPlatformIdentity UpsertPlatformSyncState error: %v", err)
			return resp, http.StatusInternalServerError, errors.New("failed to record platform sync state")
		}
		resp.Synced = true
		resp.LastSyncedAt = syncedAt
		resp.Linked = false
		resp.SteamID = lookup.SteamID
		resp.DiscordID = lookup.DiscordID
		resp.PreviousSteamIDs = lookup.PreviousSteamIDs
		resp.PreviousDiscordIDs = lookup.PreviousDiscordIDs
		return resp, http.StatusOK, nil
	}
	if lookupErr != nil {
		log.Printf("[backend] SyncPlatformIdentity platformsync error: %v", lookupErr)
		return resp, http.StatusBadGateway, errors.New("platformsync upstream failed")
	}

	resp.Linked = lookup.Linked
	resp.SteamID = lookup.SteamID
	resp.DiscordID = lookup.DiscordID
	resp.PreviousSteamIDs = lookup.PreviousSteamIDs
	resp.PreviousDiscordIDs = lookup.PreviousDiscordIDs

	links := buildPlatformSyncHardLinks(platform, lookup)
	if len(links) > 0 {
		if err := h.postgres.BulkUpsertHardLinks(ctx, links); err != nil {
			log.Printf("[backend] SyncPlatformIdentity BulkUpsertHardLinks error: %v", err)
			return resp, http.StatusInternalServerError, errors.New("failed to record hard links")
		}
		resp.HardLinkCount = len(links)
	}

	syncedAt, err := h.recordPlatformSyncState(ctx, platform, id)
	if err != nil {
		log.Printf("[backend] SyncPlatformIdentity UpsertPlatformSyncState error: %v", err)
		return resp, http.StatusInternalServerError, errors.New("failed to record platform sync state")
	}
	resp.Synced = true
	resp.LastSyncedAt = syncedAt
	return resp, http.StatusOK, nil
}

func (h *Handler) recordPlatformSyncState(ctx context.Context, platform string, id string) (time.Time, error) {
	state, err := h.postgres.UpsertPlatformSyncState(ctx, platform, id)
	if err != nil {
		return time.Time{}, err
	}
	return state.LastPlatformSyncAt, nil
}

// buildPlatformSyncHardLinks turns a platformsync result into the set of
// (steam_id, discord_id) hard links to upsert. It silently drops malformed
// IDs because the upstream input is untrusted.
func buildPlatformSyncHardLinks(platform string, lookup platformsync.Lookup) []store.BulkHardLink {
	if !lookup.Linked {
		return nil
	}

	var steamIDs, discordIDs []string

	switch platform {
	case "steam":
		// Steam was the search key — the steam_id is canonical, and we
		// pair it with every discord_id we discovered.
		if steamIDPattern.MatchString(lookup.SteamID) {
			steamIDs = []string{lookup.SteamID}
		}
		discordIDs = collectValidSnowflakes(lookup.DiscordID, lookup.PreviousDiscordIDs)
	case "discord":
		// Discord was the search key — pair the discord_id with every
		// discovered steam_id.
		if validateSnowflakeValue(lookup.DiscordID) {
			discordIDs = []string{lookup.DiscordID}
		}
		steamIDs = collectValidSteamIDs(lookup.SteamID, lookup.PreviousSteamIDs)
	default:
		return nil
	}

	if len(steamIDs) == 0 || len(discordIDs) == 0 {
		return nil
	}

	out := make([]store.BulkHardLink, 0, len(steamIDs)*len(discordIDs))
	for _, steamID := range steamIDs {
		for _, discordID := range discordIDs {
			out = append(out, store.BulkHardLink{
				SteamID:   steamID,
				DiscordID: discordID,
				Source:    platformSyncSource,
			})
		}
	}
	return out
}

func collectValidSnowflakes(primary string, extras []string) []string {
	seen := make(map[string]struct{}, 1+len(extras))
	out := make([]string, 0, 1+len(extras))
	add := func(value string) {
		if !validateSnowflakeValue(value) {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(primary)
	for _, v := range extras {
		add(v)
	}
	return out
}

func collectValidSteamIDs(primary string, extras []string) []string {
	seen := make(map[string]struct{}, 1+len(extras))
	out := make([]string, 0, 1+len(extras))
	add := func(value string) {
		if !steamIDPattern.MatchString(value) {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(primary)
	for _, v := range extras {
		add(v)
	}
	return out
}
