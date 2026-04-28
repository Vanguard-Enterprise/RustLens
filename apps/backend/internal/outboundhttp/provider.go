package outboundhttp

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

var ErrProxyEndpointsRequired = errors.New("proxy endpoints are required when proxying is enabled")

type Provider struct {
	mu      sync.RWMutex
	enabled bool
	direct  *http.Client
	proxies []proxyClient
	next    atomic.Uint64
	timeout time.Duration
}

type proxyClient struct {
	endpoint string
	client   *http.Client
}

func NewProvider(enabled bool, endpoints []string, timeout time.Duration) (*Provider, error) {
	direct := &http.Client{Timeout: timeout}
	proxies, err := buildProxyClients(enabled, endpoints, timeout)
	if err != nil {
		return nil, err
	}

	return &Provider{
		enabled: enabled,
		direct:  direct,
		proxies: proxies,
		timeout: timeout,
	}, nil
}

func buildProxyClients(enabled bool, endpoints []string, timeout time.Duration) ([]proxyClient, error) {
	if !enabled {
		return nil, nil
	}

	if len(endpoints) == 0 {
		return nil, ErrProxyEndpointsRequired
	}

	proxies := make([]proxyClient, 0, len(endpoints))
	for _, endpoint := range endpoints {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("parse proxy endpoint %q: %w", endpoint, err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("proxy endpoint %q must include scheme and host", endpoint)
		}

		proxyURL := parsed
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyURL(proxyURL)

		proxies = append(proxies, proxyClient{
			endpoint: proxyURL.String(),
			client: &http.Client{
				Timeout:   timeout,
				Transport: transport,
			},
		})
	}

	return proxies, nil
}

func (p *Provider) Replace(enabled bool, endpoints []string) error {
	if p == nil {
		return nil
	}

	proxies, err := buildProxyClients(enabled, endpoints, p.timeout)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled = enabled
	p.proxies = proxies
	p.next.Store(0)
	return nil
}

func (p *Provider) ProxyCount() int {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.enabled {
		return 0
	}
	return len(p.proxies)
}

func (p *Provider) Prepare() (*http.Client, string, bool) {
	if p == nil {
		return http.DefaultClient, "", false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.enabled || len(p.proxies) == 0 {
		return p.direct, "", false
	}

	index := p.next.Add(1) - 1
	selected := p.proxies[index%uint64(len(p.proxies))]
	return selected.client, selected.endpoint, true
}

func RedactProxyEndpoint(raw string) string {
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.User == nil {
		return parsed.String()
	}

	parsed.User = nil
	return parsed.String()
}
