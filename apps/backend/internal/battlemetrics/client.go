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

type PlayerServer struct {
	ServerID    string
	ServerName  string
	TimePlayed  int64
	FirstSeenAt *time.Time
	LastSeenAt  *time.Time
}

type PlayerNameIdentifier struct {
	Name     string
	LastSeen *time.Time
}

type PlayerProfile struct {
	BmID            string
	BmName          string
	CreatedAt       *time.Time
	Servers         []PlayerServer
	NameIdentifiers []PlayerNameIdentifier
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

func (c *Client) FetchPlayerProfile(ctx context.Context, playerID string) (PlayerProfile, error) {
	if !numericPattern.MatchString(playerID) {
		return PlayerProfile{}, fmt.Errorf("invalid player ID: %q", playerID)
	}

	endpoint, err := url.Parse(fmt.Sprintf("%s/players/%s?include=server,identifier", c.baseURL, playerID))
	if err != nil {
		return PlayerProfile{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return PlayerProfile{}, err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Accept", "application/json")

	httpClient, proxyEndpoint, proxied := c.provider.Prepare()
	log.Printf("[battlemetrics] sending request host=%s proxied=%t proxy=%q", endpoint.Host, proxied, outboundhttp.RedactProxyEndpoint(proxyEndpoint))

	response, err := httpClient.Do(req)
	if err != nil {
		return PlayerProfile{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return PlayerProfile{}, fmt.Errorf("battlemetrics returned status %d", response.StatusCode)
	}

	var payload struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Name      string     `json:"name"`
				CreatedAt *time.Time `json:"createdAt"`
			} `json:"attributes"`
		} `json:"data"`
		Included []struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				Name       string     `json:"name"`
				Type       string     `json:"type"`
				Identifier string     `json:"identifier"`
				LastSeen   *time.Time `json:"lastSeen"`
			} `json:"attributes"`
			Meta struct {
				TimePlayed int64      `json:"timePlayed"`
				FirstSeen  *time.Time `json:"firstSeen"`
				LastSeen   *time.Time `json:"lastSeen"`
			} `json:"meta"`
		} `json:"included"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return PlayerProfile{}, err
	}

	profile := PlayerProfile{
		BmID:      payload.Data.ID,
		BmName:    payload.Data.Attributes.Name,
		CreatedAt: payload.Data.Attributes.CreatedAt,
	}

	for _, item := range payload.Included {
		switch item.Type {
		case "server":
			profile.Servers = append(profile.Servers, PlayerServer{
				ServerID:    item.ID,
				ServerName:  item.Attributes.Name,
				TimePlayed:  item.Meta.TimePlayed,
				FirstSeenAt: item.Meta.FirstSeen,
				LastSeenAt:  item.Meta.LastSeen,
			})
		case "identifier":
			if item.Attributes.Type != "name" {
				continue
			}
			lastSeen := item.Attributes.LastSeen
			if lastSeen == nil {
				lastSeen = item.Meta.LastSeen
			}
			profile.NameIdentifiers = append(profile.NameIdentifiers, PlayerNameIdentifier{
				Name:     item.Attributes.Identifier,
				LastSeen: lastSeen,
			})
		}
	}

	return profile, nil
}
