package battlemetrics

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestFetchServerPlayersFiltersIncludedPlayers(t *testing.T) {
	var capturedAuth string
	var capturedAccept string
	client := &Client{
		baseURL: "https://api.battlemetrics.com",
		apiKey:  "test-api-key",
		provider: staticProvider{client: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				capturedAuth = request.Header.Get("Authorization")
				capturedAccept = request.Header.Get("Accept")
				body := `{
					"data": {
						"type": "server",
						"id": "16741517"
					},
					"included": [
						{
							"type": "player",
							"id": "1179655263",
							"attributes": {
								"id": "1179655263",
								"name": "turn it up"
							}
						},
						{
							"type": "server",
							"id": "16741517",
							"attributes": {
								"name": "Atlas"
							}
						}
					]
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}),
		}},
	}

	players, err := client.FetchServerPlayers(context.Background(), "16741517")
	if err != nil {
		t.Fatalf("FetchServerPlayers returned error: %v", err)
	}

	if len(players) != 1 {
		t.Fatalf("expected 1 player, got %d", len(players))
	}

	if players[0].BmID != "1179655263" {
		t.Fatalf("unexpected player id: %s", players[0].BmID)
	}

	if players[0].Name != "turn it up" {
		t.Fatalf("unexpected player name: %s", players[0].Name)
	}

	if capturedAuth != "Bearer test-api-key" {
		t.Fatalf("unexpected auth header: %s", capturedAuth)
	}

	if capturedAccept != "application/json" {
		t.Fatalf("unexpected accept header: %s", capturedAccept)
	}
}

func TestFetchServerPlayersLogsSelectedProxy(t *testing.T) {
	var logOutput strings.Builder
	originalWriter := log.Writer()
	log.SetOutput(&logOutput)
	defer log.SetOutput(originalWriter)

	client := &Client{
		baseURL: "https://api.battlemetrics.com",
		provider: staticProvider{
			client: &http.Client{
				Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"included":[]}`)),
						Header:     make(http.Header),
					}, nil
				}),
			},
			endpoint: "http://user:secret@proxy-one:8080",
			proxied:  true,
		},
	}

	if _, err := client.FetchServerPlayers(context.Background(), "16741517"); err != nil {
		t.Fatalf("FetchServerPlayers returned error: %v", err)
	}

	logged := logOutput.String()
	if !strings.Contains(logged, `proxied=true`) {
		t.Fatalf("expected proxied log entry, got %q", logged)
	}
	if !strings.Contains(logged, `proxy="http://user@proxy-one:8080"`) {
		t.Fatalf("expected redacted proxy endpoint in log, got %q", logged)
	}
}

func TestFetchPlayerProfileParsesSteamIDIdentifier(t *testing.T) {
	client := &Client{
		baseURL: "https://api.battlemetrics.com",
		provider: staticProvider{client: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body := `{
					"data": {
						"type": "player",
						"id": "918543318",
						"attributes": {
							"name": "Kappu",
							"createdAt": "2019-12-15T15:49:15.568Z"
						}
					},
					"included": [
						{
							"type": "identifier",
							"id": "272422867",
							"attributes": {
								"type": "steamID",
								"identifier": "76561198888013469"
							}
						},
						{
							"type": "identifier",
							"id": "272422868",
							"attributes": {
								"type": "name",
								"identifier": "Kappu",
								"lastSeen": "2026-04-24T02:38:08.723Z"
							}
						}
					]
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}),
		}},
	}

	profile, err := client.FetchPlayerProfile(context.Background(), "918543318")
	if err != nil {
		t.Fatalf("FetchPlayerProfile returned error: %v", err)
	}

	if profile.SteamID != "76561198888013469" {
		t.Fatalf("unexpected steam id: %s", profile.SteamID)
	}
	if len(profile.NameIdentifiers) != 1 || profile.NameIdentifiers[0].Name != "Kappu" {
		t.Fatalf("unexpected name identifiers: %#v", profile.NameIdentifiers)
	}
}

func TestFetchPlayerSessionsAllServersDoesNotSendServerFilter(t *testing.T) {
	var capturedQuery string
	client := &Client{
		baseURL: "https://api.battlemetrics.com",
		provider: staticProvider{client: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				capturedQuery = request.URL.RawQuery
				body := `{
					"data": [
						{
							"type": "session",
							"attributes": {
								"start": "2026-04-25T10:00:00Z",
								"stop": "2026-04-25T11:00:00Z"
							}
						}
					]
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}),
		}},
	}

	sessions, err := client.FetchPlayerSessionsAllServers(context.Background(), "918543318", 100, 2)
	if err != nil {
		t.Fatalf("FetchPlayerSessionsAllServers returned error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if strings.Contains(capturedQuery, "filter%5Bservers%5D") || strings.Contains(capturedQuery, "filter[servers]") {
		t.Fatalf("expected no server filter, got query %q", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "page%5Bsize%5D=100") {
		t.Fatalf("expected page size in query, got %q", capturedQuery)
	}
}

func TestQuickMatchPlayerProfileBySteamIDUsesAuthorizedPost(t *testing.T) {
	var capturedMethod string
	var capturedAuth string
	var capturedAccept string
	var capturedContentType string
	var capturedQuery string
	var capturedBody string

	client := &Client{
		baseURL: "https://api.battlemetrics.com",
		apiKey:  "test-api-key",
		provider: staticProvider{client: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				capturedMethod = request.Method
				capturedAuth = request.Header.Get("Authorization")
				capturedAccept = request.Header.Get("Accept")
				capturedContentType = request.Header.Get("Content-Type")
				capturedQuery = request.URL.RawQuery
				body, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				capturedBody = string(body)

				responseBody := `{
					"data": [
						{
							"type": "identifier",
							"attributes": {
								"type": "steamID",
								"identifier": "76561198888013469"
							},
							"relationships": {
								"player": {
									"data": {
										"type": "player",
										"id": "918543318"
									}
								}
							}
						}
					],
					"included": [
						{
							"type": "identifier",
							"id": "272422868",
							"attributes": {
								"type": "name",
								"identifier": "Kappu"
							}
						}
					]
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(responseBody)),
					Header:     make(http.Header),
				}, nil
			}),
		}},
	}

	profile, err := client.QuickMatchPlayerProfileBySteamID(context.Background(), "76561198888013469")
	if err != nil {
		t.Fatalf("QuickMatchPlayerProfileBySteamID returned error: %v", err)
	}

	if capturedMethod != http.MethodPost {
		t.Fatalf("unexpected method: %s", capturedMethod)
	}
	if capturedAuth != "Bearer test-api-key" {
		t.Fatalf("unexpected auth header: %s", capturedAuth)
	}
	if capturedAccept != "application/json" {
		t.Fatalf("unexpected accept header: %s", capturedAccept)
	}
	if capturedContentType != "application/json" {
		t.Fatalf("unexpected content-type header: %s", capturedContentType)
	}
	if capturedQuery != "include=identifier" {
		t.Fatalf("unexpected query string: %s", capturedQuery)
	}
	if !strings.Contains(capturedBody, `"identifier":"76561198888013469"`) {
		t.Fatalf("unexpected request body: %s", capturedBody)
	}
	if profile.BmID != "918543318" {
		t.Fatalf("unexpected bm id: %s", profile.BmID)
	}
	if profile.SteamID != "76561198888013469" {
		t.Fatalf("unexpected steam id: %s", profile.SteamID)
	}
	if profile.BmName != "" {
		t.Fatalf("unexpected bm name: %s", profile.BmName)
	}
	if len(profile.Servers) != 0 {
		t.Fatalf("unexpected servers: %#v", profile.Servers)
	}
}

type staticProvider struct {
	client   *http.Client
	endpoint string
	proxied  bool
}

func (p staticProvider) Prepare() (*http.Client, string, bool) {
	return p.client, p.endpoint, p.proxied
}
