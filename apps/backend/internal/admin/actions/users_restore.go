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

type UsersRestoreParams struct {
	UserID string `json:"userId"`
}

type usersRestoreAction struct{ pool *pgxpool.Pool }

// NewUsersRestoreAction clears deleted_at/deleted_by on a soft-deleted user.
func NewUsersRestoreAction(pool *pgxpool.Pool) AdminAction {
	return &usersRestoreAction{pool: pool}
}

func (a *usersRestoreAction) Type() string                 { return "users.restore" }
func (a *usersRestoreAction) Permission() string           { return rbac.PermUsersDelete }
func (a *usersRestoreAction) RequiresReason() bool         { return false }
func (a *usersRestoreAction) Dangerous() bool              { return false }
func (a *usersRestoreAction) SensitiveParamKeys() []string { return nil }
func (a *usersRestoreAction) ResourceType() string         { return "user" }

func (a *usersRestoreAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersRestoreParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.UserID == "" {
		return ValidatedParams{}, errors.New("userId is required")
	}
	target := p.UserID
	return ValidatedParams{ResourceID: p.UserID, TargetUserID: &target}, nil
}

func (a *usersRestoreAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersRestoreParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	var id string
	err := a.pool.QueryRow(ctx, `
		UPDATE app_users
		   SET deleted_at = NULL, deleted_by = NULL, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NOT NULL
		 RETURNING id
	`, p.UserID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("%w: user missing or not deleted", ErrConflict)
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
