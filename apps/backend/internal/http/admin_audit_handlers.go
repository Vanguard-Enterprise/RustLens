package httpapi

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// auditEventTypesCache holds a 60-second in-process cache for the
// distinct-event-types endpoint. The DISTINCT scan is cheap on small data
// but can grow expensive once the audit tables have millions of rows; this
// cache dampens the cost without introducing a separate cache layer.
var auditEventTypesCache struct {
	mu    sync.Mutex
	at    time.Time
	types []string
}

const auditEventTypesCacheTTL = 60 * time.Second

func resetAuditEventTypesCacheForTest() {
	auditEventTypesCache.mu.Lock()
	auditEventTypesCache.at = time.Time{}
	auditEventTypesCache.types = nil
	auditEventTypesCache.mu.Unlock()
}

// ListAdminAuditEventTypes: GET /api/app/admin/audit/event-types
// Returns sorted distinct values from audit_events.action_type
// UNION app_audit_events.event_type. Cached 60s in-process.
func (h *Handler) ListAdminAuditEventTypes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	auditEventTypesCache.mu.Lock()
	if !auditEventTypesCache.at.IsZero() && time.Since(auditEventTypesCache.at) < auditEventTypesCacheTTL {
		out := append([]string(nil), auditEventTypesCache.types...)
		auditEventTypesCache.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"types": out})
		return
	}
	auditEventTypesCache.mu.Unlock()

	pool := h.postgres.Pool()
	types, err := loadAuditEventTypes(ctx, pool)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	auditEventTypesCache.mu.Lock()
	auditEventTypesCache.at = time.Now()
	auditEventTypesCache.types = append([]string(nil), types...)
	auditEventTypesCache.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"types": types})
}

func loadAuditEventTypes(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	hasLegacy, _ := tableExists(ctx, pool, "app_audit_events")
	q := `SELECT DISTINCT action_type AS t FROM audit_events WHERE action_type IS NOT NULL AND action_type <> ''`
	if hasLegacy {
		q += ` UNION SELECT DISTINCT event_type AS t FROM app_audit_events WHERE event_type IS NOT NULL AND event_type <> ''`
	}
	rows, err := pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
