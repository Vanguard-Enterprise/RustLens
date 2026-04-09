package outboundhttp

import (
	"net/http"
	"testing"
	"time"
)

func TestNewProviderRequiresEndpointsWhenEnabled(t *testing.T) {
	_, err := NewProvider(true, nil, 10*time.Second)
	if err == nil {
		t.Fatal("expected error when proxying enabled without endpoints")
	}
}

func TestPrepareRoundRobin(t *testing.T) {
	provider, err := NewProvider(true, []string{
		"http://proxy-one:8080",
		"http://proxy-two:8080",
	}, 10*time.Second)
	if err != nil {
		t.Fatalf("NewProvider returned error: %v", err)
	}

	_, first, proxied := provider.Prepare()
	if !proxied {
		t.Fatal("expected proxied request")
	}
	_, second, _ := provider.Prepare()
	_, third, _ := provider.Prepare()

	if first != "http://proxy-one:8080" {
		t.Fatalf("first proxy = %q", first)
	}
	if second != "http://proxy-two:8080" {
		t.Fatalf("second proxy = %q", second)
	}
	if third != "http://proxy-one:8080" {
		t.Fatalf("third proxy = %q", third)
	}
}

func TestPrepareDirectWhenDisabled(t *testing.T) {
	provider, err := NewProvider(false, []string{"http://proxy-one:8080"}, 10*time.Second)
	if err != nil {
		t.Fatalf("NewProvider returned error: %v", err)
	}

	client, endpoint, proxied := provider.Prepare()
	if client == nil {
		t.Fatal("expected direct client")
	}
	if proxied {
		t.Fatal("expected proxied to be false")
	}
	if endpoint != "" {
		t.Fatalf("endpoint = %q, want empty", endpoint)
	}
}

func TestPrepareNilProviderFallsBackToDefaultClient(t *testing.T) {
	var provider *Provider

	client, endpoint, proxied := provider.Prepare()
	if client != http.DefaultClient {
		t.Fatal("expected default client")
	}
	if proxied {
		t.Fatal("expected proxied to be false")
	}
	if endpoint != "" {
		t.Fatalf("endpoint = %q, want empty", endpoint)
	}
}

func TestRedactProxyEndpointPreservesUsernameOnly(t *testing.T) {
	got := RedactProxyEndpoint("http://user:secret@proxy-one:8080")
	if got != "http://user@proxy-one:8080" {
		t.Fatalf("RedactProxyEndpoint = %q", got)
	}
}
