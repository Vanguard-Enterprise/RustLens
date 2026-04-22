package users

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is the narrow interface satisfied by *pgxpool.Pool, pgx.Conn, and
// pgx.Tx — enough for RevokeUserSessions to be called inside or outside of a
// transaction.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// RevokeUserSessions increments the user's session_epoch, invalidating all
// previously-signed sessions for this user. Safe to call inside a transaction.
func RevokeUserSessions(ctx context.Context, db Execer, userID string) error {
	_, err := db.Exec(ctx,
		`UPDATE app_users SET session_epoch = session_epoch + 1, updated_at = now() WHERE id = $1`,
		userID)
	return err
}
