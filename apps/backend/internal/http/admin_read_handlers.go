package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Read-side admin endpoints consumed by the frontend admin pages.
// Deliberately minimal — the shapes match the TypeScript admin item types in
// apps/frontend/lib/rustlens-api.ts. These will be expanded in the per-area
// feature specs (C/D/E/F).

type adminUserItem struct {
	User                 adminAppUser `json:"user"`
	DiscordID            string       `json:"discordId"`
	DiscordUsername      string       `json:"discordUsername"`
	PrimaryEmail         string       `json:"primaryEmail"`
	EmailVerified        bool         `json:"emailVerified"`
	GuildCount           int          `json:"guildCount"`
	SteamConnectionCount int          `json:"steamConnectionCount"`
	AdminRoles           []string     `json:"adminRoles"`
	SuspendedAt          *string      `json:"suspendedAt,omitempty"`
	DeletedAt            *string      `json:"deletedAt,omitempty"`
}

type adminAppUser struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	AvatarURL   string    `json:"avatarUrl"`
	Roles       []string  `json:"roles"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	LastLoginAt time.Time `json:"lastLoginAt"`
}

type adminGuildItem struct {
	GuildID        string `json:"guildId"`
	ActiveMembers  int    `json:"activeMembers"`
	LinkedMembers  int    `json:"linkedMembers"`
	TrackedServers int    `json:"trackedServers"`
	AdminRoleID    string `json:"adminRoleId"`
}

type adminServerItem struct {
	ServerBmID    string  `json:"serverBmId"`
	Name          string  `json:"name"`
	LastPolledAt  *string `json:"lastPolledAt"`
	LastPollError string  `json:"lastPollError"`
	GuildCount    int     `json:"guildCount"`
}

type auditEventItem struct {
	ID           int64          `json:"id"`
	ActorUserID  string         `json:"actorUserId"`
	TargetUserID string         `json:"targetUserId"`
	EventType    string         `json:"eventType"`
	Payload      map[string]any `json:"payload"`
	CreatedAt    time.Time      `json:"createdAt"`
}

// ListAdminUsers: GET /api/app/admin/users
// Query:
//   q=<substring>           matches display_name / primary email / discord username / app_users.id
//   role=<roleKey>          (repeatable) any matched role
//   status=active|suspended|deleted|any (default: active)
//   discord=any|linked|unlinked
//   steam=any|linked|unlinked
//   emailVerified=any|yes|no
//   sort=<field> or -<field>  field in {createdAt, lastLoginAt, displayName}
//   page=1 pageSize=50 (max 200)
func (h *Handler) ListAdminUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pool := h.postgres.Pool()
	q := r.URL.Query()

	page := 1
	pageSize := 50
	if v := q.Get("page"); v != "" {
		parseLimit(v, &page)
		if page < 1 {
			page = 1
		}
	}
	if v := q.Get("pageSize"); v != "" {
		parseLimit(v, &pageSize)
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	status := q.Get("status")
	if status == "" {
		status = "active"
	}

	args := []any{}
	where := []string{}

	switch status {
	case "active":
		where = append(where, "u.suspended_at IS NULL AND u.deleted_at IS NULL")
	case "suspended":
		where = append(where, "u.suspended_at IS NOT NULL AND u.deleted_at IS NULL")
	case "deleted":
		where = append(where, "u.deleted_at IS NOT NULL")
	case "any":
		// no filter
	default:
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	if s := strings.TrimSpace(q.Get("q")); s != "" {
		args = append(args, "%"+s+"%")
		p := fmt.Sprintf("$%d", len(args))
		where = append(where, fmt.Sprintf(`(
			u.display_name ILIKE %[1]s
			OR u.id ILIKE %[1]s
			OR EXISTS (SELECT 1 FROM user_emails e WHERE e.user_id = u.id AND e.email ILIKE %[1]s)
			OR EXISTS (SELECT 1 FROM user_connected_accounts d WHERE d.user_id = u.id AND d.provider='discord' AND (d.username ILIKE %[1]s OR d.provider_account_id ILIKE %[1]s))
		)`, p))
	}

	if roles, ok := q["role"]; ok && len(roles) > 0 {
		args = append(args, roles)
		p := fmt.Sprintf("$%d", len(args))
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM admin_user_roles aur JOIN admin_roles r ON r.id = aur.role_id
			WHERE aur.user_id = u.id AND r.key = ANY(%s)
		)`, p))
	}

	switch q.Get("discord") {
	case "", "any":
	case "linked":
		where = append(where, `EXISTS (SELECT 1 FROM user_connected_accounts d WHERE d.user_id = u.id AND d.provider='discord')`)
	case "unlinked":
		where = append(where, `NOT EXISTS (SELECT 1 FROM user_connected_accounts d WHERE d.user_id = u.id AND d.provider='discord')`)
	default:
		writeError(w, http.StatusBadRequest, "invalid discord")
		return
	}

	switch q.Get("steam") {
	case "", "any":
	case "linked":
		where = append(where, `EXISTS (SELECT 1 FROM user_connected_accounts s WHERE s.user_id = u.id AND s.provider='steam')`)
	case "unlinked":
		where = append(where, `NOT EXISTS (SELECT 1 FROM user_connected_accounts s WHERE s.user_id = u.id AND s.provider='steam')`)
	default:
		writeError(w, http.StatusBadRequest, "invalid steam")
		return
	}

	switch q.Get("emailVerified") {
	case "", "any":
	case "yes":
		where = append(where, `EXISTS (SELECT 1 FROM user_emails e WHERE e.user_id = u.id AND e.is_primary=true AND e.is_verified=true)`)
	case "no":
		where = append(where, `EXISTS (SELECT 1 FROM user_emails e WHERE e.user_id = u.id AND e.is_primary=true AND e.is_verified=false)`)
	default:
		writeError(w, http.StatusBadRequest, "invalid emailVerified")
		return
	}

	orderBy := ""
	sort := q.Get("sort")
	desc := strings.HasPrefix(sort, "-")
	sortField := strings.TrimPrefix(sort, "-")
	switch sortField {
	case "", "createdAt":
		orderBy = "u.created_at"
	case "lastLoginAt":
		orderBy = "u.last_login_at"
	case "displayName":
		orderBy = "u.display_name"
	default:
		writeError(w, http.StatusBadRequest, "invalid sort")
		return
	}
	if desc || sortField == "" {
		orderBy += " DESC"
	} else {
		orderBy += " ASC"
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	var total int
	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM app_users u %s`, whereSQL)
	if err := pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	args = append(args, pageSize, offset)
	listSQL := fmt.Sprintf(`
		SELECT u.id, u.display_name, u.avatar_url, u.roles::text,
		       u.created_at, u.updated_at, u.last_login_at,
		       u.suspended_at, u.deleted_at,
		       COALESCE(d.provider_account_id, ''),
		       COALESCE(d.username, ''),
		       COALESCE((SELECT email FROM user_emails e WHERE e.user_id = u.id AND e.is_primary=true LIMIT 1), ''),
		       COALESCE((SELECT is_verified FROM user_emails e WHERE e.user_id = u.id AND e.is_primary=true LIMIT 1), false),
		       COALESCE((SELECT COUNT(*) FROM user_guilds g WHERE g.discord_id = d.provider_account_id AND g.left_at IS NULL), 0),
		       (SELECT COUNT(*) FROM user_connected_accounts s WHERE s.user_id = u.id AND s.provider='steam'),
		       COALESCE(array_agg(DISTINCT r.key) FILTER (WHERE r.key IS NOT NULL), ARRAY[]::text[])
		FROM app_users u
		LEFT JOIN user_connected_accounts d ON d.user_id = u.id AND d.provider='discord'
		LEFT JOIN admin_user_roles aur ON aur.user_id = u.id
		LEFT JOIN admin_roles r ON r.id = aur.role_id
		%s
		GROUP BY u.id, d.provider_account_id, d.username
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, whereSQL, orderBy, len(args)-1, len(args))

	rows, err := pool.Query(ctx, listSQL, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := []adminUserItem{}
	for rows.Next() {
		var it adminUserItem
		var rolesLegacyJSON string
		var suspendedAt, deletedAt *time.Time
		var guildCount, steamCount int64
		var emailVerified bool
		var adminRoleKeys []string
		if err := rows.Scan(
			&it.User.ID, &it.User.DisplayName, &it.User.AvatarURL, &rolesLegacyJSON,
			&it.User.CreatedAt, &it.User.UpdatedAt, &it.User.LastLoginAt,
			&suspendedAt, &deletedAt,
			&it.DiscordID, &it.DiscordUsername, &it.PrimaryEmail, &emailVerified,
			&guildCount, &steamCount, &adminRoleKeys,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if rolesLegacyJSON != "" {
			_ = json.Unmarshal([]byte(rolesLegacyJSON), &it.User.Roles)
		}
		if it.User.Roles == nil {
			it.User.Roles = []string{}
		}
		it.GuildCount = int(guildCount)
		it.SteamConnectionCount = int(steamCount)
		it.EmailVerified = emailVerified
		it.AdminRoles = adminRoleKeys
		if it.AdminRoles == nil {
			it.AdminRoles = []string{}
		}
		if suspendedAt != nil {
			t := suspendedAt.Format(time.RFC3339)
			it.SuspendedAt = &t
		}
		if deletedAt != nil {
			t := deletedAt.Format(time.RFC3339)
			it.DeletedAt = &t
		}
		items = append(items, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "pageSize": pageSize,
	})
}

// ListAdminGuilds: GET /api/app/admin/guilds
func (h *Handler) ListAdminGuilds(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pool := h.postgres.Pool()

	rows, err := pool.Query(ctx, `
		SELECT s.guild_id,
		       s.admin_role_id,
		       (SELECT COUNT(*) FROM user_guilds g WHERE g.guild_id = s.guild_id AND g.left_at IS NULL),
		       (SELECT COUNT(*) FROM guild_steam_discord_links l WHERE l.guild_id = s.guild_id),
		       (SELECT COUNT(*) FROM guild_tracked_servers t WHERE t.guild_id = s.guild_id)
		FROM guild_settings s
		ORDER BY s.guild_id
		LIMIT 500
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := []adminGuildItem{}
	for rows.Next() {
		var (
			item           adminGuildItem
			active, linked int64
			tracked        int64
		)
		if err := rows.Scan(&item.GuildID, &item.AdminRoleID, &active, &linked, &tracked); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		item.ActiveMembers = int(active)
		item.LinkedMembers = int(linked)
		item.TrackedServers = int(tracked)
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ListAdminServers: GET /api/app/admin/servers
func (h *Handler) ListAdminServers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pool := h.postgres.Pool()

	rows, err := pool.Query(ctx, `
		SELECT t.bm_id,
		       COALESCE(t.name, ''),
		       t.last_polled_at,
		       COALESCE(t.last_poll_error, ''),
		       (SELECT COUNT(*) FROM guild_tracked_servers g WHERE g.server_bm_id = t.bm_id)
		FROM tracked_servers t
		ORDER BY t.bm_id
		LIMIT 500
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := []adminServerItem{}
	for rows.Next() {
		var (
			item        adminServerItem
			lastPolled  *time.Time
			guildCount  int64
		)
		if err := rows.Scan(&item.ServerBmID, &item.Name, &lastPolled, &item.LastPollError, &guildCount); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if lastPolled != nil {
			s := lastPolled.Format(time.RFC3339)
			item.LastPolledAt = &s
		}
		item.GuildCount = int(guildCount)
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ListAdminAudit: GET /api/app/admin/audit?limit=N
// Reads from the legacy app_audit_events table used by the vendored frontend.
// The new RBAC audit_events table is populated by the action dispatcher and
// has a different shape; it's exposed separately.
func (h *Handler) ListAdminAudit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		// best-effort parse; cap at 500
		parseLimit(v, &limit)
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	pool := h.postgres.Pool()

	if exists, _ := tableExists(ctx, pool, "app_audit_events"); !exists {
		writeJSON(w, http.StatusOK, map[string]any{"items": []auditEventItem{}})
		return
	}

	rows, err := pool.Query(ctx, `
		SELECT id,
		       COALESCE(actor_user_id, ''),
		       COALESCE(target_user_id, ''),
		       event_type,
		       COALESCE(payload, '{}'::jsonb),
		       created_at
		FROM app_audit_events
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := []auditEventItem{}
	for rows.Next() {
		var (
			item        auditEventItem
			payloadJSON []byte
		)
		if err := rows.Scan(&item.ID, &item.ActorUserID, &item.TargetUserID, &item.EventType, &payloadJSON, &item.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		item.Payload = map[string]any{}
		if len(payloadJSON) > 0 {
			_ = json.Unmarshal(payloadJSON, &item.Payload)
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func parseLimit(s string, out *int) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return
		}
		n = n*10 + int(c-'0')
		if n > 10000 {
			return
		}
	}
	*out = n
}

func tableExists(ctx context.Context, pool *pgxpool.Pool, table string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
		table,
	).Scan(&exists)
	return exists, err
}
