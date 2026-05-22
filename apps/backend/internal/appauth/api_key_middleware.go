package appauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/clientip"
	"rustlens/backend/internal/store"
)

const apiKeyAuthHeaderPrefix = "Bearer rl_admin_"
const apiKeyPrefixLen = 16
const lastUsedDebounce = 60 * time.Second

// UserLoader is the minimal interface the API-key middleware needs to check
// whether the key's creator is still active. active=false means the user is
// suspended or soft-deleted; the middleware auto-revokes the key in that case.
type UserLoader interface {
	LoadUserStatus(ctx context.Context, userID string) (active bool, suspended bool, deleted bool, err error)
}

// APIKeyAuth holds the state for the API-key authentication middleware.
type APIKeyAuth struct {
	store   *store.APIKeyStore
	users   UserLoader
	headers clientip.Headers
	mu      sync.Mutex
	recent  map[string]time.Time // keyID -> last debounced write
}

// NewAPIKeyAuth returns the middleware constructor. Both s and u may be nil
// only on code paths where no Authorization header is presented (pass-through).
func NewAPIKeyAuth(s *store.APIKeyStore, u UserLoader, h clientip.Headers) func(http.Handler) http.Handler {
	a := &APIKeyAuth{store: s, users: u, headers: h, recent: map[string]time.Time{}}
	go a.pruneLoop()
	return a.handler
}

func (a *APIKeyAuth) pruneLoop() {
	t := time.NewTicker(lastUsedDebounce)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-lastUsedDebounce)
		a.mu.Lock()
		for id, last := range a.recent {
			if last.Before(cutoff) {
				delete(a.recent, id)
			}
		}
		a.mu.Unlock()
	}
}

func (a *APIKeyAuth) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := r.Header.Get("Authorization")
		if !strings.HasPrefix(hdr, apiKeyAuthHeaderPrefix) {
			next.ServeHTTP(w, r)
			return
		}
		secret := strings.TrimPrefix(hdr, "Bearer ")
		if len(secret) < apiKeyPrefixLen {
			writeJSONError(w, http.StatusUnauthorized, "invalid_api_key")
			return
		}
		prefix := secret[:apiKeyPrefixLen]

		ctx := r.Context()
		key, err := a.store.GetByPrefix(ctx, prefix)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "invalid_api_key")
			return
		}

		presented := sha256.Sum256([]byte(secret))
		if subtle.ConstantTimeCompare(presented[:], key.SecretHash) != 1 {
			writeJSONError(w, http.StatusUnauthorized, "invalid_api_key")
			return
		}
		if key.RevokedAt != nil {
			writeJSONError(w, http.StatusUnauthorized, "api_key_revoked")
			return
		}
		if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
			writeJSONError(w, http.StatusUnauthorized, "api_key_expired")
			return
		}

		active, suspended, deleted, err := a.users.LoadUserStatus(ctx, key.CreatedByUserID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal")
			return
		}
		if !active {
			reason := "creator_suspended"
			if deleted {
				reason = "creator_deleted"
			} else if !suspended {
				reason = "creator_inactive"
			}
			_ = a.store.Revoke(context.Background(), key.ID, reason)
			writeJSONError(w, http.StatusUnauthorized, "api_key_revoked")
			return
		}

		ctx = actions.WithAPIKeyAuth(ctx, &actions.APIKeyAuthContext{
			KeyID:          key.ID,
			KeyName:        key.Name,
			UserID:         key.CreatedByUserID,
			KeyPermissions: key.Permissions,
		})

		// Best-effort debounced last-used update.
		a.maybeUpdateLastUsed(key.ID, clientip.Extract(r, a.headers))

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *APIKeyAuth) maybeUpdateLastUsed(keyID, ip string) {
	a.mu.Lock()
	last, ok := a.recent[keyID]
	if ok && time.Since(last) < lastUsedDebounce {
		a.mu.Unlock()
		return
	}
	a.recent[keyID] = time.Now()
	a.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.store.UpdateLastUsed(ctx, keyID, ip)
	}()
}

