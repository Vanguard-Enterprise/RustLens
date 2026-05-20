package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CommandUsageInsert is the write-side input for a single command/interaction
// invocation. AppUserID is a hard FK; the caller must resolve it before
// calling InsertCommandUsage.
type CommandUsageInsert struct {
	AppUserID       string
	DiscordID       string
	GuildID         *string
	ChannelID       *string
	InteractionType string // 'command' | 'button' | 'modal' | 'string_select'
	Command         string
	Subcommand      *string
	Target          *string
	Options         json.RawMessage // JSONB; must be a valid JSON object
	Outcome         string          // 'success' | 'error' | 'banned' | 'denied'
	ErrorCode       *string
	LatencyMs       int
}

// CommandUsageRow is the read-side shape returned to handlers.
type CommandUsageRow struct {
	ID              int64           `json:"id"`
	AppUserID       string          `json:"appUserId"`
	DiscordID       string          `json:"discordId"`
	GuildID         *string         `json:"guildId"`
	ChannelID       *string         `json:"channelId"`
	InteractionType string          `json:"interactionType"`
	Command         string          `json:"command"`
	Subcommand      *string         `json:"subcommand"`
	Target          *string         `json:"target"`
	Options         json.RawMessage `json:"options"`
	Outcome         string          `json:"outcome"`
	ErrorCode       *string         `json:"errorCode"`
	LatencyMs       int             `json:"latencyMs"`
	CreatedAt       time.Time       `json:"createdAt"`
}

type CommandUsagePage struct {
	Items []CommandUsageRow
	Total int64
}

type CommandUsageTopCommand struct {
	Command string `json:"command"`
	Count   int64  `json:"count"`
}

type CommandUsageSummary struct {
	TotalCount  int64                    `json:"totalCount"`
	ErrorCount  int64                    `json:"errorCount"`
	TopCommands []CommandUsageTopCommand `json:"topCommands"`
}

func (p *PostgresStore) InsertCommandUsage(ctx context.Context, row CommandUsageInsert) error {
	if len(row.Options) == 0 {
		row.Options = []byte("{}")
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO command_usage (
			app_user_id, discord_id, guild_id, channel_id,
			interaction_type, command, subcommand, target,
			options, outcome, error_code, latency_ms
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12)
	`,
		row.AppUserID, row.DiscordID, row.GuildID, row.ChannelID,
		row.InteractionType, row.Command, row.Subcommand, row.Target,
		string(row.Options), row.Outcome, row.ErrorCode, row.LatencyMs,
	)
	if err != nil {
		return fmt.Errorf("insert command_usage: %w", err)
	}
	return nil
}

func (p *PostgresStore) ListUserCommandUsage(ctx context.Context, appUserID string, limit, offset int) (CommandUsagePage, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var page CommandUsagePage
	if err := p.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM command_usage WHERE app_user_id = $1
	`, appUserID).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count command_usage: %w", err)
	}
	if page.Total == 0 {
		return page, nil
	}

	rows, err := p.pool.Query(ctx, `
		SELECT id, app_user_id, discord_id, guild_id, channel_id,
		       interaction_type, command, subcommand, target,
		       options, outcome, error_code, latency_ms, created_at
		FROM command_usage
		WHERE app_user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, appUserID, limit, offset)
	if err != nil {
		return page, fmt.Errorf("list command_usage: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var r CommandUsageRow
		if err := rows.Scan(
			&r.ID, &r.AppUserID, &r.DiscordID, &r.GuildID, &r.ChannelID,
			&r.InteractionType, &r.Command, &r.Subcommand, &r.Target,
			&r.Options, &r.Outcome, &r.ErrorCode, &r.LatencyMs, &r.CreatedAt,
		); err != nil {
			return page, fmt.Errorf("scan command_usage: %w", err)
		}
		page.Items = append(page.Items, r)
	}
	return page, rows.Err()
}

func (p *PostgresStore) SummarizeUserCommandUsage(ctx context.Context, appUserID string, window time.Duration) (CommandUsageSummary, error) {
	var sum CommandUsageSummary
	cutoff := time.Now().Add(-window)

	if err := p.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE outcome = 'error') AS errors
		FROM command_usage
		WHERE app_user_id = $1 AND created_at >= $2
	`, appUserID, cutoff).Scan(&sum.TotalCount, &sum.ErrorCount); err != nil {
		return sum, fmt.Errorf("summarize command_usage: %w", err)
	}
	if sum.TotalCount == 0 {
		return sum, nil
	}

	rows, err := p.pool.Query(ctx, `
		SELECT command, COUNT(*) AS c
		FROM command_usage
		WHERE app_user_id = $1 AND created_at >= $2
		GROUP BY command
		ORDER BY c DESC, command ASC
		LIMIT 5
	`, appUserID, cutoff)
	if err != nil {
		return sum, fmt.Errorf("top commands command_usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tc CommandUsageTopCommand
		if err := rows.Scan(&tc.Command, &tc.Count); err != nil {
			return sum, fmt.Errorf("scan top command: %w", err)
		}
		sum.TopCommands = append(sum.TopCommands, tc)
	}
	return sum, rows.Err()
}

// PurgeCommandUsageBefore deletes rows older than cutoff. Returns rows affected.
// Used by internal/cleanup.
func (p *PostgresStore) PurgeCommandUsageBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := p.pool.Exec(ctx, `DELETE FROM command_usage WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge command_usage: %w", err)
	}
	return tag.RowsAffected(), nil
}
