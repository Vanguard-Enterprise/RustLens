package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

const minPasswordLength = 12

var (
	ErrInvalidCredentials        = errors.New("invalid credentials")
	ErrEmailAlreadyRegistered    = errors.New("email already registered")
	ErrWeakPassword              = errors.New("password must be at least 12 characters")
	ErrPasswordResetTokenInvalid = errors.New("password reset token invalid")
	ErrEmailVerificationInvalid  = errors.New("email verification token invalid")
	ErrEmailAlreadyVerified      = errors.New("email already verified")
)

func validatePassword(password string) error {
	if len(strings.TrimSpace(password)) < minPasswordLength {
		return ErrWeakPassword
	}
	return nil
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func (p *PostgresStore) CreatePasswordUser(ctx context.Context, email string, password string, displayName string) (CurrentUser, error) {
	normalizedEmail := normalizeEmail(email)
	if normalizedEmail == "" {
		return CurrentUser{}, ErrEmailNotLinked
	}
	if err := validatePassword(password); err != nil {
		return CurrentUser{}, err
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		return CurrentUser{}, err
	}

	if strings.TrimSpace(displayName) == "" {
		displayName = strings.Split(normalizedEmail, "@")[0]
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return CurrentUser{}, err
	}
	defer tx.Rollback(ctx)

	var existingUserID string
	err = tx.QueryRow(ctx, `
		SELECT user_id
		FROM user_emails
		WHERE email = $1
	`, normalizedEmail).Scan(&existingUserID)
	if err == nil {
		var existingHasPassword bool
		err = tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM user_password_credentials
				WHERE user_id = $1
			)
		`, existingUserID).Scan(&existingHasPassword)
		if err != nil {
			return CurrentUser{}, err
		}
		if existingHasPassword {
			return CurrentUser{}, ErrEmailAlreadyRegistered
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO user_password_credentials (user_id, password_hash)
			VALUES ($1, $2)
		`, existingUserID, passwordHash)
		if err != nil {
			return CurrentUser{}, err
		}

		if strings.TrimSpace(displayName) != "" {
			_, err = tx.Exec(ctx, `
				UPDATE app_users
				SET display_name = $2,
					updated_at = NOW(),
					last_login_at = NOW()
				WHERE id = $1
			`, existingUserID, displayName)
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE app_users
				SET updated_at = NOW(),
					last_login_at = NOW()
				WHERE id = $1
			`, existingUserID)
		}
		if err != nil {
			return CurrentUser{}, err
		}

		if err := p.insertAuditEventTx(ctx, tx, existingUserID, existingUserID, "auth.password.enable", map[string]any{
			"email": normalizedEmail,
		}); err != nil {
			return CurrentUser{}, err
		}

		if err := tx.Commit(ctx); err != nil {
			return CurrentUser{}, err
		}

		return p.GetCurrentUserByID(ctx, existingUserID)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return CurrentUser{}, err
	}

	userID, err := generateAppUserID()
	if err != nil {
		return CurrentUser{}, err
	}

	encodedRoles, err := encodeStringSlice([]string{"user"})
	if err != nil {
		return CurrentUser{}, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO app_users (id, display_name, avatar_url, roles, last_login_at)
		VALUES ($1, $2, '', $3::jsonb, NOW())
	`, userID, displayName, encodedRoles)
	if err != nil {
		return CurrentUser{}, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO user_emails (email, user_id, is_primary, is_verified)
		VALUES ($1, $2, TRUE, FALSE)
	`, normalizedEmail, userID)
	if err != nil {
		return CurrentUser{}, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO user_password_credentials (user_id, password_hash)
		VALUES ($1, $2)
	`, userID, passwordHash)
	if err != nil {
		return CurrentUser{}, err
	}

	if err := p.insertAuditEventTx(ctx, tx, userID, userID, "auth.password.register", map[string]any{
		"email": normalizedEmail,
	}); err != nil {
		return CurrentUser{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return CurrentUser{}, err
	}

	return p.GetCurrentUserByID(ctx, userID)
}

func (p *PostgresStore) AuthenticatePasswordUser(ctx context.Context, email string, password string) (CurrentUser, error) {
	normalizedEmail := normalizeEmail(email)
	if normalizedEmail == "" {
		return CurrentUser{}, ErrInvalidCredentials
	}

	var (
		userID       string
		passwordHash string
	)
	err := p.pool.QueryRow(ctx, `
		SELECT ue.user_id, upc.password_hash
		FROM user_emails ue
		INNER JOIN user_password_credentials upc ON upc.user_id = ue.user_id
		WHERE ue.email = $1
	`, normalizedEmail).Scan(&userID, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return CurrentUser{}, ErrInvalidCredentials
	}
	if err != nil {
		return CurrentUser{}, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return CurrentUser{}, ErrInvalidCredentials
	}

	_, err = p.pool.Exec(ctx, `
		UPDATE app_users
		SET updated_at = NOW(),
			last_login_at = NOW()
		WHERE id = $1
	`, userID)
	if err != nil {
		return CurrentUser{}, err
	}

	if err := p.InsertAppAuditEvent(ctx, userID, userID, "auth.password.login", map[string]any{
		"email": normalizedEmail,
	}); err != nil {
		return CurrentUser{}, err
	}

	return p.GetCurrentUserByID(ctx, userID)
}

func (p *PostgresStore) CreatePasswordResetToken(ctx context.Context, email string, ttl time.Duration) (string, bool, error) {
	normalizedEmail := normalizeEmail(email)
	if normalizedEmail == "" {
		return "", false, ErrEmailNotLinked
	}

	var userID string
	err := p.pool.QueryRow(ctx, `
		SELECT ue.user_id
		FROM user_emails ue
		INNER JOIN user_password_credentials upc ON upc.user_id = ue.user_id
		WHERE ue.email = $1
	`, normalizedEmail).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	rawToken, err := generateEmailToken()
	if err != nil {
		return "", false, err
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO password_reset_tokens (token_hash, user_id, email, expires_at)
		VALUES ($1, $2, $3, $4)
	`, hashEmailToken(rawToken), userID, normalizedEmail, time.Now().Add(ttl))
	if err != nil {
		return "", false, err
	}

	if err := p.InsertAppAuditEvent(ctx, userID, userID, "auth.password.reset.request", map[string]any{
		"email": normalizedEmail,
	}); err != nil {
		return "", false, err
	}

	return rawToken, true, nil
}

func (p *PostgresStore) ResetPasswordWithToken(ctx context.Context, rawToken string, newPassword string) (CurrentUser, error) {
	if err := validatePassword(newPassword); err != nil {
		return CurrentUser{}, err
	}

	passwordHash, err := hashPassword(newPassword)
	if err != nil {
		return CurrentUser{}, err
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return CurrentUser{}, err
	}
	defer tx.Rollback(ctx)

	var userID string
	err = tx.QueryRow(ctx, `
		SELECT user_id
		FROM password_reset_tokens
		WHERE token_hash = $1
			AND consumed_at IS NULL
			AND expires_at > NOW()
	`, hashEmailToken(rawToken)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CurrentUser{}, ErrPasswordResetTokenInvalid
	}
	if err != nil {
		return CurrentUser{}, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE user_password_credentials
		SET password_hash = $2,
			password_updated_at = NOW(),
			updated_at = NOW()
		WHERE user_id = $1
	`, userID, passwordHash)
	if err != nil {
		return CurrentUser{}, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE password_reset_tokens
		SET consumed_at = NOW()
		WHERE token_hash = $1
	`, hashEmailToken(rawToken))
	if err != nil {
		return CurrentUser{}, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE app_users
		SET updated_at = NOW(),
			last_login_at = NOW()
		WHERE id = $1
	`, userID)
	if err != nil {
		return CurrentUser{}, err
	}

	if err := p.insertAuditEventTx(ctx, tx, userID, userID, "auth.password.reset.consume", map[string]any{}); err != nil {
		return CurrentUser{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return CurrentUser{}, err
	}

	return p.GetCurrentUserByID(ctx, userID)
}

func (p *PostgresStore) CreateEmailVerificationToken(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	var (
		email      string
		isVerified bool
	)
	err := p.pool.QueryRow(ctx, `
		SELECT email, is_verified
		FROM user_emails
		WHERE user_id = $1
		ORDER BY is_primary DESC, updated_at DESC
		LIMIT 1
	`, userID).Scan(&email, &isVerified)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrEmailNotLinked
	}
	if err != nil {
		return "", err
	}
	if isVerified {
		return "", ErrEmailAlreadyVerified
	}

	rawToken, err := generateEmailToken()
	if err != nil {
		return "", err
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO email_verification_tokens (token_hash, user_id, email, expires_at)
		VALUES ($1, $2, $3, $4)
	`, hashEmailToken(rawToken), userID, email, time.Now().Add(ttl))
	if err != nil {
		return "", err
	}

	if err := p.InsertAppAuditEvent(ctx, userID, userID, "auth.email.verify.request", map[string]any{
		"email": email,
	}); err != nil {
		return "", err
	}

	return rawToken, nil
}

func (p *PostgresStore) VerifyEmailWithToken(ctx context.Context, rawToken string) (CurrentUser, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return CurrentUser{}, err
	}
	defer tx.Rollback(ctx)

	var (
		userID string
		email  string
	)
	err = tx.QueryRow(ctx, `
		SELECT user_id, email
		FROM email_verification_tokens
		WHERE token_hash = $1
			AND consumed_at IS NULL
			AND expires_at > NOW()
	`, hashEmailToken(rawToken)).Scan(&userID, &email)
	if errors.Is(err, pgx.ErrNoRows) {
		return CurrentUser{}, ErrEmailVerificationInvalid
	}
	if err != nil {
		return CurrentUser{}, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE user_emails
		SET is_verified = TRUE,
			updated_at = NOW()
		WHERE user_id = $1 AND email = $2
	`, userID, email)
	if err != nil {
		return CurrentUser{}, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE email_verification_tokens
		SET consumed_at = NOW()
		WHERE token_hash = $1
	`, hashEmailToken(rawToken))
	if err != nil {
		return CurrentUser{}, err
	}

	if err := p.insertAuditEventTx(ctx, tx, userID, userID, "auth.email.verify.consume", map[string]any{
		"email": email,
	}); err != nil {
		return CurrentUser{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return CurrentUser{}, err
	}

	return p.GetCurrentUserByID(ctx, userID)
}
