package platformsync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// withBaseURL temporarily redirects the package-level baseURL at test scope.
func withBaseURL(t *testing.T, url string) {
	t.Helper()
	original := baseURL
	baseURL = url
	t.Cleanup(func() { baseURL = original })
}

func TestLookupBySteamID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("token"); got != "secret" {
			t.Errorf("token = %q, want secret", got)
		}
		if got := r.URL.Query().Get("id"); got != "76561198888013469" {
			t.Errorf("id = %q, want 76561198888013469", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"linked":true,"steam_id":"76561198888013469","discord_id":"500975995751366657","previous_discord_ids":"NA"}`))
	}))
	defer server.Close()
	withBaseURL(t, server.URL)

	client := NewClient("secret", time.Second)
	out, err := client.Lookup(context.Background(), "76561198888013469")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !out.Linked {
		t.Errorf("Linked = false, want true")
	}
	if out.SteamID != "76561198888013469" {
		t.Errorf("SteamID = %q", out.SteamID)
	}
	if out.DiscordID != "500975995751366657" {
		t.Errorf("DiscordID = %q", out.DiscordID)
	}
	if len(out.PreviousDiscordIDs) != 0 {
		t.Errorf("PreviousDiscordIDs = %v, want empty", out.PreviousDiscordIDs)
	}
}

func TestLookupParsesPreviousIDsList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"linked":true,"steam_id":"76561198888013469","discord_id":"500975995751366657","previous_steam_ids":"76561111111111111, 76561222222222222"}`))
	}))
	defer server.Close()
	withBaseURL(t, server.URL)

	client := NewClient("secret", time.Second)
	out, err := client.Lookup(context.Background(), "500975995751366657")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(out.PreviousSteamIDs) != 2 {
		t.Fatalf("PreviousSteamIDs len = %d, want 2", len(out.PreviousSteamIDs))
	}
	if out.PreviousSteamIDs[0] != "76561111111111111" || out.PreviousSteamIDs[1] != "76561222222222222" {
		t.Errorf("PreviousSteamIDs = %v", out.PreviousSteamIDs)
	}
}

func TestLookupNotLinkedReturnsErrNotLinked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"linked":false}`))
	}))
	defer server.Close()
	withBaseURL(t, server.URL)

	client := NewClient("secret", time.Second)
	_, err := client.Lookup(context.Background(), "76561198888013469")
	if !errors.Is(err, ErrNotLinked) {
		t.Errorf("err = %v, want ErrNotLinked", err)
	}
}

func TestUnconfiguredClientReturnsErrNotConfigured(t *testing.T) {
	client := NewClient("", 0)
	if _, err := client.Lookup(context.Background(), "76561198888013469"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}
