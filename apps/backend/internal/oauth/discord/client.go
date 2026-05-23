package discord

import (
	"bytes"
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

// UserGuild is the subset of fields returned by GET /users/@me/guilds we use.
type UserGuild struct {
	ID                     string
	Name                   string
	IconHash               string
	ApproximateMemberCount *int32
	Owner                  bool
}

// FetchUserGuilds retrieves the authenticated user's guild list from
// /users/@me/guilds. Requires the "guilds" OAuth scope.
func (c *Client) FetchUserGuilds(ctx context.Context, accessToken string) ([]UserGuild, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/users/@me/guilds?with_counts=true", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Cache-Control", "no-store")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("discord guilds fetch: %d %s", resp.StatusCode, string(body))
	}
	var raw []struct {
		ID                     string  `json:"id"`
		Name                   string  `json:"name"`
		Icon                   *string `json:"icon"`
		Owner                  bool    `json:"owner"`
		ApproximateMemberCount *int32  `json:"approximate_member_count"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	out := make([]UserGuild, 0, len(raw))
	for _, g := range raw {
		ug := UserGuild{
			ID:                     g.ID,
			Name:                   g.Name,
			Owner:                  g.Owner,
			ApproximateMemberCount: g.ApproximateMemberCount,
		}
		if g.Icon != nil {
			ug.IconHash = *g.Icon
		}
		out = append(out, ug)
	}
	return out, nil
}

// ErrRefreshRevoked indicates that Discord rejected the refresh token as
// no longer valid. Callers should mark the row revoked and prompt re-link.
var ErrRefreshRevoked = errors.New("discord refresh token revoked")

// RefreshToken exchanges a long-lived refresh_token for a new access_token
// (and possibly a rotated refresh_token). Returns ErrRefreshRevoked when
// Discord responds 400 with error="invalid_grant".
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (access, refresh string, expiresIn int, err error) {
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cache-Control", "no-store")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusBadRequest && strings.Contains(string(body), "invalid_grant") {
		return "", "", 0, ErrRefreshRevoked
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", 0, fmt.Errorf("discord refresh: %d %s", resp.StatusCode, string(body))
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", 0, err
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		return "", "", 0, errors.New("discord refresh: empty access_token or refresh_token")
	}
	return out.AccessToken, out.RefreshToken, out.ExpiresIn, nil
}

// ErrAccessTokenInvalid indicates the access token is expired or revoked.
// Callers should attempt a refresh, then retry once.
var ErrAccessTokenInvalid = errors.New("discord access token invalid")

// RoleConnectionBody is the JSON payload for PUT /users/@me/applications/{id}/role-connection.
type RoleConnectionBody struct {
	PlatformName     string            `json:"platform_name"`
	PlatformUsername string            `json:"platform_username,omitempty"`
	Metadata         map[string]string `json:"metadata"`
}

// PutRoleConnection pushes the caller's role-connection metadata for our app.
// Returns ErrAccessTokenInvalid on 401 so the caller can refresh-and-retry.
func (c *Client) PutRoleConnection(ctx context.Context, accessToken, applicationID string, body RoleConnectionBody) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.baseURL+"/api/users/@me/applications/"+applicationID+"/role-connection",
		bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Cache-Control", "no-store")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrAccessTokenInvalid
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord put role-connection: %d %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ApplicationMetadataRecord is one entry in the role-connections metadata schema.
// Type values per https://discord.com/developers/docs/resources/application-role-connection-metadata
// (6 = datetime_greater_than_or_equal, 7 = boolean_equal).
type ApplicationMetadataRecord struct {
	Type        int    `json:"type"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// RegisterApplicationMetadata PUTs the role-connections metadata schema for
// the application. Idempotent - PUT replaces the entire schema. Authenticated
// with the bot token, NOT the OAuth client secret.
func (c *Client) RegisterApplicationMetadata(ctx context.Context, applicationID, botToken string, records []ApplicationMetadataRecord) error {
	payload, err := json.Marshal(records)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.baseURL+"/api/applications/"+applicationID+"/role-connections/metadata",
		bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+botToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord register app metadata: %d %s", resp.StatusCode, string(body))
	}
	return nil
}

// ExchangeCodeForTokens is like ExchangeCode but additionally returns the
// refresh_token and expires_in fields. Used by the role-connection flow which
// needs long-lived push access.
func (c *Client) ExchangeCodeForTokens(ctx context.Context, code, codeVerifier string) (access, refresh string, expiresIn int, err error) {
	if c.redirectURI == "" {
		return "", "", 0, errors.New("discord client: redirect_uri is required for code exchange")
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
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cache-Control", "no-store")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", 0, fmt.Errorf("discord token exchange: %d %s", resp.StatusCode, string(body))
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", 0, err
	}
	if out.AccessToken == "" {
		return "", "", 0, errors.New("discord token exchange: empty access_token")
	}
	return out.AccessToken, out.RefreshToken, out.ExpiresIn, nil
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
