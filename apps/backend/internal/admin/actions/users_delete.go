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

type UsersDeleteParams struct {
	UserID string `json:"userId"`
	Reason string `json:"reason,omitempty"`
}

type usersDeleteAction struct {
	pool *pgxpool.Pool
	pub  events.Publisher
}

// NewUsersDeleteAction hard-deletes a user row. FK cascades remove the user's
// admin role assignments; audit_events.target_user_id has no FK so the audit
// trail survives the delete.
func NewUsersDeleteAction(pool *pgxpool.Pool, pub events.Publisher) AdminAction {
	return &usersDeleteAction{pool: pool, pub: pub}
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

	// Confirm the user exists before issuing DELETE (so we can return 404).
	var id string
	err := a.pool.QueryRow(ctx, `SELECT id FROM app_users WHERE id=$1`, p.UserID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("%w: user %s", ErrNotFound, p.UserID)
	}
	if err != nil {
		return ActionResult{}, err
	}

	if _, err := a.pool.Exec(ctx, `DELETE FROM app_users WHERE id=$1`, p.UserID); err != nil {
		return ActionResult{}, err
	}

	if a.pub != nil {
		a.pub.PublishWithFanout(p.UserID, events.Event{Type: events.TypeSessionRevoked, Reason: events.ReasonDeleted})
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID, "deleted": true},
	}, nil
}
