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
)

const defaultBaseURL = "https://api.steampowered.com"

var (
	steamIDPattern             = regexp.MustCompile(`^\d{17}$`)
	ErrUnsupportedProfileInput = errors.New("input must be a 17-digit Steam ID or a supported steamcommunity.com profile URL")
	ErrSteamAPIKeyRequired     = errors.New("steam api key is required to resolve vanity URLs")
	ErrVanityNotFound          = errors.New("steam vanity URL could not be resolved")
)

type Client struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		baseURL: defaultBaseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 20 * time.Second},
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

	log.Printf("[steamapi] sending request host=%s proxied=false proxy=%q", endpoint.Host, "")

	response, err := c.client.Do(req)
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
