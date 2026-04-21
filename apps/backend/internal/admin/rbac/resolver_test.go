package rbac

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

type fakeLoader struct {
	perms map[uuid.UUID][]string
	calls int
}

func (f *fakeLoader) PermissionsForUser(ctx context.Context, userID uuid.UUID) ([]string, error) {
	f.calls++
	return f.perms[userID], nil
}

func TestResolver_HasPermission(t *testing.T) {
	user := uuid.New()
	f := &fakeLoader{perms: map[uuid.UUID][]string{user: {"users.suspend"}}}
	r := NewResolver(f)
	ctx := r.WithRequest(context.Background())

	ok, err := r.HasPermission(ctx, user, "users.suspend")
	if err != nil || !ok {
		t.Fatalf("expected true, got ok=%v err=%v", ok, err)
	}
	ok, _ = r.HasPermission(ctx, user, "proxies.edit")
	if ok {
		t.Errorf("expected false for proxies.edit")
	}
}

func TestResolver_CachesPerRequest(t *testing.T) {
	user := uuid.New()
	f := &fakeLoader{perms: map[uuid.UUID][]string{user: {"users.suspend"}}}
	r := NewResolver(f)
	ctx := r.WithRequest(context.Background())

	_, _ = r.HasPermission(ctx, user, "users.suspend")
	_, _ = r.HasPermission(ctx, user, "proxies.edit")
	_, _ = r.HasPermission(ctx, user, "audit.read")
	if f.calls != 1 {
		t.Errorf("expected 1 loader call per request, got %d", f.calls)
	}
}
