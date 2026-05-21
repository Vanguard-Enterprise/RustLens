// Package bmmatch is a thin client for the external Steam ↔ BattleMetrics
// matching service that replaces the previous authenticated BattleMetrics
// API flow used to backfill steam_accounts.
//
// The service exposes:
//   GET /steam-to-bm/{steam_id} → { ...: <bmId> }
//   GET /bm-to-steam/{bm_id}    → { ...: <steamId> }
//
// Both endpoints return a single-entry JSON object whose value is the
// requested ID as a string. The key name is not part of the published
// schema, so we accept any single-string value.
//
// Auth is a static Bearer token (the service's "Shared API token"). Requests
// are sent directly — they are NOT proxied.
package bmmatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"rustlens/backend/internal/outboundhttp"
)

var (
	ErrNotFound  = errors.New("bmmatch: identity not found")
	ErrNotConfigured = errors.New("bmmatch: client not configured")
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient returns a client. If baseURL or token is empty, the returned
// client is non-nil but every call returns ErrNotConfigured — this lets the
// server still boot in environments that haven't set the new env vars yet.
func NewClient(baseURL, token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: timeout},
	}
}

func (c *Client) configured() bool {
	return c != nil && c.baseURL != "" && c.token != ""
}

// SteamToBM resolves a 17-digit Steam ID to a BattleMetrics player ID.
func (c *Client) SteamToBM(ctx context.Context, steamID string) (string, error) {
	if !c.configured() {
		return "", ErrNotConfigured
	}
	return c.lookup(ctx, "/steam-to-bm/"+steamID)
}

// BMToSteam resolves a BattleMetrics player ID to a 17-digit Steam ID.
func (c *Client) BMToSteam(ctx context.Context, bmID string) (string, error) {
	if !c.configured() {
		return "", ErrNotConfigured
	}
	return c.lookup(ctx, "/bm-to-steam/"+bmID)
}

func (c *Client) lookup(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	outboundhttp.WithTarget(ctx, "bmmatch")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bmmatch: upstream returned status %d", resp.StatusCode)
	}

	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("bmmatch: decode response: %w", err)
	}

	for _, value := range payload {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed, nil
		}
	}
	return "", ErrNotFound
}
