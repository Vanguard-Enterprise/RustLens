package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestHiddenDiscordLinkIdentitySuppressesLinkLookups(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	suffix := time.Now().UnixNano() % 1_000_000_000
	steamID := fmt.Sprintf("76561198%09d", suffix)
	discordID := fmt.Sprintf("100000000%09d", suffix)
	guildID := fmt.Sprintf("200000000%09d", suffix)

	if _, err := s.CreateSteamDiscordLink(ctx, SteamDiscordLink{
		SteamID:   steamID,
		DiscordID: discordID,
		Type:      "soft",
		Source:    "test",
	}); err != nil {
		t.Fatalf("CreateSteamDiscordLink: %v", err)
	}
	if _, err := s.CreateGuildSteamDiscordLink(ctx, guildID, discordID, steamID); err != nil {
		t.Fatalf("CreateGuildSteamDiscordLink: %v", err)
	}

	linksByDiscord, err := s.ListLinksByDiscordID(ctx, discordID)
	if err != nil {
		t.Fatalf("ListLinksByDiscordID before hide: %v", err)
	}
	if len(linksByDiscord) != 1 {
		t.Fatalf("expected 1 link by discord before hide, got %d", len(linksByDiscord))
	}

	linksBySteam, err := s.ListLinksBySteamID(ctx, steamID)
	if err != nil {
		t.Fatalf("ListLinksBySteamID before hide: %v", err)
	}
	if len(linksBySteam) != 1 {
		t.Fatalf("expected 1 link by steam before hide, got %d", len(linksBySteam))
	}

	guildLinksBySteam, err := s.ListGuildLinksByGuildAndSteam(ctx, guildID, steamID)
	if err != nil {
		t.Fatalf("ListGuildLinksByGuildAndSteam before hide: %v", err)
	}
	if len(guildLinksBySteam) != 1 {
		t.Fatalf("expected 1 guild link by steam before hide, got %d", len(guildLinksBySteam))
	}

	if _, err := s.HideDiscordLinkIdentity(ctx, discordID, "test", "test"); err != nil {
		t.Fatalf("HideDiscordLinkIdentity: %v", err)
	}

	linksByDiscord, err = s.ListLinksByDiscordID(ctx, discordID)
	if err != nil {
		t.Fatalf("ListLinksByDiscordID after hide: %v", err)
	}
	if len(linksByDiscord) != 0 {
		t.Fatalf("expected 0 links by discord after hide, got %d", len(linksByDiscord))
	}

	linksBySteam, err = s.ListLinksBySteamID(ctx, steamID)
	if err != nil {
		t.Fatalf("ListLinksBySteamID after hide: %v", err)
	}
	if len(linksBySteam) != 0 {
		t.Fatalf("expected 0 links by steam after hide, got %d", len(linksBySteam))
	}

	guildLinksBySteam, err = s.ListGuildLinksByGuildAndSteam(ctx, guildID, steamID)
	if err != nil {
		t.Fatalf("ListGuildLinksByGuildAndSteam after hide: %v", err)
	}
	if len(guildLinksBySteam) != 0 {
		t.Fatalf("expected 0 guild links by steam after hide, got %d", len(guildLinksBySteam))
	}

	if err := s.UnhideDiscordLinkIdentity(ctx, discordID); err != nil {
		t.Fatalf("UnhideDiscordLinkIdentity: %v", err)
	}

	linksBySteam, err = s.ListLinksBySteamID(ctx, steamID)
	if err != nil {
		t.Fatalf("ListLinksBySteamID after unhide: %v", err)
	}
	if len(linksBySteam) != 1 {
		t.Fatalf("expected 1 link by steam after unhide, got %d", len(linksBySteam))
	}
}
