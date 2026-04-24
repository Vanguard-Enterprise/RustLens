package steamapi

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rustlens/backend/internal/outboundhttp"
)

func TestResolveSteamIDDirectProfileID(t *testing.T) {
	client := NewClient("", nil)

	steamID, err := client.ResolveSteamID(context.Background(), "https://steamcommunity.com/profiles/76561198000000000/")
	if err != nil {
		t.Fatalf("ResolveSteamID returned error: %v", err)
	}
	if steamID != "76561198000000000" {
		t.Fatalf("ResolveSteamID = %q, want %q", steamID, "76561198000000000")
	}
}

func TestResolveSteamIDVanityURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ISteamUser/ResolveVanityURL/v1/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("key"); got != "steam-key" {
			t.Fatalf("unexpected key: %s", got)
		}
		if got := r.URL.Query().Get("vanityurl"); got != "rustlens" {
			t.Fatalf("unexpected vanityurl: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"steamid":"76561198000000001","success":1}}`))
	}))
	defer server.Close()

	client := NewClient("steam-key", nil)
	client.baseURL = server.URL

	steamID, err := client.ResolveSteamID(context.Background(), "https://steamcommunity.com/id/rustlens")
	if err != nil {
		t.Fatalf("ResolveSteamID returned error: %v", err)
	}
	if steamID != "76561198000000001" {
		t.Fatalf("ResolveSteamID = %q, want %q", steamID, "76561198000000001")
	}
}

func TestResolveSteamIDNumericIDPathReturnsDirectSteamID(t *testing.T) {
	client := NewClient("", nil)

	steamID, err := client.ResolveSteamID(context.Background(), "https://steamcommunity.com/id/76561198000000002")
	if err != nil {
		t.Fatalf("ResolveSteamID returned error: %v", err)
	}
	if steamID != "76561198000000002" {
		t.Fatalf("ResolveSteamID = %q, want %q", steamID, "76561198000000002")
	}
}

func TestResolveSteamIDRejectsUnsupportedInput(t *testing.T) {
	client := NewClient("", nil)

	if _, err := client.ResolveSteamID(context.Background(), "rustlens"); err == nil {
		t.Fatal("ResolveSteamID unexpectedly succeeded for unsupported input")
	}
}

func TestResolveSteamIDLogsSelectedProxy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"steamid":"76561198000000001","success":1}}`))
	}))
	defer server.Close()

	client := NewClient("steam-key", staticProvider{
		client:   server.Client(),
		endpoint: "http://user:secret@proxy-one:8080",
		proxied:  true,
	})
	client.baseURL = server.URL

	var logOutput strings.Builder
	originalWriter := log.Writer()
	log.SetOutput(&logOutput)
	defer log.SetOutput(originalWriter)

	_, err := client.ResolveSteamID(context.Background(), "https://steamcommunity.com/id/rustlens")
	if err != nil {
		t.Fatalf("ResolveSteamID returned error: %v", err)
	}

	logged := logOutput.String()
	if !strings.Contains(logged, `proxied=true`) {
		t.Fatalf("expected proxied log entry, got %q", logged)
	}
	if !strings.Contains(logged, `proxy="http://user@proxy-one:8080"`) {
		t.Fatalf("expected redacted proxy endpoint in log, got %q", logged)
	}
}

func TestGetPlayerBans(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ISteamUser/GetPlayerBans/v1/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("key"); got != "steam-key" {
			t.Fatalf("unexpected key: %s", got)
		}
		if got := r.URL.Query().Get("steamids"); got != "76561198000000001,76561198000000002" {
			t.Fatalf("unexpected steamids: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"players":[{"SteamId":"76561198000000001","CommunityBanned":false,"VACBanned":true,"NumberOfVACBans":1,"DaysSinceLastBan":1667,"NumberOfGameBans":0,"EconomyBan":"none"},{"SteamId":"76561198000000002","CommunityBanned":false,"VACBanned":false,"NumberOfVACBans":0,"DaysSinceLastBan":12,"NumberOfGameBans":1,"EconomyBan":"none"}]}`))
	}))
	defer server.Close()

	client := NewClient("steam-key", nil)
	client.baseURL = server.URL

	bans, err := client.GetPlayerBans(context.Background(), []string{"76561198000000001", "76561198000000002"})
	if err != nil {
		t.Fatalf("GetPlayerBans returned error: %v", err)
	}
	if len(bans) != 2 {
		t.Fatalf("GetPlayerBans returned %d bans, want 2", len(bans))
	}
	if bans[0].NumberOfGameBans != 0 {
		t.Fatalf("first NumberOfGameBans = %d, want 0", bans[0].NumberOfGameBans)
	}
	if bans[1].DaysSinceLastBan != 12 {
		t.Fatalf("second DaysSinceLastBan = %d, want 12", bans[1].DaysSinceLastBan)
	}
}

func TestGetPlayerSummaryParsesCommunityVisibilityState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ISteamUser/GetPlayerSummaries/v2/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"players":[{"steamid":"76561198000000001","communityvisibilitystate":1,"avatarfull":"https://example.com/avatar.jpg"}]}}`))
	}))
	defer server.Close()

	client := NewClient("steam-key", nil)
	client.baseURL = server.URL

	summary, err := client.GetPlayerSummary(context.Background(), "76561198000000001")
	if err != nil {
		t.Fatalf("GetPlayerSummary returned error: %v", err)
	}
	if summary.CommunityVisibilityState != 1 {
		t.Fatalf("CommunityVisibilityState = %d, want 1", summary.CommunityVisibilityState)
	}
	if summary.AvatarFull != "https://example.com/avatar.jpg" {
		t.Fatalf("AvatarFull = %q", summary.AvatarFull)
	}
}

type staticProvider struct {
	client   *http.Client
	endpoint string
	proxied  bool
}

func (p staticProvider) Prepare() (*http.Client, string, bool) {
	if p.client == nil {
		fallback, _ := outboundhttp.NewProvider(false, nil, 10*time.Second)
		return fallback.Prepare()
	}
	return p.client, p.endpoint, p.proxied
}
