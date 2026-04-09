package battlemetrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

var numericPattern = regexp.MustCompile(`^\d+$`)

const defaultBaseURL = "https://api.battlemetrics.com"

type Player struct {
	BmID string
	Name string
}

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		baseURL: defaultBaseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
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

	response, err := c.httpClient.Do(req)
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
