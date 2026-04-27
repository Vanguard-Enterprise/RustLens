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

type UsersDeleteParams struct {
	UserID string `json:"userId"`
	Reason string `json:"reason,omitempty"`
}

type usersDeleteAction struct{ pool *pgxpool.Pool }

// NewUsersDeleteAction soft-deletes a user (sets deleted_at/deleted_by) and
// bumps session_epoch to invalidate any active sessions.
func NewUsersDeleteAction(pool *pgxpool.Pool) AdminAction {
	return &usersDeleteAction{pool: pool}
}

func (a *usersDeleteAction) Type() string                 { return "users.delete" }
func (a *usersDeleteAction) Permission() string           { return rbac.PermUsersDelete }
func (a *usersDeleteAction) RequiresReason() bool         { return true }
func (a *usersDeleteAction) Dangerous() bool              { return true }
func (a *usersDeleteAction) SensitiveParamKeys() []string { return nil }
func (a *usersDeleteAction) ResourceType() string         { return "user" }

func (a *usersDeleteAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersDeleteParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.UserID == "" {
		return ValidatedParams{}, errors.New("userId is required")
	}
	if len(p.Reason) > 500 {
		return ValidatedParams{}, errors.New("reason too long")
	}
	target := p.UserID
	return ValidatedParams{
		ResourceID:           p.UserID,
		TargetUserID:         &target,
		ConfirmationExpected: p.UserID,
	}, nil
}

func (a *usersDeleteAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersDeleteParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := GuardActingOnSelf(in.ActorUserID, p.UserID); err != nil {
		return ActionResult{}, err
	}
	if err := GuardLastSuperadminByUser(ctx, a.pool, p.UserID); err != nil {
		return ActionResult{}, err
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	defer tx.Rollback(ctx)

	var id string
	err = tx.QueryRow(ctx, `
		UPDATE app_users
		   SET deleted_at = now(), deleted_by = $2, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL
		 RETURNING id
	`, p.UserID, in.ActorUserID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("%w: user missing or already deleted", ErrConflict)
	}
	if err != nil {
		return ActionResult{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE app_users SET session_epoch = session_epoch + 1 WHERE id = $1`, p.UserID); err != nil {
		return ActionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ActionResult{}, err
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID},
	}, nil
}
