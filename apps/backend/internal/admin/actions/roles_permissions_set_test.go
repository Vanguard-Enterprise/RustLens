package actions_test

import (
	"context"
	"net/http"
	"sort"
	"testing"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

func currentPerms(t *testing.T, s *store.PostgresStore, roleID string) []string {
	t.Helper()
	rows, err := s.Pool().Query(context.Background(),
		`SELECT permission_key FROM admin_role_permissions WHERE role_id::text=$1 ORDER BY permission_key`, roleID,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		out = append(out, k)
	}
	return out
}

func TestRolesPermissionsSet_HappyPath(t *testing.T) {
	srv, s, _, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	id := insertCustomRole(t, s, "rpset_happy", "Custom")
	// Pre-seed one permission so we can observe diff (delete + insert).
	if _, err := s.Pool().Exec(context.Background(),
		`INSERT INTO admin_role_permissions (role_id, permission_key) VALUES ($1::uuid, $2)`,
		id, rbac.PermUsersRead,
	); err != nil {
		t.Fatal(err)
	}

	body := mustJSON(t, map[string]any{
		"params": map[string]any{
			"id":          id,
			"permissions": []string{rbac.PermUsersExport, rbac.PermAuditRead},
		},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.permissions.set", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}

	got := currentPerms(t, s, id)
	want := []string{rbac.PermAuditRead, rbac.PermUsersExport}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("perms=%v want=%v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("perms=%v want=%v", got, want)
		}
	}
}

func TestRolesPermissionsSet_IdempotentReset(t *testing.T) {
	srv, s, _, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	id := insertCustomRole(t, s, "rpset_idem", "Custom")

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"id": id, "permissions": []string{rbac.PermUsersRead}},
		"reason": "t",
	})
	if r := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.permissions.set", body); r.StatusCode != 200 {
		t.Fatalf("first call status=%d body=%s", r.StatusCode, readBody(r))
	}
	if r := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.permissions.set", body); r.StatusCode != 200 {
		t.Fatalf("second call status=%d body=%s", r.StatusCode, readBody(r))
	}
	got := currentPerms(t, s, id)
	if len(got) != 1 || got[0] != rbac.PermUsersRead {
		t.Errorf("after idempotent reset, perms=%v", got)
	}
}

func TestRolesPermissionsSet_BadPermissionRejected(t *testing.T) {
	srv, s, _, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	id := insertCustomRole(t, s, "rpset_badperm", "Custom")

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"id": id, "permissions": []string{"not.a.permission"}},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.permissions.set", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status=%d; want 400", resp.StatusCode)
	}
}

func TestRolesPermissionsSet_SystemRoleRejected(t *testing.T) {
	srv, s, rb, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	sysID, err := rb.RoleIDByKey(context.Background(), rbac.RoleModerator)
	if err != nil {
		t.Fatal(err)
	}

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"id": sysID.String(), "permissions": []string{rbac.PermUsersRead}},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.permissions.set", body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status=%d; want 403", resp.StatusCode)
	}
}
