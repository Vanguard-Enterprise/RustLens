package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"rustlens/backend/internal/admin/audit"
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
// provided filters. Requires the users.export permission. Writes paired
// users.export.started + users.export.completed audit events so every export
// attempt is permanently logged with full filter context, even if streaming
// fails or the client disconnects mid-flight.
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

	// Build the started audit event before any bytes leave the server. If
	// streaming fails halfway through, we still know an export was attempted.
	exportID := uuid.NewString()
	filters := extractExportFilters(r.URL.Query())
	var targetUserID *string
	if ids, ok := filters["ids"].([]string); ok && len(ids) == 1 {
		t := ids[0]
		targetUserID = &t
	}
	actorID := session.UserID
	ip := clientIPForAudit(r)
	ua := r.Header.Get("User-Agent")
	var ipPtr *string
	if parsed := net.ParseIP(ip); parsed != nil {
		s := parsed.String()
		ipPtr = &s
	}

	startedParams, _ := json.Marshal(map[string]any{
		"exportId":         exportID,
		"filters":          filters,
		"expectedRowCount": total,
	})
	if err := h.auditStore.Record(ctx, audit.Entry{
		ActionType:        "users.export.started",
		ActorUserID:       &actorID,
		ActorRoleSnapshot: session.Roles,
		ResourceType:      "user_export",
		ResourceID:        exportID,
		TargetUserID:      targetUserID,
		Params:            startedParams,
		Result:            "success",
		IPAddress:         ipPtr,
		UserAgent:         &ua,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// completionResult is captured for the paired completion row written below.
	completionResult := "success"
	var completionErr *string
	rowsWritten := 0
	startedAt := time.Now()
	cw := &countingWriter{w: w}

	// Defer the completion row so timeouts, panics during streaming, or
	// client disconnects still produce a paired event.
	defer func() {
		if rec := recover(); rec != nil {
			completionResult = "error"
			msg := fmt.Sprintf("panic during export: %v", rec)
			completionErr = &msg
			// fall through to write the row, then re-panic
			defer panic(rec)
		}
		duration := time.Since(startedAt).Milliseconds()
		// Use a fresh context so we still write completion even if the request
		// context was cancelled or timed out.
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer bgCancel()
		completedParams, _ := json.Marshal(map[string]any{
			"exportId":     exportID,
			"rowsWritten":  rowsWritten,
			"bytesWritten": cw.n,
			"durationMs":   duration,
		})
		_ = h.auditStore.Record(bgCtx, audit.Entry{
			ActionType:        "users.export.completed",
			ActorUserID:       &actorID,
			ActorRoleSnapshot: session.Roles,
			ResourceType:      "user_export",
			ResourceID:        exportID,
			TargetUserID:      targetUserID,
			Params:            completedParams,
			Result:            completionResult,
			ErrorMessage:      completionErr,
			IPAddress:         ipPtr,
			UserAgent:         &ua,
		})
	}()

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
		completionResult = "error"
		msg := err.Error()
		completionErr = &msg
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	writer := csv.NewWriter(cw)
	flusher, _ := w.(http.Flusher)

	if err := writer.Write(exportRowColumns); err != nil {
		completionResult = "error"
		msg := err.Error()
		completionErr = &msg
		return
	}

	for rows.Next() {
		var (
			id, name, status                      string
			discordID, discordName, discordGlobal string
			adminRoles                            []string
			guildCount                            int64
			createdAt, lastLoginAt                time.Time
		)
		if err := rows.Scan(
			&id, &name, &discordID, &discordName, &discordGlobal, &status,
			&adminRoles, &guildCount, &createdAt, &lastLoginAt,
		); err != nil {
			completionResult = "error"
			msg := err.Error()
			completionErr = &msg
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
			completionResult = "error"
			msg := err.Error()
			completionErr = &msg
			return
		}
		rowsWritten++
		if rowsWritten%csvFlushInterval == 0 {
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
	if err := rows.Err(); err != nil {
		completionResult = "error"
		msg := err.Error()
		completionErr = &msg
		return
	}
}

// countingWriter wraps http.ResponseWriter to count bytes written, used for
// the bytesWritten audit field. It only forwards Write — Header/Flush still go
// through the underlying ResponseWriter via the original handle.
type countingWriter struct {
	w http.ResponseWriter
	n int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += n
	return n, err
}

// clientIPForAudit returns the request's remote IP suitable for audit
// logging. Uses net.SplitHostPort so IPv6 addresses like [::1]:1234 are
// handled correctly. RealIP middleware can leave RemoteAddr as a bare IP
// without a port; in that case SplitHostPort returns an error and we return
// the raw value as-is.
func clientIPForAudit(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RealIP middleware can leave RemoteAddr as a bare IP without a port.
		return r.RemoteAddr
	}
	return host
}

// extractExportFilters captures the filter querystring as a structured map for
// audit logging. Only includes fields that were actually supplied; this keeps
// audit rows readable and makes JSONB queries (e.g. params -> 'filters' ->
// 'ids' ? $userId) reliable.
func extractExportFilters(q url.Values) map[string]any {
	out := map[string]any{}
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		out["q"] = v
	}
	if roles, ok := q["role"]; ok && len(roles) > 0 {
		out["role"] = roles
	}
	if v := q.Get("status"); v != "" {
		out["status"] = v
	}
	if v := q.Get("discord"); v != "" {
		out["discord"] = v
	}
	if v := q.Get("steam"); v != "" {
		out["steam"] = v
	}
	if v := q.Get("sort"); v != "" {
		out["sort"] = v
	}
	if rawIDs := strings.TrimSpace(q.Get("ids")); rawIDs != "" {
		parts := strings.Split(rawIDs, ",")
		cleaned := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				cleaned = append(cleaned, p)
			}
		}
		out["ids"] = cleaned
	}
	return out
}
