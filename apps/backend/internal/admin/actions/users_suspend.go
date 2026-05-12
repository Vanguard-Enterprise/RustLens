package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/events"
)

type UsersSuspendParams struct {
	UserID string `json:"userId"`
	Reason string `json:"reason,omitempty"`
}

type usersSuspendAction struct {
	pool *pgxpool.Pool
	pub  events.Publisher
}

// NewUsersSuspendAction returns the users.suspend admin action. Marks a user
// as suspended and bumps the session_epoch so any active signed session is
// rejected on its next request.
func NewUsersSuspendAction(pool *pgxpool.Pool, pub events.Publisher) AdminAction {
	return &usersSuspendAction{pool: pool, pub: pub}
}

func (a *usersSuspendAction) Type() string                 { return "users.suspend" }
func (a *usersSuspendAction) Permission() string           { return rbac.PermUsersSuspend }
func (a *usersSuspendAction) RequiresReason() bool         { return true }
func (a *usersSuspendAction) Dangerous() bool              { return true }
func (a *usersSuspendAction) SensitiveParamKeys() []string { return nil }
func (a *usersSuspendAction) ResourceType() string         { return "user" }

func (a *usersSuspendAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersSuspendParams
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

func (a *usersSuspendAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersSuspendParams
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

	var updatedID string
	var reasonArg any
	if in.Reason != "" {
		reasonArg = in.Reason
	} else if p.Reason != "" {
		reasonArg = p.Reason
	} else {
		reasonArg = nil
	}
	err = tx.QueryRow(ctx, `
		UPDATE app_users
		   SET suspended_at = now(), suspended_by = $2, suspend_reason = $3, updated_at = now()
		 WHERE id = $1 AND suspended_at IS NULL AND deleted_at IS NULL
		 RETURNING id
	`, p.UserID, in.ActorUserID, reasonArg).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("%w: user missing, already suspended, or deleted", ErrConflict)
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

	if a.pub != nil {
		a.pub.PublishWithFanout(p.UserID, events.Event{Type: events.TypeSessionRevoked, Reason: events.ReasonSuspended})
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID},
	}, nil
}
