package actions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
)

type UsersPasswordForceResetParams struct {
	UserID string `json:"userId"`
}

type usersPasswordForceResetAction struct {
	pool    *pgxpool.Pool
	baseURL string
}

// NewUsersPasswordForceResetAction generates a one-time password reset token
// for the given user, invalidating any outstanding tokens, and returns the
// consumption URL in the action output. The URL is shown once to the admin
// and is NOT persisted in the audit log.
func NewUsersPasswordForceResetAction(pool *pgxpool.Pool, appBaseURL string) AdminAction {
	return &usersPasswordForceResetAction{pool: pool, baseURL: strings.TrimRight(appBaseURL, "/")}
}

func (a *usersPasswordForceResetAction) Type() string                 { return "users.password.forceReset" }
func (a *usersPasswordForceResetAction) Permission() string           { return rbac.PermUsersSecurityReset }
func (a *usersPasswordForceResetAction) RequiresReason() bool         { return true }
func (a *usersPasswordForceResetAction) Dangerous() bool              { return true }
func (a *usersPasswordForceResetAction) SensitiveParamKeys() []string { return nil }
func (a *usersPasswordForceResetAction) ResourceType() string         { return "user" }

func (a *usersPasswordForceResetAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersPasswordForceResetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.UserID == "" {
		return ValidatedParams{}, errors.New("userId is required")
	}
	target := p.UserID
	return ValidatedParams{
		ResourceID:           p.UserID,
		TargetUserID:         &target,
		ConfirmationExpected: p.UserID,
	}, nil
}

func (a *usersPasswordForceResetAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	if a.baseURL == "" {
		return ActionResult{}, errors.New("APP_BASE_URL is not configured")
	}
	var p UsersPasswordForceResetParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}

	// Verify user exists and is not soft-deleted.
	var deletedAt *string
	var primaryEmail *string
	err := a.pool.QueryRow(ctx, `
		SELECT u.deleted_at::text,
		       (SELECT email FROM user_emails WHERE user_id=u.id AND is_primary LIMIT 1)
		  FROM app_users u WHERE u.id=$1
	`, p.UserID).Scan(&deletedAt, &primaryEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("%w: user %s", ErrNotFound, p.UserID)
	}
	if err != nil {
		return ActionResult{}, err
	}
	if deletedAt != nil {
		return ActionResult{}, fmt.Errorf("%w: user is deleted", ErrConflict)
	}

	// Invalidate any outstanding unconsumed, unexpired tokens.
	if _, err := a.pool.Exec(ctx, `
		UPDATE password_reset_tokens
		   SET consumed_at = now()
		 WHERE user_id = $1 AND consumed_at IS NULL AND expires_at > now()
	`, p.UserID); err != nil {
		return ActionResult{}, err
	}

	// Generate 32 random bytes, base64url-encode for the URL, sha256 for storage.
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ActionResult{}, err
	}
	rawToken := base64.RawURLEncoding.EncodeToString(buf[:])
	hashBytes := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hashBytes[:])

	// If the user has no primary email (rare but possible), store NULL so we
	// don't trip the password_reset_tokens.email FK to user_emails.email.
	var emailArg any
	if primaryEmail != nil && *primaryEmail != "" {
		emailArg = *primaryEmail
	}

	if _, err := a.pool.Exec(ctx, `
		INSERT INTO password_reset_tokens (token_hash, user_id, email, expires_at, created_at)
		VALUES ($1, $2, $3, now() + interval '1 hour', now())
	`, tokenHash, p.UserID, emailArg); err != nil {
		return ActionResult{}, err
	}

	u := a.baseURL + "/reset-password?token=" + url.QueryEscape(rawToken)
	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID, "tokenUrl": u},
	}, nil
}
