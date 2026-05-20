package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/store"
)

// AdminGetUserCommandUsage: GET /api/app/admin/users/{id}/command-usage?page=&pageSize=&q=&command=&guildId=&outcome=&interactionType=&windowDays=
//
// Returns a paginated list of bot interactions for the user (with embedded
// targets) plus a 90-day summary (total, error count, error rate, top 5 commands).
func (h *Handler) AdminGetUserCommandUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}

	q := r.URL.Query()

	page := 1
	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	pageSize := 25
	if v := q.Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}
	offset := (page - 1) * pageSize

	filters := store.CommandUsageListFilters{
		Q:       q.Get("q"),
		Command: q.Get("command"),
		GuildID: q.Get("guildId"),
	}
	if v := q.Get("outcome"); v != "" {
		filters.Outcomes = splitAndValidate(v, map[string]struct{}{
			"success": {}, "error": {}, "banned": {}, "denied": {},
		})
	}
	if v := q.Get("interactionType"); v != "" {
		filters.InteractionTypes = splitAndValidate(v, map[string]struct{}{
			"command": {}, "button": {}, "modal": {}, "string_select": {},
		})
	}
	if v := q.Get("windowDays"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			switch n {
			case 1, 7, 30, 90:
				filters.WindowDays = n
			}
		}
	}

	listed, err := h.postgres.ListUserCommandUsageFiltered(ctx, id, filters, pageSize, offset)
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
		items = []store.CommandUsageRowWithTargets{}
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

// splitAndValidate splits a comma-separated string and keeps only values that
// appear in the allowed set. Unknown values are silently dropped.
func splitAndValidate(csv string, allowed map[string]struct{}) []string {
	out := make([]string, 0, 4)
	for _, p := range strings.Split(csv, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := allowed[p]; ok {
			out = append(out, p)
		}
	}
	return out
}

// AdminGetUserCommandUsageFilterOptions: GET /api/app/admin/users/{id}/command-usage/filter-options
func (h *Handler) AdminGetUserCommandUsageFilterOptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	windowDays := 90
	if v := r.URL.Query().Get("windowDays"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			switch n {
			case 1, 7, 30, 90:
				windowDays = n
			}
		}
	}
	opts, err := h.postgres.CommandUsageFilterOptions(ctx, id, windowDays)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load filter options")
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

// AdminGetCommandUsageChildren: GET /api/app/admin/users/{id}/command-usage/{usageId}/children
//
// Returns follow-up rows (modal submits, button presses, select interactions)
// that share the given parent's id, scoped to the user in the URL path. A
// usageId that exists but belongs to a different user returns an empty list
// (not 404) so admins can't enumerate other users' usage row IDs.
func (h *Handler) AdminGetCommandUsageChildren(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	idStr := chi.URLParam(r, "usageId")
	usageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || usageID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid usageId")
		return
	}
	children, err := h.postgres.ListUserCommandUsageChildren(ctx, id, usageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load children")
		return
	}
	if children == nil {
		children = []store.CommandUsageRowWithTargets{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": children})
}

// AdminGetCommandUsageTargets: GET /api/app/admin/users/{id}/command-usage/{usageId}/targets
//
// Returns the full target list for a single usage row, scoped to the user
// in the URL path. A usageId that exists but belongs to a different user
// returns an empty list (not 404) so admins can't enumerate other users'
// usage row IDs.
func (h *Handler) AdminGetCommandUsageTargets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	idStr := chi.URLParam(r, "usageId")
	usageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || usageID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid usageId")
		return
	}
	targets, err := h.postgres.ListCommandUsageTargetsForUser(ctx, id, usageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load targets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": targets})
}
