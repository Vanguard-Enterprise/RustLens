package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UsersPurgeParams struct {
	UserID    string `json:"userId"`
	ConfirmID string `json:"confirmId"`
}

type usersPurgeAction struct{ pool *pgxpool.Pool }

// NewUsersPurgeAction hard-deletes a user row. Superadmin-only; bypasses the
// normal permission-key check via SuperadminOnly(). FK cascades remove the
// user's emails, credentials, connected accounts, admin role assignments and
// outstanding auth tokens.
func NewUsersPurgeAction(pool *pgxpool.Pool) AdminAction {
	return &usersPurgeAction{pool: pool}
}

func (a *usersPurgeAction) Type() string                 { return "users.purge" }
func (a *usersPurgeAction) Permission() string           { return "" }
func (a *usersPurgeAction) RequiresReason() bool         { return true }
func (a *usersPurgeAction) Dangerous() bool              { return true }
func (a *usersPurgeAction) SensitiveParamKeys() []string { return nil }
func (a *usersPurgeAction) ResourceType() string         { return "user" }

// SuperadminOnly signals the dispatcher to skip the Permission() key check
// and instead require the actor to hold the superadmin role.
func (a *usersPurgeAction) SuperadminOnly() bool { return true }

func (a *usersPurgeAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersPurgeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.UserID == "" {
		return ValidatedParams{}, errors.New("userId is required")
	}
	if p.ConfirmID != p.UserID {
		return ValidatedParams{}, errors.New("confirmId must equal userId")
	}
	target := p.UserID
	return ValidatedParams{
		ResourceID:           p.UserID,
		TargetUserID:         &target,
		ConfirmationExpected: p.UserID,
	}, nil
}

func (a *usersPurgeAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersPurgeParams
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

	// FK cascades on user_emails, user_password_credentials,
	// user_connected_accounts, admin_user_roles, and the token tables handle
	// the children. audit_events.target_user_id does NOT have a FK so the
	// audit trail survives the purge.
	if _, err := a.pool.Exec(ctx, `DELETE FROM app_users WHERE id=$1`, p.UserID); err != nil {
		return ActionResult{}, err
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID, "purged": true},
	}, nil
}
