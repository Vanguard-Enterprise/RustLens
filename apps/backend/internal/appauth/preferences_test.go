package appauth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"rustlens/backend/internal/appauth"
)

const prefsTestUserID = "usr_prefs_user_1"

func TestRepo_LoadNotificationPreferences_LazyDefault(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-1', 'prefs_tester', 'Prefs')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	repo := appauth.NewRepo(pool)
	prefs, err := repo.LoadNotificationPreferences(ctx, prefsTestUserID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !prefs.NotifyInApp || !prefs.NotifyDiscordAnnouncements {
		t.Fatalf("expected both defaults true, got %+v", prefs)
	}

	// Row should now exist.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_notification_preferences WHERE user_id = $1`,
		prefsTestUserID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 prefs row after lazy default, got %d", n)
	}
}

func TestRepo_UpsertNotificationPreferences_PartialPatch(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-2', 'prefs_tester2', 'Prefs2')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	repo := appauth.NewRepo(pool)
	// Patch only the in-app flag; the announcements flag must stay at default true.
	falseVal := false
	got, err := repo.UpsertNotificationPreferences(ctx, prefsTestUserID, appauth.NotificationPreferencesPatch{
		NotifyInApp: &falseVal,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got.NotifyInApp {
		t.Errorf("NotifyInApp: got true, want false")
	}
	if !got.NotifyDiscordAnnouncements {
		t.Errorf("NotifyDiscordAnnouncements: got false, want true (default preserved)")
	}

	// Now patch the announcements flag too; in-app must remain false.
	trueVal := true
	got, err = repo.UpsertNotificationPreferences(ctx, prefsTestUserID, appauth.NotificationPreferencesPatch{
		NotifyDiscordAnnouncements: &trueVal,
	})
	if err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	if got.NotifyInApp {
		t.Errorf("after second patch NotifyInApp: got true, want false")
	}
	if !got.NotifyDiscordAnnouncements {
		t.Errorf("after second patch NotifyDiscordAnnouncements: got false, want true")
	}
}

func TestRepo_UpsertNotificationPreferences_EmptyPatchReadsCurrent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-3', 'prefs_tester3', 'Prefs3')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	repo := appauth.NewRepo(pool)
	got, err := repo.UpsertNotificationPreferences(ctx, prefsTestUserID, appauth.NotificationPreferencesPatch{})
	if err != nil {
		t.Fatalf("upsert empty: %v", err)
	}
	if !got.NotifyInApp || !got.NotifyDiscordAnnouncements {
		t.Errorf("empty patch must leave defaults intact, got %+v", got)
	}
}

// withSession wraps a handler so the appauth.SessionUser is attached the same
// way the RequireSession middleware would. Mirrors the pattern used elsewhere
// in this package for tests that bypass the middleware.
func withSession(userID string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := appauth.InjectSessionForTest(r.Context(), &appauth.SessionUser{UserID: userID})
		h.ServeHTTP(w, r.WithContext(ctx))
	}
}

func TestHandler_Preferences_Get_LazyDefault(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-h1', 'prefs_h1', 'P')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := appauth.NewHandler(appauth.NewRepo(pool))

	req := httptest.NewRequest(http.MethodGet, "/api/app/me/preferences", nil)
	rec := httptest.NewRecorder()
	withSession(prefsTestUserID, h.Preferences)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got appauth.NotificationPreferences
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.NotifyInApp || !got.NotifyDiscordAnnouncements {
		t.Errorf("expected both defaults true, got %+v", got)
	}
}

func TestHandler_Preferences_Patch_PartialUpdate(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-h2', 'prefs_h2', 'P2')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := appauth.NewHandler(appauth.NewRepo(pool))

	body := bytes.NewBufferString(`{"notifyInApp": false}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/app/me/preferences", body)
	rec := httptest.NewRecorder()
	withSession(prefsTestUserID, h.UpdatePreferences)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got appauth.NotificationPreferences
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.NotifyInApp {
		t.Errorf("NotifyInApp: got true, want false")
	}
	if !got.NotifyDiscordAnnouncements {
		t.Errorf("NotifyDiscordAnnouncements: got false, want true (untouched)")
	}
}

func TestHandler_Preferences_Patch_RejectsUnknownField(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-h3', 'prefs_h3', 'P3')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := appauth.NewHandler(appauth.NewRepo(pool))

	body := bytes.NewBufferString(`{"emailMarketing": true}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/app/me/preferences", body)
	rec := httptest.NewRecorder()
	withSession(prefsTestUserID, h.UpdatePreferences)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_Preferences_Patch_RejectsNonBoolean(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-h4', 'prefs_h4', 'P4')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := appauth.NewHandler(appauth.NewRepo(pool))

	body := bytes.NewBufferString(`{"notifyInApp": "yes"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/app/me/preferences", body)
	rec := httptest.NewRecorder()
	withSession(prefsTestUserID, h.UpdatePreferences)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_Preferences_Patch_EmptyBodyIsNoOp(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-h5', 'prefs_h5', 'P5')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := appauth.NewHandler(appauth.NewRepo(pool))

	req := httptest.NewRequest(http.MethodPatch, "/api/app/me/preferences", nil)
	rec := httptest.NewRecorder()
	withSession(prefsTestUserID, h.UpdatePreferences)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got appauth.NotificationPreferences
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.NotifyInApp || !got.NotifyDiscordAnnouncements {
		t.Errorf("empty-body PATCH must leave defaults intact, got %+v", got)
	}
}

func TestHandler_Preferences_NoSession_401(t *testing.T) {
	pool := testPool(t)
	h := appauth.NewHandler(appauth.NewRepo(pool))

	req := httptest.NewRequest(http.MethodGet, "/api/app/me/preferences", nil)
	rec := httptest.NewRecorder()
	h.Preferences(rec, req) // no withSession wrapper

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}
