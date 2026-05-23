package roleconnection

import (
	"net/http"
	"net/url"
	"strings"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/oauth/discord"
)

// HandlerConfig wires runtime dependencies for the OAuth + disconnect handlers.
type HandlerConfig struct {
	ClientID     string
	ClientSecret string
	AppOrigin    string
	Syncer       *Syncer
	DiscordAPI   *discord.Client
}

type Handler struct {
	cfg HandlerConfig
}

func NewHandler(cfg HandlerConfig) *Handler { return &Handler{cfg: cfg} }

func (h *Handler) frontendURL(path string) string {
	return strings.TrimRight(h.cfg.AppOrigin, "/") + path
}

// Initiate handles GET /api/auth/discord/role-connection.
// Requires a valid session; sets short-lived PKCE/state/return cookies; 302s to Discord.
func (h *Handler) Initiate(w http.ResponseWriter, r *http.Request) {
	if appauth.SessionFromContext(r.Context()) == nil {
		http.Redirect(w, r, h.frontendURL("/login?notice=session-required"), http.StatusFound)
		return
	}
	if h.cfg.ClientID == "" {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-config"), http.StatusFound)
		return
	}
	state, err := discord.NewState()
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-config"), http.StatusFound)
		return
	}
	verifier, err := discord.NewPkceVerifier()
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-config"), http.StatusFound)
		return
	}
	returnTo := r.URL.Query().Get("return")
	if returnTo == "" {
		returnTo = "dashboard"
	}

	setShortCookie(w, discord.CookieRCState, state)
	setShortCookie(w, discord.CookieRCVerifier, verifier)
	setShortCookie(w, discord.CookieRCReturn, returnTo)

	authorize, _ := url.Parse("https://discord.com/oauth2/authorize")
	q := authorize.Query()
	q.Set("client_id", h.cfg.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", discord.RoleConnectionRedirectURI(h.cfg.AppOrigin))
	q.Set("scope", discord.RoleConnectionScopes)
	q.Set("prompt", "consent")
	q.Set("state", state)
	q.Set("code_challenge", discord.PkceChallenge(verifier))
	q.Set("code_challenge_method", "S256")
	authorize.RawQuery = q.Encode()

	http.Redirect(w, r, authorize.String(), http.StatusFound)
}

func setShortCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   600, // 10 minutes
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}

// Ensure clearCookie is referenced so it is available for Callback (Task 17)
// without triggering a compile error. Package-level functions are never flagged
// as unused by the Go compiler, so no sentinel reference is needed.
var _ = clearCookie
