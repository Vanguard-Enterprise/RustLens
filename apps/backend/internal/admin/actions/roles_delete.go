package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
)

// RolesDeleteParams holds the validated input for roles.delete.
type RolesDeleteParams struct {
	ID string `json:"id"`
}

type rolesDeleteAction struct{ pool *pgxpool.Pool }

// NewRolesDeleteAction permanently deletes a non-system role. FK ON DELETE
// CASCADE removes admin_role_permissions and admin_user_roles rows.
func NewRolesDeleteAction(pool *pgxpool.Pool) AdminAction {
	return &rolesDeleteAction{pool: pool}
}

func (a *rolesDeleteAction) Type() string                 { return "roles.delete" }
func (a *rolesDeleteAction) Permission() string           { return rbac.PermAdminsGrant }
func (a *rolesDeleteAction) RequiresReason() bool         { return true }
func (a *rolesDeleteAction) Dangerous() bool              { return false }
func (a *rolesDeleteAction) SensitiveParamKeys() []string { return nil }
func (a *rolesDeleteAction) ResourceType() string         { return "role" }

func (a *rolesDeleteAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p RolesDeleteParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.ID == "" {
		return ValidatedParams{}, errors.New("id is required")
	}
	return ValidatedParams{ResourceID: p.ID}, nil
}

func (a *rolesDeleteAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p RolesDeleteParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}

	if err := AssertNotSystemRole(ctx, a.pool, p.ID); err != nil {
		return ActionResult{}, err
	}

	tag, err := a.pool.Exec(ctx,
		`DELETE FROM admin_roles WHERE id::text=$1 AND is_system=FALSE`, p.ID,
	)
	if err != nil {
		return ActionResult{}, err
	}
	if tag.RowsAffected() == 0 {
		return ActionResult{}, fmt.Errorf("%w: role %s", ErrNotFound, p.ID)
	}

	return ActionResult{
		ResourceID: p.ID,
		Payload:    map[string]any{"id": p.ID},
	}, nil
}
