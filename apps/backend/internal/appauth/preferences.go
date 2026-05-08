package appauth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// NotificationPreferences is the per-user notification toggle state.
// Both flags default to TRUE (opt-out model). They are storage-only at
// this point — no delivery system reads them yet.
type NotificationPreferences struct {
	NotifyInApp                bool `json:"notifyInApp"`
	NotifyDiscordAnnouncements bool `json:"notifyDiscordAnnouncements"`
}

// NotificationPreferencesPatch is a partial update. Nil pointers leave
// the corresponding column unchanged.
type NotificationPreferencesPatch struct {
	NotifyInApp                *bool
	NotifyDiscordAnnouncements *bool
}

// LoadNotificationPreferences returns the user's notification preferences,
// creating a default row if none exists. The lazy-default behaviour means
// callers never have to handle a "missing row" case.
func (r *Repo) LoadNotificationPreferences(ctx context.Context, userID string) (NotificationPreferences, error) {
	var prefs NotificationPreferences
	err := r.pool.QueryRow(ctx, `
		INSERT INTO user_notification_preferences (user_id)
		VALUES ($1)
		ON CONFLICT (user_id) DO UPDATE SET user_id = EXCLUDED.user_id
		RETURNING notify_in_app, notify_discord_announcements
	`, userID).Scan(&prefs.NotifyInApp, &prefs.NotifyDiscordAnnouncements)
	if errors.Is(err, pgx.ErrNoRows) {
		return NotificationPreferences{}, ErrUserNotFound
	}
	if err != nil {
		return NotificationPreferences{}, err
	}
	return prefs, nil
}

// UpsertNotificationPreferences applies a partial update and returns the
// resulting row. Nil pointers in the patch leave that column unchanged.
// An empty patch is a no-op that returns current values (creating the
// default row if absent, just like LoadNotificationPreferences).
func (r *Repo) UpsertNotificationPreferences(ctx context.Context, userID string, patch NotificationPreferencesPatch) (NotificationPreferences, error) {
	var prefs NotificationPreferences
	// COALESCE($N::boolean, column) keeps the existing column value when the
	// patch field is nil; the boolean cast is required because pgx encodes a
	// nil *bool as a typeless NULL otherwise.
	err := r.pool.QueryRow(ctx, `
		INSERT INTO user_notification_preferences (user_id, notify_in_app, notify_discord_announcements, updated_at)
		VALUES ($1, COALESCE($2::boolean, TRUE), COALESCE($3::boolean, TRUE), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			notify_in_app                = COALESCE($2::boolean, user_notification_preferences.notify_in_app),
			notify_discord_announcements = COALESCE($3::boolean, user_notification_preferences.notify_discord_announcements),
			updated_at                   = NOW()
		RETURNING notify_in_app, notify_discord_announcements
	`, userID, patch.NotifyInApp, patch.NotifyDiscordAnnouncements).
		Scan(&prefs.NotifyInApp, &prefs.NotifyDiscordAnnouncements)
	if err != nil {
		return NotificationPreferences{}, err
	}
	return prefs, nil
}
