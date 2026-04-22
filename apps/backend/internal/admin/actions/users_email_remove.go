package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
)

type UsersEmailRemoveParams struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
}

type usersEmailRemoveAction struct {
	pool *pgxpool.Pool
}

// NewUsersEmailRemoveAction removes a secondary email from a user. Primary
// emails are protected (409); missing emails are 404.
func NewUsersEmailRemoveAction(pool *pgxpool.Pool) AdminAction {
	return &usersEmailRemoveAction{pool: pool}
}

func (a *usersEmailRemoveAction) Type() string                 { return "users.email.remove" }
func (a *usersEmailRemoveAction) Permission() string           { return rbac.PermUsersEmailManage }
func (a *usersEmailRemoveAction) RequiresReason() bool         { return true }
func (a *usersEmailRemoveAction) Dangerous() bool              { return true }
func (a *usersEmailRemoveAction) SensitiveParamKeys() []string { return nil }
func (a *usersEmailRemoveAction) ResourceType() string         { return "user" }

func (a *usersEmailRemoveAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersEmailRemoveParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.UserID == "" {
		return ValidatedParams{}, errors.New("userId is required")
	}
	if p.Email == "" {
		return ValidatedParams{}, errors.New("email is required")
	}
	target := p.UserID
	return ValidatedParams{
		ResourceID:           p.UserID,
		TargetUserID:         &target,
		ConfirmationExpected: strings.ToLower(strings.TrimSpace(p.Email)),
	}, nil
}

func (a *usersEmailRemoveAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersEmailRemoveParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	email := strings.ToLower(strings.TrimSpace(p.Email))

	var isPrimary bool
	err := a.pool.QueryRow(ctx,
		`SELECT is_primary FROM user_emails WHERE user_id=$1 AND email=$2`,
		p.UserID, email,
	).Scan(&isPrimary)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("%w: email %s for user %s", ErrNotFound, email, p.UserID)
	}
	if err != nil {
		return ActionResult{}, err
	}
	if isPrimary {
		return ActionResult{}, fmt.Errorf("%w: cannot remove primary email", ErrConflict)
	}

	tag, err := a.pool.Exec(ctx,
		`DELETE FROM user_emails WHERE user_id=$1 AND email=$2`, p.UserID, email)
	if err != nil {
		return ActionResult{}, err
	}
	if tag.RowsAffected() == 0 {
		return ActionResult{}, fmt.Errorf("%w: email %s", ErrNotFound, email)
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID, "email": email},
	}, nil
}
