package discord

import (
	"net/http"
	"net/url"

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
