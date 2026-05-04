package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
)

// RolesUpdateParams holds the validated input for roles.update.
type RolesUpdateParams struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type rolesUpdateAction struct{ pool *pgxpool.Pool }

// NewRolesUpdateAction updates a non-system role's name and description.
// Key is intentionally immutable post-create.
func NewRolesUpdateAction(pool *pgxpool.Pool) AdminAction {
	return &rolesUpdateAction{pool: pool}
}

func (a *rolesUpdateAction) Type() string                 { return "roles.update" }
func (a *rolesUpdateAction) Permission() string           { return rbac.PermAdminsGrant }
func (a *rolesUpdateAction) RequiresReason() bool         { return true }
func (a *rolesUpdateAction) Dangerous() bool              { return false }
func (a *rolesUpdateAction) SensitiveParamKeys() []string { return nil }
func (a *rolesUpdateAction) ResourceType() string         { return "role" }

func (a *rolesUpdateAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p RolesUpdateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.ID == "" {
		return ValidatedParams{}, errors.New("id is required")
	}
	if p.Name == "" || len(p.Name) > 80 {
		return ValidatedParams{}, errors.New("name required, max 80 chars")
	}
	if len(p.Description) > 500 {
		return ValidatedParams{}, errors.New("description too long")
	}
	return ValidatedParams{ResourceID: p.ID}, nil
}

func (a *rolesUpdateAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p RolesUpdateParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}

	if err := AssertNotSystemRole(ctx, a.pool, p.ID); err != nil {
		return ActionResult{}, err
	}

	tag, err := a.pool.Exec(ctx,
		`UPDATE admin_roles SET name=$2, description=$3, updated_at=NOW() WHERE id::text=$1 AND is_system=FALSE`,
		p.ID, p.Name, p.Description,
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
