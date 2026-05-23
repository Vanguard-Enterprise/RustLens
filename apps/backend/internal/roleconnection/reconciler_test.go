package roleconnection

import (
	"context"
	"testing"

	"rustlens/backend/internal/oauth/discord"
	"rustlens/backend/internal/store"
)

type recorderSyncer struct {
	pushed []string
	err    error
}

func (r *recorderSyncer) Push(_ context.Context, userID string) error {
	r.pushed = append(r.pushed, userID)
	return r.err
}

func TestReconcilerStep_PushesEachActiveRow(t *testing.T) {
	s := &fakeStore{
		listActive: []store.DiscordRoleConnection{
			{UserID: "a"}, {UserID: "b"}, {UserID: "c"},
		},
	}
	rec := &recorderSyncer{}
	r := NewReconciler(ReconcilerConfig{Store: s, Pusher: rec, Sleep: 0})
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(rec.pushed) != 3 {
		t.Errorf("pushed = %v, want 3", rec.pushed)
	}
}

func TestReconcilerStep_ContinuesAfterPerRowError(t *testing.T) {
	s := &fakeStore{
		listActive: []store.DiscordRoleConnection{
			{UserID: "a"}, {UserID: "b"},
		},
	}
	rec := &recorderSyncer{err: discord.ErrRefreshRevoked}
	r := NewReconciler(ReconcilerConfig{Store: s, Pusher: rec, Sleep: 0})
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(rec.pushed) != 2 {
		t.Errorf("pushed = %v", rec.pushed)
	}
}
