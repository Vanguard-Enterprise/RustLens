package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
)

type UsersUnsuspendParams struct {
	UserID string `json:"userId"`
}

type usersUnsuspendAction struct{ pool *pgxpool.Pool }

// NewUsersUnsuspendAction returns the users.unsuspend admin action. Clears
// the suspension columns on a previously-suspended user. No epoch bump is
// needed — prior sessions were already invalidated at suspend time.
func NewUsersUnsuspendAction(pool *pgxpool.Pool) AdminAction {
	return &usersUnsuspendAction{pool: pool}
}

func (a *usersUnsuspendAction) Type() string                 { return "users.unsuspend" }
func (a *usersUnsuspendAction) Permission() string           { return rbac.PermUsersSuspend }
func (a *usersUnsuspendAction) RequiresReason() bool         { return false }
func (a *usersUnsuspendAction) Dangerous() bool              { return false }
func (a *usersUnsuspendAction) SensitiveParamKeys() []string { return nil }
func (a *usersUnsuspendAction) ResourceType() string         { return "user" }

func (a *usersUnsuspendAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersUnsuspendParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.UserID == "" {
		return ValidatedParams{}, errors.New("userId is required")
	}
	target := p.UserID
	return ValidatedParams{ResourceID: p.UserID, TargetUserID: &target}, nil
}

func (a *usersUnsuspendAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersUnsuspendParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}

	var id string
	err := a.pool.QueryRow(ctx, `
		UPDATE app_users
		   SET suspended_at = NULL, suspended_by = NULL, suspend_reason = NULL, updated_at = now()
		 WHERE id = $1 AND suspended_at IS NOT NULL
		 RETURNING id
	`, p.UserID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("%w: user missing or not suspended", ErrConflict)
	}
	if err != nil {
		return ActionResult{}, err
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID},
	}, nil
}
