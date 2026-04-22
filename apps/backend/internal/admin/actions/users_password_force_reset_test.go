package actions_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
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

func newForceResetHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, func(map[string]bool)) {
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
	if err := reg.Register(actions.NewUsersPasswordForceResetAction(s.Pool(), "http://localhost:3000")); err != nil {
		t.Fatalf("register: %v", err)
	}
	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, func(a map[string]bool) { perms.allow = a }
}

type forceResetResp struct {
	OK     bool `json:"ok"`
	Result struct {
		UserID   string `json:"userId"`
		TokenURL string `json:"tokenUrl"`
	} `json:"result"`
}

func TestUsersPasswordForceReset_HappyPath(t *testing.T) {
	srv, s, setPerms := newForceResetHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSecurityReset: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_fr_t"
	seedAppUser(t, s, tid, "Target", false)
	// clear any prior tokens for this user
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM password_reset_tokens WHERE user_id=$1`, tid)
	_, _ = s.Pool().Exec(context.Background(),
		`INSERT INTO user_emails (email, user_id, is_primary, is_verified) VALUES ($1, $2, true, true) ON CONFLICT (email) DO NOTHING`,
		tid+"@example.com", tid)

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": tid},
		"reason":            "forgot",
		"typedConfirmation": tid,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.password.forceReset", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var parsed forceResetResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(parsed.Result.TokenURL, "http://localhost:3000/reset-password?token=") {
		t.Fatalf("tokenUrl shape wrong: %q", parsed.Result.TokenURL)
	}

	// Extract the raw token from the URL, hash it, and confirm the DB stored
	// the hash (not the raw token).
	u, err := url.Parse(parsed.Result.TokenURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	raw := u.Query().Get("token")
	if raw == "" {
		t.Fatalf("missing token query param")
	}
	h := sha256.Sum256([]byte(raw))
	want := hex.EncodeToString(h[:])

	var stored string
	var consumedAt *string
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT token_hash, consumed_at::text FROM password_reset_tokens WHERE user_id=$1 ORDER BY created_at DESC LIMIT 1`,
		tid).Scan(&stored, &consumedAt); err != nil {
		t.Fatalf("query token: %v", err)
	}
	if stored != want {
		t.Errorf("stored hash = %q; want %q (raw was %q)", stored, want, raw)
	}
	if stored == raw {
		t.Errorf("stored raw token instead of hash")
	}
	if consumedAt != nil {
		t.Errorf("new token already consumed?")
	}
}

func TestUsersPasswordForceReset_InvalidatesPrevious(t *testing.T) {
	srv, s, setPerms := newForceResetHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSecurityReset: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_fr_twice"
	seedAppUser(t, s, tid, "Target", false)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM password_reset_tokens WHERE user_id=$1`, tid)
	_, _ = s.Pool().Exec(context.Background(),
		`INSERT INTO user_emails (email, user_id, is_primary, is_verified) VALUES ($1, $2, true, true) ON CONFLICT (email) DO NOTHING`,
		tid+"@example.com", tid)

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": tid},
		"reason":            "first",
		"typedConfirmation": tid,
	})
	if resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.password.forceReset", body); resp.StatusCode != 200 {
		t.Fatalf("first: status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	body2 := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": tid},
		"reason":            "second",
		"typedConfirmation": tid,
	})
	if resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.password.forceReset", body2); resp.StatusCode != 200 {
		t.Fatalf("second: status=%d body=%s", resp.StatusCode, readBody(resp))
	}

	// Exactly one row with consumed_at IS NULL (the newest one).
	var open int
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT COUNT(*) FROM password_reset_tokens WHERE user_id=$1 AND consumed_at IS NULL`, tid).Scan(&open); err != nil {
		t.Fatalf("count: %v", err)
	}
	if open != 1 {
		t.Errorf("open tokens after second reset = %d; want 1", open)
	}
}

func TestUsersPasswordForceReset_PermissionDenied(t *testing.T) {
	srv, s, setPerms := newForceResetHarness(t)
	setPerms(map[string]bool{}) // no perms
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_fr_nope"
	seedAppUser(t, s, tid, "Target", false)
	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": tid},
		"reason":            "t",
		"typedConfirmation": tid,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.password.forceReset", body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status=%d; want 403 body=%s", resp.StatusCode, readBody(resp))
	}
}
