package appauth

import (
	"context"
	"encoding/json"
	"net/http"

	"rustlens/backend/internal/store"
)

// RoleConnectionSummaryStore is the subset of store needed by Me to surface
// the dashboard role-connection summary.
type RoleConnectionSummaryStore interface {
	GetDiscordRoleConnectionSummary(ctx context.Context, userID string) (store.DiscordRoleConnectionSummary, error)
}

type Handler struct {
	repo    *Repo
	rcStore RoleConnectionSummaryStore // may be nil
}

func NewHandler(repo *Repo) *Handler { return &Handler{repo: repo} }

func NewHandlerWithRoleConnection(repo *Repo, rc RoleConnectionSummaryStore) *Handler {
	return &Handler{repo: repo, rcStore: rc}
}

// Me handles GET /api/app/me. Requires the RequireSession middleware.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	session := SessionFromContext(r.Context())
	if session == nil {
		writeJSONError(w, http.StatusUnauthorized, "session_required")
		return
	}
	cu, err := h.repo.LoadCurrentUser(r.Context(), session.UserID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "user_not_found")
		return
	}
	if h.rcStore != nil {
		summary, err := h.rcStore.GetDiscordRoleConnectionSummary(r.Context(), session.UserID)
		if err == nil {
			cu.DiscordRoleConnection = &summary
		}
	}
	writeJSON(w, http.StatusOK, cu)
}

// Logout is a no-op on the backend — sessions are held in a signed cookie on
// the frontend. The frontend clears the cookie; this endpoint exists so the
// frontend can call a named logout route if it wants.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
