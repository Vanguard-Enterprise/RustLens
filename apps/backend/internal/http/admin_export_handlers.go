package httpapi

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/appauth"
)

// adminUsersExportRowCap bounds the number of rows that can stream through
// ExportAdminUsersCSV. Tests may override this to exercise the 413 path.
var adminUsersExportRowCap = 100_000

// adminUsersExportTimeout is the wall-clock cap on a single export stream.
var adminUsersExportTimeout = 60 * time.Second

// csvFlushInterval is how many rows are written between explicit flushes.
const csvFlushInterval = 1000

// exportRowColumns is the ordered list of columns written to the CSV. The
// email and email-verified columns from the legacy schema were dropped along
// with user_emails; Discord identity columns replace them as the user's
// primary handle.
var exportRowColumns = []string{
	"id", "display_name", "discord_id", "discord_username",
	"discord_global_name", "status", "admin_roles",
	"guild_count", "created_at", "last_login_at",
}

// ExportAdminUsersCSV streams a CSV snapshot of admin users matching the
// provided filters. Requires the users.export permission.
func (h *Handler) ExportAdminUsersCSV(w http.ResponseWriter, r *http.Request) {
	session := appauth.SessionFromContext(r.Context())
	if session == nil || session.UserID == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), adminUsersExportTimeout)
	defer cancel()

	allowed, err := h.rbacResolver.HasPermission(ctx, session.UserID, rbac.PermUsersExport)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "permission_denied")
		return
	}

	whereSQL, args, orderBy, err := buildUsersQuery(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	pool := h.postgres.Pool()

	// Count first to enforce the row cap.
	var total int
	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM app_users u %s`, whereSQL)
	if err := pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if total > adminUsersExportRowCap {
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("row count %d exceeds export cap %d", total, adminUsersExportRowCap))
		return
	}

	// Streaming headers.
	filename := fmt.Sprintf("users-%s.csv", time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")

	listSQL := fmt.Sprintf(`
		SELECT u.id, u.display_name,
		       COALESCE(u.discord_id, '') AS discord_id,
		       COALESCE(u.discord_username, '') AS discord_username,
		       COALESCE(u.discord_global_name, '') AS discord_global_name,
		       CASE
		         WHEN u.deleted_at IS NOT NULL THEN 'deleted'
		         WHEN u.suspended_at IS NOT NULL THEN 'suspended'
		         ELSE 'active'
		       END AS status,
		       COALESCE(array_agg(DISTINCT r.key) FILTER (WHERE r.key IS NOT NULL), ARRAY[]::text[]) AS admin_roles,
		       COALESCE((SELECT COUNT(*) FROM user_guilds g WHERE g.discord_id = u.discord_id AND g.left_at IS NULL), 0) AS guild_count,
		       u.created_at,
		       u.last_login_at
		FROM app_users u
		LEFT JOIN admin_user_roles aur ON aur.user_id = u.id
		LEFT JOIN admin_roles r ON r.id = aur.role_id
		%s
		GROUP BY u.id
		ORDER BY %s
	`, whereSQL, orderBy)

	rows, err := pool.Query(ctx, listSQL, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	writer := csv.NewWriter(w)
	flusher, _ := w.(http.Flusher)

	if err := writer.Write(exportRowColumns); err != nil {
		return
	}

	written := 0
	for rows.Next() {
		var (
			id, name, status                                string
			discordID, discordName, discordGlobal           string
			adminRoles                                      []string
			guildCount                                      int64
			createdAt, lastLoginAt                          time.Time
		)
		if err := rows.Scan(
			&id, &name, &discordID, &discordName, &discordGlobal, &status,
			&adminRoles, &guildCount, &createdAt, &lastLoginAt,
		); err != nil {
			return
		}
		record := []string{
			id,
			name,
			discordID,
			discordName,
			discordGlobal,
			status,
			strings.Join(adminRoles, "|"),
			strconv.FormatInt(guildCount, 10),
			createdAt.UTC().Format(time.RFC3339),
			lastLoginAt.UTC().Format(time.RFC3339),
		}
		if err := writer.Write(record); err != nil {
			return
		}
		written++
		if written%csvFlushInterval == 0 {
			writer.Flush()
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
	writer.Flush()
	if flusher != nil {
		flusher.Flush()
	}
}

