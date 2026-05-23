package roleconnection

import (
	"context"

	"rustlens/backend/internal/oauth/discord"
)

// RegisterSchema PUTs our application metadata schema (is_pro boolean,
// pro_since datetime) to Discord. Idempotent: calling repeatedly with the
// same records is a no-op as far as Discord is concerned.
func RegisterSchema(ctx context.Context, client *discord.Client, applicationID, botToken string) error {
	records := []discord.ApplicationMetadataRecord{
		{Type: 7, Key: "is_pro", Name: "PRO subscriber", Description: "Active RustLens PRO status"},
		{Type: 6, Key: "pro_since", Name: "PRO since", Description: "Date PRO was granted"},
	}
	return client.RegisterApplicationMetadata(ctx, applicationID, botToken, records)
}
