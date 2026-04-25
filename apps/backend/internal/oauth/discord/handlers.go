package discord

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/appauth"
)

// HandlerConfig wires runtime dependencies into the OAuth handlers.
type HandlerConfig struct {
	ClientID      string
	ClientSecret  string
	AppOrigin     string
	SessionSecret string
	Repo          *appauth.Repo
	Pool          *pgxpool.Pool
	Client        *Client // optional — when nil, NewHandler builds one from ClientID/ClientSecret
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
	// Refuse if a valid session cookie is already present.
	if _, err := appauth.SessionFromCookie(r, h.cfg.SessionSecret); err == nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-already-signed-in"), http.StatusFound)
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
	q.Set("scope", "identify")
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
	if err := h.cfg.Repo.TouchLastLogin(ctx, userID); err != nil {
		// Log-and-continue: failing to touch last_login_at must not block sign-in.
		log.Printf("discord callback: TouchLastLogin(%s): %v", userID, err)
	}
	h.mintAndRedirect(w, r, userID, "/dashboard")
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
		Epoch:       cu.User.SessionEpoch,
		ExpiresAt:   time.Now().Add(14 * 24 * time.Hour).UnixMilli(),
	}
	if cu.DiscordAccount != nil {
		session.DiscordID = cu.DiscordAccount.ProviderAccountID
	}
	if cu.PrimaryEmail != nil {
		session.Email = cu.PrimaryEmail.Email
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
		SameSite: http.SameSiteLaxMode,
	})
}
