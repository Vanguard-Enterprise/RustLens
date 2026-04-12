package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrAppUserNotFound   = errors.New("app user not found")
	ErrEmailTokenInvalid = errors.New("email token invalid")
	ErrEmailNotLinked    = errors.New("email not linked")
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

type UserEmail struct {
	Email      string
	IsPrimary  bool
	IsVerified bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
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
	Emails         []UserEmail
	PrimaryEmail   *UserEmail
	SteamAccounts  []LinkedSteamAccount
	Guilds         []UserGuildMembership
}

type DiscordAuthInput struct {
	DiscordID           string
	Username            string
	DisplayName         string
	AvatarURL           string
	Email               string
	EmailVerified       bool
	AssignInternalAdmin bool
}

type AdminUserListItem struct {
	User                 AppUser
	DiscordID            string
	DiscordUsername      string
	PrimaryEmail         string
	GuildCount           int
	SteamConnectionCount int
}

type AdminGuildOverview struct {
	GuildID        string
	ActiveMembers  int
	LinkedMembers  int
	TrackedServers int
	AdminRoleID    string
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

func scanConnectedAccount(scanner rowScanner) (ConnectedAccount, error) {
	var record ConnectedAccount
	err := scanner.Scan(
		&record.Provider,
		&record.ProviderAccountID,
		&record.Username,
		&record.DisplayName,
		&record.AvatarURL,
		&record.ProfileEmail,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return ConnectedAccount{}, err
	}
	return record, nil
}

func scanUserEmail(scanner rowScanner) (UserEmail, error) {
	var record UserEmail
	err := scanner.Scan(
		&record.Email,
		&record.IsPrimary,
		&record.IsVerified,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return UserEmail{}, err
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
		&record.PrimaryEmail,
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

func scanAdminGuildOverview(scanner rowScanner) (AdminGuildOverview, error) {
	var record AdminGuildOverview
	err := scanner.Scan(
		&record.GuildID,
		&record.ActiveMembers,
		&record.LinkedMembers,
		&record.TrackedServers,
		&record.AdminRoleID,
	)
	if err != nil {
		return AdminGuildOverview{}, err
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

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func generateAppUserID() (string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("usr_%s", hex.EncodeToString(bytes)), nil
}

func hashEmailToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

func generateEmailToken() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
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

func (p *PostgresStore) UpsertDiscordAccount(ctx context.Context, input DiscordAuthInput) (CurrentUser, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return CurrentUser{}, err
	}
	defer tx.Rollback(ctx)

	normalizedEmail := normalizeEmail(input.Email)

	var userID string
	err = tx.QueryRow(ctx, `
		SELECT user_id
		FROM user_connected_accounts
		WHERE provider = 'discord' AND provider_account_id = $1
	`, input.DiscordID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		userID = ""
		err = nil
	}
	if err != nil {
		return CurrentUser{}, err
	}

	if userID == "" && normalizedEmail != "" {
		err = tx.QueryRow(ctx, `
			SELECT user_id
			FROM user_emails
			WHERE email = $1
		`, normalizedEmail).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			userID = ""
			err = nil
		}
		if err != nil {
			return CurrentUser{}, err
		}
	}

	var roles []string
	if userID == "" {
		userID, err = generateAppUserID()
		if err != nil {
			return CurrentUser{}, err
		}
		roles = ensureRoleSet(nil, input.AssignInternalAdmin)
		encodedRoles, err := encodeStringSlice(roles)
		if err != nil {
			return CurrentUser{}, err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO app_users (id, display_name, avatar_url, roles, last_login_at)
			VALUES ($1, $2, $3, $4::jsonb, NOW())
		`, userID, input.DisplayName, input.AvatarURL, encodedRoles)
		if err != nil {
			return CurrentUser{}, err
		}
	} else {
		existingUser, err := scanAppUser(tx.QueryRow(ctx, `
			SELECT id, display_name, avatar_url, roles, created_at, updated_at, last_login_at
			FROM app_users
			WHERE id = $1
		`, userID))
		if err != nil {
			return CurrentUser{}, err
		}
		roles = ensureRoleSet(existingUser.Roles, input.AssignInternalAdmin)
		encodedRoles, err := encodeStringSlice(roles)
		if err != nil {
			return CurrentUser{}, err
		}
		_, err = tx.Exec(ctx, `
			UPDATE app_users
			SET display_name = $2,
				avatar_url = $3,
				roles = $4::jsonb,
				updated_at = NOW(),
				last_login_at = NOW()
			WHERE id = $1
		`, userID, input.DisplayName, input.AvatarURL, encodedRoles)
		if err != nil {
			return CurrentUser{}, err
		}
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO user_connected_accounts (
			user_id,
			provider,
			provider_account_id,
			username,
			display_name,
			avatar_url,
			profile_email
		)
		VALUES ($1, 'discord', $2, $3, $4, $5, $6)
		ON CONFLICT (provider, provider_account_id) DO UPDATE
		SET user_id = EXCLUDED.user_id,
			username = EXCLUDED.username,
			display_name = EXCLUDED.display_name,
			avatar_url = EXCLUDED.avatar_url,
			profile_email = EXCLUDED.profile_email,
			updated_at = NOW()
	`, userID, input.DiscordID, input.Username, input.DisplayName, input.AvatarURL, normalizedEmail)
	if err != nil {
		return CurrentUser{}, err
	}

	if normalizedEmail != "" {
		_, err = tx.Exec(ctx, `
			UPDATE user_emails
			SET is_primary = FALSE,
				updated_at = NOW()
			WHERE user_id = $1
		`, userID)
		if err != nil {
			return CurrentUser{}, err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO user_emails (user_id, email, is_primary, is_verified)
			VALUES ($1, $2, TRUE, $3)
			ON CONFLICT (email) DO UPDATE
			SET user_id = EXCLUDED.user_id,
				is_primary = TRUE,
				is_verified = user_emails.is_verified OR EXCLUDED.is_verified,
				updated_at = NOW()
		`, userID, normalizedEmail, input.EmailVerified)
		if err != nil {
			return CurrentUser{}, err
		}
	}

	if err := p.insertAuditEventTx(ctx, tx, userID, userID, "auth.discord.login", map[string]any{
		"discordId": input.DiscordID,
		"email":     normalizedEmail,
	}); err != nil {
		return CurrentUser{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return CurrentUser{}, err
	}

	return p.GetCurrentUserByID(ctx, userID)
}

func (p *PostgresStore) CreateEmailSignInToken(ctx context.Context, email string, ttl time.Duration) (string, bool, error) {
	normalizedEmail := normalizeEmail(email)
	if normalizedEmail == "" {
		return "", false, ErrEmailNotLinked
	}

	var userID string
	err := p.pool.QueryRow(ctx, `
		SELECT user_id
		FROM user_emails
		WHERE email = $1
	`, normalizedEmail).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	rawToken, err := generateEmailToken()
	if err != nil {
		return "", false, err
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO email_sign_in_tokens (token_hash, email, expires_at)
		VALUES ($1, $2, $3)
	`, hashEmailToken(rawToken), normalizedEmail, time.Now().Add(ttl))
	if err != nil {
		return "", false, err
	}

	if err := p.InsertAppAuditEvent(ctx, userID, userID, "auth.email.request", map[string]any{
		"email": normalizedEmail,
	}); err != nil {
		return "", false, err
	}

	return rawToken, true, nil
}

func (p *PostgresStore) ConsumeEmailSignInToken(ctx context.Context, rawToken string) (CurrentUser, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return CurrentUser{}, err
	}
	defer tx.Rollback(ctx)

	var userID string
	err = tx.QueryRow(ctx, `
		SELECT ue.user_id
		FROM email_sign_in_tokens est
		INNER JOIN user_emails ue ON ue.email = est.email
		WHERE est.token_hash = $1
			AND est.consumed_at IS NULL
			AND est.expires_at > NOW()
	`, hashEmailToken(rawToken)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CurrentUser{}, ErrEmailTokenInvalid
	}
	if err != nil {
		return CurrentUser{}, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE email_sign_in_tokens
		SET consumed_at = NOW()
		WHERE token_hash = $1
	`, hashEmailToken(rawToken))
	if err != nil {
		return CurrentUser{}, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE app_users
		SET updated_at = NOW(),
			last_login_at = NOW()
		WHERE id = $1
	`, userID)
	if err != nil {
		return CurrentUser{}, err
	}

	if err := p.insertAuditEventTx(ctx, tx, userID, userID, "auth.email.login", map[string]any{}); err != nil {
		return CurrentUser{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return CurrentUser{}, err
	}

	return p.GetCurrentUserByID(ctx, userID)
}

func (p *PostgresStore) GetCurrentUserByID(ctx context.Context, userID string) (CurrentUser, error) {
	user, err := p.GetAppUserByID(ctx, userID)
	if err != nil {
		return CurrentUser{}, err
	}

	current := CurrentUser{
		User:          user,
		Emails:        []UserEmail{},
		SteamAccounts: []LinkedSteamAccount{},
		Guilds:        []UserGuildMembership{},
	}

	discordAccount, err := scanConnectedAccount(p.pool.QueryRow(ctx, `
		SELECT provider, provider_account_id, username, display_name, avatar_url, profile_email, created_at, updated_at
		FROM user_connected_accounts
		WHERE user_id = $1 AND provider = 'discord'
	`, userID))
	if err == nil {
		current.DiscordAccount = &discordAccount
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return CurrentUser{}, err
	}

	emailRows, err := p.pool.Query(ctx, `
		SELECT email, is_primary, is_verified, created_at, updated_at
		FROM user_emails
		WHERE user_id = $1
		ORDER BY is_primary DESC, updated_at DESC
	`, userID)
	if err != nil {
		return CurrentUser{}, err
	}
	defer emailRows.Close()

	for emailRows.Next() {
		record, err := scanUserEmail(emailRows)
		if err != nil {
			return CurrentUser{}, err
		}
		current.Emails = append(current.Emails, record)
		if current.PrimaryEmail == nil && record.IsPrimary {
			primary := record
			current.PrimaryEmail = &primary
		}
	}
	if err := emailRows.Err(); err != nil {
		return CurrentUser{}, err
	}
	if current.PrimaryEmail == nil && len(current.Emails) > 0 {
		primary := current.Emails[0]
		current.PrimaryEmail = &primary
	}

	if current.DiscordAccount == nil {
		return current, nil
	}

	steamRows, err := p.pool.Query(ctx, `
		SELECT DISTINCT sa.steam_id,
			COALESCE(sa.bm_id, ''),
			COALESCE(sa.bm_name, ''),
			COALESCE(sa.avatar_image, ''),
			sdl.type,
			sdl.source
		FROM steam_discord_links sdl
		INNER JOIN steam_accounts sa ON sa.steam_id = sdl.steam_id
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
			COALESCE(d.provider_account_id, ''),
			COALESCE(d.username, ''),
			COALESCE(e.email, ''),
			COALESCE(g.guild_count, 0),
			COALESCE(s.steam_count, 0)
		FROM app_users u
		LEFT JOIN user_connected_accounts d
			ON d.user_id = u.id AND d.provider = 'discord'
		LEFT JOIN LATERAL (
			SELECT email
			FROM user_emails
			WHERE user_id = u.id
			ORDER BY is_primary DESC, is_verified DESC, updated_at DESC
			LIMIT 1
		) e ON TRUE
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS guild_count
			FROM user_guilds ug
			WHERE ug.discord_id = d.provider_account_id
				AND ug.is_active = TRUE
		) g ON TRUE
		LEFT JOIN LATERAL (
			SELECT COUNT(DISTINCT steam_id)::int AS steam_count
			FROM steam_discord_links sdl
			WHERE sdl.discord_id = d.provider_account_id
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

func (p *PostgresStore) ListAdminGuilds(ctx context.Context) ([]AdminGuildOverview, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT ug.guild_id,
			COUNT(*) FILTER (WHERE ug.is_active)::int AS active_members,
			COUNT(DISTINCT gsl.discord_id)::int AS linked_members,
			COUNT(DISTINCT gts.server_bm_id)::int AS tracked_servers,
			COALESCE(gs.admin_role_id, '') AS admin_role_id
		FROM user_guilds ug
		LEFT JOIN guild_steam_discord_links gsl
			ON gsl.guild_id = ug.guild_id
			AND gsl.discord_id = ug.discord_id
		LEFT JOIN guild_tracked_servers gts
			ON gts.guild_id = ug.guild_id
		LEFT JOIN guild_settings gs
			ON gs.guild_id = ug.guild_id
		GROUP BY ug.guild_id, gs.admin_role_id
		ORDER BY active_members DESC, ug.guild_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AdminGuildOverview, 0)
	for rows.Next() {
		record, err := scanAdminGuildOverview(rows)
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
