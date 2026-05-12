package events

// Event is the JSON payload that travels over SSE and Redis pub/sub.
// Keep this struct small and stable — the frontend mirrors it in
// apps/frontend/lib/events-client.ts. Reason values are documented
// alongside each event type in
// docs/superpowers/specs/2026-05-12-auth-session-improvements-design.md.
type Event struct {
	Type      string `json:"type"`
	Reason    string `json:"reason,omitempty"`
	ExpiresAt int64  `json:"expiresAt,omitempty"` // ms since unix epoch, set only for session.expiring
}

// Event type constants. Use these instead of string literals at call sites
// so a typo is a compile error.
const (
	TypeRolesChanged    = "roles.changed"
	TypeSessionRevoked  = "session.revoked"
	TypeSessionExpiring = "session.expiring"
	TypePing            = "ping"
)

// Reason constants for session.revoked.
const (
	ReasonForceLogout = "force_logout"
	ReasonSuspended   = "suspended"
	ReasonDeleted     = "deleted"
)

// Reason constants for roles.changed.
const (
	ReasonGrant            = "grant"
	ReasonRevoke           = "revoke"
	ReasonRolePermsUpdated = "role_perms_updated"
)
