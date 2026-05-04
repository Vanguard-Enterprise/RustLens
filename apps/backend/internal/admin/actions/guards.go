package actions

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GuardActingOnSelf returns ErrConflict if the actor is trying to act on their
// own user account. Passed-in empty actor id is treated as a no-op (the
// dispatcher already rejects unauthenticated calls).
func GuardActingOnSelf(actorID, targetID string) error {
	if actorID != "" && actorID == targetID {
		return fmt.Errorf("%w: cannot perform this action on your own account", ErrConflict)
	}
	return nil
}

// AssertNotSystemRole returns ErrPermissionDenied if the role at roleID has
// is_system=true, ErrNotFound if no row exists, or any underlying database
// error otherwise. roleID must be a role UUID string (not a role key) — the
// query matches against admin_roles.id.
func AssertNotSystemRole(ctx context.Context, pool *pgxpool.Pool, roleID string) error {
	var isSystem bool
	err := pool.QueryRow(ctx,
		`SELECT is_system FROM admin_roles WHERE id::text = $1`, roleID,
	).Scan(&isSystem)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: role %s", ErrNotFound, roleID)
	}
	if err != nil {
		return err
	}
	if isSystem {
		return fmt.Errorf("%w: cannot modify system role", ErrPermissionDenied)
	}
	return nil
}

// GuardLastSuperadminByUser returns ErrConflict if the target user holds the
// superadmin role and removing/disabling them would leave zero users with the
// superadmin role. Returns nil if the target is not a superadmin, or if there
// is at least one other superadmin in the system.
func GuardLastSuperadminByUser(ctx context.Context, pool *pgxpool.Pool, userID string) error {
	var hasSuperadmin bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM admin_user_roles aur
			JOIN admin_roles r ON r.id = aur.role_id
			WHERE aur.user_id = $1 AND r.key = 'superadmin'
		)
	`, userID).Scan(&hasSuperadmin); err != nil {
		return err
	}
	if !hasSuperadmin {
		return nil
	}
	var total int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT aur.user_id)
		FROM admin_user_roles aur
		JOIN admin_roles r ON r.id = aur.role_id
		WHERE r.key = 'superadmin'
	`).Scan(&total); err != nil {
		return err
	}
	if total <= 1 {
		return fmt.Errorf("%w: cannot perform this action on the last superadmin", ErrConflict)
	}
	return nil
}
