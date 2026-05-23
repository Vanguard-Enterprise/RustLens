// Package roleconnection orchestrates Discord's linked-roles application
// metadata for the dashboard PRO role: storing per-user OAuth tokens,
// refreshing them, and pushing metadata so Discord re-evaluates the
// linked role membership. See docs/superpowers/plans/2026-05-23-discord-linked-roles-pro-sync.md.
package roleconnection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"rustlens/backend/internal/oauth/discord"
	"rustlens/backend/internal/store"
)

// PlatformName is the user-visible label shown in Discord's "Connections" UI
// for the linked account. Capped at 50 chars by Discord; keep it stable.
const PlatformName = "RustLens"

// BuildMetadata translates a user's PRO state into Discord's role-connection
// metadata. proGrantedAt nil => not PRO. Times are formatted as RFC3339 with
// numeric timezone offset (Discord rejects 'Z' suffix in some clients).
func BuildMetadata(platformUsername string, proGrantedAt *time.Time) discord.RoleConnectionBody {
	md := map[string]string{}
	if proGrantedAt != nil {
		md["is_pro"] = "1"
		md["pro_since"] = proGrantedAt.UTC().Format("2006-01-02T15:04:05-07:00")
	} else {
		md["is_pro"] = "0"
	}
	return discord.RoleConnectionBody{
		PlatformName:     PlatformName,
		PlatformUsername: platformUsername,
		Metadata:         md,
	}
}

// HashMetadata returns a stable sha256 hex digest of the JSON-marshaled body.
// Used to skip no-op pushes when the user's metadata is unchanged.
func HashMetadata(body discord.RoleConnectionBody) string {
	raw, _ := json.Marshal(body)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ---- Syncer ----

// SyncerStore is the subset of store calls the Syncer needs. Defined as an
// interface so the Syncer can be tested with a fake.
type SyncerStore interface {
	GetDiscordRoleConnection(ctx context.Context, userID string) (*store.DiscordRoleConnection, error)
	UpsertDiscordRoleConnection(ctx context.Context, in store.DiscordRoleConnection) error
	DeleteDiscordRoleConnection(ctx context.Context, userID string) error
	MarkDiscordRoleConnectionRevoked(ctx context.Context, userID, errMsg string) error
	RecordDiscordRoleConnectionPushed(ctx context.Context, userID, payloadHash string) error
	RecordDiscordRoleConnectionPushError(ctx context.Context, userID, errMsg string) error
	ListActiveDiscordRoleConnections(ctx context.Context) ([]store.DiscordRoleConnection, error)
	GetUserProGrantedAt(ctx context.Context, userID string) (*time.Time, error)
	GetAppUserDisplayName(ctx context.Context, userID string) (string, error)
}

// DiscordAPI is the subset of discord.Client calls the Syncer needs.
type DiscordAPI interface {
	PutRoleConnection(ctx context.Context, accessToken, applicationID string, body discord.RoleConnectionBody) error
	RefreshToken(ctx context.Context, refreshToken string) (newAccess, newRefresh string, expiresIn int, err error)
}

type SyncerConfig struct {
	Store         SyncerStore
	Discord       DiscordAPI
	ApplicationID string
	Cipher        TokenCipher
}

type Syncer struct {
	cfg SyncerConfig
}

func NewSyncer(cfg SyncerConfig) *Syncer { return &Syncer{cfg: cfg} }

// pushSkipDuration controls how often Push runs even when the hash is unchanged.
// The periodic reconciler always re-pushes after this many hours regardless of
// hash equality, so any silent Discord-side drift gets corrected.
const pushSkipDuration = 24 * time.Hour

// tokenSkew is the leeway against access-token expiry. We refresh slightly
// before actual expiry to avoid races with Discord clock skew.
const tokenSkew = 60 * time.Second

// Push synchronously pushes the current PRO metadata for userID to Discord.
// No-op when there is no role-connection row or when the row is revoked.
// Returns discord.ErrRefreshRevoked when the row had to be marked revoked
// during this call.
func (s *Syncer) Push(ctx context.Context, userID string) error {
	row, err := s.cfg.Store.GetDiscordRoleConnection(ctx, userID)
	if err != nil {
		return err
	}
	if row == nil || row.RevokedAt != nil {
		return nil
	}

	access := row.AccessTokenCT
	accessNonce := row.AccessTokenNonce
	expiresAt := row.AccessExpiresAt

	if time.Now().Add(tokenSkew).After(expiresAt) {
		refreshPT, err := s.cfg.Cipher.Decrypt(row.RefreshTokenCT, row.RefreshTokenNonce)
		if err != nil {
			return err
		}
		newAccess, newRefresh, expiresIn, err := s.cfg.Discord.RefreshToken(ctx, refreshPT)
		if err != nil {
			if errors.Is(err, discord.ErrRefreshRevoked) {
				_ = s.cfg.Store.MarkDiscordRoleConnectionRevoked(ctx, userID, "invalid_grant")
			}
			return err
		}
		newAccessCT, newAccessNonce, err := s.cfg.Cipher.Encrypt(newAccess)
		if err != nil {
			return err
		}
		newRefreshCT, newRefreshNonce, err := s.cfg.Cipher.Encrypt(newRefresh)
		if err != nil {
			return err
		}
		newExpiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
		updated := *row
		updated.AccessTokenCT = newAccessCT
		updated.AccessTokenNonce = newAccessNonce
		updated.RefreshTokenCT = newRefreshCT
		updated.RefreshTokenNonce = newRefreshNonce
		updated.AccessExpiresAt = newExpiresAt
		if err := s.cfg.Store.UpsertDiscordRoleConnection(ctx, updated); err != nil {
			return err
		}
		access = newAccessCT
		accessNonce = newAccessNonce
		expiresAt = newExpiresAt
		_ = expiresAt
	}

	accessPT, err := s.cfg.Cipher.Decrypt(access, accessNonce)
	if err != nil {
		return err
	}

	grantedAt, err := s.cfg.Store.GetUserProGrantedAt(ctx, userID)
	if err != nil {
		return err
	}
	displayName, err := s.cfg.Store.GetAppUserDisplayName(ctx, userID)
	if err != nil {
		return err
	}
	body := BuildMetadata(displayName, grantedAt)
	hash := HashMetadata(body)

	// Skip when nothing changed AND we pushed recently. The reconciler will
	// re-push past pushSkipDuration to catch silent drift.
	if row.LastPushedHash != nil && *row.LastPushedHash == hash &&
		row.LastPushedAt != nil && time.Since(*row.LastPushedAt) < pushSkipDuration {
		return nil
	}

	if err := s.cfg.Discord.PutRoleConnection(ctx, accessPT, s.cfg.ApplicationID, body); err != nil {
		_ = s.cfg.Store.RecordDiscordRoleConnectionPushError(ctx, userID, err.Error())
		return err
	}
	return s.cfg.Store.RecordDiscordRoleConnectionPushed(ctx, userID, hash)
}
