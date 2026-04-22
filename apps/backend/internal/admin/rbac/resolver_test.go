package rbac

import (
	"context"
	"testing"
)

type fakeLoader struct {
	perms map[string][]string
	calls int
}

func (f *fakeLoader) PermissionsForUser(ctx context.Context, userID string) ([]string, error) {
	f.calls++
	return f.perms[userID], nil
}

func TestResolver_HasPermission(t *testing.T) {
	user := "usr_001"
	f := &fakeLoader{perms: map[string][]string{user: {"users.suspend"}}}
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
	user := "usr_001"
	f := &fakeLoader{perms: map[string][]string{user: {"users.suspend"}}}
	r := NewResolver(f)
	ctx := r.WithRequest(context.Background())

	_, _ = r.HasPermission(ctx, user, "users.suspend")
	_, _ = r.HasPermission(ctx, user, "proxies.edit")
	_, _ = r.HasPermission(ctx, user, "audit.read")
	if f.calls != 1 {
		t.Errorf("expected 1 loader call per request, got %d", f.calls)
	}
}
