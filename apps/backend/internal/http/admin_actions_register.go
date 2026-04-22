package httpapi

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/rbac"
)

// registerAdminActions wires the concrete AdminAction implementations into
// the action registry at handler boot. Keep this list alphabetised.
func registerAdminActions(reg *actions.Registry, pool *pgxpool.Pool, resolver *rbac.Resolver) error {
	_ = resolver // used once grant/revoke actions are registered
	if err := reg.Register(actions.NewUsersEditAction(pool)); err != nil {
		return err
	}
	return nil
}
