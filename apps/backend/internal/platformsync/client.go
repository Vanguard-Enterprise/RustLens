// Package platformsync is a thin client for the external link.platformsync.io
// service that resolves Steam ↔ Discord identities, including any previously
// linked IDs.
//
// Endpoint shape:
//
//	GET https://link.platformsync.io/api.php?id=<STEAM_OR_DISCORD_ID>&token=<TOKEN>
//
// When called with a Steam ID the response is shaped:
//
//	{
//	  "linked": true,
//	  "steam_id": "76561198888013469",
//	  "discord_id": "500975995751366657",
//	  "previous_discord_ids": "NA"
//	}
//
// When called with a Discord ID the response is shaped:
//
//	{
//	  "linked": true,
//	  "steam_id": "76561198888013469",
//	  "discord_id": "500975995751366657",
//	  "previous_steam_ids": "NA"
//	}
//
// `previous_*_ids` is either the literal string "NA" (no previous IDs known)
// or a comma-separated list of additional IDs. Requests are sent directly —
// they are NOT proxied.
package platformsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"rustlens/backend/internal/outboundhttp"
)

var (
	ErrNotConfigured = errors.New("platformsync: client not configured")
	ErrNotLinked     = errors.New("platformsync: identity not linked")
)

// baseURL is var rather than const so tests can swap it for an httptest server.
var baseURL = "https://link.platformsync.io/api.php"

type Client struct {
	token string
	http  *http.Client
}

type Lookup struct {
	Linked             bool
	SteamID            string
	DiscordID          string
	PreviousSteamIDs   []string
	PreviousDiscordIDs []string
}

// NewClient returns a client. If token is empty, the returned client is
// non-nil but every call returns ErrNotConfigured — this matches the
// bmmatch client convention so the server still boots when the env var
// is missing.
func NewClient(token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		token: strings.TrimSpace(token),
		http:  &http.Client{Timeout: timeout},
	}
}

func (c *Client) configured() bool {
	return c != nil && c.token != ""
}

// Lookup resolves a Steam or Discord ID through platformsync. The same
// endpoint accepts either ID type — the caller passes whatever they have.
func (c *Client) Lookup(ctx context.Context, id string) (Lookup, error) {
	if !c.configured() {
		return Lookup{}, ErrNotConfigured
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return Lookup{}, errors.New("platformsync: empty id")
	}

	endpoint := baseURL + "?" + url.Values{
		"id":    {id},
		"token": {c.token},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Lookup{}, err
	}
	req.Header.Set("Accept", "application/json")

	outboundhttp.WithTarget(ctx, "platformsync")
	resp, err := c.http.Do(req)
	if err != nil {
		return Lookup{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Lookup{}, fmt.Errorf("platformsync: upstream returned status %d", resp.StatusCode)
	}

	var payload rawResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Lookup{}, fmt.Errorf("platformsync: decode response: %w", err)
	}

	out := Lookup{
		Linked:             payload.Linked,
		SteamID:            strings.TrimSpace(payload.SteamID),
		DiscordID:          strings.TrimSpace(payload.DiscordID),
		PreviousSteamIDs:   parsePreviousIDs(payload.PreviousSteamIDs),
		PreviousDiscordIDs: parsePreviousIDs(payload.PreviousDiscordIDs),
	}

	if !out.Linked {
		return out, ErrNotLinked
	}

	return out, nil
}

type rawResponse struct {
	Linked             bool   `json:"linked"`
	SteamID            string `json:"steam_id"`
	DiscordID          string `json:"discord_id"`
	PreviousSteamIDs   string `json:"previous_steam_ids"`
	PreviousDiscordIDs string `json:"previous_discord_ids"`
}

func parsePreviousIDs(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.EqualFold(trimmed, "NA") {
		return nil
	}

	parts := strings.Split(trimmed, ",")
	ids := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" || strings.EqualFold(p, "NA") {
			continue
		}
		ids = append(ids, p)
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}
