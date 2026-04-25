package appauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type contextKey string

const (
	appSecretHeader  = "X-RustLens-App-Secret"
	sessionHeader    = "X-RustLens-Session"
	sessionCtxKey    = contextKey("appauth.session")
)

// RequireAppSecret rejects any request whose X-RustLens-App-Secret does not
// match the configured shared secret. It's a perimeter guard around the
// /api/app/* surface — only the frontend server should call these routes.
func RequireAppSecret(secret string) func(http.Handler) http.Handler {
	want := []byte(secret)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := []byte(r.Header.Get(appSecretHeader))
			if len(got) == 0 || subtle.ConstantTimeCompare(got, want) != 1 {
				writeJSONError(w, http.StatusUnauthorized, "app_secret_required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSession parses and verifies the session token on X-RustLens-Session
// and attaches it to the request context. Rejects missing / invalid tokens
// with 401. When pool is non-nil, also validates the embedded session epoch
// against app_users.session_epoch and rejects revoked sessions.
func RequireSession(sessionSecret string, pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get(sessionHeader)
			if token == "" {
				writeJSONError(w, http.StatusUnauthorized, "session_required")
				return
			}
			session, err := ValidateSession(r.Context(), token, sessionSecret, pool)
			if err != nil {
				if errors.Is(err, ErrSessionRevoked) {
					writeJSONError(w, http.StatusUnauthorized, "session_revoked")
					return
				}
				writeJSONError(w, http.StatusUnauthorized, "invalid_session")
				return
			}
			ctx := context.WithValue(r.Context(), sessionCtxKey, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SessionFromContext returns the session attached by RequireSession, if any.
func SessionFromContext(ctx context.Context) *SessionUser {
	s, _ := ctx.Value(sessionCtxKey).(*SessionUser)
	return s
}

// SessionCookieName is the cookie set by the frontend's setSession() and by
// the backend after a successful Discord OAuth sign-in. Mirrors
// SESSION_COOKIE in apps/frontend/lib/session.ts.
const SessionCookieName = "rustlens_session"

// SessionFromCookie reads and verifies the rustlens_session cookie on the
// given request. Returns ErrMalformedToken / ErrBadSignature / ErrExpired on
// invalid tokens, or http.ErrNoCookie when the cookie isn't present.
func SessionFromCookie(r *http.Request, secret string) (*SessionUser, error) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil, err
	}
	return ParseSession(c.Value, secret)
}

// ValidateSessionFromCookie reads the rustlens_session cookie and runs full
// validation including the session_epoch revocation check against the
// database. Returns the same errors as ValidateSession plus http.ErrNoCookie
// when the cookie is absent.
func ValidateSessionFromCookie(ctx context.Context, r *http.Request, secret string, pool *pgxpool.Pool) (*SessionUser, error) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil, err
	}
	return ValidateSession(ctx, c.Value, secret, pool)
}

// InjectSessionForTest is a test-only helper that attaches a synthetic
// SessionUser to a context. Production code paths always populate the session
// via RequireSession middleware; this helper lets internal handler tests
// exercise session-dependent code without a full session-issuing flow.
func InjectSessionForTest(ctx context.Context, s *SessionUser) context.Context {
	return context.WithValue(ctx, sessionCtxKey, s)
}
