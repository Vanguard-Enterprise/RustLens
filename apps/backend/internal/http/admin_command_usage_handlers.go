package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/store"
)

// AdminGetUserCommandUsage: GET /api/app/admin/users/{id}/command-usage?page=&pageSize=
//
// Returns a paginated list of bot interactions for the user plus a 90-day
// summary (total, error count, error rate, top 5 commands).
func (h *Handler) AdminGetUserCommandUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}

	page := 1
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	pageSize := 25
	if v := r.URL.Query().Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}
	offset := (page - 1) * pageSize

	listed, err := h.postgres.ListUserCommandUsage(ctx, id, pageSize, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list command usage")
		return
	}

	summary, err := h.postgres.SummarizeUserCommandUsage(ctx, id, 90*24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to summarize command usage")
		return
	}

	var errorRate float64
	if summary.TotalCount > 0 {
		errorRate = float64(summary.ErrorCount) / float64(summary.TotalCount)
	}

	// Ensure items is never null in the JSON response.
	items := listed.Items
	if items == nil {
		items = []store.CommandUsageRow{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"total":    listed.Total,
		"page":     page,
		"pageSize": pageSize,
		"summary": map[string]any{
			"totalCount":  summary.TotalCount,
			"errorCount":  summary.ErrorCount,
			"errorRate":   errorRate,
			"topCommands": summary.TopCommands,
		},
	})
}
