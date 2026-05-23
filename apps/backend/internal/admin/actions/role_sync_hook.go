package actions

// RoleSyncHook is invoked after a successful users.roles.grant or revoke for
// the "pro" role. Implementations should not block - they are called with the
// user_id and must fire-and-forget any side effects (e.g., Discord push).
type RoleSyncHook interface {
	OnProRoleChanged(userID string)
}

// NopRoleSyncHook is the default when no hook is wired.
type NopRoleSyncHook struct{}

func (NopRoleSyncHook) OnProRoleChanged(string) {}
