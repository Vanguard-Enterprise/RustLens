package outboundhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var ErrProxyEndpointsRequired = errors.New("proxy endpoints are required when proxying is enabled")

const (
	proxyHealthWindow      = 10 * time.Minute
	proxyHealthMinRequests = 10
)

type Provider struct {
	mu                  sync.RWMutex
	enabled             bool
	direct              *http.Client
	proxies             []proxyClient
	next                atomic.Uint64
	timeout             time.Duration
	results             map[string][]proxyRequestResult
	autoDisabled        map[string]bool
	autoDisableCallback func(string)
}

type proxyClient struct {
	endpoint         string
	redactedEndpoint string
	client           *http.Client
}

type proxyRequestResult struct {
	at      time.Time
	success bool
}

type proxyStatsTransport struct {
	base             http.RoundTripper
	redactedEndpoint string
	record           func(string, bool)
}

func (t proxyStatsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(req)
	success := err == nil && response != nil && response.StatusCode < http.StatusInternalServerError && response.StatusCode != http.StatusTooManyRequests
	t.record(t.redactedEndpoint, success)
	return response, err
}

func NewProvider(enabled bool, endpoints []string, timeout time.Duration) (*Provider, error) {
	direct := &http.Client{
		Timeout: timeout,
		Transport: otelhttp.NewTransport(http.DefaultTransport,
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return r.Method + " " + r.URL.Host
			}),
		),
	}
	provider := &Provider{
		enabled:      enabled,
		direct:       direct,
		timeout:      timeout,
		results:      make(map[string][]proxyRequestResult),
		autoDisabled: make(map[string]bool),
	}

	proxies, err := provider.buildProxyClients(enabled, endpoints)
	if err != nil {
		return nil, err
	}
	provider.proxies = proxies

	return provider, nil
}

func (p *Provider) buildProxyClients(enabled bool, endpoints []string) ([]proxyClient, error) {
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
		redactedEndpoint := RedactProxyEndpoint(proxyURL.String())

		statsTransport := proxyStatsTransport{
			base:             transport,
			redactedEndpoint: redactedEndpoint,
			record:           p.recordProxyResult,
		}
		proxies = append(proxies, proxyClient{
			endpoint:         proxyURL.String(),
			redactedEndpoint: redactedEndpoint,
			client: &http.Client{
				Timeout: p.timeout,
				Transport: otelhttp.NewTransport(statsTransport,
					otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
						return r.Method + " " + r.URL.Host
					}),
				),
			},
		})
	}

	return proxies, nil
}

func (p *Provider) Replace(enabled bool, endpoints []string) error {
	if p == nil {
		return nil
	}

	proxies, err := p.buildProxyClients(enabled, endpoints)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled = enabled
	p.proxies = proxies
	p.results = make(map[string][]proxyRequestResult)
	p.autoDisabled = make(map[string]bool)
	p.next.Store(0)
	return nil
}

func (p *Provider) SetAutoDisableCallback(callback func(string)) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.autoDisableCallback = callback
}

func (p *Provider) recordProxyResult(redactedEndpoint string, success bool) {
	if p == nil || redactedEndpoint == "" {
		return
	}

	now := time.Now()
	cutoff := now.Add(-proxyHealthWindow)
	var callback func(string)
	shouldDisable := false

	p.mu.Lock()
	results := append(p.results[redactedEndpoint], proxyRequestResult{at: now, success: success})
	kept := results[:0]
	failures := 0
	for _, result := range results {
		if result.at.Before(cutoff) {
			continue
		}
		if !result.success {
			failures++
		}
		kept = append(kept, result)
	}
	p.results[redactedEndpoint] = kept

	if len(kept) >= proxyHealthMinRequests && failures*2 >= len(kept) && !p.autoDisabled[redactedEndpoint] {
		p.autoDisabled[redactedEndpoint] = true
		p.removeProxyLocked(redactedEndpoint)
		callback = p.autoDisableCallback
		shouldDisable = true
	}
	p.mu.Unlock()

	if shouldDisable {
		autoDisabledCounter.Add(context.Background(), 1, metric.WithAttributes(attribute.String("target", classifyEndpoint(redactedEndpoint))))
		if callback != nil {
			go callback(redactedEndpoint)
		}
	}
}

func (p *Provider) removeProxyLocked(redactedEndpoint string) {
	kept := p.proxies[:0]
	for _, proxy := range p.proxies {
		if proxy.redactedEndpoint == redactedEndpoint {
			continue
		}
		kept = append(kept, proxy)
	}
	p.proxies = kept
	p.next.Store(0)
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
