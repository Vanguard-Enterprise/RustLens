package actions_test

import (
	"context"
	"net/http"
	"testing"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

func insertCustomRole(t *testing.T, s *store.PostgresStore, key, name string) string {
	t.Helper()
	var id string
	if err := s.Pool().QueryRow(context.Background(),
		`INSERT INTO admin_roles (key, name, description, is_system) VALUES ($1,$2,'',FALSE) RETURNING id::text`,
		key, name,
	).Scan(&id); err != nil {
		t.Fatalf("insert custom role: %v", err)
	}
	t.Cleanup(func() { _, _ = s.Pool().Exec(context.Background(), `DELETE FROM admin_roles WHERE id::text=$1`, id) })
	return id
}

func TestRolesUpdate_HappyPath(t *testing.T) {
	srv, s, _, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	id := insertCustomRole(t, s, "rupd_happy", "Original")

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"id": id, "name": "Updated", "description": "new desc"},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.update", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var name, desc string
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT name, COALESCE(description,'') FROM admin_roles WHERE id::text=$1`, id,
	).Scan(&name, &desc); err != nil {
		t.Fatal(err)
	}
	if name != "Updated" || desc != "new desc" {
		t.Errorf("name=%q desc=%q", name, desc)
	}
}

func TestRolesUpdate_SystemRoleRejected(t *testing.T) {
	srv, s, rb, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	sysID, err := rb.RoleIDByKey(context.Background(), rbac.RoleModerator)
	if err != nil {
		t.Fatal(err)
	}

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"id": sysID.String(), "name": "X", "description": ""},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.update", body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status=%d; want 403 body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestRolesUpdate_NonExistentID(t *testing.T) {
	srv, s, _, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"id": "00000000-0000-0000-0000-000000000000", "name": "X", "description": ""},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.update", body)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status=%d; want 404 body=%s", resp.StatusCode, readBody(resp))
	}
}
