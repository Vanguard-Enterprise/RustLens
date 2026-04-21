package actions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"rustlens/backend/internal/admin/actions"
)

// -- test action implementations --

type suspendAction struct {
	executeCalled bool
	failExecute   bool
}

func (a *suspendAction) Type() string                 { return "users.suspend" }
func (a *suspendAction) Permission() string           { return "users.suspend" }
func (a *suspendAction) RequiresReason() bool         { return true }
func (a *suspendAction) Dangerous() bool              { return false }
func (a *suspendAction) SensitiveParamKeys() []string { return nil }
func (a *suspendAction) ResourceType() string         { return "user" }
func (a *suspendAction) Validate(raw json.RawMessage) (actions.ValidatedParams, error) {
	var p struct {
		UserID uuid.UUID `json:"userId"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return actions.ValidatedParams{}, err
	}
	if p.UserID == uuid.Nil {
		return actions.ValidatedParams{}, errors.New("userId required")
	}
	tuid := p.UserID
	return actions.ValidatedParams{ResourceID: p.UserID.String(), TargetUserID: &tuid}, nil
}
func (a *suspendAction) Execute(ctx context.Context, in actions.ActionInput) (actions.ActionResult, error) {
	a.executeCalled = true
	if a.failExecute {
		return actions.ActionResult{}, errors.New("boom")
	}
	return actions.ActionResult{ResourceID: in.Validated.ResourceID, TargetUserID: in.Validated.TargetUserID, Payload: map[string]string{"status": "suspended"}}, nil
}

// -- fake audit sink --

type fakeAudit struct{ entries []actions.AuditEntryInput }

func (f *fakeAudit) RecordEntry(ctx context.Context, e actions.AuditEntryInput) error {
	f.entries = append(f.entries, e)
	return nil
}

// -- fake permission check --

type fakePermResolver struct{ allow map[string]bool }

func (f *fakePermResolver) HasPermission(ctx context.Context, userID uuid.UUID, key string) (bool, error) {
	return f.allow[key], nil
}
func (f *fakePermResolver) PermissionsForUser(ctx context.Context, _ uuid.UUID) ([]string, error) {
	out := []string{}
	for k, v := range f.allow {
		if v {
			out = append(out, k)
		}
	}
	return out, nil
}

// -- helpers --

func newDispatcher(t *testing.T, a actions.AdminAction, resolver *fakePermResolver) (*httptest.Server, *fakeAudit) {
	t.Helper()
	reg := actions.NewRegistry(func(string) bool { return true })
	if err := reg.Register(a); err != nil {
		t.Fatalf("register: %v", err)
	}
	au := &fakeAudit{}
	d := actions.NewDispatcher(reg, resolver, au, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", d.ServeHTTP)
	return httptest.NewServer(r), au
}

func fixedActor(r *http.Request) (uuid.UUID, []string, error) {
	return uuid.MustParse("00000000-0000-0000-0000-000000000001"), []string{"moderator"}, nil
}

// -- tests --

func TestDispatcher_Success(t *testing.T) {
	a := &suspendAction{}
	srv, au := newDispatcher(t, a, &fakePermResolver{allow: map[string]bool{"users.suspend": true}})
	defer srv.Close()

	body := mustJSON(t, map[string]any{
		"params": map[string]string{"userId": "11111111-1111-1111-1111-111111111111"},
		"reason": "spamming",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.suspend", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, body=%s", resp.StatusCode, readBody(resp))
	}
	if !a.executeCalled {
		t.Error("expected Execute to be called")
	}
	if len(au.entries) != 1 || au.entries[0].Result != "success" {
		t.Errorf("audit = %+v", au.entries)
	}
}

func TestDispatcher_PermissionDenied(t *testing.T) {
	a := &suspendAction{}
	srv, _ := newDispatcher(t, a, &fakePermResolver{allow: map[string]bool{}})
	defer srv.Close()
	body := mustJSON(t, map[string]any{"params": map[string]string{"userId": "11111111-1111-1111-1111-111111111111"}, "reason": "x"})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.suspend", body)
	if resp.StatusCode != 403 {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestDispatcher_ReasonRequired(t *testing.T) {
	a := &suspendAction{}
	srv, _ := newDispatcher(t, a, &fakePermResolver{allow: map[string]bool{"users.suspend": true}})
	defer srv.Close()
	body := mustJSON(t, map[string]any{"params": map[string]string{"userId": "11111111-1111-1111-1111-111111111111"}})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.suspend", body)
	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestDispatcher_UnknownAction(t *testing.T) {
	srv, _ := newDispatcher(t, &suspendAction{}, &fakePermResolver{allow: map[string]bool{"users.suspend": true}})
	defer srv.Close()
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/does.not.exist", []byte(`{}`))
	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDispatcher_ExecutionErrorWritesAuditError(t *testing.T) {
	a := &suspendAction{failExecute: true}
	srv, au := newDispatcher(t, a, &fakePermResolver{allow: map[string]bool{"users.suspend": true}})
	defer srv.Close()
	body := mustJSON(t, map[string]any{"params": map[string]string{"userId": "11111111-1111-1111-1111-111111111111"}, "reason": "x"})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.suspend", body)
	if resp.StatusCode < 500 {
		t.Errorf("expected 5xx, got %d", resp.StatusCode)
	}
	if len(au.entries) != 1 || au.entries[0].Result != "error" {
		t.Errorf("expected error audit, got %+v", au.entries)
	}
}

// -- util --

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
func mustPOST(t *testing.T, url string, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}
func readBody(r *http.Response) string {
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(r.Body)
	return buf.String()
}
