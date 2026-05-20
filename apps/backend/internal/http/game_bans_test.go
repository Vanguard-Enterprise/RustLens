package httpapi

import (
	"testing"
	"time"

	"rustlens/backend/internal/store"
)

func TestParseGameBanTimeframe(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		wantValue  string
		wantWindow time.Duration
		wantErr    bool
	}{
		{name: "default", wantValue: "90d", wantWindow: 90 * 24 * time.Hour},
		{name: "30d", value: "30d", wantValue: "30d", wantWindow: 30 * 24 * time.Hour},
		{name: "90d", value: "90d", wantValue: "90d", wantWindow: 90 * 24 * time.Hour},
		{name: "180d", value: "180d", wantValue: "180d", wantWindow: 180 * 24 * time.Hour},
		{name: "all", value: "all", wantValue: "all"},
		{name: "invalid", value: "365d", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGameBanTimeframe(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseGameBanTimeframe() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGameBanTimeframe() error = %v", err)
			}
			if got.Value != tt.wantValue {
				t.Errorf("Value = %q, want %q", got.Value, tt.wantValue)
			}
			if got.Window != tt.wantWindow {
				t.Errorf("Window = %s, want %s", got.Window, tt.wantWindow)
			}
		})
	}
}

func TestRecentGameBansFromCandidatesHonorsTimeframe(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	thirtyFiveDaysAgo := now.Add(-35 * 24 * time.Hour)
	candidates := []gameBanCandidate{
		{GuildID: "guild", DiscordID: "discord", SteamID: "steam", Source: gameBanSourceDirectGuildLink},
	}
	cacheBySteamID := map[string]store.SteamGameBanCache{
		"steam": {
			SteamID:      "steam",
			GameBanned:   true,
			GameBannedAt: &thirtyFiveDaysAgo,
		},
	}

	if got := recentGameBansFromCandidates(candidates, cacheBySteamID, now, gameBanTimeframe{Value: "30d", Window: 30 * 24 * time.Hour}); len(got) != 0 {
		t.Fatalf("30d len = %d, want 0", len(got))
	}
	if got := recentGameBansFromCandidates(candidates, cacheBySteamID, now, gameBanTimeframe{Value: "90d", Window: 90 * 24 * time.Hour}); len(got) != 1 {
		t.Fatalf("90d len = %d, want 1", len(got))
	}
	if got := recentGameBansFromCandidates(candidates, cacheBySteamID, now, gameBanTimeframe{Value: "all"}); len(got) != 1 {
		t.Fatalf("all len = %d, want 1", len(got))
	}
}

func TestRecentGameBansFromCandidatesIncludesSource(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	candidates := []gameBanCandidate{
		{GuildID: "guild", DiscordID: "discord", SteamID: "steam", Source: gameBanSourceLinkedAccount},
	}
	cacheBySteamID := map[string]store.SteamGameBanCache{
		"steam": {
			SteamID:      "steam",
			GameBanned:   true,
			GameBannedAt: &now,
		},
	}

	got := recentGameBansFromCandidates(candidates, cacheBySteamID, now, gameBanTimeframe{Value: "90d", Window: 90 * 24 * time.Hour})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Source != gameBanSourceLinkedAccount {
		t.Fatalf("Source = %q, want %q", got[0].Source, gameBanSourceLinkedAccount)
	}
}
