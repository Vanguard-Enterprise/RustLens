package battlemetrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"rustlens/backend/internal/outboundhttp"
)

type ServerPlayerCountPoint struct {
	Timestamp time.Time
	Min       int
	Value     float64
	Max       int
}

type serverPlayerCountHistoryPayload struct {
	Data []struct {
		Type       string `json:"type"`
		Attributes struct {
			Timestamp time.Time `json:"timestamp"`
			Min       int       `json:"min"`
			Value     float64   `json:"value"`
			Max       int       `json:"max"`
		} `json:"attributes"`
	} `json:"data"`
}

func (c *Client) FetchServerPlayerCountHistory(ctx context.Context, serverID string, start time.Time, stop time.Time) ([]ServerPlayerCountPoint, error) {
	if !numericPattern.MatchString(serverID) {
		return nil, fmt.Errorf("invalid server ID: %q", serverID)
	}
	if !stop.After(start) {
		return nil, fmt.Errorf("invalid history range")
	}

	endpoint, err := url.Parse(fmt.Sprintf("%s/servers/%s/player-count-history", c.baseURL, serverID))
	if err != nil {
		return nil, err
	}
	params := endpoint.Query()
	params.Set("start", start.UTC().Format(time.RFC3339))
	params.Set("stop", stop.UTC().Format(time.RFC3339))
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Del("Authorization")

	httpClient, proxyEndpoint, proxied := c.provider.Prepare()
	log.Printf("[battlemetrics] server history request server=%s proxied=%t proxy=%q", serverID, proxied, outboundhttp.RedactProxyEndpoint(proxyEndpoint))

	response, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return nil, ErrPlayerNotFound
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return nil, fmt.Errorf("battlemetrics server history returned status %d: %s", response.StatusCode, string(body))
	}

	var payload serverPlayerCountHistoryPayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}

	out := make([]ServerPlayerCountPoint, 0, len(payload.Data))
	for _, item := range payload.Data {
		if item.Type != "dataPoint" || item.Attributes.Timestamp.IsZero() {
			continue
		}
		out = append(out, ServerPlayerCountPoint{
			Timestamp: item.Attributes.Timestamp.UTC(),
			Min:       item.Attributes.Min,
			Value:     item.Attributes.Value,
			Max:       item.Attributes.Max,
		})
	}
	return out, nil
}
