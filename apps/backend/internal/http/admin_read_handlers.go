package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/store"
)

// Read-side admin endpoints consumed by the frontend admin pages.
// Deliberately minimal — the shapes match the TypeScript admin item types in
// apps/frontend/lib/rustlens-api.ts.

type adminUserItem struct {
	User                 adminAppUser `json:"user"`
	DiscordID            string       `json:"discordId"`
	DiscordUsername      string       `json:"discordUsername"`
	DiscordGlobalName    string       `json:"discordGlobalName,omitempty"`
	DiscordAvatar        string       `json:"discordAvatar,omitempty"`
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
	GuildID                string  `json:"guildId"`
	Name                   string  `json:"name"`
	IconHash               *string `json:"iconHash"`
	OwnerDiscordID         *string `json:"ownerDiscordId"`
	OwnerUsername          *string `json:"ownerUsername"`
	OwnerGlobalName        *string `json:"ownerGlobalName"`
	OwnerAvatarHash        *string `json:"ownerAvatarHash"`
	ApproximateMemberCount *int32  `json:"approximateMemberCount"`
	IsInstalled            bool    `json:"isInstalled"`
	LeftAt                 *string `json:"leftAt"`
	IsConfigured           bool    `json:"isConfigured"`
	TrackedServerCount     int     `json:"trackedServerCount"`
}

type adminServerItem struct {
	ServerBmID    string  `json:"serverBmId"`
	Name          string  `json:"name"`
	LastPolledAt  *string `json:"lastPolledAt"`
	LastPollError string  `json:"lastPollError"`
	GuildCount    int     `json:"guildCount"`
}

type auditEventItem struct {
	ID                string         `json:"id"`
	ActorUserID       string         `json:"actorUserId"`
	TargetUserID      string         `json:"targetUserId"`
	EventType         string         `json:"eventType"`
	Payload           map[string]any `json:"payload"`
	CreatedAt         time.Time      `json:"createdAt"`
	ActorDisplayName  *string        `json:"actorDisplayName,omitempty"`
	ActorAvatarURL    *string        `json:"actorAvatarUrl,omitempty"`
	TargetDisplayName *string        `json:"targetDisplayName,omitempty"`
	TargetAvatarURL   *string        `json:"targetAvatarUrl,omitempty"`
	Result            *string        `json:"result,omitempty"`
	Reason            *string        `json:"reason,omitempty"`
}

type adminUserGuild struct {
	GuildID string `json:"guildId"`
	Nick    string `json:"nick,omitempty"`
}

type adminUserDetail struct {
	User              adminAppUser     `json:"user"`
	DiscordID         string           `json:"discordId"`
	DiscordUsername   string           `json:"discordUsername"`
	DiscordGlobalName string           `json:"discordGlobalName,omitempty"`
	DiscordAvatar     string           `json:"discordAvatar,omitempty"`
	Guilds            []adminUserGuild `json:"guilds"`
	AdminRoles        []string         `json:"adminRoles"`
	SuspendedAt       *string          `json:"suspendedAt,omitempty"`
	SuspendReason     string           `json:"suspendReason,omitempty"`
	DeletedAt         *string          `json:"deletedAt,omitempty"`
	RecentAudit       []auditEventItem `json:"recentAudit"`
}

// ListAdminUsers: GET /api/app/admin/users
// Query:
//   q=<substring>           matches display_name / app_users.id / discord_username / discord_id
//   role=<roleKey>          (repeatable) any matched role
//   status=active|suspended|deleted|any (default: active)
//   discord=any|linked|unlinked
//   steam=any|linked|unlinked
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

	whereSQL, args, orderBy, err := buildUsersQuery(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
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
		       COALESCE(u.discord_id, ''),
		       COALESCE(u.discord_username, ''),
		       COALESCE(u.discord_global_name, ''),
		       COALESCE(u.discord_avatar, ''),
		       COALESCE((SELECT COUNT(*) FROM user_guilds g WHERE g.discord_id = u.discord_id AND g.left_at IS NULL), 0),
		       0 AS steam_count,
		       COALESCE(array_agg(DISTINCT r.key) FILTER (WHERE r.key IS NOT NULL), ARRAY[]::text[])
		FROM app_users u
		LEFT JOIN admin_user_roles aur ON aur.user_id = u.id
		LEFT JOIN admin_roles r ON r.id = aur.role_id
		%s
		GROUP BY u.id
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
		var adminRoleKeys []string
		if err := rows.Scan(
			&it.User.ID, &it.User.DisplayName, &it.User.AvatarURL, &rolesLegacyJSON,
			&it.User.CreatedAt, &it.User.UpdatedAt, &it.User.LastLoginAt,
			&suspendedAt, &deletedAt,
			&it.DiscordID, &it.DiscordUsername, &it.DiscordGlobalName, &it.DiscordAvatar,
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
// Query:
//   q=<substring>            matches bot_guilds.name (substring), guild_id (prefix), owner_username (substring); ILIKE
//   page=1 (default)         1-based
//   pageSize=20 (default)    capped at 100
func (h *Handler) ListAdminGuilds(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	page := 1
	pageSize := 20
	if v := q.Get("page"); v != "" {
		parseLimit(v, &page)
		if page < 1 {
			page = 1
		}
	}
	if v := q.Get("pageSize"); v != "" {
		parseLimit(v, &pageSize)
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	rows, total, err := h.postgres.ListAdminGuilds(ctx, store.ListAdminGuildsOpts{
		Q:        q.Get("q"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]adminGuildItem, 0, len(rows))
	for _, r := range rows {
		it := adminGuildItem{
			GuildID:                r.GuildID,
			Name:                   r.Name,
			IconHash:               r.IconHash,
			OwnerDiscordID:         r.OwnerDiscordID,
			OwnerUsername:          r.OwnerUsername,
			OwnerGlobalName:        r.OwnerGlobalName,
			OwnerAvatarHash:        r.OwnerAvatarHash,
			ApproximateMemberCount: r.ApproximateMemberCount,
			IsInstalled:            r.IsInstalled,
			IsConfigured:           r.IsConfigured,
			TrackedServerCount:     int(r.TrackedServerCount),
		}
		if r.LeftAt != nil {
			s := r.LeftAt.Format(time.RFC3339)
			it.LeftAt = &s
		}
		items = append(items, it)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
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
// Returns a unified view over the new RBAC audit_events table (written by the
// action dispatcher) and the legacy app_audit_events table (used by the
// vendored frontend). Each row is augmented with the actor's and target's
// display name and avatar URL in a single extra lookup against app_users so the
// frontend can render human-readable entries without an N+1 fan-out.
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

	hasLegacy, _ := tableExists(ctx, pool, "app_audit_events")

	// New RBAC audit_events table: action_type is the event key, params is the
	// JSON payload, result and reason are first-class columns.
	newQ := `
		SELECT id::text,
		       COALESCE(actor_user_id::text, '') AS actor_user_id,
		       COALESCE(target_user_id::text, '') AS target_user_id,
		       action_type AS event_type,
		       COALESCE(params, '{}'::jsonb) AS payload,
		       NULLIF(result, '') AS result,
		       reason,
		       created_at
		FROM audit_events
	`

	// Legacy app_audit_events table: no result/reason columns, so pad with NULL.
	legacyQ := `
		SELECT id::text,
		       COALESCE(actor_user_id, '') AS actor_user_id,
		       COALESCE(target_user_id, '') AS target_user_id,
		       event_type,
		       COALESCE(payload, '{}'::jsonb) AS payload,
		       NULL::text AS result,
		       NULL::text AS reason,
		       created_at
		FROM app_audit_events
	`

	var q string
	if hasLegacy {
		q = fmt.Sprintf(`
			%s
			UNION ALL
			%s
			ORDER BY created_at DESC
			LIMIT $1
		`, newQ, legacyQ)
	} else {
		q = fmt.Sprintf(`
			%s
			ORDER BY created_at DESC
			LIMIT $1
		`, newQ)
	}

	rows, err := pool.Query(ctx, q, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := []auditEventItem{}
	idSet := map[string]struct{}{}
	for rows.Next() {
		var (
			item        auditEventItem
			payloadJSON []byte
			result      *string
			reason      *string
		)
		if err := rows.Scan(&item.ID, &item.ActorUserID, &item.TargetUserID, &item.EventType, &payloadJSON, &result, &reason, &item.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		item.Payload = map[string]any{}
		if len(payloadJSON) > 0 {
			_ = json.Unmarshal(payloadJSON, &item.Payload)
		}
		item.Result = result
		item.Reason = reason
		if item.ActorUserID != "" {
			idSet[item.ActorUserID] = struct{}{}
		}
		if item.TargetUserID != "" {
			idSet[item.TargetUserID] = struct{}{}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Single lookup for actor + target display names.
	type userLite struct {
		name   string
		avatar string
	}
	lookup := map[string]userLite{}
	if len(idSet) > 0 {
		ids := make([]string, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		urows, err := pool.Query(ctx, `
			SELECT id::text, COALESCE(display_name, ''), COALESCE(avatar_url, '')
			FROM app_users WHERE id::text = ANY($1)
		`, ids)
		if err == nil {
			for urows.Next() {
				var id, name, avatar string
				if err := urows.Scan(&id, &name, &avatar); err == nil {
					lookup[id] = userLite{name: name, avatar: avatar}
				}
			}
			urows.Close()
		}
	}

	strPtr := func(s string) *string {
		if s == "" {
			return nil
		}
		v := s
		return &v
	}
	for i := range items {
		if u, ok := lookup[items[i].ActorUserID]; ok {
			items[i].ActorDisplayName = strPtr(u.name)
			items[i].ActorAvatarURL = strPtr(u.avatar)
		}
		if u, ok := lookup[items[i].TargetUserID]; ok {
			items[i].TargetDisplayName = strPtr(u.name)
			items[i].TargetAvatarURL = strPtr(u.avatar)
		}
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

// GetAdminUser: GET /api/app/admin/users/{id}
func (h *Handler) GetAdminUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	pool := h.postgres.Pool()

	var d adminUserDetail
	var suspendedAt, deletedAt *time.Time
	var rolesJSON string
	err := pool.QueryRow(ctx, `
		SELECT id, display_name, avatar_url, roles::text,
		       created_at, updated_at, last_login_at,
		       suspended_at, COALESCE(suspend_reason,''), deleted_at,
		       COALESCE(discord_id, ''), COALESCE(discord_username, ''),
		       COALESCE(discord_global_name, ''), COALESCE(discord_avatar, '')
		FROM app_users WHERE id = $1
	`, id).Scan(
		&d.User.ID, &d.User.DisplayName, &d.User.AvatarURL, &rolesJSON,
		&d.User.CreatedAt, &d.User.UpdatedAt, &d.User.LastLoginAt,
		&suspendedAt, &d.SuspendReason, &deletedAt,
		&d.DiscordID, &d.DiscordUsername, &d.DiscordGlobalName, &d.DiscordAvatar,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rolesJSON != "" {
		_ = json.Unmarshal([]byte(rolesJSON), &d.User.Roles)
	}
	if d.User.Roles == nil {
		d.User.Roles = []string{}
	}
	if suspendedAt != nil {
		s := suspendedAt.Format(time.RFC3339)
		d.SuspendedAt = &s
	}
	if deletedAt != nil {
		s := deletedAt.Format(time.RFC3339)
		d.DeletedAt = &s
	}

	// Admin roles
	d.AdminRoles = []string{}
	rrows, err := pool.Query(ctx, `
		SELECT r.key FROM admin_roles r
		JOIN admin_user_roles aur ON aur.role_id = r.id
		WHERE aur.user_id = $1 ORDER BY r.key
	`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for rrows.Next() {
		var k string
		if err := rrows.Scan(&k); err == nil {
			d.AdminRoles = append(d.AdminRoles, k)
		}
	}
	rrows.Close()

	// Guilds via the user's discord_id (stored directly on app_users now).
	d.Guilds = []adminUserGuild{}
	if d.DiscordID != "" {
		grows, err := pool.Query(ctx, `SELECT guild_id, COALESCE(nickname,'') FROM user_guilds WHERE discord_id = $1 AND left_at IS NULL ORDER BY guild_id`, d.DiscordID)
		if err == nil {
			for grows.Next() {
				var g adminUserGuild
				if err := grows.Scan(&g.GuildID, &g.Nick); err == nil {
					d.Guilds = append(d.Guilds, g)
				}
			}
			grows.Close()
		}
	}

	// Recent audit (5 rows)
	d.RecentAudit = fetchUserAudit(ctx, pool, id, 5, "")
	writeJSON(w, http.StatusOK, d)
}

// ListAdminUserAudit: GET /api/app/admin/users/{id}/audit?limit=&before=
func (h *Handler) ListAdminUserAudit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		parseLimit(v, &limit)
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	before := r.URL.Query().Get("before")
	items := fetchUserAudit(ctx, h.postgres.Pool(), id, limit, before)
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// fetchUserAudit merges audit_events ∪ app_audit_events for a given target user,
// ordered by created_at desc, with an optional RFC3339 before cursor.
func fetchUserAudit(ctx context.Context, pool *pgxpool.Pool, userID string, limit int, before string) []auditEventItem {
	args := []any{userID}
	cursorClause := ""
	if before != "" {
		if t, err := time.Parse(time.RFC3339, before); err == nil {
			args = append(args, t)
			cursorClause = fmt.Sprintf(" AND created_at < $%d", len(args))
		}
	}
	args = append(args, limit)
	limitIdx := len(args)

	hasLegacy, _ := tableExists(ctx, pool, "app_audit_events")

	// New audit_events: match by target_user_id OR by params.filters.ids
	// containing the userId. The latter surfaces export events that targeted
	// this user via ?ids=<list>.
	newQ := fmt.Sprintf(`
		SELECT id::text, COALESCE(actor_user_id::text,''), COALESCE(target_user_id::text,''),
		       action_type, COALESCE(params,'{}'::jsonb), created_at
		FROM audit_events
		WHERE (
			target_user_id::text = $1
			OR (params -> 'filters' -> 'ids') @> to_jsonb($1::text)
		)%s
	`, cursorClause)

	var q string
	if hasLegacy {
		q = fmt.Sprintf(`%s
			UNION ALL
			SELECT id::text, COALESCE(actor_user_id,''), COALESCE(target_user_id,''),
			       event_type, COALESCE(payload,'{}'::jsonb), created_at
			FROM app_audit_events
			WHERE target_user_id = $1%s
			ORDER BY created_at DESC
			LIMIT $%d
		`, newQ, cursorClause, limitIdx)
	} else {
		q = fmt.Sprintf(`%s
			ORDER BY created_at DESC
			LIMIT $%d
		`, newQ, limitIdx)
	}

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return []auditEventItem{}
	}
	defer rows.Close()
	out := []auditEventItem{}
	for rows.Next() {
		var it auditEventItem
		var payload []byte
		if err := rows.Scan(&it.ID, &it.ActorUserID, &it.TargetUserID, &it.EventType, &payload, &it.CreatedAt); err != nil {
			continue
		}
		it.Payload = map[string]any{}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &it.Payload)
		}
		out = append(out, it)
	}
	return out
}

func tableExists(ctx context.Context, pool *pgxpool.Pool, table string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
		table,
	).Scan(&exists)
	return exists, err
}
