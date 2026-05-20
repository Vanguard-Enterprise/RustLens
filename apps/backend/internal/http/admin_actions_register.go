package httpapi

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/events"
)

// registerAdminActions wires the concrete AdminAction implementations into
// the action registry at handler boot. Keep this list alphabetised.
//
// appBaseURL is unused now that the email/password admin actions are gone, but
// the parameter is kept on the call site so future token-generating actions
// can be wired here without churn.
func registerAdminActions(reg *actions.Registry, pool *pgxpool.Pool, resolver *rbac.Resolver, pub events.Publisher, _ string) error {
	if err := reg.Register(actions.NewAPIKeysCreateAction(pool, resolver)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewAPIKeysRotateAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewAPIKeysRevokeAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewRolesCreateAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewRolesDeleteAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewRolesPermissionsSetAction(pool, pub)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewRolesUpdateAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersEditAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersForceLogoutAction(pool, pub)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersRolesGrantAction(pool, resolver, pub)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersRolesRevokeAction(pool, resolver, pub)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersSuspendAction(pool, pub)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersUnsuspendAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersDeleteAction(pool, pub)); err != nil {
		return err
	}
	return nil
}
