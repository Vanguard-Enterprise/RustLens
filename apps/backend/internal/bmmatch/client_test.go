package bmmatch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSteamToBMReturnsBmID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer secret")
		}
		if !strings.HasPrefix(r.URL.Path, "/steam-to-bm/") {
			t.Errorf("path = %q, want /steam-to-bm/...", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bm_id": "918543318"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", time.Second)
	bmID, err := client.SteamToBM(context.Background(), "76561198000000000")
	if err != nil {
		t.Fatalf("SteamToBM: %v", err)
	}
	if bmID != "918543318" {
		t.Errorf("bmID = %q, want %q", bmID, "918543318")
	}
}

func TestBMToSteamReturnsSteamID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/bm-to-steam/") {
			t.Errorf("path = %q, want /bm-to-steam/...", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"steam_id":"76561198000000000"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", time.Second)
	steamID, err := client.BMToSteam(context.Background(), "918543318")
	if err != nil {
		t.Fatalf("BMToSteam: %v", err)
	}
	if steamID != "76561198000000000" {
		t.Errorf("steamID = %q, want %q", steamID, "76561198000000000")
	}
}

func TestLookup404IsErrNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", time.Second)
	_, err := client.SteamToBM(context.Background(), "76561198000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestEmptyValueIsTreatedAsNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bm_id": "  "}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", time.Second)
	_, err := client.SteamToBM(context.Background(), "76561198000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestUnconfiguredClientReturnsErrNotConfigured(t *testing.T) {
	client := NewClient("", "", 0)
	if _, err := client.SteamToBM(context.Background(), "76561198000000000"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("SteamToBM err = %v, want ErrNotConfigured", err)
	}
	if _, err := client.BMToSteam(context.Background(), "918543318"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("BMToSteam err = %v, want ErrNotConfigured", err)
	}
}
