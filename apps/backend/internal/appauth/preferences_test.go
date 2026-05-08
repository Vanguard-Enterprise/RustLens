package appauth_test

import (
	"context"
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
