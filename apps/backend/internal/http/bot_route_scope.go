package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"rustlens/backend/internal/admin/actions"
)

// requireBotRouteScope is mounted on the /api/internal/bot/* subrouter after
// the APIKeyAuth middleware and before the bot JWT middleware. When the
// request was authenticated by an API key (apiKeyAuth on context), it requires
// bot_routes.read on GET/HEAD and bot_routes.write on everything else, and
// writes one audit_events row per write. When apiKeyAuth is not on the
// context the middleware is a no-op (bot JWT traffic).
func requireBotRouteScope(perms actions.PermissionChecker, audit actions.AuditSink) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := actions.APIKeyAuthFromContext(r.Context())
			if k == nil {
				next.ServeHTTP(w, r)
				return
			}

			isRead := r.Method == http.MethodGet || r.Method == http.MethodHead
			required := "bot_routes.write"
			if isRead {
				required = "bot_routes.read"
			}

			ok, err := perms.HasPermission(r.Context(), k.UserID, required)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"permission_check_failed"}`))
				return
			}
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"forbidden"}`))
				return
			}

			if isRead {
				next.ServeHTTP(w, r)
				return
			}

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			result := "success"
			if rec.status >= 400 {
				result = "error"
			}
			paramsJSON, _ := json.Marshal(map[string]any{
				"method": r.Method,
				"status": rec.status,
			})
			id := k.KeyID
			name := k.KeyName
			ip := clientIP(r)
			_ = audit.RecordEntry(context.Background(), actions.AuditEntryInput{
				ActionType:    "bot_route.write",
				ActorUserID:   k.UserID,
				ResourceType:  "bot_route",
				ResourceID:    r.URL.Path,
				Params:        paramsJSON,
				Result:        result,
				IPAddress:     optionalStringPtr(ip),
				ViaAPIKeyID:   &id,
				ViaAPIKeyName: &name,
			})
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func optionalStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
