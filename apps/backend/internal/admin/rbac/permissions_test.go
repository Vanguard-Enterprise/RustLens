package rbac

import "testing"

func TestPermissions_ContainsExpectedKeys(t *testing.T) {
	expected := []string{
		"users.suspend",
		"users.force_unlink_steam",
		"proxies.edit",
		"proxies.rotate_credentials",
		"links.review",
		"links.approve",
		"guilds.untrack_server",
		"admins.grant",
		"audit.read",
	}
	got := map[string]bool{}
	for _, p := range AllPermissions() {
		got[p.Key] = true
	}
	for _, key := range expected {
		if !got[key] {
			t.Errorf("expected permission %q in registry", key)
		}
	}
}

func TestPermissions_NoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range AllPermissions() {
		if seen[p.Key] {
			t.Errorf("duplicate permission key: %q", p.Key)
		}
		seen[p.Key] = true
	}
}

func TestAPIKeyPermissionsRegistered(t *testing.T) {
	for _, key := range []string{
		PermAPIKeysManage,
		PermBotRoutesRead,
		PermBotRoutesWrite,
	} {
		if !IsValidPermission(key) {
			t.Errorf("expected permission %q to be registered", key)
		}
	}
}

func TestInfraAdminHasAPIKeysManage(t *testing.T) {
	for _, role := range SystemRoles() {
		if role.Key != RoleInfraAdmin {
			continue
		}
		found := false
		for _, p := range role.Permissions {
			if p == PermAPIKeysManage {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected infra_admin to include %q", PermAPIKeysManage)
		}
		return
	}
	t.Fatalf("infra_admin role not found")
}
