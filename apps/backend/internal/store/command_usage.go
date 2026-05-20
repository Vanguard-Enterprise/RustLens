package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MaxTargetsPerUsage caps how many command_usage_targets rows we persist for
// a single command_usage row. Excess is dropped at write time.
const MaxTargetsPerUsage = 500

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
	Targets         []CommandUsageTargetInsert // capped at 500; excess silently dropped
}

// CommandUsageTargetInsert is one target row to associate with a usage row.
type CommandUsageTargetInsert struct {
	Kind        string  // 'user'|'role'|'channel'|'guild'|'steam'
	Snowflake   string
	DisplayName *string
	AvatarHash  *string
	Relation    string  // 'recipient'|'scope'|'subject'
}

// CommandUsageTargetRow is the read-side shape returned to handlers.
type CommandUsageTargetRow struct {
	Kind        string  `json:"kind"`
	Snowflake   string  `json:"snowflake"`
	DisplayName *string `json:"displayName"`
	AvatarHash  *string `json:"avatarHash"`
	Relation    string  `json:"relation"`
	AppUserID   *string `json:"appUserId,omitempty"`
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

// CommandUsageSummary aggregates command_usage rows for a single user over
// a time window. errorRate is intentionally not stored on this type —
// the admin read handler computes it from TotalCount and ErrorCount when
// rendering the response.
type CommandUsageSummary struct {
	TotalCount  int64                    `json:"totalCount"`
	ErrorCount  int64                    `json:"errorCount"`
	TopCommands []CommandUsageTopCommand `json:"topCommands"`
}

func (p *PostgresStore) InsertCommandUsage(ctx context.Context, row CommandUsageInsert) error {
	if len(row.Options) == 0 || string(row.Options) == "null" {
		row.Options = []byte("{}")
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin command_usage tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var usageID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO command_usage (
			app_user_id, discord_id, guild_id, channel_id,
			interaction_type, command, subcommand, target,
			options, outcome, error_code, latency_ms
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12)
		RETURNING id
	`,
		row.AppUserID, row.DiscordID, row.GuildID, row.ChannelID,
		row.InteractionType, row.Command, row.Subcommand, row.Target,
		string(row.Options), row.Outcome, row.ErrorCode, row.LatencyMs,
	).Scan(&usageID); err != nil {
		return fmt.Errorf("insert command_usage: %w", err)
	}

	targets := row.Targets
	if len(targets) > MaxTargetsPerUsage {
		targets = targets[:MaxTargetsPerUsage]
	}
	for _, t := range targets {
		if _, err := tx.Exec(ctx, `
			INSERT INTO command_usage_targets (
				command_usage_id, kind, snowflake, display_name, avatar_hash, relation
			) VALUES ($1,$2,$3,$4,$5,$6)
		`, usageID, t.Kind, t.Snowflake, t.DisplayName, t.AvatarHash, t.Relation); err != nil {
			return fmt.Errorf("insert command_usage_target: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit command_usage: %w", err)
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
	// COUNT and SELECT are intentionally non-transactional. For an admin
	// pagination view over a 90-day window per user, any phantom-read
	// discrepancy from concurrent inserts is acceptable.
	if err := p.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM command_usage WHERE app_user_id = $1
	`, appUserID).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count command_usage: %w", err)
	}
	if page.Total == 0 {
		page.Items = []CommandUsageRow{}
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
	sum := CommandUsageSummary{TopCommands: []CommandUsageTopCommand{}}
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

// CommandUsageListFilters narrows ListUserCommandUsageFiltered. All fields optional.
type CommandUsageListFilters struct {
	Q                string   // ILIKE %q% across command/subcommand/target + targets.display_name/snowflake
	Outcomes         []string // success|error|banned|denied
	InteractionTypes []string // command|button|modal|string_select
	Command          string   // exact match
	GuildID          string   // exact match
	WindowDays       int      // 0 = no extra window filter; 1/7/30/90 supported
}

// CommandUsageRowWithTargets is a row plus its inline target snapshot list.
type CommandUsageRowWithTargets struct {
	CommandUsageRow
	Targets     []CommandUsageTargetRow `json:"targets"`
	TargetTotal *int                    `json:"targetTotal,omitempty"`
}

type CommandUsagePageWithTargets struct {
	Items []CommandUsageRowWithTargets
	Total int64
}

// InlineTargetCap is the maximum number of targets embedded per row in
// ListUserCommandUsageFiltered. Rows with more targets set TargetTotal.
const InlineTargetCap = 100

func (p *PostgresStore) ListUserCommandUsageFiltered(
	ctx context.Context,
	appUserID string,
	filters CommandUsageListFilters,
	limit, offset int,
) (CommandUsagePageWithTargets, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	args := []any{appUserID}
	where := []string{"cu.app_user_id = $1"}

	if filters.WindowDays > 0 {
		args = append(args, time.Now().Add(-time.Duration(filters.WindowDays)*24*time.Hour))
		where = append(where, fmt.Sprintf("cu.created_at >= $%d", len(args)))
	}
	if filters.Command != "" {
		args = append(args, filters.Command)
		where = append(where, fmt.Sprintf("cu.command = $%d", len(args)))
	}
	if filters.GuildID != "" {
		args = append(args, filters.GuildID)
		where = append(where, fmt.Sprintf("cu.guild_id = $%d", len(args)))
	}
	if len(filters.Outcomes) > 0 {
		args = append(args, filters.Outcomes)
		where = append(where, fmt.Sprintf("cu.outcome = ANY($%d)", len(args)))
	}
	if len(filters.InteractionTypes) > 0 {
		args = append(args, filters.InteractionTypes)
		where = append(where, fmt.Sprintf("cu.interaction_type = ANY($%d)", len(args)))
	}
	if filters.Q != "" {
		args = append(args, "%"+filters.Q+"%")
		qIdx := len(args)
		where = append(where, fmt.Sprintf(`(
			cu.command ILIKE $%d
			OR cu.subcommand ILIKE $%d
			OR cu.target ILIKE $%d
			OR EXISTS (
				SELECT 1 FROM command_usage_targets t
				WHERE t.command_usage_id = cu.id
				AND (t.display_name ILIKE $%d OR t.snowflake ILIKE $%d)
			)
		)`, qIdx, qIdx, qIdx, qIdx, qIdx))
	}

	whereSQL := strings.Join(where, " AND ")

	var page CommandUsagePageWithTargets
	countSQL := "SELECT COUNT(*) FROM command_usage cu WHERE " + whereSQL
	if err := p.pool.QueryRow(ctx, countSQL, args...).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count filtered command_usage: %w", err)
	}
	if page.Total == 0 {
		page.Items = []CommandUsageRowWithTargets{}
		return page, nil
	}

	args = append(args, limit, offset)
	limitIdx := len(args) - 1
	offsetIdx := len(args)
	listSQL := fmt.Sprintf(`
		SELECT
			cu.id, cu.app_user_id, cu.discord_id, cu.guild_id, cu.channel_id,
			cu.interaction_type, cu.command, cu.subcommand, cu.target,
			cu.options, cu.outcome, cu.error_code, cu.latency_ms, cu.created_at,
			COALESCE(
				(SELECT jsonb_agg(jsonb_build_object(
					'kind', t.kind,
					'snowflake', t.snowflake,
					'displayName', t.display_name,
					'avatarHash', t.avatar_hash,
					'relation', t.relation
				) ORDER BY t.id)
				 FROM (
					SELECT id, kind, snowflake, display_name, avatar_hash, relation
					FROM command_usage_targets
					WHERE command_usage_id = cu.id
					ORDER BY id ASC
					LIMIT %d
				 ) t),
				'[]'::jsonb
			) AS targets_json,
			(SELECT COUNT(*) FROM command_usage_targets WHERE command_usage_id = cu.id) AS target_total
		FROM command_usage cu
		WHERE %s
		ORDER BY cu.created_at DESC, cu.id DESC
		LIMIT $%d OFFSET $%d
	`, InlineTargetCap, whereSQL, limitIdx, offsetIdx)

	rows, err := p.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return page, fmt.Errorf("list filtered command_usage: %w", err)
	}

	for rows.Next() {
		var r CommandUsageRowWithTargets
		var targetsJSON []byte
		var targetTotal int64
		if err := rows.Scan(
			&r.ID, &r.AppUserID, &r.DiscordID, &r.GuildID, &r.ChannelID,
			&r.InteractionType, &r.Command, &r.Subcommand, &r.Target,
			&r.Options, &r.Outcome, &r.ErrorCode, &r.LatencyMs, &r.CreatedAt,
			&targetsJSON, &targetTotal,
		); err != nil {
			rows.Close()
			return page, fmt.Errorf("scan filtered command_usage: %w", err)
		}
		if err := json.Unmarshal(targetsJSON, &r.Targets); err != nil {
			rows.Close()
			return page, fmt.Errorf("unmarshal targets: %w", err)
		}
		if r.Targets == nil {
			r.Targets = []CommandUsageTargetRow{}
		}
		if targetTotal > int64(InlineTargetCap) {
			total := int(targetTotal)
			r.TargetTotal = &total
		}
		page.Items = append(page.Items, r)
	}
	// Capture rows.Err() before closing so we can return it after enrichment.
	rowsErr := rows.Err()
	// Explicitly close before enrichment queries — pgx does not allow a second
	// query on the same connection while a rows iterator is still open.
	rows.Close()
	if rowsErr != nil {
		return page, rowsErr
	}

	// Collect all distinct user-target snowflakes and guild snowflakes across the page.
	userSnowflakes := map[string]struct{}{}
	guildSnowflakes := map[string]struct{}{}
	for _, item := range page.Items {
		for _, t := range item.Targets {
			switch t.Kind {
			case "user":
				userSnowflakes[t.Snowflake] = struct{}{}
			case "guild":
				guildSnowflakes[t.Snowflake] = struct{}{}
			}
		}
	}

	type userEnrichment struct {
		AppUserID   string
		DisplayName *string
		AvatarHash  *string
	}
	userMap := map[string]userEnrichment{}
	if len(userSnowflakes) > 0 {
		ids := make([]string, 0, len(userSnowflakes))
		for s := range userSnowflakes {
			ids = append(ids, s)
		}
		uRows, err := p.pool.Query(ctx, `
			SELECT discord_id, id, COALESCE(discord_global_name, discord_username), discord_avatar
			FROM app_users
			WHERE discord_id = ANY($1)
		`, ids)
		if err != nil {
			return page, fmt.Errorf("enrich users: %w", err)
		}
		for uRows.Next() {
			var discordID, appUserID string
			var displayName string
			var avatar *string
			if err := uRows.Scan(&discordID, &appUserID, &displayName, &avatar); err != nil {
				uRows.Close()
				return page, fmt.Errorf("scan enrich user: %w", err)
			}
			dn := displayName
			userMap[discordID] = userEnrichment{AppUserID: appUserID, DisplayName: &dn, AvatarHash: avatar}
		}
		uRows.Close()
	}

	type guildEnrichment struct {
		Name     string
		IconHash *string
	}
	guildMap := map[string]guildEnrichment{}
	if len(guildSnowflakes) > 0 {
		ids := make([]string, 0, len(guildSnowflakes))
		for s := range guildSnowflakes {
			ids = append(ids, s)
		}
		gRows, err := p.pool.Query(ctx, `
			SELECT guild_id, name, icon_hash
			FROM bot_guilds
			WHERE guild_id = ANY($1)
		`, ids)
		if err != nil {
			return page, fmt.Errorf("enrich guilds: %w", err)
		}
		for gRows.Next() {
			var gid, name string
			var icon *string
			if err := gRows.Scan(&gid, &name, &icon); err != nil {
				gRows.Close()
				return page, fmt.Errorf("scan enrich guild: %w", err)
			}
			guildMap[gid] = guildEnrichment{Name: name, IconHash: icon}
		}
		gRows.Close()
	}

	// Apply enrichment: live values win over snapshot.
	for i := range page.Items {
		for j := range page.Items[i].Targets {
			t := &page.Items[i].Targets[j]
			switch t.Kind {
			case "user":
				if u, ok := userMap[t.Snowflake]; ok {
					if u.DisplayName != nil {
						t.DisplayName = u.DisplayName
					}
					if u.AvatarHash != nil {
						t.AvatarHash = u.AvatarHash
					}
					t.AppUserID = &u.AppUserID
				}
			case "guild":
				if g, ok := guildMap[t.Snowflake]; ok {
					name := g.Name
					t.DisplayName = &name
					if g.IconHash != nil {
						t.AvatarHash = g.IconHash
					}
				}
			}
		}
	}

	return page, nil
}

type CommandUsageFilterGuild struct {
	GuildID  string  `json:"guildId"`
	Name     string  `json:"name"`
	IconHash *string `json:"iconHash"`
}

type CommandUsageFilterOptions struct {
	Commands []string                  `json:"commands"`
	Guilds   []CommandUsageFilterGuild `json:"guilds"`
}

func (p *PostgresStore) CommandUsageFilterOptions(
	ctx context.Context,
	appUserID string,
	windowDays int,
) (CommandUsageFilterOptions, error) {
	out := CommandUsageFilterOptions{
		Commands: []string{},
		Guilds:   []CommandUsageFilterGuild{},
	}
	if windowDays <= 0 {
		windowDays = 90
	}
	cutoff := time.Now().Add(-time.Duration(windowDays) * 24 * time.Hour)

	rows, err := p.pool.Query(ctx, `
		SELECT DISTINCT command
		FROM command_usage
		WHERE app_user_id = $1 AND created_at >= $2
		ORDER BY command ASC
	`, appUserID, cutoff)
	if err != nil {
		return out, fmt.Errorf("filter commands: %w", err)
	}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return out, fmt.Errorf("scan filter command: %w", err)
		}
		out.Commands = append(out.Commands, c)
	}
	rows.Close()

	guildRows, err := p.pool.Query(ctx, `
		SELECT DISTINCT cu.guild_id, COALESCE(bg.name, '') AS name, bg.icon_hash
		FROM command_usage cu
		LEFT JOIN bot_guilds bg ON bg.guild_id = cu.guild_id
		WHERE cu.app_user_id = $1 AND cu.created_at >= $2 AND cu.guild_id IS NOT NULL
		ORDER BY name ASC, cu.guild_id ASC
	`, appUserID, cutoff)
	if err != nil {
		return out, fmt.Errorf("filter guilds: %w", err)
	}
	defer guildRows.Close()
	for guildRows.Next() {
		var g CommandUsageFilterGuild
		if err := guildRows.Scan(&g.GuildID, &g.Name, &g.IconHash); err != nil {
			return out, fmt.Errorf("scan filter guild: %w", err)
		}
		out.Guilds = append(out.Guilds, g)
	}
	return out, guildRows.Err()
}

func (p *PostgresStore) ListCommandUsageTargets(
	ctx context.Context,
	usageID int64,
) ([]CommandUsageTargetRow, error) {
	out := []CommandUsageTargetRow{}
	rows, err := p.pool.Query(ctx, `
		SELECT kind, snowflake, display_name, avatar_hash, relation
		FROM command_usage_targets
		WHERE command_usage_id = $1
		ORDER BY id ASC
	`, usageID)
	if err != nil {
		return out, fmt.Errorf("list targets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t CommandUsageTargetRow
		if err := rows.Scan(&t.Kind, &t.Snowflake, &t.DisplayName, &t.AvatarHash, &t.Relation); err != nil {
			return out, fmt.Errorf("scan target: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
