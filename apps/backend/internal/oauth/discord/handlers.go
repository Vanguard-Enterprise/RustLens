package discord

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/appauth"
)

// GuildSyncer reconciles the caller's user_guilds rows against an authoritative
// list of guild IDs (typically from Discord's /users/@me/guilds endpoint).
type GuildSyncer interface {
	SyncUserGuildsForUser(ctx context.Context, discordID string, guildIDs []string) error
}

// HandlerConfig wires runtime dependencies into the OAuth handlers.
type HandlerConfig struct {
	ClientID      string
	ClientSecret  string
	AppOrigin     string
	SessionSecret string
	Repo          *appauth.Repo
	Pool          *pgxpool.Pool
	GuildSyncer   GuildSyncer // optional — when nil, callback skips the user-guild sync
	Client        *Client     // optional — when nil, NewHandler builds one from ClientID/ClientSecret
}

type Handler struct {
	cfg    HandlerConfig
	client *Client
}

func NewHandler(cfg HandlerConfig) *Handler {
	c := cfg.Client
	if c == nil {
		c = NewClient(cfg.ClientID, cfg.ClientSecret, WithRedirectURI(RedirectURI(cfg.AppOrigin)))
	}
	return &Handler{cfg: cfg, client: c}
}

func (h *Handler) frontendURL(path string) string {
	return strings.TrimRight(h.cfg.AppOrigin, "/") + path
}

// Initiate handles GET /api/auth/discord (sign-in).
func (h *Handler) Initiate(w http.ResponseWriter, r *http.Request) {
	// Already signed in — send to dashboard rather than back to /login,
	// which would loop if /login auto-redirects here.
	if _, err := appauth.SessionFromCookie(r, h.cfg.SessionSecret); err == nil {
		http.Redirect(w, r, h.frontendURL("/dashboard"), http.StatusFound)
		return
	}
	h.beginFlow(w, r)
}

func (h *Handler) beginFlow(w http.ResponseWriter, r *http.Request) {
	if h.cfg.ClientID == "" {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-config"), http.StatusFound)
		return
	}
	state, err := NewState()
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-config"), http.StatusFound)
		return
	}
	verifier, err := NewPkceVerifier()
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-config"), http.StatusFound)
		return
	}

	setShortCookie(w, CookieState, state)
	setShortCookie(w, CookieVerifier, verifier)

	authorize, _ := url.Parse("https://discord.com/oauth2/authorize")
	q := authorize.Query()
	q.Set("client_id", h.cfg.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", RedirectURI(h.cfg.AppOrigin))
	q.Set("scope", "identify guilds")
	q.Set("prompt", "consent")
	q.Set("state", state)
	q.Set("code_challenge", PkceChallenge(verifier))
	q.Set("code_challenge_method", "S256")
	authorize.RawQuery = q.Encode()

	http.Redirect(w, r, authorize.String(), http.StatusFound)
}

func setShortCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   CookieMaxAgeSeconds,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// Callback handles GET /api/auth/discord/callback. Validates the state cookie,
// exchanges the code with Discord, fetches the profile, then finds-or-creates
// the user and mints a session.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	// Always clear the per-flow cookies on the way out, regardless of outcome.
	defer func() {
		clearCookie(w, CookieState)
		clearCookie(w, CookieVerifier)
	}()

	stateCookie, _ := r.Cookie(CookieState)
	verifierCookie, _ := r.Cookie(CookieVerifier)
	queryState := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if stateCookie == nil || verifierCookie == nil ||
		queryState == "" || code == "" || queryState != stateCookie.Value {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-state"), http.StatusFound)
		return
	}

	ctx := r.Context()
	accessToken, err := h.client.ExchangeCode(ctx, code, verifierCookie.Value)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-token"), http.StatusFound)
		return
	}
	profile, err := h.client.FetchProfile(ctx, accessToken)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-profile"), http.StatusFound)
		return
	}

	dp := appauth.DiscordProfile{
		ID:         profile.ID,
		Username:   profile.Username,
		GlobalName: profile.GlobalName,
		AvatarHash: profile.AvatarHash,
	}
	userID, _, err := h.cfg.Repo.FindOrCreateByDiscordID(ctx, dp)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
		return
	}
	if notice, blocked, err := h.checkAccountStatus(ctx, userID); err != nil {
		log.Printf("discord callback: account status(%s): %v", userID, err)
	} else if blocked {
		http.Redirect(w, r, h.frontendURL("/login?notice="+notice), http.StatusFound)
		return
	}
	if err := h.cfg.Repo.TouchLastLogin(ctx, userID); err != nil {
		// Log-and-continue: failing to touch last_login_at must not block sign-in.
		log.Printf("discord callback: TouchLastLogin(%s): %v", userID, err)
	}
	if h.cfg.GuildSyncer != nil {
		guilds, err := h.client.FetchUserGuilds(ctx, accessToken)
		if err != nil {
			// Log-and-continue: a stale user_guilds list must not block sign-in.
			log.Printf("discord callback: FetchUserGuilds(%s): %v", profile.ID, err)
		} else {
			ids := make([]string, 0, len(guilds))
			for _, g := range guilds {
				ids = append(ids, g.ID)
			}
			if err := h.cfg.GuildSyncer.SyncUserGuildsForUser(ctx, profile.ID, ids); err != nil {
				log.Printf("discord callback: SyncUserGuildsForUser(%s): %v", profile.ID, err)
			}
		}
	}
	h.mintAndRedirect(w, r, userID, "/dashboard")
}

// checkAccountStatus inspects suspended_at/deleted_at for a freshly-resolved
// app_user and returns a /login?notice= slug when the account should be
// rejected at sign-in. Mirrors ValidateSession's later-render guard so a
// suspended user sees the ban notice instead of being redirected into a
// dashboard that immediately bounces them back out.
func (h *Handler) checkAccountStatus(ctx context.Context, userID string) (string, bool, error) {
	if h.cfg.Pool == nil {
		return "", false, nil
	}
	var (
		suspendedAt *time.Time
		deletedAt   *time.Time
	)
	err := h.cfg.Pool.QueryRow(ctx,
		`SELECT suspended_at, deleted_at FROM app_users WHERE id = $1`,
		userID,
	).Scan(&suspendedAt, &deletedAt)
	if err != nil {
		return "", false, err
	}
	if deletedAt != nil {
		return "deleted", true, nil
	}
	if suspendedAt != nil {
		return "suspended", true, nil
	}
	return "", false, nil
}

func (h *Handler) mintAndRedirect(w http.ResponseWriter, r *http.Request, userID string, target string) {
	cu, err := h.cfg.Repo.LoadCurrentUser(r.Context(), userID)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
		return
	}
	session := appauth.SessionUser{
		UserID:      cu.User.ID,
		DisplayName: cu.User.DisplayName,
		AvatarURL:   cu.User.AvatarURL,
		Roles:       cu.User.Roles,
		AdminRoles:  cu.AdminRoles,
		Permissions: cu.Permissions,
		DiscordID:   cu.User.DiscordID,
		Epoch:       cu.User.SessionEpoch,
		ExpiresAt:   time.Now().Add(14 * 24 * time.Hour).UnixMilli(),
	}
	tok, err := appauth.SerializeSession(session, h.cfg.SessionSecret)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     appauth.SessionCookieName,
		Value:    tok,
		Path:     "/",
		MaxAge:   60 * 60 * 24 * 14,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, h.frontendURL(target), http.StatusFound)
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}
