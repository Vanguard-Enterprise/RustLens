package rbac_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

var suffixCounter int

func randSuffix() string {
	suffixCounter++
	return string(rune('a'+suffixCounter%26)) + string(rune('a'+(suffixCounter/26)%26))
}

func seedAppUser(t *testing.T, pool *pgxpool.Pool, id string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO app_users (id, discord_id, discord_username)
		 VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING`,
		id, "d_"+id, "u_"+id)
	if err != nil {
		t.Fatalf("seed app_user %s: %v", id, err)
	}
}

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
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
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

	userID := "usr_rbac_mod_" + randSuffix()
	seedAppUser(t, pool, userID)
	modRoleID, err := s.RoleIDByKey(ctx, rbac.RoleModerator)
	if err != nil {
		t.Fatalf("role id: %v", err)
	}
	if err := s.AssignRole(ctx, userID, modRoleID, ""); err != nil {
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

	userID := "usr_rbac_sa_" + randSuffix()
	seedAppUser(t, pool, userID)
	saID, err := s.RoleIDByKey(ctx, rbac.RoleSuperadmin)
	if err != nil {
		t.Fatalf("role id: %v", err)
	}
	if err := s.AssignRole(ctx, userID, saID, ""); err != nil {
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

func TestListRolesWithMemberCount(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	s := rbac.NewStore(pool)
	if err := s.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Use a unique custom role so member counts are deterministic regardless of
	// other tests that may assign users to system roles.
	customKey := "test_member_count_" + randSuffix()
	var roleIDStr string
	if err := pool.QueryRow(ctx,
		`INSERT INTO admin_roles (key, name, is_system) VALUES ($1, $2, false) RETURNING id::text`,
		customKey, customKey,
	).Scan(&roleIDStr); err != nil {
		t.Fatalf("insert custom role: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM admin_roles WHERE key = $1`, customKey)
	})

	// Seed two users and assign both to the custom role.
	userA := "usr_mc_a_" + randSuffix()
	userB := "usr_mc_b_" + randSuffix()
	seedAppUser(t, pool, userA)
	seedAppUser(t, pool, userB)

	for _, uid := range []string{userA, userB} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO admin_user_roles (user_id, role_id) VALUES ($1, $2::uuid) ON CONFLICT DO NOTHING`,
			uid, roleIDStr,
		); err != nil {
			t.Fatalf("assign role to %s: %v", uid, err)
		}
	}

	roles, err := s.ListRolesWithMemberCount(ctx)
	if err != nil {
		t.Fatalf("ListRolesWithMemberCount: %v", err)
	}

	found := false
	for _, r := range roles {
		if r.Key == customKey {
			found = true
			if r.MemberCount < 2 {
				t.Errorf("custom role memberCount: want >= 2, got %d", r.MemberCount)
			}
			break
		}
	}
	if !found {
		t.Errorf("custom role %q not found in ListRolesWithMemberCount result", customKey)
	}
}

func TestSeedPermissionsAndRoles_InstallsUsersPerms(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	s := rbac.NewStore(pool)
	if err := s.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if err := s.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	want := []string{
		"users.read", "users.edit", "users.roles.assign", "users.suspend",
		"users.security.reset", "users.email.manage", "users.create",
		"users.connections.manage", "users.delete", "users.export", "users.audit.read",
	}
	for _, p := range want {
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM admin_permissions WHERE key = $1`, p,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("permission %s: want 1 row, got %d", p, count)
		}
	}
	for _, r := range []string{"user_support", "user_admin", "user_auditor"} {
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM admin_roles WHERE key = $1`, r,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("role %s: want 1 row, got %d", r, count)
		}
	}
}
