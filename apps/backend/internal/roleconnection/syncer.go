// Package roleconnection orchestrates Discord's linked-roles application
// metadata for the dashboard PRO role: storing per-user OAuth tokens,
// refreshing them, and pushing metadata so Discord re-evaluates the
// linked role membership. See docs/superpowers/plans/2026-05-23-discord-linked-roles-pro-sync.md.
package roleconnection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"rustlens/backend/internal/oauth/discord"
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
