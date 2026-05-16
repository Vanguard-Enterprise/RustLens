// Package players holds shared player-lookup logic used by both the
// bot-internal HTTP routes and the public /api/app/players/* routes.
package players

import (
	"regexp"
	"strings"
)

// QueryKind is the shape of a raw search-box input.
type QueryKind int

// QueryKind values describe how the search box input was classified.
const (
	KindEmpty    QueryKind = iota // empty / whitespace-only input
	KindSteamID                   // bare 17-digit Steam64 ID
	KindBmID                      // bare BattleMetrics player ID or BM player URL
	KindSteamURL                  // steamcommunity.com profile or vanity URL
	KindName                      // free-form name search fallback
)

var (
	steamIDPattern     = regexp.MustCompile(`^\d{17}$`)
	bmIDPattern        = regexp.MustCompile(`^\d{6,16}$`)
	steamURLPattern    = regexp.MustCompile(`^https?://(?:www\.)?steamcommunity\.com/(?:profiles/(\d{17})|id/[A-Za-z0-9_-]{1,32})/?(?:[?#].*)?$`)
	bmPlayerURLPattern = regexp.MustCompile(`^https?://(?:www\.)?battlemetrics\.com/(?:rcon/)?players/(\d{6,16})(?:[/?#].*)?$`)
)

// ClassifyQuery returns the kind of input the search box was given.
// Order matters: 17-digit numerics are Steam IDs even though they would
// also satisfy the BM ID range; URL forms are checked before bare digits
// to allow embedded IDs to take precedence.
func ClassifyQuery(input string) QueryKind {
	q := strings.TrimSpace(input)
	if q == "" {
		return KindEmpty
	}
	if steamURLPattern.MatchString(q) {
		return KindSteamURL
	}
	if bmPlayerURLPattern.MatchString(q) {
		return KindBmID
	}
	if steamIDPattern.MatchString(q) {
		return KindSteamID
	}
	if bmIDPattern.MatchString(q) {
		return KindBmID
	}
	return KindName
}

// ExtractSteamIDFromURL returns the embedded Steam64 ID from a
// /profiles/<id> URL. Vanity URLs return ("", false) — those need to be
// resolved through the Steam API by the caller.
func ExtractSteamIDFromURL(input string) (string, bool) {
	q := strings.TrimSpace(input)
	m := steamURLPattern.FindStringSubmatch(q)
	if m == nil || m[1] == "" {
		return "", false
	}
	return m[1], true
}

// ExtractBmIDFromInput returns the bare BM ID for either a digits-only
// input or a battlemetrics.com player URL.
func ExtractBmIDFromInput(input string) (string, bool) {
	q := strings.TrimSpace(input)
	if bmIDPattern.MatchString(q) {
		return q, true
	}
	if m := bmPlayerURLPattern.FindStringSubmatch(q); m != nil {
		return m[1], true
	}
	return "", false
}
