package battlemetrics

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"rustlens/backend/internal/outboundhttp"
)

// PlayerSession represents a single play session record returned by the
// BattleMetrics player sessions endpoint.
type PlayerSession struct {
	Start *time.Time
	Stop  *time.Time
}

type sessionsPayload struct {
	Data []struct {
		Type       string `json:"type"`
		Attributes struct {
			Start *time.Time `json:"start"`
			Stop  *time.Time `json:"stop"`
		} `json:"attributes"`
	} `json:"data"`
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
}

// FetchPlayerSessions queries the BattleMetrics sessions endpoint for the given
// player on the given server and returns up to maxPages * pageSize sessions
// using cursor pagination via the response links. Each request is dispatched
// through the configured outbound provider so it gets proxy-rotated when proxies
// are enabled.
func (c *Client) FetchPlayerSessions(ctx context.Context, playerBmID, serverBmID string, pageSize int, maxPages int) ([]PlayerSession, error) {
	if !numericPattern.MatchString(playerBmID) {
		return nil, fmt.Errorf("invalid player ID: %q", playerBmID)
	}
	if !numericPattern.MatchString(serverBmID) {
		return nil, fmt.Errorf("invalid server ID: %q", serverBmID)
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	if maxPages <= 0 {
		maxPages = 2
	}

	endpoint := fmt.Sprintf("%s/players/%s/relationships/sessions", c.baseURL, playerBmID)
	params := url.Values{}
	params.Set("filter[servers]", serverBmID)
	params.Set("page[size]", fmt.Sprintf("%d", pageSize))
	current := endpoint + "?" + params.Encode()

	out := make([]PlayerSession, 0, pageSize*maxPages)
	for page := 0; page < maxPages && current != ""; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")

		httpClient, proxyEndpoint, proxied := c.provider.Prepare()
		log.Printf("[battlemetrics] sessions request player=%s server=%s page=%d proxied=%t proxy=%q", playerBmID, serverBmID, page+1, proxied, outboundhttp.RedactProxyEndpoint(proxyEndpoint))

		response, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if response.StatusCode == http.StatusNotFound {
			response.Body.Close()
			return out, nil
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("battlemetrics sessions returned status %d", response.StatusCode)
		}

		var payload sessionsPayload
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			response.Body.Close()
			return nil, err
		}
		response.Body.Close()

		for _, item := range payload.Data {
			if item.Type != "session" {
				continue
			}
			out = append(out, PlayerSession{
				Start: item.Attributes.Start,
				Stop:  item.Attributes.Stop,
			})
		}

		current = payload.Links.Next
	}

	return out, nil
}
