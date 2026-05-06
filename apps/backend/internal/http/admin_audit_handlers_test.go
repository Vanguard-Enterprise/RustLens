package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// seedAuditEvent inserts a row directly into audit_events. Returns the id (uuid string).
func seedAuditEvent(t *testing.T, ts *testHandlerServer, actionType, actor, target, result, reason string, params map[string]any) string {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	pj, _ := json.Marshal(params)
	var id string
	q := `
		INSERT INTO audit_events (action_type, actor_user_id, target_user_id, resource_type, resource_id, params, result, reason)
		VALUES ($1, NULLIF($2,'')::uuid, NULLIF($3,'')::uuid, 'user', COALESCE(NULLIF($3,''), ''), $4::jsonb, NULLIF($5,''), NULLIF($6,''))
		RETURNING id::text
	`
	if err := ts.pool.QueryRow(context.Background(), q, actionType, actor, target, string(pj), result, reason).Scan(&id); err != nil {
		t.Fatalf("seed audit event: %v", err)
	}
	return id
}

func truncateAuditEvents(t *testing.T, ts *testHandlerServer) {
	t.Helper()
	if _, err := ts.pool.Exec(context.Background(), `TRUNCATE audit_events`); err != nil {
		t.Fatalf("truncate audit_events: %v", err)
	}
}

func decodeAuditList(t *testing.T, body []byte) struct {
	Items      []auditEventItem `json:"items"`
	NextCursor *string          `json:"nextCursor"`
} {
	t.Helper()
	var out struct {
		Items      []auditEventItem `json:"items"`
		NextCursor *string          `json:"nextCursor"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, string(body))
	}
	return out
}

func TestListAdminAudit_FilterByTargetUserId(t *testing.T) {
	ts := newTestHandlerServer(t)
	truncateAuditEvents(t, ts)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "Actor", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "Target1", seedOpts{})
	seedUser(t, ts.pool, "33333333-3333-3333-3333-333333333333", "Target2", seedOpts{})
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "abuse", nil)
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "33333333-3333-3333-3333-333333333333", "success", "abuse", nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/audit?targetUserId=22222222-2222-2222-2222-222222222222", nil)
	ts.h.ListAdminAudit(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	got := decodeAuditList(t, rr.Body.Bytes())
	if len(got.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1; %+v", len(got.Items), got.Items)
	}
	if got.Items[0].TargetUserID != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("targetUserId = %q", got.Items[0].TargetUserID)
	}
}

func TestListAdminAudit_FilterByTargetUserId_MatchesParamsFiltersIds(t *testing.T) {
	ts := newTestHandlerServer(t)
	truncateAuditEvents(t, ts)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "Actor", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "Target", seedOpts{})
	seedAuditEvent(t, ts, "users.export", "11111111-1111-1111-1111-111111111111", "", "success", "", map[string]any{
		"filters": map[string]any{"ids": []string{"22222222-2222-2222-2222-222222222222"}},
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/audit?targetUserId=22222222-2222-2222-2222-222222222222", nil)
	ts.h.ListAdminAudit(rr, req)
	got := decodeAuditList(t, rr.Body.Bytes())
	if len(got.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1 (export-style)", len(got.Items))
	}
	if got.Items[0].EventType != "users.export" {
		t.Errorf("eventType = %q", got.Items[0].EventType)
	}
}

func TestListAdminAudit_FilterByActorUserId(t *testing.T) {
	ts := newTestHandlerServer(t)
	truncateAuditEvents(t, ts)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "ActorA", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "ActorB", seedOpts{})
	seedUser(t, ts.pool, "33333333-3333-3333-3333-333333333333", "Tgt", seedOpts{})
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "33333333-3333-3333-3333-333333333333", "success", "", nil)
	seedAuditEvent(t, ts, "users.suspend", "22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333", "success", "", nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/audit?actorUserId=11111111-1111-1111-1111-111111111111", nil)
	ts.h.ListAdminAudit(rr, req)
	got := decodeAuditList(t, rr.Body.Bytes())
	if len(got.Items) != 1 || got.Items[0].ActorUserID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("got %+v", got.Items)
	}
}

func TestListAdminAudit_FilterByEventTypeRepeatable(t *testing.T) {
	ts := newTestHandlerServer(t)
	truncateAuditEvents(t, ts)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "A", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "T", seedOpts{})
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)
	seedAuditEvent(t, ts, "users.delete", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)
	seedAuditEvent(t, ts, "users.edit", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/audit?eventType=users.suspend&eventType=users.delete", nil)
	ts.h.ListAdminAudit(rr, req)
	got := decodeAuditList(t, rr.Body.Bytes())
	if len(got.Items) != 2 {
		t.Fatalf("len = %d", len(got.Items))
	}
	for _, it := range got.Items {
		if it.EventType != "users.suspend" && it.EventType != "users.delete" {
			t.Errorf("unexpected eventType %q", it.EventType)
		}
	}
}

func TestListAdminAudit_FilterByResultExcludesLegacy(t *testing.T) {
	ts := newTestHandlerServer(t)
	truncateAuditEvents(t, ts)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "A", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "T", seedOpts{})
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "error", "boom", nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/audit?result=error", nil)
	ts.h.ListAdminAudit(rr, req)
	got := decodeAuditList(t, rr.Body.Bytes())
	if len(got.Items) != 1 || got.Items[0].Result == nil || *got.Items[0].Result != "error" {
		t.Fatalf("got %+v", got.Items)
	}
}

func TestListAdminAudit_PaginationCursor(t *testing.T) {
	ts := newTestHandlerServer(t)
	truncateAuditEvents(t, ts)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "A", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "T", seedOpts{})
	for i := 0; i < 5; i++ {
		seedAuditEvent(t, ts, fmt.Sprintf("users.evt.%d", i), "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/audit?pageSize=2", nil)
	ts.h.ListAdminAudit(rr, req)
	page1 := decodeAuditList(t, rr.Body.Bytes())
	if len(page1.Items) != 2 || page1.NextCursor == nil {
		t.Fatalf("page1 len=%d nextCursor=%v", len(page1.Items), page1.NextCursor)
	}

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/audit?pageSize=2&before="+*page1.NextCursor, nil)
	ts.h.ListAdminAudit(rr2, req2)
	page2 := decodeAuditList(t, rr2.Body.Bytes())
	if len(page2.Items) != 2 || page2.NextCursor == nil {
		t.Fatalf("page2 len=%d nextCursor=%v", len(page2.Items), page2.NextCursor)
	}
	for _, a := range page1.Items {
		for _, b := range page2.Items {
			if a.ID == b.ID {
				t.Errorf("overlap id=%s", a.ID)
			}
		}
	}

	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("GET", "/audit?pageSize=2&before="+*page2.NextCursor, nil)
	ts.h.ListAdminAudit(rr3, req3)
	page3 := decodeAuditList(t, rr3.Body.Bytes())
	if len(page3.Items) != 1 || page3.NextCursor != nil {
		t.Fatalf("page3 len=%d nextCursor=%v", len(page3.Items), page3.NextCursor)
	}
}

// auditRouter helper used by sibling tests.
func auditRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/audit", h.ListAdminAudit)
	return r
}

var _ = auditRouter
