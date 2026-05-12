package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/events"
)

// RolesPermissionsSetParams holds the validated input for roles.permissions.set.
type RolesPermissionsSetParams struct {
	ID          string   `json:"id"`
	Permissions []string `json:"permissions"`
}

type rolesPermissionsSetAction struct {
	pool *pgxpool.Pool
	pub  events.Publisher
}

// NewRolesPermissionsSetAction replaces the entire permission set on a
// non-system role. Operates as DELETE-all + bulk INSERT in one transaction.
func NewRolesPermissionsSetAction(pool *pgxpool.Pool, pub events.Publisher) AdminAction {
	return &rolesPermissionsSetAction{pool: pool, pub: pub}
}

func (a *rolesPermissionsSetAction) Type() string                 { return "roles.permissions.set" }
func (a *rolesPermissionsSetAction) Permission() string           { return rbac.PermAdminsGrant }
func (a *rolesPermissionsSetAction) RequiresReason() bool         { return true }
func (a *rolesPermissionsSetAction) Dangerous() bool              { return false }
func (a *rolesPermissionsSetAction) SensitiveParamKeys() []string { return nil }
func (a *rolesPermissionsSetAction) ResourceType() string         { return "role" }

func (a *rolesPermissionsSetAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p RolesPermissionsSetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.ID == "" {
		return ValidatedParams{}, errors.New("id is required")
	}
	seen := map[string]bool{}
	for _, k := range p.Permissions {
		if !rbac.IsValidPermission(k) {
			return ValidatedParams{}, fmt.Errorf("%w: unknown permission: %s", ErrValidation, k)
		}
		if seen[k] {
			return ValidatedParams{}, fmt.Errorf("%w: duplicate permission: %s", ErrValidation, k)
		}
		seen[k] = true
	}
	return ValidatedParams{ResourceID: p.ID}, nil
}

func (a *rolesPermissionsSetAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p RolesPermissionsSetParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}

	if err := AssertNotSystemRole(ctx, a.pool, p.ID); err != nil {
		return ActionResult{}, err
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM admin_role_permissions WHERE role_id::text=$1`, p.ID,
	); err != nil {
		return ActionResult{}, err
	}
	for _, k := range p.Permissions {
		if _, err := tx.Exec(ctx,
			`INSERT INTO admin_role_permissions (role_id, permission_key) VALUES ($1::uuid, $2)`,
			p.ID, k,
		); err != nil {
			return ActionResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ActionResult{}, err
	}

	if a.pub != nil {
		rows, qerr := a.pool.Query(ctx,
			`SELECT user_id FROM admin_user_roles WHERE role_id::text = $1`,
			p.ID,
		)
		if qerr == nil {
			defer rows.Close()
			for rows.Next() {
				var uid string
				if scanErr := rows.Scan(&uid); scanErr != nil {
					continue
				}
				a.pub.PublishWithFanout(uid, events.Event{Type: events.TypeRolesChanged, Reason: events.ReasonRolePermsUpdated})
			}
		}
	}

	return ActionResult{
		ResourceID: p.ID,
		Payload:    map[string]any{"id": p.ID, "count": len(p.Permissions)},
	}, nil
}
