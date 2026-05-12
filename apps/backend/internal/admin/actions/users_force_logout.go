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

// UsersForceLogoutParams identifies the user whose sessions should be invalidated.
type UsersForceLogoutParams struct {
	UserID string `json:"userId"`
}

type usersForceLogoutAction struct {
	pool *pgxpool.Pool
	pub  events.Publisher
}

// NewUsersForceLogoutAction bumps a user's session_epoch (invalidating every
// signed cookie they hold) and publishes a session.revoked event so any open
// SSE connections close on the receiving end. Required permission:
// users.security.reset.
func NewUsersForceLogoutAction(pool *pgxpool.Pool, pub events.Publisher) AdminAction {
	return &usersForceLogoutAction{pool: pool, pub: pub}
}

func (a *usersForceLogoutAction) Type() string                 { return "users.force_logout" }
func (a *usersForceLogoutAction) Permission() string           { return rbac.PermUsersSecurityReset }
func (a *usersForceLogoutAction) RequiresReason() bool         { return true }
func (a *usersForceLogoutAction) Dangerous() bool              { return false }
func (a *usersForceLogoutAction) SensitiveParamKeys() []string { return nil }
func (a *usersForceLogoutAction) ResourceType() string         { return "user" }

func (a *usersForceLogoutAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersForceLogoutParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.UserID == "" {
		return ValidatedParams{}, errors.New("userId is required")
	}
	target := p.UserID
	return ValidatedParams{ResourceID: p.UserID, TargetUserID: &target}, nil
}

func (a *usersForceLogoutAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersForceLogoutParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := GuardActingOnSelf(in.ActorUserID, p.UserID); err != nil {
		return ActionResult{}, err
	}

	var updatedID string
	err := a.pool.QueryRow(ctx,
		`UPDATE app_users SET session_epoch = session_epoch + 1, updated_at = now() WHERE id = $1 RETURNING id`,
		p.UserID,
	).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("%w: user not found", ErrNotFound)
	}
	if err != nil {
		return ActionResult{}, err
	}

	if a.pub != nil {
		a.pub.PublishWithFanout(p.UserID, events.Event{
			Type:   events.TypeSessionRevoked,
			Reason: events.ReasonForceLogout,
		})
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID},
	}, nil
}
