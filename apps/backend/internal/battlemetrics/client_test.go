package battlemetrics

import (
	"context"
	"io"
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
		httpClient: &http.Client{
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
		},
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
