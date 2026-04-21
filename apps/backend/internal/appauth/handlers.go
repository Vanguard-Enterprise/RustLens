package appauth

import (
	"encoding/json"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	repo *Repo
}

func NewHandler(repo *Repo) *Handler { return &Handler{repo: repo} }

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login handles POST /api/app/auth/login.
// Body: { "email", "password" }. Response: CurrentUser.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, "email_and_password_required")
		return
	}

	userID, hash, err := h.repo.LookupPasswordLogin(r.Context(), req.Email)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	if err := h.repo.TouchLastLogin(r.Context(), userID); err != nil {
		// non-fatal; the login still succeeds
	}
	cu, err := h.repo.LoadCurrentUser(r.Context(), userID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "load_user_failed")
		return
	}
	writeJSON(w, http.StatusOK, cu)
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
