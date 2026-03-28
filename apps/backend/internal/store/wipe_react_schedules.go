package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrWipeReactScheduleNotFound = errors.New("wipe react schedule not found")
var ErrWipeReactScheduleLimitReached = errors.New("wipe react schedule limit reached")

const maxWipeReactSchedulesPerGuild = 3

type WipeReactSchedule struct {
	ID                int64
	GuildID           string
	Days              []string
	IncludesForceWipe bool
	Time              string
	TimeZone          string
	Repeat            string
	PostBefore        string
	Name              string
	Description       string
	AskLateReason     bool
	AskDeclinedReason bool
	CreatedBy         string
}

func scanWipeReactSchedule(scanner rowScanner) (WipeReactSchedule, error) {
	var (
		record   WipeReactSchedule
		daysJSON []byte
	)

	err := scanner.Scan(
		&record.ID,
		&record.GuildID,
		&daysJSON,
		&record.IncludesForceWipe,
		&record.Time,
		&record.TimeZone,
		&record.Repeat,
		&record.PostBefore,
		&record.Name,
		&record.Description,
		&record.AskLateReason,
		&record.AskDeclinedReason,
		&record.CreatedBy,
	)
	if err != nil {
		return WipeReactSchedule{}, err
	}

	record.Days, err = decodeStringSlice(daysJSON)
	if err != nil {
		return WipeReactSchedule{}, err
	}

	return record, nil
}

func (p *PostgresStore) ListWipeReactSchedules(ctx context.Context, guildID string) ([]WipeReactSchedule, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, guild_id, days, includes_force_wipe, time, time_zone, repeat, post_before, name, description, ask_late_reason, ask_declined_reason, created_by
		FROM wipe_react_schedules
		WHERE guild_id = $1
		ORDER BY id ASC
	`, guildID)
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

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

func (p *PostgresStore) GetWipeReactSchedule(ctx context.Context, guildID string, scheduleID int64) (WipeReactSchedule, error) {
	record, err := scanWipeReactSchedule(p.pool.QueryRow(ctx, `
		SELECT id, guild_id, days, includes_force_wipe, time, time_zone, repeat, post_before, name, description, ask_late_reason, ask_declined_reason, created_by
		FROM wipe_react_schedules
		WHERE guild_id = $1 AND id = $2
	`, guildID, scheduleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return WipeReactSchedule{}, ErrWipeReactScheduleNotFound
	}
	return record, err
}

func (p *PostgresStore) CreateWipeReactSchedule(ctx context.Context, record WipeReactSchedule) (WipeReactSchedule, error) {
	var count int
	err := p.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM wipe_react_schedules WHERE guild_id = $1
	`, record.GuildID).Scan(&count)
	if err != nil {
		return WipeReactSchedule{}, err
	}
	if count >= maxWipeReactSchedulesPerGuild {
		return WipeReactSchedule{}, ErrWipeReactScheduleLimitReached
	}

	daysJSON, err := encodeStringSlice(record.Days)
	if err != nil {
		return WipeReactSchedule{}, err
	}

	err = p.pool.QueryRow(ctx, `
		INSERT INTO wipe_react_schedules (
			guild_id, days, includes_force_wipe, time, time_zone, repeat, post_before, name, description, ask_late_reason, ask_declined_reason, created_by
		)
		VALUES ($1, $2::jsonb, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`, record.GuildID, daysJSON, record.IncludesForceWipe, record.Time, record.TimeZone,
		record.Repeat, record.PostBefore, record.Name, record.Description, record.AskLateReason, record.AskDeclinedReason, record.CreatedBy).Scan(&record.ID)
	if err != nil {
		return WipeReactSchedule{}, err
	}

	return record, nil
}

func (p *PostgresStore) UpdateWipeReactSchedule(ctx context.Context, guildID string, scheduleID int64, updated WipeReactSchedule) (WipeReactSchedule, error) {
	daysJSON, err := encodeStringSlice(updated.Days)
	if err != nil {
		return WipeReactSchedule{}, err
	}

	tag, err := p.pool.Exec(ctx, `
		UPDATE wipe_react_schedules
		SET days = $3::jsonb,
			includes_force_wipe = $4,
			time = $5,
			time_zone = $6,
			repeat = $7,
			post_before = $8,
			name = $9,
			description = $10,
			ask_late_reason = $11,
			ask_declined_reason = $12,
			updated_at = NOW()
		WHERE guild_id = $1 AND id = $2
	`, guildID, scheduleID, daysJSON, updated.IncludesForceWipe, updated.Time, updated.TimeZone,
		updated.Repeat, updated.PostBefore, updated.Name, updated.Description, updated.AskLateReason, updated.AskDeclinedReason)
	if err != nil {
		return WipeReactSchedule{}, err
	}
	if tag.RowsAffected() == 0 {
		return WipeReactSchedule{}, ErrWipeReactScheduleNotFound
	}

	return p.GetWipeReactSchedule(ctx, guildID, scheduleID)
}

func (p *PostgresStore) DeleteWipeReactSchedule(ctx context.Context, guildID string, scheduleID int64) error {
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM wipe_react_schedules
		WHERE guild_id = $1 AND id = $2
	`, guildID, scheduleID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrWipeReactScheduleNotFound
	}

	return nil
}
