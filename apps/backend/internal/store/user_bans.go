package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type UserBan struct {
	DiscordID  string
	Categories []string
}

func (p *PostgresStore) GetUserBan(ctx context.Context, discordID string) (UserBan, error) {
	var categories []string
	err := p.pool.QueryRow(ctx, `
		SELECT categories
		FROM user_bans
		WHERE discord_id = $1
	`, discordID).Scan(&categories)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserBan{DiscordID: discordID, Categories: []string{}}, nil
	}
	if err != nil {
		return UserBan{DiscordID: discordID, Categories: []string{}}, err
	}
	if categories == nil {
		categories = []string{}
	}
	return UserBan{DiscordID: discordID, Categories: categories}, nil
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
