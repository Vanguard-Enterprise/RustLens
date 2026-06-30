// Package bmsearch is a client for bmsearch.io's public, no-auth Steam <->
// BattleMetrics resolution API. It replaces the retired bmmatch service as the
// primary source for steam_accounts.bm_id backfill.
//
// bmsearch.io is a Supabase/PostgREST application that exposes:
//   - A cache table:  GET /profiles?steam_id=eq.{id}&select=battlemetrics_id
//                     GET /profiles?battlemetrics_id=eq.{id}&select=steam_id
//                     GET /profiles?id=eq.{profileID}&select=battlemetrics_id
//   - An async resolver RPC for uncached players:
//                     POST /rpc/lookup       {p_steam_input, p_force} -> {queue_id, profile_id}
//                     GET  /rpc/lookup_status?p_queue_id=<id>          -> {status, profile_id, error, ...}
//
// Cache reads are instant. A cold Steam ID is enqueued and resolved by a
// background worker, so SteamToBM polls lookup_status until the caller's ctx
// deadline; if it hasn't resolved by then it returns steammatch.ErrPending so
// the handler can return a degraded response now and backfill later (with a
// longer ctx) so the bm_id lands for next time.
//
// Requests are sent DIRECTLY (never through the outbound proxy pool), matching
// the old bmmatch behavior. We never send p_force=true — re-resolving fresh
// data would be rude to a public service we don't own.
package bmsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"rustlens/backend/internal/outboundhttp"
	"rustlens/backend/internal/steammatch"
)

const (
	defaultBaseURL = "https://bmsearch.io/api"
	userAgent      = "RustLens/1.0 (+https://rustlens.io)"
	pollInterval   = 700 * time.Millisecond
)

// Client talks to the bmsearch.io public API. It satisfies steammatch.Resolver.
type Client struct {
	baseURL     string
	defaultWait time.Duration
	http        *http.Client
}

// NewClient returns a client. baseURL defaults to the public bmsearch.io API
// when empty. defaultWait caps how long SteamToBM polls when the caller's ctx
// has no deadline; when ctx has a deadline, that deadline wins.
func NewClient(baseURL string, defaultWait time.Duration) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if defaultWait <= 0 {
		defaultWait = 4 * time.Second
	}
	return &Client{
		baseURL:     baseURL,
		defaultWait: defaultWait,
		http: &http.Client{
			// No per-client timeout: the request ctx controls the deadline so
			// background backfills can poll far longer than inline calls.
			Transport: otelhttp.NewTransport(http.DefaultTransport,
				otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
					return r.Method + " " + r.URL.Host
				}),
			),
		},
	}
}

// SteamToBM resolves a Steam ID to a BattleMetrics player ID, hitting the cache
// first and falling back to the async lookup RPC for cold players.
func (c *Client) SteamToBM(ctx context.Context, steamID string) (string, error) {
	steamID = strings.TrimSpace(steamID)
	if steamID == "" {
		return "", steammatch.ErrNotFound
	}

	// 1. Cache read.
	if bmID, err := c.profileBmIDBySteam(ctx, steamID); err != nil {
		return "", err
	} else if bmID != "" {
		return bmID, nil
	}

	// 2. Enqueue an async lookup.
	queueID, profileID, err := c.enqueueLookup(ctx, steamID)
	if err != nil {
		return "", err
	}
	if profileID != "" {
		if bmID, err := c.profileBmIDByID(ctx, profileID); err == nil && bmID != "" {
			return bmID, nil
		}
	}

	// 3. Poll lookup_status until done or the ctx deadline expires.
	return c.pollLookup(ctx, queueID)
}

// BMToSteam resolves a BattleMetrics player ID to a Steam ID from the cache
// only. The lookup RPC only accepts Steam input, so a BM ID bmsearch has never
// cached is not resolvable here — an accepted narrow gap (the reverse direction
// is rarely needed and BattleMetrics' own /players/{id} often supplies it).
func (c *Client) BMToSteam(ctx context.Context, bmID string) (string, error) {
	bmID = strings.TrimSpace(bmID)
	if bmID == "" {
		return "", steammatch.ErrNotFound
	}
	steamID, err := c.profileSteamByBmID(ctx, bmID)
	if err != nil {
		return "", err
	}
	if steamID == "" {
		return "", steammatch.ErrNotFound
	}
	return steamID, nil
}

// --- PostgREST cache reads ---

type profileRow struct {
	BattleMetricsID *json.Number `json:"battlemetrics_id"`
	SteamID         string       `json:"steam_id"`
}

func (c *Client) profileBmIDBySteam(ctx context.Context, steamID string) (string, error) {
	rows, err := c.getProfiles(ctx, "steam_id=eq."+url.QueryEscape(steamID)+"&select=battlemetrics_id&limit=1")
	if err != nil {
		return "", err
	}
	return firstBmID(rows), nil
}

func (c *Client) profileBmIDByID(ctx context.Context, profileID string) (string, error) {
	rows, err := c.getProfiles(ctx, "id=eq."+url.QueryEscape(profileID)+"&select=battlemetrics_id&limit=1")
	if err != nil {
		return "", err
	}
	return firstBmID(rows), nil
}

func (c *Client) profileSteamByBmID(ctx context.Context, bmID string) (string, error) {
	rows, err := c.getProfiles(ctx, "battlemetrics_id=eq."+url.QueryEscape(bmID)+"&select=steam_id&limit=1")
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return strings.TrimSpace(rows[0].SteamID), nil
}

func firstBmID(rows []profileRow) string {
	if len(rows) == 0 || rows[0].BattleMetricsID == nil {
		return ""
	}
	// battlemetrics_id is a JSON number (bigint); the rest of the codebase
	// stores bm_id as TEXT, so normalize to its integer string form.
	s := strings.TrimSpace(rows[0].BattleMetricsID.String())
	if s == "" || s == "0" {
		return ""
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return strconv.FormatInt(n, 10)
	}
	return s
}

func (c *Client) getProfiles(ctx context.Context, query string) ([]profileRow, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/profiles?"+query, nil)
	if err != nil {
		return nil, err
	}
	var rows []profileRow
	if err := c.do(ctx, req, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// --- async lookup RPC ---

type lookupResponse struct {
	QueueID   *json.Number `json:"queue_id"`
	ProfileID *json.Number `json:"profile_id"`
}

type lookupStatusResponse struct {
	Status    string       `json:"status"`
	ProfileID *json.Number `json:"profile_id"`
	Error     *string      `json:"error"`
}

func (c *Client) enqueueLookup(ctx context.Context, steamID string) (queueID, profileID string, err error) {
	body, _ := json.Marshal(map[string]any{"p_steam_input": steamID, "p_force": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rpc/lookup", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	var resp lookupResponse
	if err := c.do(ctx, req, &resp); err != nil {
		return "", "", err
	}
	return numToString(resp.QueueID), numToString(resp.ProfileID), nil
}

func (c *Client) pollLookup(ctx context.Context, queueID string) (string, error) {
	if queueID == "" {
		return "", steammatch.ErrNotFound
	}

	// Bound polling by the ctx deadline; fall back to defaultWait when the ctx
	// has none (so a stray background call can't poll forever).
	pollCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, c.defaultWait)
		defer cancel()
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		status, profileID, lookupErr, err := c.lookupStatus(pollCtx, queueID)
		if err == nil {
			switch {
			case lookupErr != "":
				return "", steammatch.ErrNotFound
			case status == "done":
				if profileID == "" {
					return "", steammatch.ErrNotFound
				}
				bmID, err := c.profileBmIDByID(ctx, profileID)
				if err != nil {
					return "", err
				}
				if bmID == "" {
					return "", steammatch.ErrNotFound
				}
				return bmID, nil
			}
		}
		// On a transient status error we keep polling until the deadline.

		select {
		case <-pollCtx.Done():
			// Enqueued but not resolved in time — let the caller backfill.
			return "", steammatch.ErrPending
		case <-ticker.C:
		}
	}
}

func (c *Client) lookupStatus(ctx context.Context, queueID string) (status, profileID, lookupErr string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/rpc/lookup_status?p_queue_id="+url.QueryEscape(queueID), nil)
	if err != nil {
		return "", "", "", err
	}
	var resp lookupStatusResponse
	if err := c.do(ctx, req, &resp); err != nil {
		return "", "", "", err
	}
	if resp.Error != nil {
		lookupErr = strings.TrimSpace(*resp.Error)
	}
	return strings.TrimSpace(resp.Status), numToString(resp.ProfileID), lookupErr, nil
}

func numToString(n *json.Number) string {
	if n == nil {
		return ""
	}
	s := strings.TrimSpace(n.String())
	if s == "" || s == "0" {
		return ""
	}
	return s
}

func (c *Client) do(ctx context.Context, req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	outboundhttp.WithTarget(ctx, "bmsearch")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bmsearch: upstream returned status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("bmsearch: decode response: %w", err)
	}
	return nil
}
