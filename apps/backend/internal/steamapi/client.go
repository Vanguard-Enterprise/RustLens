package steamapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"rustlens/backend/internal/outboundhttp"
)

const defaultBaseURL = "https://api.steampowered.com"

var (
	steamIDPattern             = regexp.MustCompile(`^\d{17}$`)
	ErrUnsupportedProfileInput = errors.New("input must be a 17-digit Steam ID or a supported steamcommunity.com profile URL")
	ErrSteamAPIKeyRequired     = errors.New("steam api key is required to resolve vanity URLs")
	ErrVanityNotFound          = errors.New("steam vanity URL could not be resolved")
)

type Client struct {
	baseURL  string
	apiKey   string
	provider requestPreparer
}

type PlayerSummary struct {
	SteamID    string
	AvatarFull string
}

type OwnedGame struct {
	AppID           int
	Name            string
	PlaytimeForever int64
}

type PlayerBan struct {
	SteamID          string
	CommunityBanned  bool
	VACBanned        bool
	NumberOfVACBans  int
	DaysSinceLastBan int
	NumberOfGameBans int
	EconomyBan       string
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

func (c *Client) ResolveSteamID(ctx context.Context, input string) (string, error) {
	normalized := strings.TrimSpace(input)
	if normalized == "" {
		return "", ErrUnsupportedProfileInput
	}

	if steamIDPattern.MatchString(normalized) {
		return normalized, nil
	}

	profileURL, err := url.Parse(normalized)
	if err != nil {
		return "", ErrUnsupportedProfileInput
	}
	if !strings.EqualFold(profileURL.Scheme, "https") && !strings.EqualFold(profileURL.Scheme, "http") {
		return "", ErrUnsupportedProfileInput
	}
	if !strings.EqualFold(profileURL.Hostname(), "steamcommunity.com") && !strings.EqualFold(profileURL.Hostname(), "www.steamcommunity.com") {
		return "", ErrUnsupportedProfileInput
	}

	segments := strings.Split(strings.Trim(profileURL.Path, "/"), "/")
	if len(segments) < 2 {
		return "", ErrUnsupportedProfileInput
	}

	switch strings.ToLower(segments[0]) {
	case "profiles":
		if !steamIDPattern.MatchString(segments[1]) {
			return "", ErrUnsupportedProfileInput
		}
		return segments[1], nil
	case "id":
		vanity := strings.TrimSpace(segments[1])
		if vanity == "" {
			return "", ErrUnsupportedProfileInput
		}
		if steamIDPattern.MatchString(vanity) {
			return vanity, nil
		}
		return c.resolveVanityURL(ctx, vanity)
	default:
		return "", ErrUnsupportedProfileInput
	}
}

func (c *Client) resolveVanityURL(ctx context.Context, vanity string) (string, error) {
	if c.apiKey == "" {
		return "", ErrSteamAPIKeyRequired
	}

	endpoint, err := url.Parse(c.baseURL + "/ISteamUser/ResolveVanityURL/v1/")
	if err != nil {
		return "", err
	}

	query := endpoint.Query()
	query.Set("key", c.apiKey)
	query.Set("vanityurl", vanity)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	httpClient, proxyEndpoint, proxied := c.provider.Prepare()
	log.Printf("[steamapi] sending request host=%s proxied=%t proxy=%q", endpoint.Host, proxied, outboundhttp.RedactProxyEndpoint(proxyEndpoint))

	response, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("steam returned status %d", response.StatusCode)
	}

	var payload struct {
		Response struct {
			SteamID string `json:"steamid"`
			Success int    `json:"success"`
			Message string `json:"message"`
		} `json:"response"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", err
	}

	if payload.Response.SteamID != "" && steamIDPattern.MatchString(payload.Response.SteamID) {
		return payload.Response.SteamID, nil
	}

	return "", ErrVanityNotFound
}

func (c *Client) GetPlayerSummary(ctx context.Context, steamID string) (PlayerSummary, error) {
	if !steamIDPattern.MatchString(strings.TrimSpace(steamID)) {
		return PlayerSummary{}, ErrUnsupportedProfileInput
	}
	if c.apiKey == "" {
		return PlayerSummary{}, ErrSteamAPIKeyRequired
	}

	endpoint, err := url.Parse(c.baseURL + "/ISteamUser/GetPlayerSummaries/v2/")
	if err != nil {
		return PlayerSummary{}, err
	}

	query := endpoint.Query()
	query.Set("key", c.apiKey)
	query.Set("steamids", steamID)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return PlayerSummary{}, err
	}
	req.Header.Set("Accept", "application/json")

	httpClient, proxyEndpoint, proxied := c.provider.Prepare()
	log.Printf("[steamapi] sending request host=%s proxied=%t proxy=%q", endpoint.Host, proxied, outboundhttp.RedactProxyEndpoint(proxyEndpoint))

	response, err := httpClient.Do(req)
	if err != nil {
		return PlayerSummary{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return PlayerSummary{}, fmt.Errorf("steam returned status %d", response.StatusCode)
	}

	var payload struct {
		Response struct {
			Players []struct {
				SteamID    string `json:"steamid"`
				AvatarFull string `json:"avatarfull"`
			} `json:"players"`
		} `json:"response"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return PlayerSummary{}, err
	}

	if len(payload.Response.Players) == 0 {
		return PlayerSummary{}, ErrVanityNotFound
	}

	return PlayerSummary{
		SteamID:    payload.Response.Players[0].SteamID,
		AvatarFull: payload.Response.Players[0].AvatarFull,
	}, nil
}

func (c *Client) GetOwnedGames(ctx context.Context, steamID string) ([]OwnedGame, error) {
	if !steamIDPattern.MatchString(strings.TrimSpace(steamID)) {
		return nil, ErrUnsupportedProfileInput
	}
	if c.apiKey == "" {
		return nil, ErrSteamAPIKeyRequired
	}

	endpoint, err := url.Parse(c.baseURL + "/IPlayerService/GetOwnedGames/v1/")
	if err != nil {
		return nil, err
	}

	query := endpoint.Query()
	query.Set("key", c.apiKey)
	query.Set("steamid", steamID)
	query.Set("include_appinfo", "true")
	query.Set("include_played_free_games", "true")
	query.Set("format", "json")
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	httpClient, proxyEndpoint, proxied := c.provider.Prepare()
	log.Printf("[steamapi] sending request host=%s proxied=%t proxy=%q", endpoint.Host, proxied, outboundhttp.RedactProxyEndpoint(proxyEndpoint))

	response, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steam returned status %d", response.StatusCode)
	}

	var payload struct {
		Response struct {
			Games []struct {
				AppID           int    `json:"appid"`
				Name            string `json:"name"`
				PlaytimeForever int64  `json:"playtime_forever"`
			} `json:"games"`
		} `json:"response"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}

	games := make([]OwnedGame, 0, len(payload.Response.Games))
	for _, game := range payload.Response.Games {
		games = append(games, OwnedGame{
			AppID:           game.AppID,
			Name:            game.Name,
			PlaytimeForever: game.PlaytimeForever,
		})
	}

	return games, nil
}

func (c *Client) GetPlayerBans(ctx context.Context, steamIDs []string) ([]PlayerBan, error) {
	if len(steamIDs) == 0 {
		return []PlayerBan{}, nil
	}
	if c.apiKey == "" {
		return nil, ErrSteamAPIKeyRequired
	}

	normalized := make([]string, 0, len(steamIDs))
	for _, steamID := range steamIDs {
		trimmed := strings.TrimSpace(steamID)
		if !steamIDPattern.MatchString(trimmed) {
			return nil, ErrUnsupportedProfileInput
		}
		normalized = append(normalized, trimmed)
	}

	endpoint, err := url.Parse(c.baseURL + "/ISteamUser/GetPlayerBans/v1/")
	if err != nil {
		return nil, err
	}

	query := endpoint.Query()
	query.Set("key", c.apiKey)
	query.Set("steamids", strings.Join(normalized, ","))
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	httpClient, proxyEndpoint, proxied := c.provider.Prepare()
	log.Printf("[steamapi] sending request host=%s proxied=%t proxy=%q", endpoint.Host, proxied, outboundhttp.RedactProxyEndpoint(proxyEndpoint))

	response, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steam returned status %d", response.StatusCode)
	}

	var payload struct {
		Players []struct {
			SteamID          string `json:"SteamId"`
			CommunityBanned  bool   `json:"CommunityBanned"`
			VACBanned        bool   `json:"VACBanned"`
			NumberOfVACBans  int    `json:"NumberOfVACBans"`
			DaysSinceLastBan int    `json:"DaysSinceLastBan"`
			NumberOfGameBans int    `json:"NumberOfGameBans"`
			EconomyBan       string `json:"EconomyBan"`
		} `json:"players"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}

	bans := make([]PlayerBan, 0, len(payload.Players))
	for _, player := range payload.Players {
		bans = append(bans, PlayerBan{
			SteamID:          player.SteamID,
			CommunityBanned:  player.CommunityBanned,
			VACBanned:        player.VACBanned,
			NumberOfVACBans:  player.NumberOfVACBans,
			DaysSinceLastBan: player.DaysSinceLastBan,
			NumberOfGameBans: player.NumberOfGameBans,
			EconomyBan:       player.EconomyBan,
		})
	}

	return bans, nil
}
