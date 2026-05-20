package clientip

import (
	"net/http/httptest"
	"testing"
)

func TestExtract_TrustOff_IgnoresHeaders(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:55555"
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")

	got := Extract(r, Headers{IPHeader: "CF-Connecting-IP"})
	if got != "10.0.0.1" {
		t.Fatalf("expected RemoteAddr when trust is off, got %q", got)
	}
}

func TestExtract_TrustOn_UsesConfiguredHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:55555"
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")
	r.Header.Set("X-Forwarded-For", "198.51.100.9")

	got := Extract(r, Headers{Trust: true, IPHeader: "CF-Connecting-IP"})
	if got != "203.0.113.7" {
		t.Fatalf("expected CF-Connecting-IP, got %q", got)
	}
}

func TestExtract_InvalidHeader_FallsBackToRemoteAddr(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:55555"
	r.Header.Set("CF-Connecting-IP", "not-an-ip")

	got := Extract(r, Headers{Trust: true, IPHeader: "CF-Connecting-IP"})
	if got != "10.0.0.1" {
		t.Fatalf("expected RemoteAddr fallback on invalid header, got %q", got)
	}
}

func TestExtract_CommaSeparatedHeader_TakesFirst(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:55555"
	r.Header.Set("CF-Connecting-IP", "203.0.113.7, 198.51.100.9")

	got := Extract(r, Headers{Trust: true, IPHeader: "CF-Connecting-IP"})
	if got != "203.0.113.7" {
		t.Fatalf("expected first comma-separated value, got %q", got)
	}
}

func TestExtract_NoPortRemoteAddr(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1"

	got := Extract(r, Headers{})
	if got != "10.0.0.1" {
		t.Fatalf("expected raw RemoteAddr when no port, got %q", got)
	}
}

func TestCountry_TrustOff_ReturnsEmpty(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("CF-IPCountry", "US")

	if got := Country(r, Headers{CountryHeader: "CF-IPCountry"}); got != "" {
		t.Fatalf("expected empty when trust off, got %q", got)
	}
}

func TestCountry_TrustOn_NormalizesToUpper(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("CF-IPCountry", "us")

	got := Country(r, Headers{Trust: true, CountryHeader: "CF-IPCountry"})
	if got != "US" {
		t.Fatalf("expected normalized 'US', got %q", got)
	}
}
