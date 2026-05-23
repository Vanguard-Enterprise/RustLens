package appauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/store"
)

// AppUser mirrors the frontend AppUser type. Discord identity now lives
// directly on app_users — there is no separate connected-account row.
type AppUser struct {
	ID                string    `json:"id"`
	DiscordID         string    `json:"discordId"`
	DiscordUsername   string    `json:"discordUsername"`
	DiscordGlobalName string    `json:"discordGlobalName,omitempty"`
	DiscordAvatar     string    `json:"discordAvatar,omitempty"` // Discord CDN avatar HASH, NOT a URL
	DisplayName       string    `json:"displayName"`
	AvatarURL         string    `json:"avatarUrl"`
	Roles             []string  `json:"roles"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
	LastLoginAt       time.Time `json:"lastLoginAt"`
	SessionEpoch      int       `json:"sessionEpoch"`
}

// LinkedSteamAccount mirrors the frontend LinkedSteamAccount type.
type LinkedSteamAccount struct {
	SteamID     string `json:"steamId"`
	BmID        string `json:"bmId"`
	BmName      string `json:"bmName"`
	AvatarImage string `json:"avatarImage"`
	Type        string `json:"type"`
	Source      string `json:"source"`
}

// UserGuildMembership mirrors the frontend UserGuildMembership type.
type UserGuildMembership struct {
	GuildID     string     `json:"guildId"`
	IsActive    bool       `json:"isActive"`
	FirstSeenAt time.Time  `json:"firstSeenAt"`
	LastSeenAt  time.Time  `json:"lastSeenAt"`
	LeftAt      *time.Time `json:"leftAt"`
}

// CurrentUser is the payload returned by /api/app/me.
type CurrentUser struct {
	User          AppUser               `json:"user"`
	SteamAccounts []LinkedSteamAccount  `json:"steamAccounts"`
	Guilds        []UserGuildMembership `json:"guilds"`
	// AdminRoles are the role keys granted via admin_user_roles (e.g.
	// "superadmin", "moderator"). Separate from User.Roles which is the
	// legacy app_users.roles JSONB column.
	AdminRoles []string `json:"adminRoles"`
	// Permissions is the flattened set of permission keys resolved from
	// the user's admin roles. Superadmin implies all permissions.
	Permissions []string `json:"permissions"`
	// DiscordRoleConnection is the safe dashboard summary for the linked-roles
	// feature. Omitted when there is no connection row or the store is not
	// wired up.
	DiscordRoleConnection *store.DiscordRoleConnectionSummary `json:"discordRoleConnection,omitempty"`
}

var (
	ErrUserNotFound         = errors.New("user not found")
	ErrDiscordAlreadyLinked = errors.New("user already has a discord connection")
	ErrDiscordLinkedToOther = errors.New("discord account is linked to another user")
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// TouchLastLogin updates app_users.last_login_at and returns nothing.
func (r *Repo) TouchLastLogin(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE app_users SET last_login_at = NOW() WHERE id = $1`, userID)
	return err
}

// LoadCurrentUser assembles the CurrentUser payload for a user id.
func (r *Repo) LoadCurrentUser(ctx context.Context, userID string) (*CurrentUser, error) {
	u, err := r.loadAppUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	adminRoles, err := r.loadAdminRoleKeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	permissions, err := r.loadPermissions(ctx, userID, adminRoles)
	if err != nil {
		return nil, err
	}
	// Steam + guilds populated as empty slices; join tables exist but the
	// shapes diverge from what this minimal implementation needs and the
	// frontend is tolerant of empty arrays.
	return &CurrentUser{
		User:          *u,
		SteamAccounts: []LinkedSteamAccount{},
		Guilds:        []UserGuildMembership{},
		AdminRoles:    adminRoles,
		Permissions:   permissions,
	}, nil
}

// loadAdminRoleKeys returns the admin role keys granted to a user via the
// admin_user_roles join table. Returns an empty slice if the user has no
// admin roles.
func (r *Repo) loadAdminRoleKeys(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ar.key
		FROM admin_user_roles ur
		JOIN admin_roles ar ON ar.id = ur.role_id
		WHERE ur.user_id = $1
		ORDER BY ar.key
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// loadPermissions returns the flattened permission-key set for a user based
// on their admin role grants. If the user holds the "superadmin" role, all
// registered permissions are returned. Returns an empty slice otherwise.
func (r *Repo) loadPermissions(ctx context.Context, userID string, adminRoles []string) ([]string, error) {
	for _, key := range adminRoles {
		if key == "superadmin" {
			rows, err := r.pool.Query(ctx, `SELECT key FROM admin_permissions ORDER BY key`)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := []string{}
			for rows.Next() {
				var k string
				if err := rows.Scan(&k); err != nil {
					return nil, err
				}
				out = append(out, k)
			}
			return out, rows.Err()
		}
	}
	if len(adminRoles) == 0 {
		return []string{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT rp.permission_key
		FROM admin_user_roles ur
		JOIN admin_role_permissions rp ON rp.role_id = ur.role_id
		WHERE ur.user_id = $1
		ORDER BY rp.permission_key
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *Repo) loadAppUser(ctx context.Context, userID string) (*AppUser, error) {
	var u AppUser
	var rolesJSON []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, discord_id, discord_username,
		       COALESCE(discord_global_name, ''), COALESCE(discord_avatar, ''),
		       display_name, avatar_url, roles,
		       created_at, updated_at, last_login_at, session_epoch
		FROM app_users WHERE id = $1
	`, userID).Scan(
		&u.ID, &u.DiscordID, &u.DiscordUsername,
		&u.DiscordGlobalName, &u.DiscordAvatar,
		&u.DisplayName, &u.AvatarURL, &rolesJSON,
		&u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &u.SessionEpoch,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(rolesJSON) > 0 {
		if err := json.Unmarshal(rolesJSON, &u.Roles); err != nil {
			return nil, err
		}
	}
	if u.Roles == nil {
		u.Roles = []string{}
	}
	if u.AvatarURL == "" && u.DiscordID != "" && u.DiscordAvatar != "" {
		u.AvatarURL = fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", u.DiscordID, u.DiscordAvatar)
	}
	return &u, nil
}

// DiscordProfile is the subset of Discord OAuth profile data the appauth repo
// needs to upsert and refresh app_users rows.
type DiscordProfile struct {
	ID         string
	Username   string
	GlobalName string
	AvatarHash string // Discord CDN avatar hash, NOT a URL
}

// FindOrCreateByDiscordID looks up or creates an app_users row keyed on
// Discord ID. On hit, the cached profile fields are refreshed and the
// existing id is returned with created=false. On miss, a new row is
// inserted with roles=["user"] and created=true. Atomic — safe under
// concurrent OAuth callbacks for the same Discord account.
func (r *Repo) FindOrCreateByDiscordID(ctx context.Context, p DiscordProfile) (string, bool, error) {
	const upsertSQL = `
		INSERT INTO app_users (id, discord_id, discord_username, discord_global_name, discord_avatar, display_name, roles)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), COALESCE(NULLIF($4, ''), $3), '["user"]'::jsonb)
		ON CONFLICT (discord_id) DO UPDATE
			SET discord_username    = EXCLUDED.discord_username,
			    discord_global_name = EXCLUDED.discord_global_name,
			    discord_avatar      = EXCLUDED.discord_avatar,
			    display_name        = COALESCE(NULLIF(app_users.display_name, ''), NULLIF(EXCLUDED.discord_global_name, ''), EXCLUDED.discord_username),
			    updated_at          = NOW()
		RETURNING id, (xmax = 0) AS created`

	newID, err := newUserID()
	if err != nil {
		return "", false, fmt.Errorf("generate user id: %w", err)
	}

	var (
		id      string
		created bool
	)
	if err := r.pool.QueryRow(ctx, upsertSQL, newID, p.ID, p.Username, p.GlobalName, p.AvatarHash).
		Scan(&id, &created); err != nil {
		return "", false, fmt.Errorf("upsert app_users: %w", err)
	}
	return id, created, nil
}

// ErrAppUserNotFound is returned by FindByDiscordID when no app_users row
// matches the given Discord ID. Callers should treat this as a soft miss —
// e.g. command_usage drops the write rather than blocking the bot.
var ErrAppUserNotFound = errors.New("app_users row not found for discord id")

// FindByDiscordID returns the app_users.id for a Discord snowflake without
// creating a row. Returns ErrAppUserNotFound if no row exists.
func (r *Repo) FindByDiscordID(ctx context.Context, discordID string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		SELECT id FROM app_users WHERE discord_id = $1
	`, discordID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrAppUserNotFound
		}
		return "", fmt.Errorf("find app_users by discord id: %w", err)
	}
	return id, nil
}

// newUserID returns a "usr_<24 hex>" identifier.
func newUserID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "usr_" + hex.EncodeToString(b[:]), nil
}


