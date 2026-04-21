package rbac_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

func newPool(t *testing.T) *pgxpool.Pool {
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
	return s.Pool()
}

func TestStore_SeedAndListRoles(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	s := rbac.NewStore(pool)

	if err := s.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	roles, err := s.ListRoles(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	keys := map[string]bool{}
	for _, r := range roles {
		keys[r.Key] = true
	}
	for _, want := range []string{rbac.RoleSuperadmin, rbac.RoleModerator, rbac.RoleInfraAdmin, rbac.RoleGuildOps} {
		if !keys[want] {
			t.Errorf("role %q missing after seed", want)
		}
	}
}

func TestStore_PermissionsForUser(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	s := rbac.NewStore(pool)
	if err := s.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	userID := uuid.New()
	modRoleID, err := s.RoleIDByKey(ctx, rbac.RoleModerator)
	if err != nil {
		t.Fatalf("role id: %v", err)
	}
	if err := s.AssignRole(ctx, userID, modRoleID, nil); err != nil {
		t.Fatalf("assign: %v", err)
	}

	perms, err := s.PermissionsForUser(ctx, userID)
	if err != nil {
		t.Fatalf("perms: %v", err)
	}
	found := map[string]bool{}
	for _, p := range perms {
		found[p] = true
	}
	if !found["users.suspend"] {
		t.Errorf("expected users.suspend for moderator, got %v", perms)
	}
}

func TestStore_SuperadminImpliesAllPermissions(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	s := rbac.NewStore(pool)
	if err := s.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	userID := uuid.New()
	saID, err := s.RoleIDByKey(ctx, rbac.RoleSuperadmin)
	if err != nil {
		t.Fatalf("role id: %v", err)
	}
	if err := s.AssignRole(ctx, userID, saID, nil); err != nil {
		t.Fatalf("assign: %v", err)
	}

	perms, err := s.PermissionsForUser(ctx, userID)
	if err != nil {
		t.Fatalf("perms: %v", err)
	}
	if len(perms) != len(rbac.AllPermissions()) {
		t.Errorf("superadmin should get all %d perms, got %d", len(rbac.AllPermissions()), len(perms))
	}
}
