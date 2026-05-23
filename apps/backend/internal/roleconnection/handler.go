package roleconnection

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/oauth/discord"
	"rustlens/backend/internal/store"
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

// Callback handles GET /api/auth/discord/role-connection/callback.
// Validates the per-flow cookies, exchanges the code, confirms the linking
// Discord account matches the session user, stores the encrypted tokens,
// and synchronously pushes the current PRO metadata.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	defer func() {
		clearCookie(w, discord.CookieRCState)
		clearCookie(w, discord.CookieRCVerifier)
		clearCookie(w, discord.CookieRCReturn)
	}()

	session := appauth.SessionFromContext(r.Context())
	if session == nil {
		http.Redirect(w, r, h.frontendURL("/login?notice=session-required"), http.StatusFound)
		return
	}

	stateCookie, _ := r.Cookie(discord.CookieRCState)
	verifierCookie, _ := r.Cookie(discord.CookieRCVerifier)
	queryState := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if stateCookie == nil || verifierCookie == nil ||
		queryState == "" || code == "" || queryState != stateCookie.Value {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-state"), http.StatusFound)
		return
	}

	ctx := r.Context()
	access, refresh, expiresIn, err := h.cfg.DiscordAPI.ExchangeCodeForTokens(ctx, code, verifierCookie.Value)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-token"), http.StatusFound)
		return
	}
	profile, err := h.cfg.DiscordAPI.FetchProfile(ctx, access)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-profile"), http.StatusFound)
		return
	}
	if profile.ID != session.DiscordID {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-mismatch"), http.StatusFound)
		return
	}

	cipher := h.cfg.Syncer.Cipher()
	accessCT, accessNonce, err := cipher.Encrypt(access)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-encrypt"), http.StatusFound)
		return
	}
	refreshCT, refreshNonce, err := cipher.Encrypt(refresh)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-encrypt"), http.StatusFound)
		return
	}
	if err := h.cfg.Syncer.Store().UpsertDiscordRoleConnection(ctx, store.DiscordRoleConnection{
		UserID:            session.UserID,
		DiscordID:         profile.ID,
		AccessTokenCT:     accessCT,
		AccessTokenNonce:  accessNonce,
		RefreshTokenCT:    refreshCT,
		RefreshTokenNonce: refreshNonce,
		AccessExpiresAt:   time.Now().Add(time.Duration(expiresIn) * time.Second),
		Scopes:            discord.RoleConnectionScopes,
	}); err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-persist"), http.StatusFound)
		return
	}

	// Immediate push so the role appears before the user's redirect lands.
	// Errors are logged but do not block the redirect — the reconciler will retry.
	if err := h.cfg.Syncer.Push(ctx, session.UserID); err != nil {
		slog.Warn("roleconnection initial push failed",
			slog.String("user_id", session.UserID), slog.Any("error", err))
	}

	http.Redirect(w, r, h.frontendURL("/dashboard?notice=discord-linked"), http.StatusFound)
}
