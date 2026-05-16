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

type stubLoader struct{ perms []string }

func (s stubLoader) PermissionsForUser(_ context.Context, _ string) ([]string, error) {
	return s.perms, nil
}

func TestResolverIntersectsWithAPIKeyPermissions(t *testing.T) {
	r := NewResolver(stubLoader{perms: []string{"users.read", "users.edit"}})
	ctx := WithAPIKeyPermissions(context.Background(), []string{"users.read"})

	got, err := r.PermissionsForUser(ctx, "u1")
	if err != nil {
		t.Fatalf("perms: %v", err)
	}
	if len(got) != 1 || got[0] != "users.read" {
		t.Errorf("intersect failed: %v", got)
	}

	if ok, _ := r.HasPermission(ctx, "u1", "users.edit"); ok {
		t.Error("users.edit should be filtered out by intersection")
	}
	if ok, _ := r.HasPermission(ctx, "u1", "users.read"); !ok {
		t.Error("users.read should still pass")
	}
}
