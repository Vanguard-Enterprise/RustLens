package steamapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveSteamIDDirectProfileID(t *testing.T) {
	client := NewClient("")

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

	client := NewClient("steam-key")
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
	client := NewClient("")

	steamID, err := client.ResolveSteamID(context.Background(), "https://steamcommunity.com/id/76561198000000002")
	if err != nil {
		t.Fatalf("ResolveSteamID returned error: %v", err)
	}
	if steamID != "76561198000000002" {
		t.Fatalf("ResolveSteamID = %q, want %q", steamID, "76561198000000002")
	}
}

func TestResolveSteamIDRejectsUnsupportedInput(t *testing.T) {
	client := NewClient("")

	if _, err := client.ResolveSteamID(context.Background(), "rustlens"); err == nil {
		t.Fatal("ResolveSteamID unexpectedly succeeded for unsupported input")
	}
}
