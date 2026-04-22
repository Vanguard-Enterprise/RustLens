package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
)

var emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

type UsersEmailAddParams struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
}

type usersEmailAddAction struct {
	pool *pgxpool.Pool
}

// NewUsersEmailAddAction attaches an additional (unverified, non-primary)
// email to an existing user. Duplicate emails surface as 409 without leaking
// which account holds them.
func NewUsersEmailAddAction(pool *pgxpool.Pool) AdminAction {
	return &usersEmailAddAction{pool: pool}
}

func (a *usersEmailAddAction) Type() string                 { return "users.email.add" }
func (a *usersEmailAddAction) Permission() string           { return rbac.PermUsersEmailManage }
func (a *usersEmailAddAction) RequiresReason() bool         { return false }
func (a *usersEmailAddAction) Dangerous() bool              { return false }
func (a *usersEmailAddAction) SensitiveParamKeys() []string { return nil }
func (a *usersEmailAddAction) ResourceType() string         { return "user" }

func (a *usersEmailAddAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersEmailAddParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.UserID == "" {
		return ValidatedParams{}, errors.New("userId is required")
	}
	email := strings.ToLower(strings.TrimSpace(p.Email))
	if !emailRegex.MatchString(email) {
		return ValidatedParams{}, errors.New("email is not in a valid format")
	}
	target := p.UserID
	return ValidatedParams{ResourceID: p.UserID, TargetUserID: &target}, nil
}

func (a *usersEmailAddAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersEmailAddParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	email := strings.ToLower(strings.TrimSpace(p.Email))
	if !emailRegex.MatchString(email) {
		return ActionResult{}, fmt.Errorf("%w: email is not in a valid format", ErrValidation)
	}

	// Ensure the user exists (FK would catch this too but the error is nicer).
	var exists bool
	if err := a.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_users WHERE id=$1)`, p.UserID).Scan(&exists); err != nil {
		return ActionResult{}, err
	}
	if !exists {
		return ActionResult{}, fmt.Errorf("%w: user %s", ErrNotFound, p.UserID)
	}

	_, err := a.pool.Exec(ctx, `
		INSERT INTO user_emails (email, user_id, is_primary, is_verified)
		VALUES ($1, $2, false, false)
	`, email, p.UserID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ActionResult{}, fmt.Errorf("%w: email already in use", ErrConflict)
		}
		return ActionResult{}, err
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID, "email": email},
	}, nil
}
