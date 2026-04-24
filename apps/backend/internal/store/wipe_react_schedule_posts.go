package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrWipeReactSchedulePostNotFound = errors.New("wipe react schedule post not found")

type WipeReactSchedulePost struct {
	ID                int64
	ScheduleID        int64
	GuildID           string
	ChannelID         string
	WipeUnix          int64
	PostUnix          int64
	ClaimedBy         string
	ClaimedAt         *time.Time
	MessageID         string
	PostedChannelID   string
	CompletedAt       *time.Time
	CreatedAt         time.Time
	TimeZone          string
	Name              string
	Description       string
	AskLateReason     bool
	AskDeclinedReason bool
	CreatedBy         string
}

func scanWipeReactSchedulePost(scanner rowScanner) (WipeReactSchedulePost, error) {
	var record WipeReactSchedulePost
	err := scanner.Scan(
		&record.ID,
		&record.ScheduleID,
		&record.GuildID,
		&record.ChannelID,
		&record.WipeUnix,
		&record.PostUnix,
		&record.ClaimedBy,
		&record.ClaimedAt,
		&record.MessageID,
		&record.PostedChannelID,
		&record.CompletedAt,
		&record.CreatedAt,
		&record.TimeZone,
		&record.Name,
		&record.Description,
		&record.AskLateReason,
		&record.AskDeclinedReason,
		&record.CreatedBy,
	)
	if err != nil {
		return WipeReactSchedulePost{}, err
	}
	return record, nil
}

func (p *PostgresStore) ListAllWipeReactSchedules(ctx context.Context) ([]WipeReactSchedule, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, guild_id, days, includes_force_wipe, time, time_zone, repeat, post_before, name, description, ask_late_reason, ask_declined_reason, channel_id, last_posted_for_unix, created_by
		FROM wipe_react_schedules
		ORDER BY guild_id ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]WipeReactSchedule, 0)
	for rows.Next() {
		record, err := scanWipeReactSchedule(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

func (p *PostgresStore) EnqueueWipeReactSchedulePost(
	ctx context.Context,
	scheduleID int64,
	guildID string,
	channelID string,
	wipeUnix int64,
	postUnix int64,
) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO wipe_react_schedule_posts (schedule_id, guild_id, channel_id, wipe_unix, post_unix)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (schedule_id, wipe_unix) DO NOTHING
	`, scheduleID, guildID, channelID, wipeUnix, postUnix)
	return err
}

func (p *PostgresStore) ClaimNextWipeReactSchedulePost(
	ctx context.Context,
	guildID string,
	workerID string,
	nowUnix int64,
	staleBefore time.Time,
) (WipeReactSchedulePost, error) {
	record, err := scanWipeReactSchedulePost(p.pool.QueryRow(ctx, `
		WITH next_post AS (
			SELECT p.id
			FROM wipe_react_schedule_posts p
			WHERE p.guild_id = $1
			  AND p.completed_at IS NULL
			  AND p.post_unix <= $2
			  AND (p.claimed_at IS NULL OR p.claimed_at < $3)
			ORDER BY p.post_unix ASC, p.id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE wipe_react_schedule_posts p
		SET claimed_by = $4,
			claimed_at = NOW()
		FROM next_post,
			 wipe_react_schedules s
		WHERE p.id = next_post.id
		  AND s.id = p.schedule_id
		RETURNING p.id, p.schedule_id, p.guild_id, p.channel_id, p.wipe_unix, p.post_unix,
			p.claimed_by, p.claimed_at, p.message_id, p.posted_channel_id, p.completed_at, p.created_at,
			s.time_zone, s.name, s.description, s.ask_late_reason, s.ask_declined_reason, s.created_by
	`, guildID, nowUnix, staleBefore, workerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return WipeReactSchedulePost{}, ErrWipeReactSchedulePostNotFound
	}
	return record, err
}

func (p *PostgresStore) CompleteWipeReactSchedulePost(
	ctx context.Context,
	guildID string,
	postID int64,
	workerID string,
	messageID string,
	postedChannelID string,
) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var scheduleID int64
	var wipeUnix int64
	err = tx.QueryRow(ctx, `
		UPDATE wipe_react_schedule_posts
		SET message_id = $4,
			posted_channel_id = $5,
			completed_at = NOW()
		WHERE guild_id = $1 AND id = $2 AND claimed_by = $3 AND completed_at IS NULL
		RETURNING schedule_id, wipe_unix
	`, guildID, postID, workerID, messageID, postedChannelID).Scan(&scheduleID, &wipeUnix)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWipeReactSchedulePostNotFound
	}
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE wipe_react_schedules
		SET last_posted_for_unix = GREATEST(last_posted_for_unix, $3),
			updated_at = NOW()
		WHERE guild_id = $1 AND id = $2
	`, guildID, scheduleID, wipeUnix); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (p *PostgresStore) ReleaseWipeReactSchedulePost(
	ctx context.Context,
	guildID string,
	postID int64,
	workerID string,
) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE wipe_react_schedule_posts
		SET claimed_by = '',
			claimed_at = NULL
		WHERE guild_id = $1 AND id = $2 AND claimed_by = $3 AND completed_at IS NULL
	`, guildID, postID, workerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrWipeReactSchedulePostNotFound
	}
	return nil
}
