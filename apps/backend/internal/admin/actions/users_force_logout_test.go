package actions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/audit"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/events"
	"rustlens/backend/internal/store"
)

func newForceLogoutHarness(t *testing.T) (
	srv *httptest.Server,
	s *store.PostgresStore,
	setPerms func(map[string]bool),
	pub *events.RecordingPublisher,
) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()
	st, err := store.NewPostgresStore(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(st.Close)
	if err := st.EnsureSchema(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}
	rb := rbac.NewStore(st.Pool())
	if err := rb.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	au := audit.NewStore(st.Pool())

	perms := &fakePermResolver{allow: map[string]bool{}}
	pub = &events.RecordingPublisher{}
	reg := actions.NewRegistry(rbac.IsValidPermission)
	if err := reg.Register(actions.NewUsersForceLogoutAction(st.Pool(), pub)); err != nil {
		t.Fatalf("register: %v", err)
	}

	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv = httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, st, func(a map[string]bool) { perms.allow = a }, pub
}

func TestUsersForceLogout_BumpsEpochAndPublishesRevoked(t *testing.T) {
	srv, s, setPerms, pub := newForceLogoutHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSecurityReset: true})

	const target = "usr_force_target_1"
	seedAppUser(t, s, target, "Target", false)
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE app_users SET session_epoch = 3 WHERE id = $1`, target,
	); err != nil {
		t.Fatalf("seed epoch: %v", err)
	}
	// Also seed the actor so audit FK doesn't fail.
	seedAppUser(t, s, "usr_test_actor", "Actor", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": target},
		"reason": "abuse",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.force_logout", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}

	var epoch int
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT session_epoch FROM app_users WHERE id = $1`, target,
	).Scan(&epoch); err != nil {
		t.Fatalf("read epoch: %v", err)
	}
	if epoch != 4 {
		t.Fatalf("expected epoch 4, got %d", epoch)
	}

	recs := pub.Snapshot()
	if len(recs) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(recs))
	}
	if recs[0].UserID != target {
		t.Fatalf("publish userID = %q", recs[0].UserID)
	}
	if recs[0].Event.Type != events.TypeSessionRevoked || recs[0].Event.Reason != events.ReasonForceLogout {
		t.Fatalf("event = %+v", recs[0].Event)
	}
}

func TestUsersForceLogout_RejectsSelf(t *testing.T) {
	srv, s, setPerms, _ := newForceLogoutHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSecurityReset: true})

	// fixedActor returns "usr_test_actor" — seed that user and target the same id.
	const self = "usr_test_actor"
	seedAppUser(t, s, self, "Actor", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": self},
		"reason": "self",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.force_logout", body)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected non-200, got 200 body=%s", readBody(resp))
	}
}
