package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
)

type UsersEditParams struct {
	UserID      string  `json:"userId"`
	DisplayName *string `json:"displayName,omitempty"`
	AvatarURL   *string `json:"avatarUrl,omitempty"`
}

type usersEditAction struct{ pool *pgxpool.Pool }

// NewUsersEditAction returns the users.edit admin action. It updates
// app_users.display_name and/or avatar_url on a non-deleted user.
func NewUsersEditAction(pool *pgxpool.Pool) AdminAction {
	return &usersEditAction{pool: pool}
}

func (a *usersEditAction) Type() string                  { return "users.edit" }
func (a *usersEditAction) Permission() string            { return rbac.PermUsersEdit }
func (a *usersEditAction) RequiresReason() bool          { return false }
func (a *usersEditAction) Dangerous() bool               { return false }
func (a *usersEditAction) SensitiveParamKeys() []string  { return nil }
func (a *usersEditAction) ResourceType() string          { return "user" }

func (a *usersEditAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersEditParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.UserID == "" {
		return ValidatedParams{}, errors.New("userId is required")
	}
	if p.DisplayName == nil && p.AvatarURL == nil {
		return ValidatedParams{}, errors.New("nothing to update")
	}
	if p.DisplayName != nil && len(*p.DisplayName) > 80 {
		return ValidatedParams{}, errors.New("displayName too long")
	}
	if p.AvatarURL != nil && len(*p.AvatarURL) > 512 {
		return ValidatedParams{}, errors.New("avatarUrl too long")
	}
	target := p.UserID
	return ValidatedParams{ResourceID: p.UserID, TargetUserID: &target}, nil
}

func (a *usersEditAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersEditParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}

	// Guard: user exists and is not soft-deleted.
	var deletedAt *string
	err := a.pool.QueryRow(ctx, `SELECT deleted_at::text FROM app_users WHERE id=$1`, p.UserID).Scan(&deletedAt)
	if err != nil {
		return ActionResult{}, fmt.Errorf("%w: user %s", ErrNotFound, p.UserID)
	}
	if deletedAt != nil {
		return ActionResult{}, fmt.Errorf("%w: user is deleted", ErrConflict)
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	defer tx.Rollback(ctx)
	if p.DisplayName != nil {
		if _, err := tx.Exec(ctx, `UPDATE app_users SET display_name=$2, updated_at=now() WHERE id=$1`, p.UserID, *p.DisplayName); err != nil {
			return ActionResult{}, err
		}
	}
	if p.AvatarURL != nil {
		if _, err := tx.Exec(ctx, `UPDATE app_users SET avatar_url=$2, updated_at=now() WHERE id=$1`, p.UserID, *p.AvatarURL); err != nil {
			return ActionResult{}, err
		}
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
