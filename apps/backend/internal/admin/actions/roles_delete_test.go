package actions_test

import (
	"context"
	"net/http"
	"testing"

	"rustlens/backend/internal/admin/rbac"
)

func TestRolesDelete_HappyPath_CascadesAssignments(t *testing.T) {
	srv, s, rb, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_rdel_target", "Target", false)
	id := insertCustomRole(t, s, "rdel_happy", "Custom")

	// Assign the role to a user and a permission so cascade can be observed.
	if _, err := s.Pool().Exec(context.Background(),
		`INSERT INTO admin_user_roles (user_id, role_id) VALUES ($1, $2::uuid)`,
		"usr_rdel_target", id,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool().Exec(context.Background(),
		`INSERT INTO admin_role_permissions (role_id, permission_key) VALUES ($1::uuid, $2)`,
		id, rbac.PermUsersRead,
	); err != nil {
		t.Fatal(err)
	}

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"id": id},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.delete", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}

	var n int
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_roles WHERE id::text=$1`, id,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("role still exists; count=%d", n)
	}

	if err := s.Pool().QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_user_roles WHERE role_id::text=$1`, id,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("assignments not cascaded; count=%d", n)
	}

	if err := s.Pool().QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_role_permissions WHERE role_id::text=$1`, id,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("permissions not cascaded; count=%d", n)
	}

	_ = rb // unused
}

func TestRolesDelete_SystemRoleRejected(t *testing.T) {
	srv, s, rb, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	sysID, err := rb.RoleIDByKey(context.Background(), rbac.RoleModerator)
	if err != nil {
		t.Fatal(err)
	}

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"id": sysID.String()},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.delete", body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status=%d; want 403 body=%s", resp.StatusCode, readBody(resp))
	}
}
