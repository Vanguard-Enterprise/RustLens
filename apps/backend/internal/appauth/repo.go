package appauth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppUser mirrors the frontend AppUser type.
type AppUser struct {
	ID           string    `json:"id"`
	DisplayName  string    `json:"displayName"`
	AvatarURL    string    `json:"avatarUrl"`
	Roles        []string  `json:"roles"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	LastLoginAt  time.Time `json:"lastLoginAt"`
	SessionEpoch int       `json:"sessionEpoch"`
}

// UserEmail mirrors the frontend UserEmail type.
type UserEmail struct {
	Email      string    `json:"email"`
	IsPrimary  bool      `json:"isPrimary"`
	IsVerified bool      `json:"isVerified"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// ConnectedAccount mirrors the frontend ConnectedAccount type.
type ConnectedAccount struct {
	Provider          string    `json:"provider"`
	ProviderAccountID string    `json:"providerAccountId"`
	Username          string    `json:"username"`
	DisplayName       string    `json:"displayName"`
	AvatarURL         string    `json:"avatarUrl"`
	ProfileEmail      string    `json:"profileEmail"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
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

// CurrentUser is the payload returned by /api/app/auth/login and /api/app/me.
type CurrentUser struct {
	User           AppUser               `json:"user"`
	DiscordAccount *ConnectedAccount     `json:"discordAccount"`
	PrimaryEmail   *UserEmail            `json:"primaryEmail"`
	Emails         []UserEmail           `json:"emails"`
	SteamAccounts  []LinkedSteamAccount  `json:"steamAccounts"`
	Guilds         []UserGuildMembership `json:"guilds"`
	// AdminRoles are the role keys granted via admin_user_roles (e.g.
	// "superadmin", "moderator"). Separate from User.Roles which is the
	// legacy app_users.roles JSONB column.
	AdminRoles []string `json:"adminRoles"`
	// Permissions is the flattened set of permission keys resolved from
	// the user's admin roles. Superadmin implies all permissions.
	Permissions []string `json:"permissions"`
}

var (
	ErrUserNotFound        = errors.New("user not found")
	ErrInvalidCredentials  = errors.New("invalid credentials")
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// LookupPasswordLogin returns the user id and bcrypt hash for a given email,
// or ErrUserNotFound if no row exists. Email match is case-insensitive.
func (r *Repo) LookupPasswordLogin(ctx context.Context, email string) (userID, hash string, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	err = r.pool.QueryRow(ctx, `
		SELECT e.user_id, c.password_hash
		FROM user_emails e
		JOIN user_password_credentials c ON c.user_id = e.user_id
		WHERE lower(e.email) = $1
		LIMIT 1
	`, email).Scan(&userID, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrUserNotFound
	}
	return userID, hash, err
}

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
	emails, err := r.loadEmails(ctx, userID)
	if err != nil {
		return nil, err
	}
	var primary *UserEmail
	for i := range emails {
		if emails[i].IsPrimary {
			primary = &emails[i]
			break
		}
	}
	discord, err := r.loadDiscordAccount(ctx, userID)
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
		User:           *u,
		DiscordAccount: discord,
		PrimaryEmail:   primary,
		Emails:         emails,
		SteamAccounts:  []LinkedSteamAccount{},
		Guilds:         []UserGuildMembership{},
		AdminRoles:     adminRoles,
		Permissions:    permissions,
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
		SELECT id, display_name, avatar_url, roles, created_at, updated_at, last_login_at, session_epoch
		FROM app_users WHERE id = $1
	`, userID).Scan(&u.ID, &u.DisplayName, &u.AvatarURL, &rolesJSON, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &u.SessionEpoch)
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
	return &u, nil
}

func (r *Repo) loadEmails(ctx context.Context, userID string) ([]UserEmail, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT email, is_primary, is_verified, created_at, updated_at
		FROM user_emails WHERE user_id = $1 ORDER BY is_primary DESC, email
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserEmail{}
	for rows.Next() {
		var e UserEmail
		if err := rows.Scan(&e.Email, &e.IsPrimary, &e.IsVerified, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// FindUserByDiscordID returns the app_users.id linked to the given Discord
// account ID, or ErrUserNotFound when no row exists.
func (r *Repo) FindUserByDiscordID(ctx context.Context, discordID string) (string, error) {
	var userID string
	err := r.pool.QueryRow(ctx, `
		SELECT user_id FROM user_connected_accounts
		WHERE provider = 'discord' AND provider_account_id = $1
	`, discordID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserNotFound
	}
	return userID, err
}

// DiscordProfile is the subset of Discord OAuth profile data the appauth repo
// needs to upsert and refresh user_connected_accounts rows.
type DiscordProfile struct {
	ID          string
	Username    string
	DisplayName string
	AvatarURL   string
	Email       string
}

// RefreshDiscordConnection updates the connection row for a known Discord
// account. The user_id must already match; this is called on repeat sign-ins
// to keep username/display name/avatar/profile_email fresh.
func (r *Repo) RefreshDiscordConnection(ctx context.Context, userID string, p DiscordProfile) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE user_connected_accounts
		   SET username       = $3,
		       display_name   = $4,
		       avatar_url     = $5,
		       profile_email  = $6,
		       updated_at     = NOW()
		 WHERE user_id = $1
		   AND provider = 'discord'
		   AND provider_account_id = $2
	`, userID, p.ID, p.Username, p.DisplayName, p.AvatarURL, p.Email)
	return err
}

func (r *Repo) loadDiscordAccount(ctx context.Context, userID string) (*ConnectedAccount, error) {
	var a ConnectedAccount
	err := r.pool.QueryRow(ctx, `
		SELECT provider, provider_account_id,
		       COALESCE(username, ''), COALESCE(display_name, ''),
		       COALESCE(avatar_url, ''), COALESCE(profile_email, ''),
		       created_at, updated_at
		FROM user_connected_accounts
		WHERE user_id = $1 AND provider = 'discord'
		LIMIT 1
	`, userID).Scan(
		&a.Provider, &a.ProviderAccountID,
		&a.Username, &a.DisplayName,
		&a.AvatarURL, &a.ProfileEmail,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
