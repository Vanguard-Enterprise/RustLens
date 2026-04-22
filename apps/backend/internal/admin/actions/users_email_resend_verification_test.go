package actions_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/audit"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

func newEmailResendHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, func(map[string]bool)) {
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
	if err := reg.Register(actions.NewUsersEmailResendVerificationAction(s.Pool(), "http://localhost:3000")); err != nil {
		t.Fatalf("register: %v", err)
	}
	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, func(a map[string]bool) { perms.allow = a }
}

type emailResendResp struct {
	OK     bool `json:"ok"`
	Result struct {
		UserID   string `json:"userId"`
		Email    string `json:"email"`
		TokenURL string `json:"tokenUrl"`
	} `json:"result"`
}

func seedEmailRow(t *testing.T, s *store.PostgresStore, userID, email string, primary, verified bool) {
	t.Helper()
	if _, err := s.Pool().Exec(context.Background(),
		`INSERT INTO user_emails (email, user_id, is_primary, is_verified)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (email) DO UPDATE SET user_id=EXCLUDED.user_id, is_primary=EXCLUDED.is_primary, is_verified=EXCLUDED.is_verified`,
		email, userID, primary, verified); err != nil {
		t.Fatalf("seed email: %v", err)
	}
}

func TestUsersEmailResendVerification_HappyPath(t *testing.T) {
	srv, s, setPerms := newEmailResendHarness(t)
	setPerms(map[string]bool{rbac.PermUsersEmailManage: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_erv_t"
	seedAppUser(t, s, tid, "Target", false)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM email_verification_tokens WHERE user_id=$1`, tid)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM user_emails WHERE user_id=$1`, tid)
	email := tid + "@example.com"
	seedEmailRow(t, s, tid, email, true, false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": tid, "email": email},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.email.resendVerification", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var parsed emailResendResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(parsed.Result.TokenURL, "http://localhost:3000/verify-email?token=") {
		t.Fatalf("tokenUrl shape wrong: %q", parsed.Result.TokenURL)
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
		`SELECT token_hash FROM email_verification_tokens WHERE user_id=$1 AND email=$2 AND consumed_at IS NULL`,
		tid, email).Scan(&stored); err != nil {
		t.Fatalf("query token: %v", err)
	}
	if stored != want {
		t.Errorf("stored %q want %q", stored, want)
	}
}

func TestUsersEmailResendVerification_NotFound(t *testing.T) {
	srv, s, setPerms := newEmailResendHarness(t)
	setPerms(map[string]bool{rbac.PermUsersEmailManage: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_erv_nf"
	seedAppUser(t, s, tid, "Target", false)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM user_emails WHERE user_id=$1`, tid)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": tid, "email": "nope@example.com"},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.email.resendVerification", body)
	if resp.StatusCode != 404 {
		t.Errorf("status=%d want 404 body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestUsersEmailResendVerification_IdempotentRotation(t *testing.T) {
	srv, s, setPerms := newEmailResendHarness(t)
	setPerms(map[string]bool{rbac.PermUsersEmailManage: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_erv_rot"
	seedAppUser(t, s, tid, "Target", false)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM email_verification_tokens WHERE user_id=$1`, tid)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM user_emails WHERE user_id=$1`, tid)
	email := tid + "@example.com"
	seedEmailRow(t, s, tid, email, true, false)

	b := mustJSON(t, map[string]any{"params": map[string]any{"userId": tid, "email": email}})
	if r := mustPOST(t, srv.URL+"/api/app/admin/actions/users.email.resendVerification", b); r.StatusCode != 200 {
		t.Fatalf("first: %d body=%s", r.StatusCode, readBody(r))
	}
	if r := mustPOST(t, srv.URL+"/api/app/admin/actions/users.email.resendVerification", b); r.StatusCode != 200 {
		t.Fatalf("second: %d body=%s", r.StatusCode, readBody(r))
	}
	var open int
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT COUNT(*) FROM email_verification_tokens WHERE user_id=$1 AND email=$2 AND consumed_at IS NULL`, tid, email).Scan(&open); err != nil {
		t.Fatalf("count: %v", err)
	}
	if open != 1 {
		t.Errorf("open tokens = %d; want 1", open)
	}
}
