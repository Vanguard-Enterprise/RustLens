package battlemetrics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"rustlens/backend/internal/outboundhttp"
)

var numericPattern = regexp.MustCompile(`^\d+$`)

var ErrPlayerNotFound = errors.New("battlemetrics player not found")
var ErrAPIKeyRequired = errors.New("battlemetrics api key is required")

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
	SteamID         string
	BmName          string
	CreatedAt       *time.Time
	Servers         []PlayerServer
	NameIdentifiers []PlayerNameIdentifier
}

type playerResource struct {
	ID         string `json:"id"`
	Attributes struct {
		Name      string     `json:"name"`
		CreatedAt *time.Time `json:"createdAt"`
	} `json:"attributes"`
}

type includedResource struct {
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
}

func parsePlayerProfile(player playerResource, included []includedResource) PlayerProfile {
	profile := PlayerProfile{
		BmID:      player.ID,
		BmName:    player.Attributes.Name,
		CreatedAt: player.Attributes.CreatedAt,
	}

	for _, item := range included {
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
			switch item.Attributes.Type {
			case "name":
				lastSeen := item.Attributes.LastSeen
				if lastSeen == nil {
					lastSeen = item.Meta.LastSeen
				}
				profile.NameIdentifiers = append(profile.NameIdentifiers, PlayerNameIdentifier{
					Name:     item.Attributes.Identifier,
					LastSeen: lastSeen,
				})
			case "steamID":
				if profile.SteamID == "" {
					profile.SteamID = item.Attributes.Identifier
				}
			}
		}
	}

	return profile
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

func (c *Client) QuickMatchPlayerProfileBySteamID(ctx context.Context, steamID string) (PlayerProfile, error) {
	if !numericPattern.MatchString(steamID) {
		return PlayerProfile{}, fmt.Errorf("invalid steam ID: %q", steamID)
	}
	if c.apiKey == "" {
		return PlayerProfile{}, ErrAPIKeyRequired
	}

	endpoint, err := url.Parse(c.baseURL + "/players/quick-match?include=identifier")
	if err != nil {
		return PlayerProfile{}, err
	}

	body, err := json.Marshal(map[string]any{
		"data": []map[string]any{
			{
				"type": "identifier",
				"attributes": map[string]any{
					"type":       "steamID",
					"identifier": steamID,
				},
			},
		},
	})
	if err != nil {
		return PlayerProfile{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return PlayerProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	httpClient, proxyEndpoint, proxied := c.provider.Prepare()
	log.Printf("[battlemetrics] sending request host=%s proxied=%t proxy=%q", endpoint.Host, proxied, outboundhttp.RedactProxyEndpoint(proxyEndpoint))

	response, err := httpClient.Do(req)
	if err != nil {
		return PlayerProfile{}, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return PlayerProfile{}, ErrPlayerNotFound
	}
	if response.StatusCode != http.StatusOK {
		return PlayerProfile{}, fmt.Errorf("battlemetrics returned status %d", response.StatusCode)
	}

	var payload struct {
		Data []struct {
			Attributes struct {
				Type       string `json:"type"`
				Identifier string `json:"identifier"`
			} `json:"attributes"`
			Relationships struct {
				Player struct {
					Data struct {
						ID string `json:"id"`
					} `json:"data"`
				} `json:"player"`
			} `json:"relationships"`
		} `json:"data"`
		Included []includedResource `json:"included"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return PlayerProfile{}, err
	}
	if len(payload.Data) == 0 {
		return PlayerProfile{}, ErrPlayerNotFound
	}

	playerID := payload.Data[0].Relationships.Player.Data.ID
	if playerID == "" {
		return PlayerProfile{}, ErrPlayerNotFound
	}

	player := playerResource{ID: playerID}
	for _, item := range payload.Included {
		if item.Type != "player" || item.ID != playerID {
			continue
		}
		player.Attributes.Name = item.Attributes.Name
		break
	}

	profile := parsePlayerProfile(player, payload.Included)
	if profile.SteamID == "" && payload.Data[0].Attributes.Type == "steamID" {
		profile.SteamID = payload.Data[0].Attributes.Identifier
	}
	if profile.SteamID == "" {
		profile.SteamID = steamID
	}

	return profile, nil
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

	if response.StatusCode == http.StatusNotFound {
		return PlayerProfile{}, ErrPlayerNotFound
	}
	if response.StatusCode != http.StatusOK {
		return PlayerProfile{}, fmt.Errorf("battlemetrics returned status %d", response.StatusCode)
	}

	var payload struct {
		Data     playerResource     `json:"data"`
		Included []includedResource `json:"included"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return PlayerProfile{}, err
	}

	return parsePlayerProfile(payload.Data, payload.Included), nil
}
