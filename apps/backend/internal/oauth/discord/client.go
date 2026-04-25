package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Profile is the subset of Discord user fields we use.
type Profile struct {
	ID         string
	Username   string
	GlobalName string
	AvatarHash string // Discord CDN avatar hash, NOT a URL
}

// DisplayName returns global_name when set, falling back to username.
func (p Profile) DisplayName() string {
	if p.GlobalName != "" {
		return p.GlobalName
	}
	return p.Username
}

// Client wraps the two Discord OAuth endpoints we depend on.
type Client struct {
	clientID     string
	clientSecret string
	baseURL      string
	redirectURI  string
	httpClient   *http.Client
	timeout      time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the Discord API base URL (default https://discord.com).
// Used in tests with httptest servers.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithRedirectURI sets the redirect_uri sent during the token exchange. Must
// match what was used in the authorize redirect.
func WithRedirectURI(u string) Option { return func(c *Client) { c.redirectURI = u } }

// WithHTTPClient overrides the http.Client used for outbound calls.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

func NewClient(clientID, clientSecret string, opts ...Option) *Client {
	c := &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		baseURL:      "https://discord.com",
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		timeout:      10 * time.Second,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ExchangeCode trades an authorization code (with PKCE verifier) for an
// access token. Returns the access_token string.
func (c *Client) ExchangeCode(ctx context.Context, code, codeVerifier string) (string, error) {
	if c.redirectURI == "" {
		return "", errors.New("discord client: redirect_uri is required for code exchange")
	}
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {codeVerifier},
		"redirect_uri":  {c.redirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cache-Control", "no-store")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("discord token exchange: %d %s", resp.StatusCode, string(body))
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errors.New("discord token exchange: empty access_token")
	}
	return out.AccessToken, nil
}

// FetchProfile retrieves the authenticated user's profile from /users/@me.
func (c *Client) FetchProfile(ctx context.Context, accessToken string) (Profile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/users/@me", nil)
	if err != nil {
		return Profile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Cache-Control", "no-store")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Profile{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Profile{}, fmt.Errorf("discord profile fetch: %d %s", resp.StatusCode, string(body))
	}
	var raw struct {
		ID         string  `json:"id"`
		Username   string  `json:"username"`
		GlobalName *string `json:"global_name"`
		Avatar     *string `json:"avatar"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Profile{}, err
	}
	p := Profile{
		ID:       raw.ID,
		Username: raw.Username,
	}
	if raw.GlobalName != nil {
		p.GlobalName = *raw.GlobalName
	}
	if raw.Avatar != nil {
		p.AvatarHash = *raw.Avatar
	}
	return p, nil
}
