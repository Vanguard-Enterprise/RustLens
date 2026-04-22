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

type UsersEmailSetPrimaryParams struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
}

type usersEmailSetPrimaryAction struct {
	pool *pgxpool.Pool
}

// NewUsersEmailSetPrimaryAction promotes one of a user's verified emails to
// primary and demotes all others.
func NewUsersEmailSetPrimaryAction(pool *pgxpool.Pool) AdminAction {
	return &usersEmailSetPrimaryAction{pool: pool}
}

func (a *usersEmailSetPrimaryAction) Type() string                 { return "users.email.setPrimary" }
func (a *usersEmailSetPrimaryAction) Permission() string           { return rbac.PermUsersEmailManage }
func (a *usersEmailSetPrimaryAction) RequiresReason() bool         { return false }
func (a *usersEmailSetPrimaryAction) Dangerous() bool              { return false }
func (a *usersEmailSetPrimaryAction) SensitiveParamKeys() []string { return nil }
func (a *usersEmailSetPrimaryAction) ResourceType() string         { return "user" }

func (a *usersEmailSetPrimaryAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersEmailSetPrimaryParams
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
	return ValidatedParams{ResourceID: p.UserID, TargetUserID: &target}, nil
}

func (a *usersEmailSetPrimaryAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersEmailSetPrimaryParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	email := strings.ToLower(strings.TrimSpace(p.Email))

	tx, err := a.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ActionResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Check email exists for user and fetch verification state.
	var verified bool
	err = tx.QueryRow(ctx,
		`SELECT is_verified FROM user_emails WHERE user_id=$1 AND email=$2`,
		p.UserID, email,
	).Scan(&verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("%w: email %s for user %s", ErrNotFound, email, p.UserID)
	}
	if err != nil {
		return ActionResult{}, err
	}
	if !verified {
		return ActionResult{}, fmt.Errorf("%w: email is not verified", ErrConflict)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE user_emails SET is_primary = (email = $2) WHERE user_id = $1`,
		p.UserID, email); err != nil {
		return ActionResult{}, err
	}

	// Safety: verify exactly one primary row for this user.
	var primaryCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_emails WHERE user_id=$1 AND is_primary=true`, p.UserID).Scan(&primaryCount); err != nil {
		return ActionResult{}, err
	}
	if primaryCount != 1 {
		return ActionResult{}, fmt.Errorf("%w: expected exactly 1 primary email, got %d", ErrConflict, primaryCount)
	}

	if err := tx.Commit(ctx); err != nil {
		return ActionResult{}, err
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID, "email": email},
	}, nil
}
