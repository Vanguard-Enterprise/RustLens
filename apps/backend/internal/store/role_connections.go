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
