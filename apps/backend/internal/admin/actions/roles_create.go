package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
)

// RolesCreateParams holds the validated input for roles.create.
type RolesCreateParams struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

type rolesCreateAction struct{ pool *pgxpool.Pool }

// NewRolesCreateAction returns the roles.create admin action. It inserts a new
// non-system role and its permission assignments in one transaction. Refuses
// keys that collide with seeded system roles (case-insensitive on key).
func NewRolesCreateAction(pool *pgxpool.Pool) AdminAction {
	return &rolesCreateAction{pool: pool}
}

func (a *rolesCreateAction) Type() string                 { return "roles.create" }
func (a *rolesCreateAction) Permission() string           { return rbac.PermAdminsGrant }
func (a *rolesCreateAction) RequiresReason() bool         { return true }
func (a *rolesCreateAction) Dangerous() bool              { return false }
func (a *rolesCreateAction) SensitiveParamKeys() []string { return nil }
func (a *rolesCreateAction) ResourceType() string         { return "role" }

var roleKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

func (a *rolesCreateAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p RolesCreateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if !roleKeyRe.MatchString(p.Key) {
		return ValidatedParams{}, errors.New("key must match ^[a-z][a-z0-9_]{1,63}$")
	}
	if p.Name == "" || len(p.Name) > 80 {
		return ValidatedParams{}, errors.New("name required, max 80 chars")
	}
	if len(p.Description) > 500 {
		return ValidatedParams{}, errors.New("description too long")
	}
	seen := map[string]bool{}
	for _, k := range p.Permissions {
		if !rbac.IsValidPermission(k) {
			return ValidatedParams{}, fmt.Errorf("unknown permission: %s", k)
		}
		if seen[k] {
			return ValidatedParams{}, fmt.Errorf("duplicate permission: %s", k)
		}
		seen[k] = true
	}
	return ValidatedParams{ResourceID: p.Key}, nil
}

func (a *rolesCreateAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p RolesCreateParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}

	// System-key collision (covers both pre-existing system rows and any
	// future seeded ones).
	for _, sys := range rbac.SystemRoles() {
		if sys.Key == p.Key {
			return ActionResult{}, fmt.Errorf("%w: key %s reserved for system role", ErrConflict, p.Key)
		}
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	defer tx.Rollback(ctx)

	var newID string
	err = tx.QueryRow(ctx, `
		INSERT INTO admin_roles (key, name, description, is_system)
		VALUES ($1,$2,$3,FALSE) RETURNING id::text
	`, p.Key, p.Name, p.Description).Scan(&newID)
	if err != nil {
		// 23505 = unique_violation
		if pgErr, ok := pgxErrCode(err); ok && pgErr == "23505" {
			return ActionResult{}, fmt.Errorf("%w: key %s already exists", ErrConflict, p.Key)
		}
		return ActionResult{}, err
	}
	for _, k := range p.Permissions {
		if _, err := tx.Exec(ctx,
			`INSERT INTO admin_role_permissions (role_id, permission_key) VALUES ($1::uuid, $2)`,
			newID, k,
		); err != nil {
			return ActionResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ActionResult{}, err
	}

	return ActionResult{
		ResourceID: newID,
		Payload:    map[string]any{"id": newID, "key": p.Key},
	}, nil
}

// pgxErrCode extracts the SQLSTATE from a pgx driver error if present.
// pgx v5's *pgconn.PgError implements SQLState() so this catches it without
// importing pgconn explicitly.
func pgxErrCode(err error) (string, bool) {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState(), true
	}
	return "", false
}
