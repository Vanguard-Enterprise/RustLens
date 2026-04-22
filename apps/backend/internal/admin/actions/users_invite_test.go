package actions_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/audit"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

func newInviteHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, func(map[string]bool)) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()
	s, err := store.NewPostgresStore(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(s.Close)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}
	rb := rbac.NewStore(s.Pool())
	if err := rb.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	au := audit.NewStore(s.Pool())
	perms := &fakePermResolver{allow: map[string]bool{}}
	reg := actions.NewRegistry(rbac.IsValidPermission)
	if err := reg.Register(actions.NewUsersInviteAction(s.Pool(), "http://localhost:3000")); err != nil {
		t.Fatalf("register: %v", err)
	}
	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, func(a map[string]bool) { perms.allow = a }
}

type inviteResp struct {
	OK     bool `json:"ok"`
	Result struct {
		UserID   string `json:"userId"`
		TokenURL string `json:"tokenUrl"`
	} `json:"result"`
}

func TestUsersInvite_HappyPath(t *testing.T) {
	srv, s, setPerms := newInviteHarness(t)
	setPerms(map[string]bool{rbac.PermUsersCreate: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)

	email := "invite_ok@example.com"
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM email_sign_in_tokens WHERE email=$1`, email)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM user_emails WHERE email=$1`, email)

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"email": "Invite_OK@Example.COM", "displayName": "Invitee"},
		"reason":            "welcome",
		"typedConfirmation": email,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.invite", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var parsed inviteResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Validate user id pattern.
	if ok := regexp.MustCompile(`^usr_[0-9a-f]{24}$`).MatchString(parsed.Result.UserID); !ok {
		t.Errorf("userId = %q", parsed.Result.UserID)
	}
	if !strings.HasPrefix(parsed.Result.TokenURL, "http://localhost:3000/sign-in?token=") {
		t.Errorf("tokenUrl = %q", parsed.Result.TokenURL)
	}
	u, _ := url.Parse(parsed.Result.TokenURL)
	raw := u.Query().Get("token")
	if raw == "" {
		t.Fatalf("missing token")
	}
	h := sha256.Sum256([]byte(raw))
	want := hex.EncodeToString(h[:])

	var stored string
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT token_hash FROM email_sign_in_tokens WHERE email=$1 AND consumed_at IS NULL`, email).Scan(&stored); err != nil {
		t.Fatalf("query token: %v", err)
	}
	if stored != want {
		t.Errorf("stored %q want %q", stored, want)
	}

	// app_users is alive (not suspended / not deleted).
	var suspendedAt, deletedAt *string
	var primaryEmail string
	var isVerified bool
	if err := s.Pool().QueryRow(context.Background(), `
		SELECT u.suspended_at::text, u.deleted_at::text, e.email, e.is_verified
		  FROM app_users u JOIN user_emails e ON e.user_id=u.id AND e.is_primary
		 WHERE u.id=$1
	`, parsed.Result.UserID).Scan(&suspendedAt, &deletedAt, &primaryEmail, &isVerified); err != nil {
		t.Fatalf("query user: %v", err)
	}
	if suspendedAt != nil || deletedAt != nil {
		t.Errorf("new user should not be suspended/deleted: %v %v", suspendedAt, deletedAt)
	}
	if primaryEmail != email || isVerified {
		t.Errorf("primary email = %q verified=%v", primaryEmail, isVerified)
	}
}

func TestUsersInvite_DuplicateEmail(t *testing.T) {
	srv, s, setPerms := newInviteHarness(t)
	setPerms(map[string]bool{rbac.PermUsersCreate: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const existing = "usr_inv_dup"
	seedAppUser(t, s, existing, "Existing", false)
	shared := "dup_invite@example.com"
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM user_emails WHERE email=$1`, shared)
	seedEmailRow(t, s, existing, shared, true, true)

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"email": shared},
		"reason":            "dup",
		"typedConfirmation": shared,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.invite", body)
	if resp.StatusCode != 409 {
		t.Errorf("status=%d want 409 body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestUsersInvite_BadEmail(t *testing.T) {
	srv, s, setPerms := newInviteHarness(t)
	setPerms(map[string]bool{rbac.PermUsersCreate: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"email": "not-an-email"},
		"reason":            "bad",
		"typedConfirmation": "not-an-email",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.invite", body)
	if resp.StatusCode != 400 {
		t.Errorf("status=%d want 400 body=%s", resp.StatusCode, readBody(resp))
	}
}
