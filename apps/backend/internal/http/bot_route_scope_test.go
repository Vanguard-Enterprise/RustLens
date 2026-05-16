package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/clientip"
)

type stubResolver struct{ perms map[string]bool }

func (s stubResolver) HasPermission(_ context.Context, _ string, key string) (bool, error) {
	return s.perms[key], nil
}

type stubAuditSink struct{ recorded []actions.AuditEntryInput }

func (s *stubAuditSink) RecordEntry(_ context.Context, e actions.AuditEntryInput) error {
	s.recorded = append(s.recorded, e)
	return nil
}

func TestBotRouteScopeNoOpWhenNoAPIKey(t *testing.T) {
	mw := requireBotRouteScope(stubResolver{}, &stubAuditSink{}, clientip.Headers{})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))

	req := httptest.NewRequest(http.MethodGet, "/api/internal/bot/anything", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !called || w.Code != http.StatusOK {
		t.Fatalf("expected pass-through; got code=%d called=%v", w.Code, called)
	}
}

func TestBotRouteScopeGETRequiresRead(t *testing.T) {
	audit := &stubAuditSink{}
	mw := requireBotRouteScope(stubResolver{perms: map[string]bool{}}, audit, clientip.Headers{})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { t.Fatal("should not call") }))

	req := httptest.NewRequest(http.MethodGet, "/api/internal/bot/x", nil)
	ctx := actions.WithAPIKeyAuth(req.Context(), &actions.APIKeyAuthContext{KeyID: "k", UserID: "u"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req.WithContext(ctx))
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d", w.Code)
	}
	if len(audit.recorded) != 0 {
		t.Errorf("read audit should not be written: %+v", audit.recorded)
	}
}

func TestBotRouteScopePOSTRequiresWriteAndAudits(t *testing.T) {
	audit := &stubAuditSink{}
	mw := requireBotRouteScope(stubResolver{perms: map[string]bool{"bot_routes.write": true}}, audit, clientip.Headers{})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(201) }))

	req := httptest.NewRequest(http.MethodPost, "/api/internal/bot/x", nil)
	ctx := actions.WithAPIKeyAuth(req.Context(), &actions.APIKeyAuthContext{
		KeyID: "k", KeyName: "deploy", UserID: "u",
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req.WithContext(ctx))
	if w.Code != 201 {
		t.Fatalf("got %d", w.Code)
	}
	if len(audit.recorded) != 1 {
		t.Fatalf("want 1 audit row, got %d", len(audit.recorded))
	}
	e := audit.recorded[0]
	if e.ActionType != "bot_route.write" || e.ResourceType != "bot_route" {
		t.Errorf("bad audit: %+v", e)
	}
	if e.ViaAPIKeyID == nil || *e.ViaAPIKeyID != "k" {
		t.Errorf("missing via_api_key_id")
	}
}
