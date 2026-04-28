package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type NameGap struct {
	FromUnix int64 `json:"fromUnix"`
	ToUnix   int64 `json:"toUnix"`
}

type AnalyticsGame struct {
	AppID int    `json:"appId"`
	Name  string `json:"name"`
	Hours int64  `json:"hours"`
}

type AnalyticsServer struct {
	BmID  string `json:"bmId"`
	Name  string `json:"name"`
	Hours int64  `json:"hours"`
}

var ErrSteamAccountNotFound = errors.New("steam account not found")
var ErrLinkAlreadyExists = errors.New("steam discord link already exists")
var ErrGuildLinkNotFound = errors.New("guild steam discord link not found")

type SteamAccount struct {
	SteamID     string
	BmID        *string
	BmName      string
	AvatarImage string
	FirstSeenAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type SteamAccountCache struct {
	SteamID          string
	AvatarImage      string
	RustHours        *int64
	TotalGames       *int
	TopGames         []AnalyticsGame
	GameBanned       bool
	GameBannedAt     *time.Time
	GameBanCheckedAt *time.Time
	UpdatedAt        time.Time
}

// BmProfileCache holds BattleMetrics-derived profile data keyed by bm_id.
// This data does not require a Steam linkage to exist.
type BmProfileCache struct {
	BmID            string
	BmName          string
	FirstSeenAt     *time.Time
	VitalityHours   int64
	AtlasHours      int64
	VitalHours      int64
	ModdedHours     int64
	OfficialHours   int64
	BuildHours      int64
	BmHours         int64
	BmServersPlayed int
	NameGaps        []NameGap
	TopServers      []AnalyticsServer
	UpdatedAt       time.Time
}

type SteamGameBanCache struct {
	SteamID          string
	GameBanned       bool
	GameBannedAt     *time.Time
	GameBanCheckedAt *time.Time
}

type SteamDiscordLink struct {
	SteamID   string
	DiscordID string
	Type      string
	Source    string
	CreatedAt time.Time
}

type GuildRecentGameBan struct {
	GuildID      string
	DiscordID    string
	SteamID      string
	GameBannedAt time.Time
}

func scanSteamAccount(scanner rowScanner) (SteamAccount, error) {
	var record SteamAccount
	err := scanner.Scan(&record.SteamID, &record.BmID, &record.BmName, &record.AvatarImage, &record.FirstSeenAt, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		return SteamAccount{}, err
	}
	return record, nil
}

func scanSteamAccountCache(scanner rowScanner) (SteamAccountCache, error) {
	var record SteamAccountCache
	var topGamesJSON []byte
	err := scanner.Scan(
		&record.SteamID,
		&record.AvatarImage,
		&record.RustHours,
		&record.TotalGames,
		&topGamesJSON,
		&record.GameBanned,
		&record.GameBannedAt,
		&record.GameBanCheckedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return SteamAccountCache{}, err
	}
	if len(topGamesJSON) > 0 {
		if err := json.Unmarshal(topGamesJSON, &record.TopGames); err != nil {
			return SteamAccountCache{}, err
		}
	}
	if record.TopGames == nil {
		record.TopGames = []AnalyticsGame{}
	}
	return record, nil
}

func scanBmProfileCache(scanner rowScanner) (BmProfileCache, error) {
	var record BmProfileCache
	var nameGapsJSON []byte
	var topServersJSON []byte
	err := scanner.Scan(
		&record.BmID,
		&record.BmName,
		&record.FirstSeenAt,
		&record.VitalityHours,
		&record.AtlasHours,
		&record.VitalHours,
		&record.ModdedHours,
		&record.OfficialHours,
		&record.BuildHours,
		&record.BmHours,
		&record.BmServersPlayed,
		&nameGapsJSON,
		&topServersJSON,
		&record.UpdatedAt,
	)
	if err != nil {
		return BmProfileCache{}, err
	}
	if len(nameGapsJSON) > 0 {
		if err := json.Unmarshal(nameGapsJSON, &record.NameGaps); err != nil {
			return BmProfileCache{}, err
		}
	}
	if record.NameGaps == nil {
		record.NameGaps = []NameGap{}
	}
	if len(topServersJSON) > 0 {
		if err := json.Unmarshal(topServersJSON, &record.TopServers); err != nil {
			return BmProfileCache{}, err
		}
	}
	if record.TopServers == nil {
		record.TopServers = []AnalyticsServer{}
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
		RETURNING steam_id, bm_id, created_at
	`, steamID, bmID).Scan(&record.SteamID, &record.BmID, &record.CreatedAt)
	if err != nil {
		return SteamAccount{}, err
	}
	record.UpdatedAt = record.CreatedAt
	return record, nil
}

func (p *PostgresStore) GetSteamAccountBySteamID(ctx context.Context, steamID string) (SteamAccount, error) {
	record, err := scanSteamAccount(p.pool.QueryRow(ctx, `
		SELECT
			sa.steam_id,
			sa.bm_id,
			COALESCE(bpc.bm_name, '') AS bm_name,
			COALESCE(sac.avatar_image, '') AS avatar_image,
			bpc.first_seen_at,
			sa.created_at,
			COALESCE(sac.updated_at, sa.created_at) AS updated_at
		FROM steam_accounts sa
		LEFT JOIN steam_account_cache sac ON sac.steam_id = sa.steam_id
		LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
		WHERE sa.steam_id = $1
	`, steamID))
	if errors.Is(err, pgx.ErrNoRows) {
		return SteamAccount{}, ErrSteamAccountNotFound
	}
	return record, err
}

func (p *PostgresStore) GetSteamAccountByBmID(ctx context.Context, bmID string) (SteamAccount, error) {
	record, err := scanSteamAccount(p.pool.QueryRow(ctx, `
		SELECT
			sa.steam_id,
			sa.bm_id,
			COALESCE(bpc.bm_name, '') AS bm_name,
			COALESCE(sac.avatar_image, '') AS avatar_image,
			bpc.first_seen_at,
			sa.created_at,
			COALESCE(sac.updated_at, sa.created_at) AS updated_at
		FROM steam_accounts sa
		LEFT JOIN steam_account_cache sac ON sac.steam_id = sa.steam_id
		LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
		WHERE sa.bm_id = $1
	`, bmID))
	if errors.Is(err, pgx.ErrNoRows) {
		return SteamAccount{}, ErrSteamAccountNotFound
	}
	return record, err
}

func (p *PostgresStore) CountSteamAccounts(ctx context.Context) (int64, error) {
	var count int64
	err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM steam_accounts`).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (p *PostgresStore) GetSteamAccountCache(ctx context.Context, steamID string) (SteamAccountCache, error) {
	record, err := scanSteamAccountCache(p.pool.QueryRow(ctx, `
		SELECT steam_id, avatar_image, rust_hours, total_games, top_games,
			game_banned, game_banned_at, game_ban_checked_at, updated_at
		FROM steam_account_cache
		WHERE steam_id = $1
	`, steamID))
	if errors.Is(err, pgx.ErrNoRows) {
		return SteamAccountCache{}, ErrSteamAccountNotFound
	}
	if err != nil {
		return SteamAccountCache{}, err
	}
	return record, nil
}

func (p *PostgresStore) UpsertSteamAccountCache(ctx context.Context, cache SteamAccountCache) (SteamAccountCache, error) {
	topGames := cache.TopGames
	if topGames == nil {
		topGames = []AnalyticsGame{}
	}
	topGamesJSON, err := json.Marshal(topGames)
	if err != nil {
		return SteamAccountCache{}, err
	}

	return scanSteamAccountCache(p.pool.QueryRow(ctx, `
		INSERT INTO steam_account_cache (
			steam_id, avatar_image, rust_hours, total_games, top_games, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (steam_id) DO UPDATE
		SET avatar_image = EXCLUDED.avatar_image,
			rust_hours = EXCLUDED.rust_hours,
			total_games = EXCLUDED.total_games,
			top_games = EXCLUDED.top_games,
			updated_at = NOW()
		RETURNING steam_id, avatar_image, rust_hours, total_games, top_games,
			game_banned, game_banned_at, game_ban_checked_at, updated_at
	`,
		cache.SteamID,
		cache.AvatarImage,
		cache.RustHours,
		cache.TotalGames,
		topGamesJSON,
	))
}

func (p *PostgresStore) GetBmProfileCache(ctx context.Context, bmID string) (BmProfileCache, error) {
	record, err := scanBmProfileCache(p.pool.QueryRow(ctx, `
		SELECT bm_id, bm_name, first_seen_at, vitality_hours, atlas_hours, vital_hours,
			modded_hours, official_hours, build_hours, bm_hours, bm_servers_played,
			name_gaps, top_servers, updated_at
		FROM bm_profile_cache
		WHERE bm_id = $1
	`, bmID))
	if errors.Is(err, pgx.ErrNoRows) {
		return BmProfileCache{}, ErrSteamAccountNotFound
	}
	if err != nil {
		return BmProfileCache{}, err
	}
	return record, nil
}

func (p *PostgresStore) UpsertBmProfileCache(ctx context.Context, cache BmProfileCache) (BmProfileCache, error) {
	gaps := cache.NameGaps
	if gaps == nil {
		gaps = []NameGap{}
	}
	nameGapsJSON, err := json.Marshal(gaps)
	if err != nil {
		return BmProfileCache{}, err
	}
	topServers := cache.TopServers
	if topServers == nil {
		topServers = []AnalyticsServer{}
	}
	topServersJSON, err := json.Marshal(topServers)
	if err != nil {
		return BmProfileCache{}, err
	}

	return scanBmProfileCache(p.pool.QueryRow(ctx, `
		INSERT INTO bm_profile_cache (
			bm_id, bm_name, first_seen_at, vitality_hours, atlas_hours, vital_hours,
			modded_hours, official_hours, build_hours, bm_hours, bm_servers_played,
			name_gaps, top_servers, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW())
		ON CONFLICT (bm_id) DO UPDATE
		SET bm_name = EXCLUDED.bm_name,
			first_seen_at = EXCLUDED.first_seen_at,
			vitality_hours = EXCLUDED.vitality_hours,
			atlas_hours = EXCLUDED.atlas_hours,
			vital_hours = EXCLUDED.vital_hours,
			modded_hours = EXCLUDED.modded_hours,
			official_hours = EXCLUDED.official_hours,
			build_hours = EXCLUDED.build_hours,
			bm_hours = EXCLUDED.bm_hours,
			bm_servers_played = EXCLUDED.bm_servers_played,
			name_gaps = EXCLUDED.name_gaps,
			top_servers = EXCLUDED.top_servers,
			updated_at = NOW()
		RETURNING bm_id, bm_name, first_seen_at, vitality_hours, atlas_hours, vital_hours,
			modded_hours, official_hours, build_hours, bm_hours, bm_servers_played,
			name_gaps, top_servers, updated_at
	`,
		cache.BmID,
		cache.BmName,
		cache.FirstSeenAt,
		cache.VitalityHours,
		cache.AtlasHours,
		cache.VitalHours,
		cache.ModdedHours,
		cache.OfficialHours,
		cache.BuildHours,
		cache.BmHours,
		cache.BmServersPlayed,
		nameGapsJSON,
		topServersJSON,
	))
}

func (p *PostgresStore) ListSteamGameBanCache(ctx context.Context, steamIDs []string) ([]SteamGameBanCache, error) {
	if len(steamIDs) == 0 {
		return []SteamGameBanCache{}, nil
	}

	rows, err := p.pool.Query(ctx, `
		SELECT steam_id, game_banned, game_banned_at, game_ban_checked_at
		FROM steam_account_cache
		WHERE steam_id = ANY($1)
	`, steamIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]SteamGameBanCache, 0, len(steamIDs))
	for rows.Next() {
		var record SteamGameBanCache
		if err := rows.Scan(&record.SteamID, &record.GameBanned, &record.GameBannedAt, &record.GameBanCheckedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

func (p *PostgresStore) UpsertSteamAccountGameBanCache(
	ctx context.Context,
	steamID string,
	gameBanned bool,
	gameBannedAt *time.Time,
	checkedAt time.Time,
) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO steam_account_cache (steam_id, game_banned, game_banned_at, game_ban_checked_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (steam_id) DO UPDATE
		SET game_banned = EXCLUDED.game_banned,
			game_banned_at = EXCLUDED.game_banned_at,
			game_ban_checked_at = EXCLUDED.game_ban_checked_at
	`, steamID, gameBanned, gameBannedAt, checkedAt)
	return err
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

type GuildNicknameTarget struct {
	GuildID   string
	DiscordID string
	SteamID   string
	BmID      *string
	BmName    *string
}

type UserGuildLink struct {
	DiscordID   string
	GuildID     string
	IsActive    bool
	FirstSeenAt time.Time
	LastSeenAt  time.Time
	LeftAt      *time.Time
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
	err := scanner.Scan(
		&record.DiscordID,
		&record.GuildID,
		&record.IsActive,
		&record.FirstSeenAt,
		&record.LastSeenAt,
		&record.LeftAt,
	)
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

func (p *PostgresStore) ListGuildNicknameTargets(ctx context.Context, guildID string) ([]GuildNicknameTarget, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT guild_id, discord_id, steam_id, bm_id, bm_name
		FROM (
			SELECT
				gsl.guild_id,
				gsl.discord_id,
				gsl.steam_id,
				sa.bm_id,
				NULLIF(bpc.bm_name, '') AS bm_name,
				ROW_NUMBER() OVER (
					PARTITION BY gsl.discord_id
					ORDER BY
						CASE
							WHEN sa.bm_id IS NOT NULL
								AND sa.bm_id <> ''
								AND bpc.bm_name IS NOT NULL
								AND bpc.bm_name <> ''
							THEN 0
							ELSE 1
						END ASC,
						gsl.created_at ASC,
						gsl.steam_id ASC
				) AS nickname_rank
			FROM guild_steam_discord_links gsl
			LEFT JOIN steam_accounts sa ON sa.steam_id = gsl.steam_id
			LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
			WHERE gsl.guild_id = $1
		) nickname_targets
		WHERE nickname_rank = 1
		ORDER BY discord_id ASC
	`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]GuildNicknameTarget, 0)
	for rows.Next() {
		var record GuildNicknameTarget
		if err := rows.Scan(&record.GuildID, &record.DiscordID, &record.SteamID, &record.BmID, &record.BmName); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (p *PostgresStore) CreateUserGuildLink(ctx context.Context, discordID, guildID string) (UserGuildLink, error) {
	var record UserGuildLink
	err := p.pool.QueryRow(ctx, `
		INSERT INTO user_guilds (discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at)
		VALUES ($1, $2, TRUE, NOW(), NOW(), NULL)
		ON CONFLICT (discord_id, guild_id) DO UPDATE
		SET is_active = TRUE,
			last_seen_at = NOW(),
			left_at = NULL
		RETURNING discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at
	`, discordID, guildID).Scan(
		&record.DiscordID,
		&record.GuildID,
		&record.IsActive,
		&record.FirstSeenAt,
		&record.LastSeenAt,
		&record.LeftAt,
	)
	if err != nil {
		return UserGuildLink{}, err
	}
	return record, nil
}

func (p *PostgresStore) MarkUserGuildLeft(ctx context.Context, discordID, guildID string) (UserGuildLink, error) {
	var record UserGuildLink
	err := p.pool.QueryRow(ctx, `
		INSERT INTO user_guilds (discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at)
		VALUES ($1, $2, FALSE, NOW(), NOW(), NOW())
		ON CONFLICT (discord_id, guild_id) DO UPDATE
		SET is_active = FALSE,
			last_seen_at = NOW(),
			left_at = NOW()
		RETURNING discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at
	`, discordID, guildID).Scan(
		&record.DiscordID,
		&record.GuildID,
		&record.IsActive,
		&record.FirstSeenAt,
		&record.LastSeenAt,
		&record.LeftAt,
	)
	if err != nil {
		return UserGuildLink{}, err
	}
	return record, nil
}

func (p *PostgresStore) ListUserGuildLinksByDiscordID(ctx context.Context, discordID string) ([]UserGuildLink, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at
		FROM user_guilds
		WHERE discord_id = $1
		ORDER BY guild_id ASC
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

// IsDiscordWebsiteAdmin reports whether the given Discord user has an
// app_users account with at least one admin role granted via admin_user_roles.
func (p *PostgresStore) IsDiscordWebsiteAdmin(ctx context.Context, discordID string) (bool, error) {
	var exists bool
	err := p.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM app_users u
			JOIN admin_user_roles aur ON aur.user_id = u.id
			WHERE u.discord_id = $1
		)
	`, discordID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
