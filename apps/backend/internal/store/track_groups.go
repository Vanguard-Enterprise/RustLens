package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrTrackGroupNotFound = errors.New("track group not found")
var ErrTrackGroupNameTaken = errors.New("track group name already used in guild")

type TrackGroup struct {
	ID          int64
	GuildID     string
	Name        string
	ServerBmID  string
	ChannelID   string
	MessageID   string
	SendUpdates bool
	OnlineCount int
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type TrackGroupMember struct {
	TrackGroupID int64
	SteamID      string
	BmID         string
	CreatedAt    time.Time
}

type TrackGroupOnlineRow struct {
	SteamID     string
	BmID        string
	BmName      string
	DiscordID   string
	AvatarImage string
	IsOnline    bool
	OnlineUnix  int64
}

func scanTrackGroup(scanner rowScanner) (TrackGroup, error) {
	var record TrackGroup
	err := scanner.Scan(
		&record.ID,
		&record.GuildID,
		&record.Name,
		&record.ServerBmID,
		&record.ChannelID,
		&record.MessageID,
		&record.SendUpdates,
		&record.OnlineCount,
		&record.CreatedBy,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	return record, err
}

func (p *PostgresStore) CreateTrackGroup(ctx context.Context, group TrackGroup) (TrackGroup, error) {
	row := p.pool.QueryRow(ctx, `
		INSERT INTO track_groups (guild_id, name, server_bm_id, channel_id, message_id, send_updates, online_count, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, guild_id, name, server_bm_id, channel_id, message_id, send_updates, online_count, created_by, created_at, updated_at
	`,
		group.GuildID,
		group.Name,
		group.ServerBmID,
		group.ChannelID,
		group.MessageID,
		group.SendUpdates,
		group.OnlineCount,
		group.CreatedBy,
	)

	saved, err := scanTrackGroup(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return TrackGroup{}, ErrTrackGroupNameTaken
		}
		return TrackGroup{}, err
	}
	return saved, nil
}

func (p *PostgresStore) ListTrackGroups(ctx context.Context, guildID string) ([]TrackGroup, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, guild_id, name, server_bm_id, channel_id, message_id, send_updates, online_count, created_by, created_at, updated_at
		FROM track_groups
		WHERE guild_id = $1
		ORDER BY name ASC, id ASC
	`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TrackGroup, 0)
	for rows.Next() {
		group, err := scanTrackGroup(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, group)
	}

	return items, rows.Err()
}

func (p *PostgresStore) ListAllTrackGroups(ctx context.Context) ([]TrackGroup, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, guild_id, name, server_bm_id, channel_id, message_id, send_updates, online_count, created_by, created_at, updated_at
		FROM track_groups
		ORDER BY guild_id ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TrackGroup, 0)
	for rows.Next() {
		group, err := scanTrackGroup(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, group)
	}

	return items, rows.Err()
}

func (p *PostgresStore) GetTrackGroup(ctx context.Context, guildID string, id int64) (TrackGroup, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, guild_id, name, server_bm_id, channel_id, message_id, send_updates, online_count, created_by, created_at, updated_at
		FROM track_groups
		WHERE guild_id = $1 AND id = $2
	`, guildID, id)
	group, err := scanTrackGroup(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackGroup{}, ErrTrackGroupNotFound
	}
	return group, err
}

type TrackGroupPatch struct {
	ChannelID   *string
	MessageID   *string
	SendUpdates *bool
	OnlineCount *int
}

func (p *PostgresStore) UpdateTrackGroup(ctx context.Context, guildID string, id int64, patch TrackGroupPatch) (TrackGroup, error) {
	row := p.pool.QueryRow(ctx, `
		UPDATE track_groups
		SET channel_id = COALESCE($3, channel_id),
			message_id = COALESCE($4, message_id),
			send_updates = COALESCE($5, send_updates),
			online_count = COALESCE($6, online_count),
			updated_at = NOW()
		WHERE guild_id = $1 AND id = $2
		RETURNING id, guild_id, name, server_bm_id, channel_id, message_id, send_updates, online_count, created_by, created_at, updated_at
	`,
		guildID,
		id,
		patch.ChannelID,
		patch.MessageID,
		patch.SendUpdates,
		patch.OnlineCount,
	)
	group, err := scanTrackGroup(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackGroup{}, ErrTrackGroupNotFound
	}
	return group, err
}

func (p *PostgresStore) DeleteTrackGroup(ctx context.Context, guildID string, id int64) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM track_groups WHERE guild_id = $1 AND id = $2`, guildID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTrackGroupNotFound
	}
	return nil
}

func (p *PostgresStore) ListTrackGroupMembers(ctx context.Context, trackGroupID int64) ([]TrackGroupMember, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT track_group_id, steam_id, bm_id, created_at
		FROM track_group_members
		WHERE track_group_id = $1
		ORDER BY steam_id ASC
	`, trackGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TrackGroupMember, 0)
	for rows.Next() {
		var item TrackGroupMember
		if err := rows.Scan(&item.TrackGroupID, &item.SteamID, &item.BmID, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// AddTrackGroupMembers upserts members. Returns the steam IDs that were newly inserted.
func (p *PostgresStore) AddTrackGroupMembers(ctx context.Context, trackGroupID int64, members []TrackGroupMember) ([]string, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	added := make([]string, 0)
	for _, member := range members {
		if member.SteamID == "" {
			continue
		}
		var inserted bool
		if err := tx.QueryRow(ctx, `
			INSERT INTO track_group_members (track_group_id, steam_id, bm_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (track_group_id, steam_id) DO UPDATE
			SET bm_id = CASE WHEN EXCLUDED.bm_id <> '' THEN EXCLUDED.bm_id ELSE track_group_members.bm_id END
			RETURNING (xmax = 0)
		`, trackGroupID, member.SteamID, member.BmID).Scan(&inserted); err != nil {
			return nil, err
		}
		if inserted {
			added = append(added, member.SteamID)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return added, nil
}

func (p *PostgresStore) RemoveTrackGroupMembers(ctx context.Context, trackGroupID int64, identifiers []string) (int, error) {
	if len(identifiers) == 0 {
		return 0, nil
	}

	steamIDs := make([]string, 0)
	bmIDs := make([]string, 0)
	for _, raw := range identifiers {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if len(trimmed) == 17 {
			steamIDs = append(steamIDs, trimmed)
		} else {
			bmIDs = append(bmIDs, trimmed)
		}
	}

	tag, err := p.pool.Exec(ctx, `
		DELETE FROM track_group_members
		WHERE track_group_id = $1
		  AND (steam_id = ANY($2::text[]) OR bm_id = ANY($3::text[]))
	`, trackGroupID, steamIDs, bmIDs)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// ListTrackGroupOnlineState returns each member with their bm name and current
// online state on the group's server.
func (p *PostgresStore) ListTrackGroupOnlineState(ctx context.Context, trackGroupID int64) ([]TrackGroupOnlineRow, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT
			tgm.steam_id,
			COALESCE(NULLIF(tgm.bm_id, ''), COALESCE(sa.bm_id, '')) AS bm_id,
			COALESCE(bpc.bm_name, '') AS bm_name,
			COALESCE(gsl.discord_id, '') AS discord_id,
			COALESCE(sac.avatar_image, '') AS avatar_image,
			COALESCE(ps.is_online, FALSE) AS is_online,
			COALESCE(EXTRACT(EPOCH FROM ps.online_since)::BIGINT, 0) AS online_unix,
			COALESCE(EXTRACT(EPOCH FROM ps.offline_since)::BIGINT, 0) AS offline_unix
		FROM track_groups tg
		JOIN track_group_members tgm ON tgm.track_group_id = tg.id
		LEFT JOIN steam_accounts sa ON sa.steam_id = tgm.steam_id
		LEFT JOIN steam_account_cache sac ON sac.steam_id = tgm.steam_id
		LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = COALESCE(NULLIF(tgm.bm_id, ''), sa.bm_id)
		LEFT JOIN LATERAL (
			SELECT discord_id
			FROM guild_steam_discord_links
			WHERE guild_id = tg.guild_id AND steam_id = tgm.steam_id
			ORDER BY discord_id ASC
			LIMIT 1
		) gsl ON TRUE
		LEFT JOIN player_states ps
			ON ps.server_bm_id = tg.server_bm_id
			AND ps.player_bm_id = COALESCE(NULLIF(tgm.bm_id, ''), sa.bm_id)
		WHERE tg.id = $1
		ORDER BY is_online DESC, bm_name ASC, tgm.steam_id ASC
	`, trackGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TrackGroupOnlineRow, 0)
	for rows.Next() {
		var row TrackGroupOnlineRow
		var offlineUnix int64
		if err := rows.Scan(&row.SteamID, &row.BmID, &row.BmName, &row.DiscordID, &row.AvatarImage, &row.IsOnline, &row.OnlineUnix, &offlineUnix); err != nil {
			return nil, err
		}
		if !row.IsOnline {
			row.OnlineUnix = offlineUnix
		}
		items = append(items, row)
	}
	return items, rows.Err()
}
