package httpapi

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/rbac"
)

// registerAdminActions wires the concrete AdminAction implementations into
// the action registry at handler boot. Keep this list alphabetised.
func registerAdminActions(reg *actions.Registry, pool *pgxpool.Pool, resolver *rbac.Resolver) error {
	if err := reg.Register(actions.NewUsersEditAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersRolesGrantAction(pool, resolver)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersRolesRevokeAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersSuspendAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersUnsuspendAction(pool)); err != nil {
		return err
	}
	return nil
}
