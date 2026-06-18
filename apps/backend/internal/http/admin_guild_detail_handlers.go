package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"rustlens/backend/internal/store"
)

const adminGuildDetailListLimit = 100

type adminGuildSettingsItem struct {
	GuildID                   string `json:"guildId"`
	WipeReactLogChannelID     string `json:"wipeReactLogChannelId"`
	LinkingLogChannelID       string `json:"linkingLogChannelId"`
	TrackedServerLogChannelID string `json:"trackedServerLogChannelId"`
	TrackedUsersCategoryID    string `json:"trackedUsersCategoryId"`
	AdminRoleID               string `json:"adminRoleId"`
	AlertRoleID               string `json:"alertRoleId"`
	Limited                   bool   `json:"limited"`
}

type adminGuildDetailStats struct {
	MemberCount          int64 `json:"memberCount"`
	ActiveMemberCount    int64 `json:"activeMemberCount"`
	LinkedMemberCount    int64 `json:"linkedMemberCount"`
	TrackedServerCount   int64 `json:"trackedServerCount"`
	TrackGroupCount      int64 `json:"trackGroupCount"`
	ActiveWipeReactCount int64 `json:"activeWipeReactCount"`
	WipeReactCount       int64 `json:"wipeReactCount"`
	ScheduleCount        int64 `json:"scheduleCount"`
	Usage30dCount        int64 `json:"usage30dCount"`
	Usage30dErrorCount   int64 `json:"usage30dErrorCount"`
}

type adminGuildMemberItem struct {
	DiscordID       string     `json:"discordId"`
	AppUserID       *string    `json:"appUserId"`
	DisplayName     *string    `json:"displayName"`
	DiscordUsername *string    `json:"discordUsername"`
	GlobalName      *string    `json:"globalName"`
	AvatarHash      *string    `json:"avatarHash"`
	IsActive        bool       `json:"isActive"`
	IsOwner         bool       `json:"isOwner"`
	FirstSeenAt     time.Time  `json:"firstSeenAt"`
	LastSeenAt      time.Time  `json:"lastSeenAt"`
	LeftAt          *time.Time `json:"leftAt"`
	SteamIDs        []string   `json:"steamIds"`
}

type adminGuildSteamLinkItem struct {
	DiscordID       string    `json:"discordId"`
	SteamID         string    `json:"steamId"`
	AppUserID       *string   `json:"appUserId"`
	DisplayName     *string   `json:"displayName"`
	DiscordUsername *string   `json:"discordUsername"`
	CreatedAt       time.Time `json:"createdAt"`
}

type adminGuildTrackGroupItem struct {
	ID                   int64     `json:"id"`
	Name                 string    `json:"name"`
	ServerBmID           string    `json:"serverBmId"`
	ServerName           *string   `json:"serverName"`
	ChannelID            string    `json:"channelId"`
	MessageID            string    `json:"messageId"`
	SendUpdates          bool      `json:"sendUpdates"`
	PingAlertRoleOnEmpty bool      `json:"pingAlertRoleOnEmpty"`
	OnlineCount          int       `json:"onlineCount"`
	MemberCount          int       `json:"memberCount"`
	CreatedBy            string    `json:"createdBy"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type adminGuildUsageItem struct {
	ID              int64     `json:"id"`
	AppUserID       string    `json:"appUserId"`
	DiscordID       string    `json:"discordId"`
	ChannelID       *string   `json:"channelId"`
	ChannelName     *string   `json:"channelName"`
	InteractionType string    `json:"interactionType"`
	Command         string    `json:"command"`
	Subcommand      *string   `json:"subcommand"`
	Outcome         string    `json:"outcome"`
	ErrorCode       *string   `json:"errorCode"`
	LatencyMs       int       `json:"latencyMs"`
	CreatedAt       time.Time `json:"createdAt"`
	DisplayName     *string   `json:"displayName"`
	DiscordUsername *string   `json:"discordUsername"`
}

type adminGuildTopCommandItem struct {
	Command string `json:"command"`
	Count   int64  `json:"count"`
}

type adminGuildUsageSummary struct {
	TotalCount  int64                      `json:"totalCount"`
	ErrorCount  int64                      `json:"errorCount"`
	ErrorRate   float64                    `json:"errorRate"`
	TopCommands []adminGuildTopCommandItem `json:"topCommands"`
}

func guildSettingsItem(settings store.GuildSettings) adminGuildSettingsItem {
	return adminGuildSettingsItem{
		GuildID:                   settings.GuildID,
		WipeReactLogChannelID:     settings.WipeReactLogChannelID,
		LinkingLogChannelID:       settings.LinkingLogChannelID,
		TrackedServerLogChannelID: settings.TrackedServerLogChannelID,
		TrackedUsersCategoryID:    settings.TrackedUsersCategoryID,
		AdminRoleID:               settings.AdminRoleID,
		AlertRoleID:               settings.AlertRoleID,
		Limited:                   settings.Limited,
	}
}

// GetAdminGuild: GET /api/app/admin/guilds/{guildID}
//
// Returns a compact read-only aggregate for a single guild. Large collections
// are capped to adminGuildDetailListLimit; counts in stats reflect full totals.
func (h *Handler) GetAdminGuild(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	guildID := chi.URLParam(r, "guildID")
	if guildID == "" {
		writeError(w, http.StatusBadRequest, "missing guildID")
		return
	}

	guild, err := h.getAdminGuildItem(ctx, guildID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "guild not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	settings, err := h.postgres.GetGuildSettings(ctx, guildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	trackedServers, err := h.postgres.ListGuildTrackedServers(ctx, guildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	activeWipeReacts, err := h.postgres.ListActiveWipeReacts(ctx, guildID, time.Now().Unix())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recentWipeReacts, err := h.postgres.ListWipeReacts(ctx, guildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(recentWipeReacts) > 10 {
		recentWipeReacts = recentWipeReacts[:10]
	}
	schedules, err := h.postgres.ListWipeReactSchedules(ctx, guildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	members, err := h.listAdminGuildMembers(ctx, guildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	links, err := h.listAdminGuildSteamLinks(ctx, guildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	trackGroups, err := h.listAdminGuildTrackGroups(ctx, guildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recentUsage, err := h.listAdminGuildRecentUsage(ctx, guildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	usageSummary, err := h.adminGuildUsageSummary(ctx, guildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	stats, err := h.adminGuildStats(ctx, guildID, usageSummary)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	trackedServerItems := make([]map[string]any, 0, len(trackedServers))
	for _, item := range trackedServers {
		trackedServerItems = append(trackedServerItems, guildTrackedServerToMap(item))
	}
	activeWipeReactItems := make([]map[string]any, 0, len(activeWipeReacts))
	for _, item := range activeWipeReacts {
		activeWipeReactItems = append(activeWipeReactItems, wipeReactToMap(item))
	}
	recentWipeReactItems := make([]map[string]any, 0, len(recentWipeReacts))
	for _, item := range recentWipeReacts {
		recentWipeReactItems = append(recentWipeReactItems, wipeReactToMap(item))
	}
	scheduleItems := make([]map[string]any, 0, len(schedules))
	for _, item := range schedules {
		scheduleItems = append(scheduleItems, wipeReactScheduleToMap(item))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"guild":            guild,
		"stats":            stats,
		"settings":         guildSettingsItem(settings),
		"trackedServers":   trackedServerItems,
		"activeWipeReacts": activeWipeReactItems,
		"recentWipeReacts": recentWipeReactItems,
		"schedules":        scheduleItems,
		"members":          members,
		"links":            links,
		"trackGroups":      trackGroups,
		"recentUsage":      recentUsage,
		"usageSummary":     usageSummary,
		"limits": map[string]any{
			"members":     adminGuildDetailListLimit,
			"links":       adminGuildDetailListLimit,
			"recentUsage": adminGuildDetailListLimit,
		},
	})
}

func (h *Handler) getAdminGuildItem(ctx context.Context, guildID string) (adminGuildItem, error) {
	var row store.AdminGuildRow
	err := h.postgres.Pool().QueryRow(ctx, `
		SELECT bg.guild_id, bg.name, bg.icon_hash,
		       bg.owner_discord_id, bg.owner_username, bg.owner_global_name, bg.owner_avatar_hash,
		       bg.approximate_member_count, bg.installed_at, bg.is_installed, bg.left_at,
		       (gs.guild_id IS NOT NULL) AS is_configured,
		       COALESCE(gts.cnt, 0)      AS tracked_server_count
		FROM bot_guilds bg
		LEFT JOIN guild_settings gs ON gs.guild_id = bg.guild_id
		LEFT JOIN (
			SELECT guild_id, COUNT(*)::int AS cnt
			FROM guild_tracked_servers
			GROUP BY guild_id
		) gts ON gts.guild_id = bg.guild_id
		WHERE bg.guild_id = $1
	`, guildID).Scan(
		&row.GuildID, &row.Name, &row.IconHash,
		&row.OwnerDiscordID, &row.OwnerUsername, &row.OwnerGlobalName, &row.OwnerAvatarHash,
		&row.ApproximateMemberCount, &row.InstalledAt, &row.IsInstalled, &row.LeftAt,
		&row.IsConfigured, &row.TrackedServerCount,
	)
	if err != nil {
		return adminGuildItem{}, err
	}

	item := adminGuildItem{
		GuildID:                row.GuildID,
		Name:                   row.Name,
		IconHash:               row.IconHash,
		OwnerDiscordID:         row.OwnerDiscordID,
		OwnerUsername:          row.OwnerUsername,
		OwnerGlobalName:        row.OwnerGlobalName,
		OwnerAvatarHash:        row.OwnerAvatarHash,
		ApproximateMemberCount: row.ApproximateMemberCount,
		InstalledAt:            row.InstalledAt.Format(time.RFC3339),
		IsInstalled:            row.IsInstalled,
		IsConfigured:           row.IsConfigured,
		TrackedServerCount:     int(row.TrackedServerCount),
	}
	if row.LeftAt != nil {
		leftAt := row.LeftAt.Format(time.RFC3339)
		item.LeftAt = &leftAt
	}
	return item, nil
}

func (h *Handler) adminGuildStats(ctx context.Context, guildID string, usage adminGuildUsageSummary) (adminGuildDetailStats, error) {
	var stats adminGuildDetailStats
	err := h.postgres.Pool().QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM user_guilds WHERE guild_id = $1),
			(SELECT COUNT(*) FROM user_guilds WHERE guild_id = $1 AND is_active = TRUE AND left_at IS NULL),
			(SELECT COUNT(DISTINCT discord_id) FROM guild_steam_discord_links WHERE guild_id = $1),
			(SELECT COUNT(*) FROM guild_tracked_servers WHERE guild_id = $1),
			(SELECT COUNT(*) FROM track_groups WHERE guild_id = $1),
			(SELECT COUNT(*) FROM wipe_reacts WHERE guild_id = $1 AND scheduled_unix >= EXTRACT(EPOCH FROM NOW())::bigint),
			(SELECT COUNT(*) FROM wipe_reacts WHERE guild_id = $1),
			(SELECT COUNT(*) FROM wipe_react_schedules WHERE guild_id = $1)
	`, guildID).Scan(
		&stats.MemberCount,
		&stats.ActiveMemberCount,
		&stats.LinkedMemberCount,
		&stats.TrackedServerCount,
		&stats.TrackGroupCount,
		&stats.ActiveWipeReactCount,
		&stats.WipeReactCount,
		&stats.ScheduleCount,
	)
	if err != nil {
		return adminGuildDetailStats{}, err
	}
	stats.Usage30dCount = usage.TotalCount
	stats.Usage30dErrorCount = usage.ErrorCount
	return stats, nil
}

func (h *Handler) listAdminGuildMembers(ctx context.Context, guildID string) ([]adminGuildMemberItem, error) {
	rows, err := h.postgres.Pool().Query(ctx, `
		SELECT ug.discord_id,
		       u.id,
		       COALESCE(NULLIF(u.display_name, ''), NULLIF(u.discord_global_name, ''), NULLIF(u.discord_username, '')) AS display_name,
		       u.discord_username,
		       u.discord_global_name,
		       u.discord_avatar,
		       ug.is_active,
		       ug.is_owner,
		       ug.first_seen_at,
		       ug.last_seen_at,
		       ug.left_at,
		       COALESCE(
		         array_agg(DISTINCT gsl.steam_id ORDER BY gsl.steam_id)
		           FILTER (WHERE gsl.steam_id IS NOT NULL),
		         ARRAY[]::text[]
		       ) AS steam_ids
		FROM user_guilds ug
		LEFT JOIN app_users u ON u.discord_id = ug.discord_id
		LEFT JOIN guild_steam_discord_links gsl
		  ON gsl.guild_id = ug.guild_id AND gsl.discord_id = ug.discord_id
		WHERE ug.guild_id = $1
		GROUP BY ug.discord_id, u.id, u.display_name, u.discord_global_name, u.discord_username,
		         u.discord_avatar, ug.is_active, ug.is_owner, ug.first_seen_at, ug.last_seen_at, ug.left_at
		ORDER BY (ug.is_active AND ug.left_at IS NULL) DESC, ug.last_seen_at DESC, ug.discord_id ASC
		LIMIT $2
	`, guildID, adminGuildDetailListLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []adminGuildMemberItem{}
	for rows.Next() {
		var item adminGuildMemberItem
		if err := rows.Scan(
			&item.DiscordID,
			&item.AppUserID,
			&item.DisplayName,
			&item.DiscordUsername,
			&item.GlobalName,
			&item.AvatarHash,
			&item.IsActive,
			&item.IsOwner,
			&item.FirstSeenAt,
			&item.LastSeenAt,
			&item.LeftAt,
			&item.SteamIDs,
		); err != nil {
			return nil, err
		}
		if item.SteamIDs == nil {
			item.SteamIDs = []string{}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (h *Handler) listAdminGuildSteamLinks(ctx context.Context, guildID string) ([]adminGuildSteamLinkItem, error) {
	rows, err := h.postgres.Pool().Query(ctx, `
		SELECT gsl.discord_id, gsl.steam_id, u.id,
		       COALESCE(NULLIF(u.display_name, ''), NULLIF(u.discord_global_name, ''), NULLIF(u.discord_username, '')) AS display_name,
		       u.discord_username,
		       gsl.created_at
		FROM guild_steam_discord_links gsl
		LEFT JOIN app_users u ON u.discord_id = gsl.discord_id
		WHERE gsl.guild_id = $1
		ORDER BY gsl.created_at DESC, gsl.discord_id ASC, gsl.steam_id ASC
		LIMIT $2
	`, guildID, adminGuildDetailListLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []adminGuildSteamLinkItem{}
	for rows.Next() {
		var item adminGuildSteamLinkItem
		if err := rows.Scan(
			&item.DiscordID,
			&item.SteamID,
			&item.AppUserID,
			&item.DisplayName,
			&item.DiscordUsername,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (h *Handler) listAdminGuildTrackGroups(ctx context.Context, guildID string) ([]adminGuildTrackGroupItem, error) {
	rows, err := h.postgres.Pool().Query(ctx, `
		SELECT tg.id, tg.name, tg.server_bm_id, ts.name,
		       tg.channel_id, tg.message_id, tg.send_updates, tg.ping_alert_role_on_empty,
		       tg.online_count, COALESCE(m.member_count, 0)::int,
		       tg.created_by, tg.created_at, tg.updated_at
		FROM track_groups tg
		LEFT JOIN tracked_servers ts ON ts.bm_id = tg.server_bm_id
		LEFT JOIN (
			SELECT track_group_id, COUNT(*) AS member_count
			FROM track_group_members
			GROUP BY track_group_id
		) m ON m.track_group_id = tg.id
		WHERE tg.guild_id = $1
		ORDER BY tg.name ASC, tg.id ASC
		LIMIT $2
	`, guildID, adminGuildDetailListLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []adminGuildTrackGroupItem{}
	for rows.Next() {
		var item adminGuildTrackGroupItem
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.ServerBmID,
			&item.ServerName,
			&item.ChannelID,
			&item.MessageID,
			&item.SendUpdates,
			&item.PingAlertRoleOnEmpty,
			&item.OnlineCount,
			&item.MemberCount,
			&item.CreatedBy,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (h *Handler) listAdminGuildRecentUsage(ctx context.Context, guildID string) ([]adminGuildUsageItem, error) {
	rows, err := h.postgres.Pool().Query(ctx, `
		SELECT cu.id, cu.app_user_id, cu.discord_id, cu.channel_id, cu.channel_name,
		       cu.interaction_type, cu.command, cu.subcommand, cu.outcome, cu.error_code,
		       cu.latency_ms, cu.created_at,
		       COALESCE(NULLIF(u.display_name, ''), NULLIF(u.discord_global_name, ''), NULLIF(u.discord_username, '')) AS display_name,
		       u.discord_username
		FROM command_usage cu
		LEFT JOIN app_users u ON u.id = cu.app_user_id
		WHERE cu.guild_id = $1
		ORDER BY cu.created_at DESC, cu.id DESC
		LIMIT $2
	`, guildID, adminGuildDetailListLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []adminGuildUsageItem{}
	for rows.Next() {
		var item adminGuildUsageItem
		if err := rows.Scan(
			&item.ID,
			&item.AppUserID,
			&item.DiscordID,
			&item.ChannelID,
			&item.ChannelName,
			&item.InteractionType,
			&item.Command,
			&item.Subcommand,
			&item.Outcome,
			&item.ErrorCode,
			&item.LatencyMs,
			&item.CreatedAt,
			&item.DisplayName,
			&item.DiscordUsername,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (h *Handler) adminGuildUsageSummary(ctx context.Context, guildID string) (adminGuildUsageSummary, error) {
	var summary adminGuildUsageSummary
	err := h.postgres.Pool().QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE outcome = 'error')
		FROM command_usage
		WHERE guild_id = $1 AND created_at >= NOW() - INTERVAL '30 days'
	`, guildID).Scan(&summary.TotalCount, &summary.ErrorCount)
	if err != nil {
		return adminGuildUsageSummary{}, err
	}
	if summary.TotalCount > 0 {
		summary.ErrorRate = float64(summary.ErrorCount) / float64(summary.TotalCount)
	}

	rows, err := h.postgres.Pool().Query(ctx, `
		SELECT command, COUNT(*) AS count
		FROM command_usage
		WHERE guild_id = $1 AND created_at >= NOW() - INTERVAL '30 days'
		GROUP BY command
		ORDER BY count DESC, command ASC
		LIMIT 5
	`, guildID)
	if err != nil {
		return adminGuildUsageSummary{}, err
	}
	defer rows.Close()

	summary.TopCommands = []adminGuildTopCommandItem{}
	for rows.Next() {
		var item adminGuildTopCommandItem
		if err := rows.Scan(&item.Command, &item.Count); err != nil {
			return adminGuildUsageSummary{}, err
		}
		summary.TopCommands = append(summary.TopCommands, item)
	}
	return summary, rows.Err()
}
