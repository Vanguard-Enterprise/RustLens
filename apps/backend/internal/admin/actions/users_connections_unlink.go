package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
)

type UsersConnectionsUnlinkParams struct {
	UserID            string `json:"userId"`
	Provider          string `json:"provider"`
	ProviderAccountID string `json:"providerAccountId"`
}

type usersConnectionsUnlinkAction struct {
	pool *pgxpool.Pool
}

// NewUsersConnectionsUnlinkAction removes an external provider connection
// (Discord / Steam) from a user.
func NewUsersConnectionsUnlinkAction(pool *pgxpool.Pool) AdminAction {
	return &usersConnectionsUnlinkAction{pool: pool}
}

func (a *usersConnectionsUnlinkAction) Type() string                 { return "users.connections.unlink" }
func (a *usersConnectionsUnlinkAction) Permission() string           { return rbac.PermUsersConnectionsManage }
func (a *usersConnectionsUnlinkAction) RequiresReason() bool         { return true }
func (a *usersConnectionsUnlinkAction) Dangerous() bool              { return true }
func (a *usersConnectionsUnlinkAction) SensitiveParamKeys() []string { return nil }
func (a *usersConnectionsUnlinkAction) ResourceType() string         { return "user" }

func (a *usersConnectionsUnlinkAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersConnectionsUnlinkParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.UserID == "" {
		return ValidatedParams{}, errors.New("userId is required")
	}
	if p.Provider != "discord" && p.Provider != "steam" {
		return ValidatedParams{}, errors.New("provider must be 'discord' or 'steam'")
	}
	if p.ProviderAccountID == "" {
		return ValidatedParams{}, errors.New("providerAccountId is required")
	}
	target := p.UserID
	return ValidatedParams{
		ResourceID:           p.UserID,
		TargetUserID:         &target,
		ConfirmationExpected: p.UserID,
	}, nil
}

func (a *usersConnectionsUnlinkAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersConnectionsUnlinkParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if p.Provider != "discord" && p.Provider != "steam" {
		return ActionResult{}, fmt.Errorf("%w: provider must be 'discord' or 'steam'", ErrValidation)
	}

	tag, err := a.pool.Exec(ctx, `
		DELETE FROM user_connected_accounts
		 WHERE user_id=$1 AND provider=$2 AND provider_account_id=$3
	`, p.UserID, p.Provider, p.ProviderAccountID)
	if err != nil {
		return ActionResult{}, err
	}
	if tag.RowsAffected() == 0 {
		return ActionResult{}, fmt.Errorf("%w: connection not found", ErrNotFound)
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload: map[string]any{
			"userId":            p.UserID,
			"provider":          p.Provider,
			"providerAccountId": p.ProviderAccountID,
		},
	}, nil
}
