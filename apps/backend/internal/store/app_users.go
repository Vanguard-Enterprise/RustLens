package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrAppUserNotFound = errors.New("app user not found")
)

type AppUser struct {
	ID          string
	DisplayName string
	AvatarURL   string
	Roles       []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastLoginAt time.Time
}

type ConnectedAccount struct {
	Provider          string
	ProviderAccountID string
	Username          string
	DisplayName       string
	AvatarURL         string
	ProfileEmail      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type LinkedSteamAccount struct {
	SteamID     string
	BmID        string
	BmName      string
	AvatarImage string
	LinkType    string
	Source      string
}

type UserGuildMembership struct {
	GuildID     string
	IsActive    bool
	FirstSeenAt time.Time
	LastSeenAt  time.Time
	LeftAt      *time.Time
}

type CurrentUser struct {
	User           AppUser
	DiscordAccount *ConnectedAccount
	SteamAccounts  []LinkedSteamAccount
	Guilds         []UserGuildMembership
}

type AdminUserListItem struct {
	User                 AppUser
	DiscordID            string
	DiscordUsername      string
	GuildCount           int
	SteamConnectionCount int
}

type AdminServerOverview struct {
	ServerBMID    string
	Name          string
	LastPolledAt  *time.Time
	LastPollError string
	GuildCount    int
}

type AuditEvent struct {
	ID           int64
	ActorUserID  string
	TargetUserID string
	EventType    string
	Payload      map[string]any
	CreatedAt    time.Time
}

func scanAppUser(scanner rowScanner) (AppUser, error) {
	var (
		record    AppUser
		rolesJSON []byte
	)
	err := scanner.Scan(
		&record.ID,
		&record.DisplayName,
		&record.AvatarURL,
		&rolesJSON,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.LastLoginAt,
	)
	if err != nil {
		return AppUser{}, err
	}

	record.Roles, err = decodeStringSlice(rolesJSON)
	if err != nil {
		return AppUser{}, err
	}

	return record, nil
}

func scanLinkedSteamAccount(scanner rowScanner) (LinkedSteamAccount, error) {
	var record LinkedSteamAccount
	err := scanner.Scan(
		&record.SteamID,
		&record.BmID,
		&record.BmName,
		&record.AvatarImage,
		&record.LinkType,
		&record.Source,
	)
	if err != nil {
		return LinkedSteamAccount{}, err
	}
	return record, nil
}

func scanUserGuildMembership(scanner rowScanner) (UserGuildMembership, error) {
	var record UserGuildMembership
	err := scanner.Scan(
		&record.GuildID,
		&record.IsActive,
		&record.FirstSeenAt,
		&record.LastSeenAt,
		&record.LeftAt,
	)
	if err != nil {
		return UserGuildMembership{}, err
	}
	return record, nil
}

func scanAdminUserListItem(scanner rowScanner) (AdminUserListItem, error) {
	var (
		record    AdminUserListItem
		rolesJSON []byte
	)
	err := scanner.Scan(
		&record.User.ID,
		&record.User.DisplayName,
		&record.User.AvatarURL,
		&rolesJSON,
		&record.User.CreatedAt,
		&record.User.UpdatedAt,
		&record.User.LastLoginAt,
		&record.DiscordID,
		&record.DiscordUsername,
		&record.GuildCount,
		&record.SteamConnectionCount,
	)
	if err != nil {
		return AdminUserListItem{}, err
	}
	record.User.Roles, err = decodeStringSlice(rolesJSON)
	if err != nil {
		return AdminUserListItem{}, err
	}
	return record, nil
}

func scanAdminServerOverview(scanner rowScanner) (AdminServerOverview, error) {
	var record AdminServerOverview
	err := scanner.Scan(
		&record.ServerBMID,
		&record.Name,
		&record.LastPolledAt,
		&record.LastPollError,
		&record.GuildCount,
	)
	if err != nil {
		return AdminServerOverview{}, err
	}
	return record, nil
}

func scanAuditEvent(scanner rowScanner) (AuditEvent, error) {
	var (
		record      AuditEvent
		payloadJSON []byte
	)
	err := scanner.Scan(
		&record.ID,
		&record.ActorUserID,
		&record.TargetUserID,
		&record.EventType,
		&payloadJSON,
		&record.CreatedAt,
	)
	if err != nil {
		return AuditEvent{}, err
	}

	if len(payloadJSON) == 0 {
		record.Payload = map[string]any{}
		return record, nil
	}

	if err := json.Unmarshal(payloadJSON, &record.Payload); err != nil {
		return AuditEvent{}, err
	}

	return record, nil
}

func generateAppUserID() (string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("usr_%s", hex.EncodeToString(bytes)), nil
}

func ensureRoleSet(roles []string, assignInternalAdmin bool) []string {
	set := map[string]struct{}{
		"user": {},
	}
	for _, role := range roles {
		if strings.TrimSpace(role) == "" {
			continue
		}
		set[role] = struct{}{}
	}
	if assignInternalAdmin {
		set["internal_admin"] = struct{}{}
	}

	result := make([]string, 0, len(set))
	result = append(result, "user")
	if _, ok := set["internal_admin"]; ok {
		result = append(result, "internal_admin")
	}
	return result
}

func (p *PostgresStore) GetAppUserByID(ctx context.Context, userID string) (AppUser, error) {
	record, err := scanAppUser(p.pool.QueryRow(ctx, `
		SELECT id, display_name, avatar_url, roles, created_at, updated_at, last_login_at
		FROM app_users
		WHERE id = $1
	`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return AppUser{}, ErrAppUserNotFound
	}
	return record, err
}

// GetAppUserDiscordID returns the discord_id stored directly on app_users.
// The live auth subsystem (internal/appauth) writes the Discord snowflake
// here on sign-in; user_connected_accounts is unused in the live schema.
func (p *PostgresStore) GetAppUserDiscordID(ctx context.Context, userID string) (string, error) {
	var discordID string
	err := p.pool.QueryRow(ctx, `
		SELECT discord_id FROM app_users WHERE id = $1
	`, userID).Scan(&discordID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAppUserNotFound
	}
	return discordID, err
}

func (p *PostgresStore) GetCurrentUserByID(ctx context.Context, userID string) (CurrentUser, error) {
	user, err := p.GetAppUserByID(ctx, userID)
	if err != nil {
		return CurrentUser{}, err
	}

	current := CurrentUser{
		User:          user,
		SteamAccounts: []LinkedSteamAccount{},
		Guilds:        []UserGuildMembership{},
	}

	var (
		discordID         string
		discordUsername   string
		discordGlobalName *string
		discordAvatar     *string
	)
	err = p.pool.QueryRow(ctx, `
		SELECT discord_id, discord_username, discord_global_name, discord_avatar
		FROM app_users
		WHERE id = $1
	`, userID).Scan(&discordID, &discordUsername, &discordGlobalName, &discordAvatar)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return CurrentUser{}, err
	}
	if discordID != "" {
		account := ConnectedAccount{
			Provider:          "discord",
			ProviderAccountID: discordID,
			Username:          discordUsername,
			DisplayName:       user.DisplayName,
			AvatarURL:         user.AvatarURL,
			CreatedAt:         user.CreatedAt,
			UpdatedAt:         user.UpdatedAt,
		}
		if discordGlobalName != nil && *discordGlobalName != "" {
			account.DisplayName = *discordGlobalName
		}
		current.DiscordAccount = &account
	}

	if current.DiscordAccount == nil {
		return current, nil
	}

	steamRows, err := p.pool.Query(ctx, `
		SELECT DISTINCT sa.steam_id,
			COALESCE(sa.bm_id, ''),
			COALESCE(bpc.bm_name, ''),
			COALESCE(sac.avatar_image, ''),
			sdl.type,
			sdl.source
		FROM steam_discord_links sdl
		INNER JOIN steam_accounts sa ON sa.steam_id = sdl.steam_id
		LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
		LEFT JOIN steam_account_cache sac ON sac.steam_id = sa.steam_id
		WHERE sdl.discord_id = $1
		ORDER BY sa.steam_id
	`, current.DiscordAccount.ProviderAccountID)
	if err != nil {
		return CurrentUser{}, err
	}
	defer steamRows.Close()

	for steamRows.Next() {
		record, err := scanLinkedSteamAccount(steamRows)
		if err != nil {
			return CurrentUser{}, err
		}
		current.SteamAccounts = append(current.SteamAccounts, record)
	}
	if err := steamRows.Err(); err != nil {
		return CurrentUser{}, err
	}

	guildRows, err := p.pool.Query(ctx, `
		SELECT guild_id, is_active, first_seen_at, last_seen_at, left_at
		FROM user_guilds
		WHERE discord_id = $1
		ORDER BY is_active DESC, last_seen_at DESC
	`, current.DiscordAccount.ProviderAccountID)
	if err != nil {
		return CurrentUser{}, err
	}
	defer guildRows.Close()

	for guildRows.Next() {
		record, err := scanUserGuildMembership(guildRows)
		if err != nil {
			return CurrentUser{}, err
		}
		current.Guilds = append(current.Guilds, record)
	}
	if err := guildRows.Err(); err != nil {
		return CurrentUser{}, err
	}

	return current, nil
}

func (p *PostgresStore) ListAdminUsers(ctx context.Context) ([]AdminUserListItem, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT u.id,
			u.display_name,
			u.avatar_url,
			u.roles,
			u.created_at,
			u.updated_at,
			u.last_login_at,
			u.discord_id,
			u.discord_username,
			COALESCE(g.guild_count, 0),
			COALESCE(s.steam_count, 0)
		FROM app_users u
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS guild_count
			FROM user_guilds ug
			WHERE ug.discord_id = u.discord_id
				AND ug.is_active = TRUE
		) g ON TRUE
		LEFT JOIN LATERAL (
			SELECT COUNT(DISTINCT steam_id)::int AS steam_count
			FROM steam_discord_links sdl
			WHERE sdl.discord_id = u.discord_id
		) s ON TRUE
		ORDER BY u.last_login_at DESC, u.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AdminUserListItem, 0)
	for rows.Next() {
		record, err := scanAdminUserListItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func (p *PostgresStore) ListAdminServers(ctx context.Context) ([]AdminServerOverview, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT ts.bm_id,
			ts.name,
			ts.last_polled_at,
			ts.last_poll_error,
			COUNT(DISTINCT gts.guild_id)::int AS guild_count
		FROM tracked_servers ts
		LEFT JOIN guild_tracked_servers gts
			ON gts.server_bm_id = ts.bm_id
		GROUP BY ts.bm_id, ts.name, ts.last_polled_at, ts.last_poll_error
		ORDER BY ts.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AdminServerOverview, 0)
	for rows.Next() {
		record, err := scanAdminServerOverview(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func (p *PostgresStore) ListAuditEvents(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := p.pool.Query(ctx, `
		SELECT id,
			COALESCE(actor_user_id, ''),
			COALESCE(target_user_id, ''),
			event_type,
			payload,
			created_at
		FROM app_audit_events
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AuditEvent, 0)
	for rows.Next() {
		record, err := scanAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func (p *PostgresStore) insertAuditEventTx(ctx context.Context, tx pgx.Tx, actorUserID string, targetUserID string, eventType string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO app_audit_events (actor_user_id, target_user_id, event_type, payload)
		VALUES ($1, $2, $3, $4::jsonb)
	`, actorUserID, targetUserID, eventType, string(encoded))
	return err
}

func (p *PostgresStore) InsertAppAuditEvent(ctx context.Context, actorUserID string, targetUserID string, eventType string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO app_audit_events (actor_user_id, target_user_id, event_type, payload)
		VALUES ($1, $2, $3, $4::jsonb)
	`, actorUserID, targetUserID, eventType, string(encoded))
	return err
}

// IsGuildMember returns true if the given Discord user is recorded as an
// active member of the given guild in user_guilds.
func (p *PostgresStore) IsGuildMember(ctx context.Context, guildID, discordID string) (bool, error) {
	if guildID == "" || discordID == "" {
		return false, nil
	}
	var exists bool
	err := p.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM user_guilds
			WHERE guild_id = $1
			  AND discord_id = $2
			  AND is_active = TRUE
		)
	`, guildID, discordID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
