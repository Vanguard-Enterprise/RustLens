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
