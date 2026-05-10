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

func TestListSetSteamCandidates(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	suffix := time.Now().UnixNano() % 1_000_000_000
	discordID := fmt.Sprintf("100000001%09d", suffix)

	steamIDs := make([]string, 0, 8)
	for i := range 8 {
		steamIDs = append(steamIDs, fmt.Sprintf("76561198%08d%d", suffix%100_000_000, i))
	}

	hardOnlySteam := steamIDs[0]
	dedupSteam := steamIDs[1]
	staleSoftSteam := steamIDs[2]
	freshSofts := steamIDs[3:]

	insertSoft := func(t *testing.T, steamID string, createdAt time.Time) {
		t.Helper()
		if _, err := s.pool.Exec(ctx, `INSERT INTO steam_accounts (steam_id) VALUES ($1) ON CONFLICT DO NOTHING`, steamID); err != nil {
			t.Fatalf("insert steam_account: %v", err)
		}
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO steam_discord_links (steam_id, discord_id, type, source, created_at) VALUES ($1, $2, 'soft', 'test', $3)`,
			steamID, discordID, createdAt,
		); err != nil {
			t.Fatalf("insert soft link: %v", err)
		}
	}
	insertHard := func(t *testing.T, steamID string, createdAt time.Time) {
		t.Helper()
		if _, err := s.pool.Exec(ctx, `INSERT INTO steam_accounts (steam_id) VALUES ($1) ON CONFLICT DO NOTHING`, steamID); err != nil {
			t.Fatalf("insert steam_account: %v", err)
		}
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO steam_discord_links (steam_id, discord_id, type, source, created_at) VALUES ($1, $2, 'hard', 'battlemetrics', $3)`,
			steamID, discordID, createdAt,
		); err != nil {
			t.Fatalf("insert hard link: %v", err)
		}
	}

	now := time.Now().UTC()

	insertHard(t, hardOnlySteam, now.Add(-200*24*time.Hour))

	insertHard(t, dedupSteam, now.Add(-30*24*time.Hour))
	insertSoft(t, dedupSteam, now.Add(-1*time.Hour))

	insertSoft(t, staleSoftSteam, now.Add(-91*24*time.Hour))

	for i, steamID := range freshSofts {
		insertSoft(t, steamID, now.Add(-time.Duration(i+1)*time.Hour))
	}

	candidates, err := s.ListSetSteamCandidates(ctx, discordID)
	if err != nil {
		t.Fatalf("ListSetSteamCandidates: %v", err)
	}

	if len(candidates) != 2+5 {
		t.Fatalf("expected 7 candidates (2 hard + 5 soft), got %d", len(candidates))
	}

	if candidates[0].Type != "hard" || candidates[1].Type != "hard" {
		t.Fatalf("expected hard links first, got types %q,%q", candidates[0].Type, candidates[1].Type)
	}
	for _, c := range candidates[2:] {
		if c.Type != "soft" {
			t.Fatalf("expected soft after hard block, got %q", c.Type)
		}
		if c.SteamID == staleSoftSteam {
			t.Fatalf("stale soft (>90d) should be excluded")
		}
	}

	for _, c := range candidates {
		if c.SteamID == dedupSteam && c.Type != "hard" {
			t.Fatalf("dedup steam should resolve to hard, got %q", c.Type)
		}
	}

	softTimes := make([]time.Time, 0, 5)
	for _, c := range candidates[2:] {
		softTimes = append(softTimes, c.CreatedAt)
	}
	for i := 1; i < len(softTimes); i++ {
		if softTimes[i].After(softTimes[i-1]) {
			t.Fatalf("soft candidates not ordered by created_at DESC: %v", softTimes)
		}
	}

	for _, c := range candidates {
		if c.DisplayName == "" {
			t.Fatalf("DisplayName fallback should populate at least the steam_id, got empty")
		}
	}

	if _, err := s.HideDiscordLinkIdentity(ctx, discordID, "test", "test"); err != nil {
		t.Fatalf("HideDiscordLinkIdentity: %v", err)
	}
	hidden, err := s.ListSetSteamCandidates(ctx, discordID)
	if err != nil {
		t.Fatalf("ListSetSteamCandidates after hide: %v", err)
	}
	if len(hidden) != 0 {
		t.Fatalf("expected 0 candidates after hide, got %d", len(hidden))
	}
}
