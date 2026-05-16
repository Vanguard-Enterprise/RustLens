package players

import (
	"context"
	"errors"
	"testing"

	"rustlens/backend/internal/steamapi"
	"rustlens/backend/internal/store"
)

type stubSteam struct {
	gotInput string
	id       string
	err      error
}

func (s *stubSteam) ResolveSteamID(_ context.Context, input string) (string, error) {
	s.gotInput = input
	return s.id, s.err
}

type stubStore struct{}

func (stubStore) GetSteamAccountBySteamID(context.Context, string) (store.SteamAccount, error) {
	return store.SteamAccount{}, store.ErrSteamAccountNotFound
}
func (stubStore) GetSteamAccountByBmID(context.Context, string) (store.SteamAccount, error) {
	return store.SteamAccount{}, store.ErrSteamAccountNotFound
}
func (stubStore) SearchPlayersByName(context.Context, string, bool) ([]store.PlayerSearchHit, error) {
	return nil, nil
}

func TestResolveToSteamID_BareVanity(t *testing.T) {
	steam := &stubSteam{id: "76561198000000099"}
	svc := NewService(stubStore{}, steam, nil)

	got, err := svc.ResolveToSteamID(context.Background(), "gabelogannewell")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "76561198000000099" {
		t.Fatalf("got steamID %q", got)
	}
	if steam.gotInput != "https://steamcommunity.com/id/gabelogannewell" {
		t.Fatalf("resolver received %q", steam.gotInput)
	}
}

func TestResolveToSteamID_VanityURL(t *testing.T) {
	steam := &stubSteam{id: "76561198000000100"}
	svc := NewService(stubStore{}, steam, nil)

	got, err := svc.ResolveToSteamID(context.Background(), "https://steamcommunity.com/id/somevanity")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "76561198000000100" {
		t.Fatalf("got steamID %q", got)
	}
	if steam.gotInput != "https://steamcommunity.com/id/somevanity" {
		t.Fatalf("resolver received %q", steam.gotInput)
	}
}

func TestResolveToSteamID_VanityWithoutResolver(t *testing.T) {
	svc := NewService(stubStore{}, nil, nil)
	_, err := svc.ResolveToSteamID(context.Background(), "gabelogannewell")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestResolveToSteamID_VanityNotFoundMapsToErrNotFound(t *testing.T) {
	steam := &stubSteam{err: steamapi.ErrVanityNotFound}
	svc := NewService(stubStore{}, steam, nil)
	_, err := svc.ResolveToSteamID(context.Background(), "gabelogannewell")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
