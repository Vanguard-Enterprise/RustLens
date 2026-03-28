package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrWipeReactNotFound = errors.New("wipe react not found")

type WipeReactReactionType string

const (
	WipeReactReactionAccepted WipeReactReactionType = "accepted"
	WipeReactReactionDeclined WipeReactReactionType = "declined"
	WipeReactReactionLate     WipeReactReactionType = "late"
)

type WipeReact struct {
	GuildID           string
	ChannelID         string
	MessageID         string
	CreatorID         string
	Name              string
	Description       string
	TimeZone          string
	ScheduledUnix     int64
	AskLateReason     bool
	AskDeclinedReason bool
	AcceptedUserIDs   []string
	DeclinedUserIDs   []string
	LateUserIDs       []string
	EmbedTimestamp    string
}

type WipeReactReactionUpdate struct {
	WipeReact    WipeReact
	Changed      bool
	PreviousType *WipeReactReactionType
}

type WipeReactReactionRemoval struct {
	WipeReact   WipeReact
	Changed     bool
	RemovedType *WipeReactReactionType
}

type rowScanner interface {
	Scan(dest ...any) error
}

func encodeStringSlice(values []string) (string, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeStringSlice(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}

	var decoded []string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}

	return decoded, nil
}

func scanWipeReact(scanner rowScanner) (WipeReact, error) {
	var (
		record       WipeReact
		acceptedJSON []byte
		declinedJSON []byte
		lateJSON     []byte
	)

	err := scanner.Scan(
		&record.GuildID,
		&record.ChannelID,
		&record.MessageID,
		&record.CreatorID,
		&record.Name,
		&record.Description,
		&record.TimeZone,
		&record.ScheduledUnix,
		&record.AskLateReason,
		&record.AskDeclinedReason,
		&acceptedJSON,
		&declinedJSON,
		&lateJSON,
		&record.EmbedTimestamp,
	)
	if err != nil {
		return WipeReact{}, err
	}

	record.AcceptedUserIDs, err = decodeStringSlice(acceptedJSON)
	if err != nil {
		return WipeReact{}, err
	}
	record.DeclinedUserIDs, err = decodeStringSlice(declinedJSON)
	if err != nil {
		return WipeReact{}, err
	}
	record.LateUserIDs, err = decodeStringSlice(lateJSON)
	if err != nil {
		return WipeReact{}, err
	}

	return record, nil
}

func (p *PostgresStore) ListActiveWipeReacts(ctx context.Context, guildID string, nowUnix int64) ([]WipeReact, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT guild_id, channel_id, message_id, creator_id, name, description, time_zone,
			scheduled_unix, ask_late_reason, ask_declined_reason,
			accepted_user_ids, declined_user_ids, late_user_ids, embed_timestamp
		FROM wipe_reacts
		WHERE guild_id = $1 AND scheduled_unix > $2
		ORDER BY scheduled_unix DESC, message_id DESC
	`, guildID, nowUnix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]WipeReact, 0)
	for rows.Next() {
		record, err := scanWipeReact(rows)
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

func (p *PostgresStore) GetWipeReact(ctx context.Context, guildID string, messageID string) (WipeReact, error) {
	record, err := scanWipeReact(p.pool.QueryRow(ctx, `
		SELECT guild_id, channel_id, message_id, creator_id, name, description, time_zone,
			scheduled_unix, ask_late_reason, ask_declined_reason,
			accepted_user_ids, declined_user_ids, late_user_ids, embed_timestamp
		FROM wipe_reacts
		WHERE guild_id = $1 AND message_id = $2
	`, guildID, messageID))
	if errors.Is(err, pgx.ErrNoRows) {
		return WipeReact{}, ErrWipeReactNotFound
	}
	return record, err
}

func (p *PostgresStore) CreateWipeReact(ctx context.Context, record WipeReact) (WipeReact, error) {
	acceptedJSON, err := encodeStringSlice(record.AcceptedUserIDs)
	if err != nil {
		return WipeReact{}, err
	}
	declinedJSON, err := encodeStringSlice(record.DeclinedUserIDs)
	if err != nil {
		return WipeReact{}, err
	}
	lateJSON, err := encodeStringSlice(record.LateUserIDs)
	if err != nil {
		return WipeReact{}, err
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO wipe_reacts (
			message_id, guild_id, channel_id, creator_id, name, description, time_zone,
			scheduled_unix, ask_late_reason, ask_declined_reason,
			accepted_user_ids, declined_user_ids, late_user_ids, embed_timestamp
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10,
			$11::jsonb, $12::jsonb, $13::jsonb, $14
		)
	`, record.MessageID, record.GuildID, record.ChannelID, record.CreatorID, record.Name, record.Description,
		record.TimeZone, record.ScheduledUnix, record.AskLateReason, record.AskDeclinedReason,
		acceptedJSON, declinedJSON, lateJSON, record.EmbedTimestamp)
	if err != nil {
		return WipeReact{}, err
	}

	return record, nil
}

func (p *PostgresStore) UpdateWipeReact(ctx context.Context, guildID string, messageID string, updated WipeReact) (WipeReact, error) {
	tag, err := p.pool.Exec(ctx, `
		UPDATE wipe_reacts
		SET name = $3,
			description = $4,
			time_zone = $5,
			scheduled_unix = $6,
			ask_late_reason = $7,
			ask_declined_reason = $8,
			embed_timestamp = $9,
			updated_at = NOW()
		WHERE guild_id = $1 AND message_id = $2
	`, guildID, messageID, updated.Name, updated.Description, updated.TimeZone, updated.ScheduledUnix,
		updated.AskLateReason, updated.AskDeclinedReason, updated.EmbedTimestamp)
	if err != nil {
		return WipeReact{}, err
	}
	if tag.RowsAffected() == 0 {
		return WipeReact{}, ErrWipeReactNotFound
	}

	return p.GetWipeReact(ctx, guildID, messageID)
}

func (p *PostgresStore) DeleteWipeReact(ctx context.Context, guildID string, messageID string) error {
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM wipe_reacts
		WHERE guild_id = $1 AND message_id = $2
	`, guildID, messageID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrWipeReactNotFound
	}

	return nil
}

func findReactionType(record WipeReact, userID string) *WipeReactReactionType {
	if containsString(record.AcceptedUserIDs, userID) {
		action := WipeReactReactionAccepted
		return &action
	}
	if containsString(record.DeclinedUserIDs, userID) {
		action := WipeReactReactionDeclined
		return &action
	}
	if containsString(record.LateUserIDs, userID) {
		action := WipeReactReactionLate
		return &action
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func filterString(values []string, excluded string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value != excluded {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func upsertUnique(values []string, target string) []string {
	filtered := filterString(values, target)
	return append(filtered, target)
}

func (p *PostgresStore) SetWipeReactReaction(
	ctx context.Context,
	guildID string,
	messageID string,
	userID string,
	reactionType WipeReactReactionType,
) (WipeReactReactionUpdate, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return WipeReactReactionUpdate{}, err
	}
	defer tx.Rollback(ctx)

	record, err := scanWipeReact(tx.QueryRow(ctx, `
		SELECT guild_id, channel_id, message_id, creator_id, name, description, time_zone,
			scheduled_unix, ask_late_reason, ask_declined_reason,
			accepted_user_ids, declined_user_ids, late_user_ids, embed_timestamp
		FROM wipe_reacts
		WHERE guild_id = $1 AND message_id = $2
		FOR UPDATE
	`, guildID, messageID))
	if errors.Is(err, pgx.ErrNoRows) {
		return WipeReactReactionUpdate{}, ErrWipeReactNotFound
	}
	if err != nil {
		return WipeReactReactionUpdate{}, err
	}

	previousType := findReactionType(record, userID)
	changed := previousType == nil || *previousType != reactionType
	if changed {
		record.AcceptedUserIDs = filterString(record.AcceptedUserIDs, userID)
		record.DeclinedUserIDs = filterString(record.DeclinedUserIDs, userID)
		record.LateUserIDs = filterString(record.LateUserIDs, userID)

		switch reactionType {
		case WipeReactReactionAccepted:
			record.AcceptedUserIDs = upsertUnique(record.AcceptedUserIDs, userID)
		case WipeReactReactionDeclined:
			record.DeclinedUserIDs = upsertUnique(record.DeclinedUserIDs, userID)
		case WipeReactReactionLate:
			record.LateUserIDs = upsertUnique(record.LateUserIDs, userID)
		default:
			return WipeReactReactionUpdate{}, errors.New("invalid wipe react reaction type")
		}

		acceptedJSON, err := encodeStringSlice(record.AcceptedUserIDs)
		if err != nil {
			return WipeReactReactionUpdate{}, err
		}
		declinedJSON, err := encodeStringSlice(record.DeclinedUserIDs)
		if err != nil {
			return WipeReactReactionUpdate{}, err
		}
		lateJSON, err := encodeStringSlice(record.LateUserIDs)
		if err != nil {
			return WipeReactReactionUpdate{}, err
		}

		_, err = tx.Exec(ctx, `
			UPDATE wipe_reacts
			SET accepted_user_ids = $3::jsonb,
				declined_user_ids = $4::jsonb,
				late_user_ids = $5::jsonb,
				updated_at = NOW()
			WHERE guild_id = $1 AND message_id = $2
		`, guildID, messageID, acceptedJSON, declinedJSON, lateJSON)
		if err != nil {
			return WipeReactReactionUpdate{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return WipeReactReactionUpdate{}, err
	}

	return WipeReactReactionUpdate{
		WipeReact:    record,
		Changed:      changed,
		PreviousType: previousType,
	}, nil
}

func (p *PostgresStore) RemoveWipeReactReaction(
	ctx context.Context,
	guildID string,
	messageID string,
	userID string,
) (WipeReactReactionRemoval, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return WipeReactReactionRemoval{}, err
	}
	defer tx.Rollback(ctx)

	record, err := scanWipeReact(tx.QueryRow(ctx, `
		SELECT guild_id, channel_id, message_id, creator_id, name, description, time_zone,
			scheduled_unix, ask_late_reason, ask_declined_reason,
			accepted_user_ids, declined_user_ids, late_user_ids, embed_timestamp
		FROM wipe_reacts
		WHERE guild_id = $1 AND message_id = $2
		FOR UPDATE
	`, guildID, messageID))
	if errors.Is(err, pgx.ErrNoRows) {
		return WipeReactReactionRemoval{}, ErrWipeReactNotFound
	}
	if err != nil {
		return WipeReactReactionRemoval{}, err
	}

	removedType := findReactionType(record, userID)
	changed := removedType != nil
	if changed {
		record.AcceptedUserIDs = filterString(record.AcceptedUserIDs, userID)
		record.DeclinedUserIDs = filterString(record.DeclinedUserIDs, userID)
		record.LateUserIDs = filterString(record.LateUserIDs, userID)

		acceptedJSON, err := encodeStringSlice(record.AcceptedUserIDs)
		if err != nil {
			return WipeReactReactionRemoval{}, err
		}
		declinedJSON, err := encodeStringSlice(record.DeclinedUserIDs)
		if err != nil {
			return WipeReactReactionRemoval{}, err
		}
		lateJSON, err := encodeStringSlice(record.LateUserIDs)
		if err != nil {
			return WipeReactReactionRemoval{}, err
		}

		_, err = tx.Exec(ctx, `
			UPDATE wipe_reacts
			SET accepted_user_ids = $3::jsonb,
				declined_user_ids = $4::jsonb,
				late_user_ids = $5::jsonb,
				updated_at = NOW()
			WHERE guild_id = $1 AND message_id = $2
		`, guildID, messageID, acceptedJSON, declinedJSON, lateJSON)
		if err != nil {
			return WipeReactReactionRemoval{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return WipeReactReactionRemoval{}, err
	}

	return WipeReactReactionRemoval{
		WipeReact:   record,
		Changed:     changed,
		RemovedType: removedType,
	}, nil
}
