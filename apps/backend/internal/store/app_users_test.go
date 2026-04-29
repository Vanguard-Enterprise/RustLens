package store

import (
	"context"
	"testing"
)

// Compile-time guarantee that IsGuildMember has the expected signature.
var _ = func(p *PostgresStore, ctx context.Context, guildID, discordID string) (bool, error) {
	return p.IsGuildMember(ctx, guildID, discordID)
}

func TestIsGuildMember_SignatureCompiles(t *testing.T) {
	t.Helper()
	// Intentionally empty: the var assignment above fails the build if the
	// signature drifts.
}
