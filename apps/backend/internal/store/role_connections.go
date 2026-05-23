package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// DiscordRoleConnection mirrors the discord_role_connections table row.
// Token fields are AES-GCM ciphertext + nonce per the proxycrypto helper;
// see internal/roleconnection for the encryption layer.
type DiscordRoleConnection struct {
	UserID            string
	DiscordID         string
	AccessTokenCT     []byte
	AccessTokenNonce  []byte
	RefreshTokenCT    []byte
	RefreshTokenNonce []byte
	AccessExpiresAt   time.Time
	Scopes            string
	LinkedAt          time.Time
	LastPushedAt      *time.Time
	LastPushedHash    *string
	LastPushError     *string
	RevokedAt         *time.Time
}

// UpsertDiscordRoleConnection inserts or replaces the row keyed by user_id.
// It always clears revoked_at on insert/update so a successful re-link from
// the user wipes the revoked state from a prior failed token-refresh.
func (p *PostgresStore) UpsertDiscordRoleConnection(ctx context.Context, in DiscordRoleConnection) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO discord_role_connections (
			user_id, discord_id,
			access_token_ct, access_token_nonce,
			refresh_token_ct, refresh_token_nonce,
			access_expires_at, scopes, linked_at, revoked_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now(), NULL)
		ON CONFLICT (user_id) DO UPDATE SET
			discord_id          = EXCLUDED.discord_id,
			access_token_ct     = EXCLUDED.access_token_ct,
			access_token_nonce  = EXCLUDED.access_token_nonce,
			refresh_token_ct    = EXCLUDED.refresh_token_ct,
			refresh_token_nonce = EXCLUDED.refresh_token_nonce,
			access_expires_at   = EXCLUDED.access_expires_at,
			scopes              = EXCLUDED.scopes,
			revoked_at          = NULL
	`,
		in.UserID, in.DiscordID,
		in.AccessTokenCT, in.AccessTokenNonce,
		in.RefreshTokenCT, in.RefreshTokenNonce,
		in.AccessExpiresAt, in.Scopes,
	)
	return err
}

// GetDiscordRoleConnection returns the row for user_id, or (nil, nil) if absent.
func (p *PostgresStore) GetDiscordRoleConnection(ctx context.Context, userID string) (*DiscordRoleConnection, error) {
	var row DiscordRoleConnection
	err := p.pool.QueryRow(ctx, `
		SELECT user_id, discord_id,
			access_token_ct, access_token_nonce,
			refresh_token_ct, refresh_token_nonce,
			access_expires_at, scopes, linked_at,
			last_pushed_at, last_pushed_hash, last_push_error, revoked_at
		FROM discord_role_connections
		WHERE user_id = $1
	`, userID).Scan(
		&row.UserID, &row.DiscordID,
		&row.AccessTokenCT, &row.AccessTokenNonce,
		&row.RefreshTokenCT, &row.RefreshTokenNonce,
		&row.AccessExpiresAt, &row.Scopes, &row.LinkedAt,
		&row.LastPushedAt, &row.LastPushedHash, &row.LastPushError, &row.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteDiscordRoleConnection removes the row for user_id. No-op if absent.
func (p *PostgresStore) DeleteDiscordRoleConnection(ctx context.Context, userID string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM discord_role_connections WHERE user_id = $1`, userID)
	return err
}

// MarkDiscordRoleConnectionRevoked flips revoked_at to now() and stores the
// last error (typically "invalid_grant" from a failed refresh). Used when the
// refresh token is permanently invalid; the row is kept so the dashboard can
// show "Reconnect" and the user can re-OAuth without losing history.
func (p *PostgresStore) MarkDiscordRoleConnectionRevoked(ctx context.Context, userID, errMsg string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE discord_role_connections
		SET revoked_at = now(), last_push_error = $2
		WHERE user_id = $1
	`, userID, errMsg)
	return err
}

// RecordDiscordRoleConnectionPushed updates last_pushed_at / last_pushed_hash
// after a successful push, and clears last_push_error.
func (p *PostgresStore) RecordDiscordRoleConnectionPushed(ctx context.Context, userID, payloadHash string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE discord_role_connections
		SET last_pushed_at = now(),
		    last_pushed_hash = $2,
		    last_push_error = NULL
		WHERE user_id = $1
	`, userID, payloadHash)
	return err
}

// ListActiveDiscordRoleConnections returns all rows with revoked_at IS NULL.
// Used by the periodic reconciler. Not paginated — the expected cardinality
// is hundreds, not millions; revisit if that changes.
func (p *PostgresStore) ListActiveDiscordRoleConnections(ctx context.Context) ([]DiscordRoleConnection, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT user_id, discord_id,
			access_token_ct, access_token_nonce,
			refresh_token_ct, refresh_token_nonce,
			access_expires_at, scopes, linked_at,
			last_pushed_at, last_pushed_hash, last_push_error, revoked_at
		FROM discord_role_connections
		WHERE revoked_at IS NULL
		ORDER BY linked_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiscordRoleConnection{}
	for rows.Next() {
		var r DiscordRoleConnection
		if err := rows.Scan(
			&r.UserID, &r.DiscordID,
			&r.AccessTokenCT, &r.AccessTokenNonce,
			&r.RefreshTokenCT, &r.RefreshTokenNonce,
			&r.AccessExpiresAt, &r.Scopes, &r.LinkedAt,
			&r.LastPushedAt, &r.LastPushedHash, &r.LastPushError, &r.RevokedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordDiscordRoleConnectionPushError stores a soft failure (Discord 5xx,
// timeout, etc.) without revoking the row. The reconciler will retry later.
func (p *PostgresStore) RecordDiscordRoleConnectionPushError(ctx context.Context, userID, errMsg string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE discord_role_connections
		SET last_push_error = $2
		WHERE user_id = $1
	`, userID, errMsg)
	return err
}

// GetUserProGrantedAt returns the granted_at timestamp for the PRO admin role
// on a given user, or (nil, nil) if the user does not hold the role. The role
// key is hardcoded to "pro" because that is the only role this lookup serves.
func (p *PostgresStore) GetUserProGrantedAt(ctx context.Context, userID string) (*time.Time, error) {
	var t time.Time
	err := p.pool.QueryRow(ctx, `
		SELECT ur.granted_at
		FROM admin_user_roles ur
		JOIN admin_roles ar ON ar.id = ur.role_id
		WHERE ur.user_id = $1 AND ar.key = 'pro'
	`, userID).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetAppUserDisplayName returns app_users.display_name, used as the platform
// username when pushing metadata. Falls back to discord_username if display_name
// is empty.
func (p *PostgresStore) GetAppUserDisplayName(ctx context.Context, userID string) (string, error) {
	var displayName, discordUsername *string
	err := p.pool.QueryRow(ctx, `
		SELECT display_name, discord_username FROM app_users WHERE id = $1
	`, userID).Scan(&displayName, &discordUsername)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if displayName != nil && *displayName != "" {
		return *displayName, nil
	}
	if discordUsername != nil {
		return *discordUsername, nil
	}
	return "", nil
}
