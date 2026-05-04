package actions_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

func TestAssertNotSystemRole(t *testing.T) {
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

	sysID, err := rb.RoleIDByKey(ctx, rbac.RoleModerator)
	if err != nil {
		t.Fatalf("system role id: %v", err)
	}
	if err := actions.AssertNotSystemRole(ctx, s.Pool(), sysID.String()); !errors.Is(err, actions.ErrPermissionDenied) {
		t.Errorf("system role should be denied; got %v", err)
	}

	// Insert a custom role
	var customID string
	if err := s.Pool().QueryRow(ctx,
		`INSERT INTO admin_roles (key, name, description, is_system) VALUES ($1,$2,$3,FALSE) RETURNING id::text`,
		"custom_assert_not_system", "Custom", "",
	).Scan(&customID); err != nil {
		t.Fatalf("insert custom: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.Pool().Exec(context.Background(), `DELETE FROM admin_roles WHERE id::text=$1`, customID)
	})
	if err := actions.AssertNotSystemRole(ctx, s.Pool(), customID); err != nil {
		t.Errorf("custom role should be allowed; got %v", err)
	}

	// Missing role
	if err := actions.AssertNotSystemRole(ctx, s.Pool(), "00000000-0000-0000-0000-000000000000"); !errors.Is(err, actions.ErrNotFound) {
		t.Errorf("missing role should be ErrNotFound; got %v", err)
	}
}
