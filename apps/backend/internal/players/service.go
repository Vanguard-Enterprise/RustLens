package players

import (
	"context"
	"errors"
	"strings"

	"rustlens/backend/internal/steamapi"
	"rustlens/backend/internal/store"
)

// SteamResolver resolves a vanity URL or Steam64 input to a Steam64 ID.
// Implemented by *steamapi.Client.
type SteamResolver interface {
	ResolveSteamID(ctx context.Context, input string) (string, error)
}

// BmMatcher is the optional Steam<->BM resolver. May be nil when bmmatch
// is unconfigured. Implemented by *bmmatch.Client.
type BmMatcher interface {
	BMToSteam(ctx context.Context, bmID string) (string, error)
	SteamToBM(ctx context.Context, steamID string) (string, error)
}

// Store is the subset of *store.PostgresStore the service needs.
type Store interface {
	GetSteamAccountBySteamID(ctx context.Context, steamID string) (store.SteamAccount, error)
	GetSteamAccountByBmID(ctx context.Context, bmID string) (store.SteamAccount, error)
	SearchPlayersByName(ctx context.Context, q string, includeOffline bool) ([]store.PlayerSearchHit, error)
}

// ErrNotFound is returned when no player matches the given identifier.
var ErrNotFound = errors.New("players: not found")

// Service composes the identifier classifier with the underlying lookups.
type Service struct {
	store   Store
	steam   SteamResolver
	bmMatch BmMatcher
}

// NewService constructs a Service. bmMatch may be nil.
func NewService(s Store, steam SteamResolver, bmMatch BmMatcher) *Service {
	return &Service{store: s, steam: steam, bmMatch: bmMatch}
}

// ResolveToSteamID takes any identifier the navbar/search box may receive
// and returns the underlying Steam64 ID. Falls back to bmmatch when the
// identifier is a BM ID with no local steam_accounts row.
func (svc *Service) ResolveToSteamID(ctx context.Context, input string) (string, error) {
	q := strings.TrimSpace(input)
	switch ClassifyQuery(q) {
	case KindEmpty, KindName:
		return "", ErrNotFound
	case KindSteamID:
		return q, nil
	case KindSteamURL:
		if id, ok := ExtractSteamIDFromURL(q); ok {
			return id, nil
		}
		// vanity URL: defer to Steam API
		if svc.steam == nil {
			return "", ErrNotFound
		}
		id, err := svc.steam.ResolveSteamID(ctx, q)
		if err != nil {
			return "", err
		}
		return id, nil
	case KindSteamVanity:
		if svc.steam == nil {
			return "", ErrNotFound
		}
		id, err := svc.steam.ResolveSteamID(ctx, "https://steamcommunity.com/id/"+q)
		if err != nil {
			if errors.Is(err, steamapi.ErrVanityNotFound) {
				return "", ErrNotFound
			}
			return "", err
		}
		return id, nil
	case KindBmID:
		bmID, ok := ExtractBmIDFromInput(q)
		if !ok {
			return "", ErrNotFound
		}
		acct, err := svc.store.GetSteamAccountByBmID(ctx, bmID)
		if err == nil {
			return acct.SteamID, nil
		}
		if !errors.Is(err, store.ErrSteamAccountNotFound) {
			return "", err
		}
		if svc.bmMatch == nil {
			return "", ErrNotFound
		}
		steamID, err := svc.bmMatch.BMToSteam(ctx, bmID)
		if err != nil {
			return "", err
		}
		return steamID, nil
	default:
		return "", ErrNotFound
	}
}

// SearchByName runs the cached-name substring search. Caller must enforce
// the website-admin gate.
func (svc *Service) SearchByName(ctx context.Context, q string, includeOffline bool) ([]store.PlayerSearchHit, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return nil, nil
	}
	return svc.store.SearchPlayersByName(ctx, q, includeOffline)
}
