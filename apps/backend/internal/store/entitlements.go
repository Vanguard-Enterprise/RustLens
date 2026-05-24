package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const EntitlementPRO = "pro"

func (p *PostgresStore) GrantUserEntitlement(ctx context.Context, userID, key, grantedBy string) error {
	var gb any
	if grantedBy != "" {
		gb = grantedBy
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO user_entitlements (user_id, key, granted_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, key) DO UPDATE
		SET granted_by = EXCLUDED.granted_by,
		    granted_at = NOW(),
		    expires_at = NULL
	`, userID, key, gb)
	return err
}

func (p *PostgresStore) RevokeUserEntitlement(ctx context.Context, userID, key string) error {
	_, err := p.pool.Exec(ctx, `
		DELETE FROM user_entitlements WHERE user_id = $1 AND key = $2
	`, userID, key)
	return err
}

func (p *PostgresStore) EntitlementsForUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT key
		FROM user_entitlements
		WHERE user_id = $1
		  AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY key
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func (p *PostgresStore) DiscordUserHasEntitlement(ctx context.Context, discordID, key string) (bool, error) {
	var exists bool
	err := p.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM app_users u
			JOIN user_entitlements ue ON ue.user_id = u.id
			WHERE u.discord_id = $1
			  AND ue.key = $2
			  AND (ue.expires_at IS NULL OR ue.expires_at > NOW())
		)
	`, discordID, key).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (p *PostgresStore) GetUserProGrantedAt(ctx context.Context, userID string) (*time.Time, error) {
	var t time.Time
	err := p.pool.QueryRow(ctx, `
		SELECT granted_at
		FROM user_entitlements
		WHERE user_id = $1
		  AND key = 'pro'
		  AND (expires_at IS NULL OR expires_at > NOW())
	`, userID).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
