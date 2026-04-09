package outboundhttp

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

var ErrProxyEndpointsRequired = errors.New("proxy endpoints are required when proxying is enabled")

type Provider struct {
	enabled bool
	direct  *http.Client
	proxies []proxyClient
	next    atomic.Uint64
}

type proxyClient struct {
	endpoint string
	client   *http.Client
}

func NewProvider(enabled bool, endpoints []string, timeout time.Duration) (*Provider, error) {
	direct := &http.Client{Timeout: timeout}
	if !enabled {
		return &Provider{
			direct: direct,
		}, nil
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

	return &Provider{
		enabled: true,
		direct:  direct,
		proxies: proxies,
	}, nil
}

func (p *Provider) Prepare() (*http.Client, string, bool) {
	if p == nil || !p.enabled || len(p.proxies) == 0 {
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

	username := parsed.User.Username()
	if username == "" {
		username = "redacted"
	}
	parsed.User = url.User(strings.TrimSpace(username))
	return parsed.String()
}
