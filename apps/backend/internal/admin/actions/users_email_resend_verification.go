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

type UsersEmailResendVerificationParams struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
}

type usersEmailResendVerificationAction struct {
	pool    *pgxpool.Pool
	baseURL string
}

// NewUsersEmailResendVerificationAction rotates the email verification token
// for a user-owned email and returns the consumption URL. The URL is shown once
// to the admin and is NOT persisted in the audit log.
func NewUsersEmailResendVerificationAction(pool *pgxpool.Pool, appBaseURL string) AdminAction {
	return &usersEmailResendVerificationAction{pool: pool, baseURL: strings.TrimRight(appBaseURL, "/")}
}

func (a *usersEmailResendVerificationAction) Type() string                 { return "users.email.resendVerification" }
func (a *usersEmailResendVerificationAction) Permission() string           { return rbac.PermUsersEmailManage }
func (a *usersEmailResendVerificationAction) RequiresReason() bool         { return false }
func (a *usersEmailResendVerificationAction) Dangerous() bool              { return false }
func (a *usersEmailResendVerificationAction) SensitiveParamKeys() []string { return nil }
func (a *usersEmailResendVerificationAction) ResourceType() string         { return "user" }

func (a *usersEmailResendVerificationAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersEmailResendVerificationParams
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

func (a *usersEmailResendVerificationAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	if a.baseURL == "" {
		return ActionResult{}, errors.New("APP_BASE_URL is not configured")
	}
	var p UsersEmailResendVerificationParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	email := strings.ToLower(strings.TrimSpace(p.Email))

	// Email must exist for this user.
	var exists bool
	err := a.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM user_emails WHERE user_id=$1 AND email=$2)`,
		p.UserID, email,
	).Scan(&exists)
	if err != nil {
		return ActionResult{}, err
	}
	if !exists {
		return ActionResult{}, fmt.Errorf("%w: email %s for user %s", ErrNotFound, email, p.UserID)
	}

	// Invalidate any outstanding unconsumed, unexpired tokens for this (user, email).
	if _, err := a.pool.Exec(ctx, `
		UPDATE email_verification_tokens
		   SET consumed_at = now()
		 WHERE user_id = $1 AND email = $2 AND consumed_at IS NULL AND expires_at > now()
	`, p.UserID, email); err != nil {
		return ActionResult{}, err
	}

	// 32 random bytes → base64url raw → sha256 hash for storage.
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ActionResult{}, err
	}
	rawToken := base64.RawURLEncoding.EncodeToString(buf[:])
	hashBytes := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hashBytes[:])

	if _, err := a.pool.Exec(ctx, `
		INSERT INTO email_verification_tokens (token_hash, user_id, email, expires_at, created_at)
		VALUES ($1, $2, $3, now() + interval '24 hours', now())
	`, tokenHash, p.UserID, email); err != nil {
		// Defensive: if somehow a race caused the email to disappear, surface as NotFound.
		if errors.Is(err, pgx.ErrNoRows) {
			return ActionResult{}, fmt.Errorf("%w: email %s", ErrNotFound, email)
		}
		return ActionResult{}, err
	}

	u := a.baseURL + "/verify-email?token=" + url.QueryEscape(rawToken)
	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID, "email": email, "tokenUrl": u},
	}, nil
}
