package discord

import (
	"net/http"
	"net/url"
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
	return h.cfg.AppOrigin + path
}

// Initiate handles GET /api/auth/discord (sign-in).
func (h *Handler) Initiate(w http.ResponseWriter, r *http.Request) {
	// Refuse if a valid session cookie is already present.
	if _, err := appauth.SessionFromCookie(r, h.cfg.SessionSecret); err == nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-already-signed-in"), http.StatusFound)
		return
	}
	h.beginFlow(w, r, IntentSignin)
}

// InitiateLink handles GET /api/auth/discord/link. Requires an active session
// (refuses with /login?error=session otherwise).
func (h *Handler) InitiateLink(w http.ResponseWriter, r *http.Request) {
	if _, err := appauth.SessionFromCookie(r, h.cfg.SessionSecret); err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=session"), http.StatusFound)
		return
	}
	h.beginFlow(w, r, IntentLink)
}

func (h *Handler) beginFlow(w http.ResponseWriter, r *http.Request, intent string) {
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
	setShortCookie(w, CookieIntent, intent)

	authorize, _ := url.Parse("https://discord.com/oauth2/authorize")
	q := authorize.Query()
	q.Set("client_id", h.cfg.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", RedirectURI(h.cfg.AppOrigin))
	q.Set("scope", "identify email")
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
// exchanges the code with Discord, fetches the profile, then branches on the
// intent cookie (sign-in vs link).
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	// Always clear the per-flow cookies on the way out, regardless of outcome.
	defer func() {
		clearCookie(w, CookieState)
		clearCookie(w, CookieVerifier)
		clearCookie(w, CookieIntent)
	}()

	stateCookie, _ := r.Cookie(CookieState)
	verifierCookie, _ := r.Cookie(CookieVerifier)
	intentCookie, _ := r.Cookie(CookieIntent)
	queryState := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if stateCookie == nil || verifierCookie == nil || intentCookie == nil ||
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

	switch intentCookie.Value {
	case IntentLink:
		h.callbackLink(w, r, profile)
	default:
		h.callbackSignin(w, r, profile)
	}
}

func (h *Handler) callbackSignin(w http.ResponseWriter, r *http.Request, profile Profile) {
	ctx := r.Context()
	repo := h.cfg.Repo

	dp := appauth.DiscordProfile{
		ID:          profile.ID,
		Username:    profile.Username,
		DisplayName: profile.DisplayName(),
		AvatarURL:   profile.AvatarURL,
		Email:       profile.Email,
	}

	// Step 1: existing Discord ID? Refresh + sign in.
	if userID, err := repo.FindUserByDiscordID(ctx, profile.ID); err == nil {
		if err := repo.RefreshDiscordConnection(ctx, userID, dp); err != nil {
			http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
			return
		}
		_ = repo.TouchLastLogin(ctx, userID)
		h.mintAndRedirect(w, r, userID, "/dashboard")
		return
	}

	// Step 2: no email or unverified email from Discord → reject.
	if profile.Email == "" || !profile.Verified {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-no-email"), http.StatusFound)
		return
	}

	// Step 3: existing user_emails row?
	existingUserID, isVerified, exists, err := repo.FindUserByEmail(ctx, profile.Email)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
		return
	}
	switch {
	case exists && isVerified:
		if err := repo.LinkDiscordToUser(ctx, existingUserID, dp); err != nil {
			http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
			return
		}
		_ = repo.TouchLastLogin(ctx, existingUserID)
		h.mintAndRedirect(w, r, existingUserID, "/dashboard")
	case exists && !isVerified:
		http.Redirect(w, r, h.frontendURL("/login?error=discord-email-in-use"), http.StatusFound)
	default:
		newID, err := repo.CreateDiscordUser(ctx, dp)
		if err != nil {
			http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
			return
		}
		h.mintAndRedirect(w, r, newID, "/dashboard")
	}
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

// callbackLink is implemented in Task 14. For now, a temporary stub so the
// file compiles while sign-in tests exercise the IntentSignin path.
func (h *Handler) callbackLink(w http.ResponseWriter, r *http.Request, profile Profile) {
	http.Redirect(w, r, h.frontendURL("/dashboard/settings"), http.StatusFound)
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
