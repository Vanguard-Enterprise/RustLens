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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
)

type UsersInviteParams struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

type usersInviteAction struct {
	pool    *pgxpool.Pool
	baseURL string
}

// NewUsersInviteAction creates a new user account from an email address and
// returns a one-time sign-in URL that the invitee uses to complete sign-up.
// The URL is shown once to the admin and is NOT persisted in the audit log.
func NewUsersInviteAction(pool *pgxpool.Pool, appBaseURL string) AdminAction {
	return &usersInviteAction{pool: pool, baseURL: strings.TrimRight(appBaseURL, "/")}
}

func (a *usersInviteAction) Type() string                 { return "users.invite" }
func (a *usersInviteAction) Permission() string           { return rbac.PermUsersCreate }
func (a *usersInviteAction) RequiresReason() bool         { return true }
func (a *usersInviteAction) Dangerous() bool              { return true }
func (a *usersInviteAction) SensitiveParamKeys() []string { return nil }
func (a *usersInviteAction) ResourceType() string         { return "user" }

func (a *usersInviteAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersInviteParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	email := strings.ToLower(strings.TrimSpace(p.Email))
	if !emailRegex.MatchString(email) {
		return ValidatedParams{}, errors.New("email is not in a valid format")
	}
	return ValidatedParams{
		ResourceID:           email,
		ConfirmationExpected: email,
	}, nil
}

func generateUserID() (string, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "usr_" + hex.EncodeToString(buf[:]), nil
}

func (a *usersInviteAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	if a.baseURL == "" {
		return ActionResult{}, errors.New("APP_BASE_URL is not configured")
	}
	var p UsersInviteParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	email := strings.ToLower(strings.TrimSpace(p.Email))
	if !emailRegex.MatchString(email) {
		return ActionResult{}, fmt.Errorf("%w: email is not in a valid format", ErrValidation)
	}
	displayName := strings.TrimSpace(p.DisplayName)

	// Fast reject: email already present in user_emails (any user, including soft-deleted).
	var taken bool
	if err := a.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM user_emails WHERE email=$1)`, email).Scan(&taken); err != nil {
		return ActionResult{}, err
	}
	if taken {
		return ActionResult{}, fmt.Errorf("%w: email already in use", ErrConflict)
	}

	tx, err := a.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ActionResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Generate user ID. Retry once on unlikely collision.
	var userID string
	for attempt := 0; attempt < 2; attempt++ {
		userID, err = generateUserID()
		if err != nil {
			return ActionResult{}, err
		}
		_, ierr := tx.Exec(ctx, `
			INSERT INTO app_users (id, display_name, avatar_url, roles)
			VALUES ($1, $2, '', '["user"]'::jsonb)
		`, userID, displayName)
		if ierr == nil {
			break
		}
		var pgErr *pgconn.PgError
		if errors.As(ierr, &pgErr) && pgErr.Code == "23505" && attempt == 0 {
			continue
		}
		return ActionResult{}, ierr
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO user_emails (email, user_id, is_primary, is_verified)
		VALUES ($1, $2, true, false)
	`, email, userID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ActionResult{}, fmt.Errorf("%w: email already in use", ErrConflict)
		}
		return ActionResult{}, err
	}

	// Generate sign-in token (32 random bytes, sha256 hashed in DB, raw in URL).
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ActionResult{}, err
	}
	rawToken := base64.RawURLEncoding.EncodeToString(buf[:])
	hashBytes := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hashBytes[:])

	if _, err := tx.Exec(ctx, `
		INSERT INTO email_sign_in_tokens (token_hash, email, expires_at, created_at)
		VALUES ($1, $2, now() + interval '24 hours', now())
	`, tokenHash, email); err != nil {
		return ActionResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ActionResult{}, err
	}

	u := a.baseURL + "/sign-in?token=" + url.QueryEscape(rawToken)
	target := userID
	return ActionResult{
		ResourceID:   userID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": userID, "tokenUrl": u},
	}, nil
}
