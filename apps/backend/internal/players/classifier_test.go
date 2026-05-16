package players

import "testing"

func TestClassifyQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want QueryKind
	}{
		{"empty", "", KindEmpty},
		{"whitespace only", "   ", KindEmpty},
		{"steam id 17 digits", "76561198000000001", KindSteamID},
		{"steam id with whitespace", "  76561198000000001 ", KindSteamID},
		{"sixteen digit number is bm id", "7656119800000000", KindBmID},
		{"bm id 6 digits", "123456", KindBmID},
		{"bm id 16 digits", "1234567890123456", KindBmID},
		{"bm id 5 digits is name", "12345", KindName},
		{"bm id 17 digits is steam id", "12345678901234567", KindSteamID},
		{"steamcommunity profiles url", "https://steamcommunity.com/profiles/76561198000000001", KindSteamURL},
		{"steamcommunity profiles url trailing slash", "https://steamcommunity.com/profiles/76561198000000001/", KindSteamURL},
		{"steamcommunity vanity url", "https://steamcommunity.com/id/some-vanity_1", KindSteamURL},
		{"battlemetrics player url", "https://www.battlemetrics.com/players/123456", KindBmID},
		{"battlemetrics rcon url", "https://www.battlemetrics.com/rcon/players/123456", KindBmID},
		{"name with spaces", "Mr Bean", KindName},
		{"name that looks numeric but too short", "42", KindName},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyQuery(tc.in); got != tc.want {
				t.Fatalf("ClassifyQuery(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractFromIdentifier(t *testing.T) {
	t.Run("steam url profiles extracts steam id", func(t *testing.T) {
		got, ok := ExtractSteamIDFromURL("https://steamcommunity.com/profiles/76561198000000001/")
		if !ok || got != "76561198000000001" {
			t.Fatalf("ExtractSteamIDFromURL got %q ok=%v", got, ok)
		}
	})
	t.Run("vanity url returns false", func(t *testing.T) {
		_, ok := ExtractSteamIDFromURL("https://steamcommunity.com/id/some-vanity")
		if ok {
			t.Fatalf("vanity url should not yield steam id")
		}
	})
	t.Run("battlemetrics url extracts bm id", func(t *testing.T) {
		got, ok := ExtractBmIDFromInput("https://www.battlemetrics.com/players/123456")
		if !ok || got != "123456" {
			t.Fatalf("ExtractBmIDFromInput got %q ok=%v", got, ok)
		}
	})
}
