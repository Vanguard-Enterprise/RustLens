package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type UserBan struct {
	DiscordID  string
	Categories []string
	// Suspended indicates the linked app_user (matched by discord_id) is
	// currently suspended on the website. The bot treats this as a blanket
	// ban across every category.
	Suspended bool
	// Deleted indicates the linked app_user has been hard-deleted on the
	// website. Hard delete removes the app_users row entirely, so this is
	// only set when the row used to exist but no longer does — which is
	// indistinguishable from "never had an account" here. Reserved for
	// future use; current code paths leave it false.
	Deleted bool
}

func (p *PostgresStore) GetUserBan(ctx context.Context, discordID string) (UserBan, error) {
	var categories []string
	err := p.pool.QueryRow(ctx, `
		SELECT categories
		FROM user_bans
		WHERE discord_id = $1
	`, discordID).Scan(&categories)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return UserBan{DiscordID: discordID, Categories: []string{}}, err
	}
	if categories == nil {
		categories = []string{}
	}

	var suspended bool
	suspendedErr := p.pool.QueryRow(ctx, `
		SELECT (suspended_at IS NOT NULL)
		FROM app_users
		WHERE discord_id = $1
	`, discordID).Scan(&suspended)
	if suspendedErr != nil && !errors.Is(suspendedErr, pgx.ErrNoRows) {
		return UserBan{DiscordID: discordID, Categories: categories}, suspendedErr
	}

	return UserBan{DiscordID: discordID, Categories: categories, Suspended: suspended}, nil
}

func (p *PostgresStore) UpsertUserBan(ctx context.Context, discordID string, categories []string, updatedBy string) (UserBan, error) {
	if len(categories) == 0 {
		if _, err := p.pool.Exec(ctx, `
			DELETE FROM user_bans
			WHERE discord_id = $1
		`, discordID); err != nil {
			return UserBan{}, err
		}
		return UserBan{DiscordID: discordID, Categories: []string{}}, nil
	}

	_, err := p.pool.Exec(ctx, `
		INSERT INTO user_bans (discord_id, categories, updated_by, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (discord_id) DO UPDATE
		SET categories = EXCLUDED.categories,
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()
	`, discordID, categories, updatedBy)
	if err != nil {
		return UserBan{}, err
	}

	return UserBan{DiscordID: discordID, Categories: categories}, nil
}
