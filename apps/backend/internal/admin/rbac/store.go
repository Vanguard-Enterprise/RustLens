package rbac

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Role struct {
	ID          uuid.UUID
	Key         string
	Name        string
	Description string
	IsSystem    bool
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) SeedPermissionsAndRoles(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, p := range AllPermissions() {
		if _, err := tx.Exec(ctx,
			`INSERT INTO admin_permissions (key, description) VALUES ($1, $2)
			 ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description`,
			p.Key, p.Description,
		); err != nil {
			return err
		}
	}
	for _, r := range SystemRoles() {
		var roleID uuid.UUID
		err := tx.QueryRow(ctx,
			`INSERT INTO admin_roles (key, name, description, is_system)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (key) DO UPDATE
			   SET name = EXCLUDED.name,
			       description = EXCLUDED.description,
			       is_system = EXCLUDED.is_system,
			       updated_at = NOW()
			 RETURNING id`,
			r.Key, r.Name, r.Description, r.IsSystem,
		).Scan(&roleID)
		if err != nil {
			return err
		}
		if r.Key == RoleSuperadmin {
			continue // no explicit perms
		}
		if _, err := tx.Exec(ctx, `DELETE FROM admin_role_permissions WHERE role_id = $1`, roleID); err != nil {
			return err
		}
		for _, p := range r.Permissions {
			if _, err := tx.Exec(ctx,
				`INSERT INTO admin_role_permissions (role_id, permission_key) VALUES ($1, $2)`,
				roleID, p,
			); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, key, name, COALESCE(description, ''), is_system FROM admin_roles ORDER BY key`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Key, &r.Name, &r.Description, &r.IsSystem); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RoleWithMemberCount extends Role with the count of distinct app_users
// currently holding this role (admin_user_roles rows).
type RoleWithMemberCount struct {
	Role
	MemberCount int
}

func (s *Store) ListRolesWithMemberCount(ctx context.Context) ([]RoleWithMemberCount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.key, r.name, COALESCE(r.description, ''), r.is_system,
		       COUNT(ur.user_id)::int AS member_count
		FROM admin_roles r
		LEFT JOIN admin_user_roles ur ON ur.role_id = r.id
		GROUP BY r.id, r.key, r.name, r.description, r.is_system
		ORDER BY r.key
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoleWithMemberCount
	for rows.Next() {
		var r RoleWithMemberCount
		if err := rows.Scan(&r.Role.ID, &r.Role.Key, &r.Role.Name, &r.Role.Description, &r.Role.IsSystem, &r.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RoleIDByKey(ctx context.Context, key string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM admin_roles WHERE key = $1`, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrRoleNotFound
	}
	return id, err
}

var ErrRoleNotFound = errors.New("role not found")

// AssignRole attaches a role to the given app_users(id). grantedBy is the
// optional acting user's TEXT id; pass an empty string to omit.
func (s *Store) AssignRole(ctx context.Context, userID string, roleID uuid.UUID, grantedBy string) error {
	var gb any
	if grantedBy != "" {
		gb = grantedBy
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO admin_user_roles (user_id, role_id, granted_by)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, role_id) DO NOTHING`,
		userID, roleID, gb,
	)
	return err
}

func (s *Store) RevokeRole(ctx context.Context, userID string, roleID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM admin_user_roles WHERE user_id = $1 AND role_id = $2`,
		userID, roleID,
	)
	return err
}

func (s *Store) RolesForUser(ctx context.Context, userID string) ([]Role, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.key, r.name, COALESCE(r.description, ''), r.is_system
		FROM admin_user_roles ur
		JOIN admin_roles r ON r.id = ur.role_id
		WHERE ur.user_id = $1
		ORDER BY r.key
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Key, &r.Name, &r.Description, &r.IsSystem); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) PermissionsForRole(ctx context.Context, roleID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT permission_key FROM admin_role_permissions WHERE role_id = $1 ORDER BY permission_key`,
		roleID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) PermissionsForUser(ctx context.Context, userID string) ([]string, error) {
	roles, err := s.RolesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, r := range roles {
		if r.Key == RoleSuperadmin {
			all := AllPermissions()
			keys := make([]string, len(all))
			for i, p := range all {
				keys[i] = p.Key
			}
			return keys, nil
		}
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT rp.permission_key
		FROM admin_user_roles ur
		JOIN admin_role_permissions rp ON rp.role_id = ur.role_id
		WHERE ur.user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
