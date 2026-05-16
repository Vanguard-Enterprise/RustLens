package httpapi

import (
	"net/http"
	"strconv"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/store"
)

func (h *Handler) ListAdminAPIKeys(w http.ResponseWriter, r *http.Request) {
	var userID string
	if k := actions.APIKeyAuthFromContext(r.Context()); k != nil {
		userID = k.UserID
	} else if session := appauth.SessionFromContext(r.Context()); session != nil && session.UserID != "" {
		userID = session.UserID
	} else {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return
	}
	ok, err := h.rbacResolver.HasPermission(r.Context(), userID, rbac.PermAPIKeysManage)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "permission_check_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	q := store.APIKeyListQuery{
		Q:              r.URL.Query().Get("q"),
		IncludeRevoked: r.URL.Query().Get("includeRevoked") == "true",
		Page:           parseIntOr(r.URL.Query().Get("page"), 1),
		PageSize:       parseIntOr(r.URL.Query().Get("pageSize"), 20),
	}
	items, total, err := store.NewAPIKeyStore(h.postgres.Pool()).List(r.Context(), q)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}

	type respItem struct {
		ID                   string   `json:"id"`
		Name                 string   `json:"name"`
		Prefix               string   `json:"prefix"`
		Last4                string   `json:"last4"`
		Permissions          []string `json:"permissions"`
		CreatedByUserID      string   `json:"createdByUserId"`
		CreatedByDisplayName string   `json:"createdByDisplayName"`
		ExpiresAt            *string  `json:"expiresAt"`
		RevokedAt            *string  `json:"revokedAt"`
		RevokedReason        *string  `json:"revokedReason"`
		LastUsedAt           *string  `json:"lastUsedAt"`
		LastUsedIP           *string  `json:"lastUsedIp"`
		CreatedAt            string   `json:"createdAt"`
	}
	out := make([]respItem, 0, len(items))
	for _, it := range items {
		ri := respItem{
			ID: it.ID, Name: it.Name, Prefix: it.Prefix, Last4: it.Last4,
			Permissions: it.Permissions, CreatedByUserID: it.CreatedByUserID,
			CreatedByDisplayName: it.CreatedByDisplayName,
			LastUsedIP:           it.LastUsedIP,
			CreatedAt:            it.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if it.ExpiresAt != nil {
			s := it.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
			ri.ExpiresAt = &s
		}
		if it.RevokedAt != nil {
			s := it.RevokedAt.UTC().Format("2006-01-02T15:04:05Z")
			ri.RevokedAt = &s
		}
		ri.RevokedReason = it.RevokedReason
		if it.LastUsedAt != nil {
			s := it.LastUsedAt.UTC().Format("2006-01-02T15:04:05Z")
			ri.LastUsedAt = &s
		}
		out = append(out, ri)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":    out,
		"total":    total,
		"page":     q.Page,
		"pageSize": q.PageSize,
	})
}

func parseIntOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
