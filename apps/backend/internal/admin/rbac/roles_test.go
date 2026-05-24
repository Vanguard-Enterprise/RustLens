package rbac

import "testing"

func TestSystemRoles_ContainsSuperadminAndIsSystem(t *testing.T) {
	roles := SystemRoles()
	var sa *RoleSeed
	for i := range roles {
		if roles[i].Key == RoleSuperadmin {
			sa = &roles[i]
		}
	}
	if sa == nil {
		t.Fatalf("expected %q in SystemRoles", RoleSuperadmin)
	}
	if !sa.IsSystem {
		t.Errorf("superadmin must be is_system")
	}
	if len(sa.Permissions) != 0 {
		t.Errorf("superadmin should not enumerate permissions (implicit all)")
	}
}

func TestSystemRoles_ModeratorHasExpectedPermissions(t *testing.T) {
	roles := SystemRoles()
	var mod *RoleSeed
	for i := range roles {
		if roles[i].Key == RoleModerator {
			mod = &roles[i]
		}
	}
	if mod == nil {
		t.Fatalf("expected moderator role")
	}
	want := map[string]bool{
		"users.suspend":            true,
		"users.force_unlink_steam": true,
		"links.review":             true,
		"links.approve":            true,
		"audit.read":               true,
	}
	got := map[string]bool{}
	for _, p := range mod.Permissions {
		got[p] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("moderator missing permission %q", k)
		}
	}
}

func TestSystemRoles_DoesNotContainPROEntitlement(t *testing.T) {
	roles := SystemRoles()
	for _, role := range roles {
		if role.Key == "pro" {
			t.Fatalf("PRO must be a user entitlement, not an admin role")
		}
	}
}
