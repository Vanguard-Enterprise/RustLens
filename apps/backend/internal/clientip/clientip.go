// Package clientip extracts the real client IP and Cloudflare-provided
// geo metadata from incoming HTTP requests.
//
// Edge deployments (Cloudflare in front of our reverse proxy) inject
// CF-Connecting-IP and CF-IPCountry. Trusting those values is only safe
// when the edge strips them from inbound traffic before re-injecting,
// so the trust toggle is opt-in via TRUST_PROXY_HEADERS=true.
package clientip

import (
	"net"
	"net/http"
	"strings"
)

// Headers carries the set of headers consulted to resolve the real client
// IP and the Cloudflare-provided country code. A zero value falls back to
// http.Request.RemoteAddr only.
type Headers struct {
	// Trust enables reading edge-injected headers. When false, only
	// RemoteAddr is used regardless of the other fields.
	Trust bool

	// IPHeader is the canonical client-IP header injected by the edge,
	// e.g. "CF-Connecting-IP". Case-insensitive.
	IPHeader string

	// CountryHeader is the country-code header injected by the edge,
	// e.g. "CF-IPCountry". Case-insensitive.
	CountryHeader string
}

// Extract returns the best-guess client IP for r. Without trusted-edge
// configuration it returns the bare RemoteAddr (port stripped) so the
// `inet` column in admin audit entries stays acceptable.
func Extract(r *http.Request, h Headers) string {
	if r == nil {
		return ""
	}
	if h.Trust && h.IPHeader != "" {
		if ip := firstAddr(r.Header.Get(h.IPHeader)); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// Country returns the Cloudflare country code (ISO-3166 alpha-2, or "XX"
// for unknown / "T1" for Tor) when trusted-edge headers are enabled.
// Returns "" when the header is missing or trust is off.
func Country(r *http.Request, h Headers) string {
	if r == nil || !h.Trust || h.CountryHeader == "" {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(r.Header.Get(h.CountryHeader)))
}

// firstAddr extracts the first comma-separated token (some edges chain
// multiple values) and validates it parses as an IP. Invalid values
// fall through so the caller can use RemoteAddr instead.
func firstAddr(raw string) string {
	if raw == "" {
		return ""
	}
	if i := strings.IndexByte(raw, ','); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if net.ParseIP(raw) == nil {
		return ""
	}
	return raw
}
