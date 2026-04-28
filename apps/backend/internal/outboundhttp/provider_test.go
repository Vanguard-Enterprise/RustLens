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

func TestReplaceUpdatesProxySet(t *testing.T) {
	provider, err := NewProvider(false, nil, 10*time.Second)
	if err != nil {
		t.Fatalf("NewProvider returned error: %v", err)
	}

	if err := provider.Replace(true, []string{"http://proxy-two:8080"}); err != nil {
		t.Fatalf("Replace returned error: %v", err)
	}

	_, endpoint, proxied := provider.Prepare()
	if !proxied {
		t.Fatal("expected proxied request")
	}
	if endpoint != "http://proxy-two:8080" {
		t.Fatalf("endpoint = %q", endpoint)
	}
}

func TestReplaceRejectsInvalidProxyWithoutMutating(t *testing.T) {
	provider, err := NewProvider(true, []string{"http://proxy-one:8080"}, 10*time.Second)
	if err != nil {
		t.Fatalf("NewProvider returned error: %v", err)
	}

	err = provider.Replace(true, []string{"not-a-url"})
	if err == nil {
		t.Fatal("expected Replace error")
	}

	_, endpoint, proxied := provider.Prepare()
	if !proxied || endpoint != "http://proxy-one:8080" {
		t.Fatalf("provider mutated after failed Replace: endpoint=%q proxied=%t", endpoint, proxied)
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

func TestRedactProxyEndpointRemovesCredentials(t *testing.T) {
	got := RedactProxyEndpoint("http://user:secret@proxy-one:8080")
	if got != "http://proxy-one:8080" {
		t.Fatalf("RedactProxyEndpoint = %q", got)
	}
}
