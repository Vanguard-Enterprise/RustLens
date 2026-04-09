package battlemetrics

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"rustlens/backend/internal/outboundhttp"
)

var numericPattern = regexp.MustCompile(`^\d+$`)

const defaultBaseURL = "https://api.battlemetrics.com"

type Player struct {
	BmID string
	Name string
}

type Client struct {
	baseURL  string
	apiKey   string
	provider requestPreparer
}

type requestPreparer interface {
	Prepare() (*http.Client, string, bool)
}

func NewClient(apiKey string, provider requestPreparer) *Client {
	if provider == nil {
		fallbackProvider, _ := outboundhttp.NewProvider(false, nil, 10*time.Second)
		provider = fallbackProvider
	}

	return &Client{
		baseURL:  defaultBaseURL,
		apiKey:   apiKey,
		provider: provider,
	}
}

func (c *Client) FetchServerPlayers(ctx context.Context, serverID string) ([]Player, error) {
	if !numericPattern.MatchString(serverID) {
		return nil, fmt.Errorf("invalid server ID: %q", serverID)
	}

	endpoint, err := url.Parse(fmt.Sprintf("%s/servers/%s?include=player", c.baseURL, serverID))
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Accept", "application/json")

	httpClient, proxyEndpoint, proxied := c.provider.Prepare()
	log.Printf("[battlemetrics] sending request host=%s proxied=%t proxy=%q", endpoint.Host, proxied, outboundhttp.RedactProxyEndpoint(proxyEndpoint))

	response, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("battlemetrics returned status %d", response.StatusCode)
	}

	var payload struct {
		Included []struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"attributes"`
		} `json:"included"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}

	players := make([]Player, 0, len(payload.Included))
	for _, item := range payload.Included {
		if item.Type != "player" {
			continue
		}

		playerID := item.ID
		if item.Attributes.ID != "" {
			playerID = item.Attributes.ID
		}
		if playerID == "" {
			continue
		}

		players = append(players, Player{
			BmID: playerID,
			Name: item.Attributes.Name,
		})
	}

	return players, nil
}
