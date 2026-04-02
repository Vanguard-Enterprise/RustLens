package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrSteamAccountNotFound = errors.New("steam account not found")
var ErrLinkAlreadyExists = errors.New("steam discord link already exists")
var ErrGuildLinkNotFound = errors.New("guild steam discord link not found")

type SteamAccount struct {
	SteamID     string
	BmID        *string
	BmName      string
	AvatarImage string
	CreatedAt   time.Time
}

type SteamDiscordLink struct {
	SteamID   string
	DiscordID string
	Type      string
	Source    string
	CreatedAt time.Time
}

func scanSteamAccount(scanner rowScanner) (SteamAccount, error) {
	var record SteamAccount
	err := scanner.Scan(&record.SteamID, &record.BmID, &record.BmName, &record.AvatarImage, &record.CreatedAt)
	if err != nil {
		return SteamAccount{}, err
	}
	return record, nil
}

func scanSteamDiscordLink(scanner rowScanner) (SteamDiscordLink, error) {
	var record SteamDiscordLink
	err := scanner.Scan(&record.SteamID, &record.DiscordID, &record.Type, &record.Source, &record.CreatedAt)
	if err != nil {
		return SteamDiscordLink{}, err
	}
	return record, nil
}

func (p *PostgresStore) UpsertSteamAccount(ctx context.Context, steamID string, bmID *string) (SteamAccount, error) {
	var record SteamAccount
	err := p.pool.QueryRow(ctx, `
		INSERT INTO steam_accounts (steam_id, bm_id)
		VALUES ($1, $2)
		ON CONFLICT (steam_id) DO UPDATE
		SET bm_id = COALESCE(EXCLUDED.bm_id, steam_accounts.bm_id)
		RETURNING steam_id, bm_id, bm_name, avatar_image, created_at
	`, steamID, bmID).Scan(&record.SteamID, &record.BmID, &record.BmName, &record.AvatarImage, &record.CreatedAt)
	if err != nil {
		return SteamAccount{}, err
	}
	return record, nil
}

func (p *PostgresStore) GetSteamAccountBySteamID(ctx context.Context, steamID string) (SteamAccount, error) {
	record, err := scanSteamAccount(p.pool.QueryRow(ctx, `
		SELECT steam_id, bm_id, bm_name, avatar_image, created_at
		FROM steam_accounts
		WHERE steam_id = $1
	`, steamID))
	if errors.Is(err, pgx.ErrNoRows) {
		return SteamAccount{}, ErrSteamAccountNotFound
	}
	return record, err
}

func (p *PostgresStore) GetSteamAccountByBmID(ctx context.Context, bmID string) (SteamAccount, error) {
	record, err := scanSteamAccount(p.pool.QueryRow(ctx, `
		SELECT steam_id, bm_id, bm_name, avatar_image, created_at
		FROM steam_accounts
		WHERE bm_id = $1
	`, bmID))
	if errors.Is(err, pgx.ErrNoRows) {
		return SteamAccount{}, ErrSteamAccountNotFound
	}
	return record, err
}

func (p *PostgresStore) CreateSteamDiscordLink(ctx context.Context, link SteamDiscordLink) (SteamDiscordLink, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return SteamDiscordLink{}, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO steam_accounts (steam_id)
		VALUES ($1)
		ON CONFLICT (steam_id) DO NOTHING
	`, link.SteamID)
	if err != nil {
		return SteamDiscordLink{}, err
	}

	var created SteamDiscordLink
	err = tx.QueryRow(ctx, `
		INSERT INTO steam_discord_links (steam_id, discord_id, type, source)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (steam_id, discord_id, type) DO NOTHING
		RETURNING steam_id, discord_id, type, source, created_at
	`, link.SteamID, link.DiscordID, link.Type, link.Source).Scan(
		&created.SteamID, &created.DiscordID, &created.Type, &created.Source, &created.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SteamDiscordLink{}, ErrLinkAlreadyExists
	}
	if err != nil {
		return SteamDiscordLink{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return SteamDiscordLink{}, err
	}

	return created, nil
}

func (p *PostgresStore) ListLinksBySteamID(ctx context.Context, steamID string) ([]SteamDiscordLink, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT steam_id, discord_id, type, source, created_at
		FROM steam_discord_links
		WHERE steam_id = $1
		ORDER BY created_at ASC
	`, steamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]SteamDiscordLink, 0)
	for rows.Next() {
		record, err := scanSteamDiscordLink(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (p *PostgresStore) ListLinksByDiscordID(ctx context.Context, discordID string) ([]SteamDiscordLink, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT steam_id, discord_id, type, source, created_at
		FROM steam_discord_links
		WHERE discord_id = $1
		ORDER BY created_at ASC
	`, discordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]SteamDiscordLink, 0)
	for rows.Next() {
		record, err := scanSteamDiscordLink(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (p *PostgresStore) DeleteSoftLink(ctx context.Context, steamID string, discordID string) error {
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM steam_discord_links
		WHERE steam_id = $1 AND discord_id = $2 AND type = 'soft'
	`, steamID, discordID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSteamAccountNotFound
	}
	return nil
}

type BulkHardLink struct {
	SteamID   string
	BmID      *string
	DiscordID string
	Source    string
}

func (p *PostgresStore) BulkUpsertHardLinks(ctx context.Context, links []BulkHardLink) error {
	if len(links) == 0 {
		return nil
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, link := range links {
		_, err := tx.Exec(ctx, `
			INSERT INTO steam_accounts (steam_id, bm_id)
			VALUES ($1, $2)
			ON CONFLICT (steam_id) DO UPDATE
			SET bm_id = COALESCE(EXCLUDED.bm_id, steam_accounts.bm_id)
		`, link.SteamID, link.BmID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO steam_discord_links (steam_id, discord_id, type, source)
			VALUES ($1, $2, 'hard', $3)
			ON CONFLICT (steam_id, discord_id, type) DO NOTHING
		`, link.SteamID, link.DiscordID, link.Source)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

type GuildSteamDiscordLink struct {
	GuildID   string
	DiscordID string
	SteamID   string
	CreatedAt time.Time
}

type UserGuildLink struct {
	DiscordID string
	GuildID   string
	CreatedAt time.Time
}

func scanGuildSteamDiscordLink(scanner rowScanner) (GuildSteamDiscordLink, error) {
	var record GuildSteamDiscordLink
	err := scanner.Scan(&record.GuildID, &record.DiscordID, &record.SteamID, &record.CreatedAt)
	if err != nil {
		return GuildSteamDiscordLink{}, err
	}
	return record, nil
}

func scanUserGuildLink(scanner rowScanner) (UserGuildLink, error) {
	var record UserGuildLink
	err := scanner.Scan(&record.DiscordID, &record.GuildID, &record.CreatedAt)
	if err != nil {
		return UserGuildLink{}, err
	}
	return record, nil
}

func (p *PostgresStore) CreateGuildSteamDiscordLink(ctx context.Context, guildID, discordID, steamID string) (GuildSteamDiscordLink, error) {
	var record GuildSteamDiscordLink
	err := p.pool.QueryRow(ctx, `
		INSERT INTO guild_steam_discord_links (guild_id, discord_id, steam_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (guild_id, discord_id, steam_id) DO NOTHING
		RETURNING guild_id, discord_id, steam_id, created_at
	`, guildID, discordID, steamID).Scan(&record.GuildID, &record.DiscordID, &record.SteamID, &record.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return GuildSteamDiscordLink{}, ErrLinkAlreadyExists
	}
	if err != nil {
		return GuildSteamDiscordLink{}, err
	}
	return record, nil
}

func (p *PostgresStore) ListGuildLinksByGuildAndDiscord(ctx context.Context, guildID, discordID string) ([]GuildSteamDiscordLink, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT guild_id, discord_id, steam_id, created_at
		FROM guild_steam_discord_links
		WHERE guild_id = $1 AND discord_id = $2
		ORDER BY created_at ASC
	`, guildID, discordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]GuildSteamDiscordLink, 0)
	for rows.Next() {
		record, err := scanGuildSteamDiscordLink(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (p *PostgresStore) ListGuildLinksByGuildAndSteam(ctx context.Context, guildID, steamID string) ([]GuildSteamDiscordLink, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT guild_id, discord_id, steam_id, created_at
		FROM guild_steam_discord_links
		WHERE guild_id = $1 AND steam_id = $2
		ORDER BY created_at ASC
	`, guildID, steamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]GuildSteamDiscordLink, 0)
	for rows.Next() {
		record, err := scanGuildSteamDiscordLink(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (p *PostgresStore) ListGuildLinks(ctx context.Context, guildID string) ([]GuildSteamDiscordLink, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT guild_id, discord_id, steam_id, created_at
		FROM guild_steam_discord_links
		WHERE guild_id = $1
		ORDER BY created_at ASC
	`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]GuildSteamDiscordLink, 0)
	for rows.Next() {
		record, err := scanGuildSteamDiscordLink(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (p *PostgresStore) CreateUserGuildLink(ctx context.Context, discordID, guildID string) (UserGuildLink, error) {
	var record UserGuildLink
	err := p.pool.QueryRow(ctx, `
		INSERT INTO user_guilds (discord_id, guild_id)
		VALUES ($1, $2)
		ON CONFLICT (discord_id, guild_id) DO UPDATE
		SET guild_id = EXCLUDED.guild_id
		RETURNING discord_id, guild_id, created_at
	`, discordID, guildID).Scan(&record.DiscordID, &record.GuildID, &record.CreatedAt)
	if err != nil {
		return UserGuildLink{}, err
	}
	return record, nil
}

func (p *PostgresStore) ListUserGuildLinksByDiscordID(ctx context.Context, discordID string) ([]UserGuildLink, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT discord_id, guild_id, created_at
		FROM user_guilds
		WHERE discord_id = $1
		ORDER BY created_at ASC, guild_id ASC
	`, discordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]UserGuildLink, 0)
	for rows.Next() {
		record, err := scanUserGuildLink(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (p *PostgresStore) DeleteGuildSteamDiscordLink(ctx context.Context, guildID, discordID, steamID string) error {
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM guild_steam_discord_links
		WHERE guild_id = $1 AND discord_id = $2 AND steam_id = $3
	`, guildID, discordID, steamID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrGuildLinkNotFound
	}
	return nil
}
