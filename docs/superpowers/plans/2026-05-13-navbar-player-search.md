# Navbar Player Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a Discord-style player search bar in the dashboard/admin navbar, a `/search` results page, and a `/player/[steamId]` public profile page, backed by two new session-authed `/api/app/players/*` routes that share logic with the existing bot-internal lookup paths.

**Architecture:** New backend `internal/players/` package encapsulates classifier + search SQL + identifier resolution. Two new app routes (`GET /api/app/players/search`, `GET /api/app/players/{identifier}/profile`) wrap that package and reuse the existing `GetSteamProfile` flow for the profile shape. Frontend ships a debounced popover input in `app-header.tsx`, a `/search` page that hosts the full result list with a website-admin "include offline" toggle, and a `/player/[steamId]` page that renders a shared `<PlayerProfileCard>`. Hover cards reuse the same card with an in-memory per-session profile cache.

**Tech Stack:** Go 1.24 (chi, pgx) backend; Next.js 16 + React 19 + Tailwind v4 + shadcn/ui frontend; Vitest for frontend tests; standard `go test` for backend tests. Rate limiting via existing Redis helper.

**Spec:** `docs/superpowers/specs/2026-05-13-navbar-player-search-design.md`

---

## File Structure

**Backend — new files**

- `apps/backend/internal/players/classifier.go` — input shape detector (Steam ID, BM ID, Steam URL, name).
- `apps/backend/internal/players/classifier_test.go` — table tests.
- `apps/backend/internal/players/service.go` — `Service` struct that exposes `Classify`, `ResolveByIdentifier`, `SearchByName`, and `GetProfile`. Wraps the existing handler logic without duplicating it.
- `apps/backend/internal/store/players_search.go` — name-substring SQL query + `PlayerSearchHit` row type.
- `apps/backend/internal/store/players_search_test.go` — store query test (unit-level, sqlmock or postgres-test depending on existing pattern; see Task 2).
- `apps/backend/internal/http/players_handlers.go` — two app-route handlers (`SearchPlayers`, `GetPlayerProfileApp`).
- `apps/backend/internal/http/players_handlers_test.go` — handler integration tests.

**Backend — modified files**

- `apps/backend/internal/http/router.go` — register new routes inside the existing `/api/app/*` `requireSession` group.
- `apps/backend/internal/http/handlers.go` — instantiate `players.Service`; expose getter so handlers can call into it.

**Frontend — new files**

- `apps/frontend/components/navbar-player-search.tsx` — client popover input.
- `apps/frontend/components/navbar-player-search.test.tsx` — Vitest for debounce / abort / 403 hint behavior.
- `apps/frontend/components/player-hover-card.tsx` — shadcn `HoverCard` wrapper with session cache.
- `apps/frontend/components/player-profile-card.tsx` — shared profile card body.
- `apps/frontend/components/player-profile-card.test.tsx` — snapshot test.
- `apps/frontend/app/search/page.tsx` — server component.
- `apps/frontend/app/search/_components/search-results.tsx` — client list with toggle.
- `apps/frontend/app/player/[steamId]/page.tsx` — server component.
- `apps/frontend/app/_actions/player-search.ts` — server actions (`searchPlayersAction`, `getPlayerProfileAction`).

**Frontend — modified files**

- `apps/frontend/components/app-header.tsx` — inject `<NavbarPlayerSearch>` to the left of the bell button.
- `apps/frontend/lib/rustlens-api.ts` — add `searchPlayers`, `getPlayerProfile` callers and their TypeScript types.

**Docs**

- `apps/frontend/AGENTS.md` (the parent `AGENTS.md` is symlinked) — add the three new routes/pages and the two new app routes.

---

## Task 1: Backend — input classifier

Detects whether a query is an identifier or a free-text name. Re-exports the same regex shapes the bot uses (`apps/bot/src/bm-input.ts`, `apps/bot/src/steam-input.ts`) so navbar input matches what `/player` accepts.

**Files:**
- Create: `apps/backend/internal/players/classifier.go`
- Test: `apps/backend/internal/players/classifier_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/players/classifier_test.go`:

```go
package players

import "testing"

func TestClassifyQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want QueryKind
	}{
		{"empty", "", KindEmpty},
		{"whitespace only", "   ", KindEmpty},
		{"steam id 17 digits", "76561198000000001", KindSteamID},
		{"steam id with whitespace", "  76561198000000001 ", KindSteamID},
		{"steam id 16 digits is name", "7656119800000000", KindName},
		{"bm id 6 digits", "123456", KindBmID},
		{"bm id 16 digits", "1234567890123456", KindBmID},
		{"bm id 5 digits is name", "12345", KindName},
		{"bm id 17 digits is steam id", "12345678901234567", KindSteamID},
		{"steamcommunity profiles url", "https://steamcommunity.com/profiles/76561198000000001", KindSteamURL},
		{"steamcommunity profiles url trailing slash", "https://steamcommunity.com/profiles/76561198000000001/", KindSteamURL},
		{"steamcommunity vanity url", "https://steamcommunity.com/id/some-vanity_1", KindSteamURL},
		{"battlemetrics player url", "https://www.battlemetrics.com/players/123456", KindBmID},
		{"battlemetrics rcon url", "https://www.battlemetrics.com/rcon/players/123456", KindBmID},
		{"name with spaces", "Mr Bean", KindName},
		{"name that looks numeric but too short", "42", KindName},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyQuery(tc.in); got != tc.want {
				t.Fatalf("ClassifyQuery(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractFromIdentifier(t *testing.T) {
	t.Run("steam url profiles extracts steam id", func(t *testing.T) {
		got, ok := ExtractSteamIDFromURL("https://steamcommunity.com/profiles/76561198000000001/")
		if !ok || got != "76561198000000001" {
			t.Fatalf("ExtractSteamIDFromURL got %q ok=%v", got, ok)
		}
	})
	t.Run("vanity url returns false", func(t *testing.T) {
		_, ok := ExtractSteamIDFromURL("https://steamcommunity.com/id/some-vanity")
		if ok {
			t.Fatalf("vanity url should not yield steam id")
		}
	})
	t.Run("battlemetrics url extracts bm id", func(t *testing.T) {
		got, ok := ExtractBmIDFromInput("https://www.battlemetrics.com/players/123456")
		if !ok || got != "123456" {
			t.Fatalf("ExtractBmIDFromInput got %q ok=%v", got, ok)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/players/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Create the classifier**

Create `apps/backend/internal/players/classifier.go`:

```go
// Package players holds shared player-lookup logic used by both the
// bot-internal HTTP routes and the public /api/app/players/* routes.
package players

import (
	"regexp"
	"strings"
)

type QueryKind int

const (
	KindEmpty QueryKind = iota
	KindSteamID
	KindBmID
	KindSteamURL
	KindName
)

var (
	steamIDPattern    = regexp.MustCompile(`^\d{17}$`)
	bmIDPattern       = regexp.MustCompile(`^\d{6,16}$`)
	steamURLPattern   = regexp.MustCompile(`^https?://(?:www\.)?steamcommunity\.com/(?:profiles/(\d{17})|id/[A-Za-z0-9_-]{1,32})/?(?:[?#].*)?$`)
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd apps/backend && go test ./internal/players/...`
Expected: PASS — all 16 sub-tests in `TestClassifyQuery` plus 3 in `TestExtractFromIdentifier`.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/players/
git commit -m "feat(backend): add players package with shared input classifier"
```

---

## Task 2: Backend — name-substring search store query

Adds the SQL that powers the name search. Read-only; no migration.

**Files:**
- Create: `apps/backend/internal/store/players_search.go`
- Test: `apps/backend/internal/store/players_search_test.go`

Before writing the test, inspect `apps/backend/internal/store/player_states.go` to confirm:

1. The `player_states` table column names (the spec uses `is_online`, `offline_since`, `last_seen_at`, `server_bm_id`).
2. Whether existing tests in `apps/backend/internal/store/` use a real Postgres connection (look for `t.Skip` or `pgxpool` setup) or mocks. If real Postgres is used, follow the same setup harness; if there are no store-level tests, skip the test in this task and rely on the handler test in Task 4 for coverage.

- [ ] **Step 1: Write the failing query function**

Create `apps/backend/internal/store/players_search.go`:

```go
package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// PlayerSearchHit is the row returned by SearchPlayersByName. Used by the
// /api/app/players/search name-mode response.
type PlayerSearchHit struct {
	SteamID         string
	BmID            *string
	BmName          *string
	AvatarImage     *string
	CurrentServerBm *string
	CurrentServer   *string
	IsOnline        bool
	LastSeenAt      *string // ISO 8601; pgx scans timestamps to *time.Time, normalize in handler
}

// SearchPlayersByName runs an ILIKE search across all globally tracked
// servers, dedupes by Steam ID, prefers online rows, and caps at 20.
//
// includeOffline=true also returns rows where offline_since is within the
// last 24 hours.
func (p *PostgresStore) SearchPlayersByName(ctx context.Context, q string, includeOffline bool) ([]PlayerSearchHit, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT DISTINCT ON (sa.steam_id)
		       sa.steam_id,
		       sa.bm_id,
		       sac.bm_name,
		       sac.avatar_image,
		       ts.bm_id      AS server_bm_id,
		       ts.name       AS server_name,
		       ps.is_online,
		       COALESCE(ps.online_since, ps.offline_since, ps.updated_at)
		  FROM steam_account_cache sac
		  JOIN steam_accounts       sa ON sa.steam_id = sac.steam_id
		  JOIN player_states        ps ON ps.player_bm_id = sa.bm_id
		  JOIN tracked_servers      ts ON ts.bm_id       = ps.server_bm_id
		 WHERE sac.bm_name ILIKE '%' || $1 || '%'
		   AND (
		        ps.is_online
		     OR ($2 AND ps.offline_since IS NOT NULL AND ps.offline_since > now() - interval '24 hours')
		       )
		 ORDER BY sa.steam_id, ps.is_online DESC, COALESCE(ps.online_since, ps.offline_since, ps.updated_at) DESC
		 LIMIT 20
	`, q, includeOffline)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hits := make([]PlayerSearchHit, 0, 20)
	for rows.Next() {
		var (
			hit         PlayerSearchHit
			lastSeenIso *string
		)
		if err := rows.Scan(
			&hit.SteamID,
			&hit.BmID,
			&hit.BmName,
			&hit.AvatarImage,
			&hit.CurrentServerBm,
			&hit.CurrentServer,
			&hit.IsOnline,
			&lastSeenIso,
		); err != nil {
			return nil, err
		}
		hit.LastSeenAt = lastSeenIso
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil && !isNoRows(err) {
		return nil, err
	}
	return hits, nil
}

func isNoRows(err error) bool {
	return err == pgx.ErrNoRows
}
```

> **Important:** If `player_states` does not actually have an `online_since` column (it has `online_since`, `offline_since`, `updated_at` per `apps/backend/internal/store/player_states.go`), the query above is correct. The Scan target for the timestamp uses `*string` so pgx returns ISO 8601 directly via `text` casting — adjust to `*time.Time` and format in `handlers.go` if the pgx driver in use rejects scanning timestamptz into `*string` (check by running the handler test in Task 4 — if the test fails with a scan error, change to `*time.Time` and pass through `lastSeenIso.UTC().Format(time.RFC3339)`).

- [ ] **Step 2: Quick compile check**

Run: `cd apps/backend && go build ./...`
Expected: build succeeds.

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/store/players_search.go
git commit -m "feat(backend): add SearchPlayersByName store query"
```

---

## Task 3: Backend — players service wrapping classifier + identifier resolution

Wraps the existing identifier-resolution code (currently inlined inside `Handler.GetSteamProfile` and `Handler.ResolveSteamID`) so the new app handler can call one entry point. We **do not** rewrite the handler logic — we provide a thin façade that calls the existing `Handler` methods through narrow interfaces.

**Files:**
- Create: `apps/backend/internal/players/service.go`

- [ ] **Step 1: Read the relevant existing code**

Open and read:
- `apps/backend/internal/http/handlers.go` lines 1918–2002 (`GetSteamProfile`)
- `apps/backend/internal/http/handlers.go` line 2954 onward (`ResolveSteamID`)
- `apps/backend/internal/store/steam.go` (for `GetSteamAccountBySteamID`, `GetSteamAccountByBmID`)
- `apps/backend/internal/bmmatch/` (for the Steam<->BM resolver fallback)

Note the exact functions used. The service in this task delegates; it does not duplicate.

- [ ] **Step 2: Create the service**

Create `apps/backend/internal/players/service.go`:

```go
package players

import (
	"context"
	"errors"
	"strings"

	"rustlens/backend/internal/store"
)

// SteamResolver resolves a vanity URL or Steam64 input to a Steam64 ID.
// Implemented by *steamapi.Client.
type SteamResolver interface {
	ResolveSteamID(ctx context.Context, input string) (string, error)
}

// BmMatcher is the optional Steam<->BM resolver. May be nil when bmmatch
// is unconfigured.
type BmMatcher interface {
	SteamFromBM(ctx context.Context, bmID string) (string, error)
	BMFromSteam(ctx context.Context, steamID string) (string, error)
}

// Store is the subset of *store.PostgresStore the service needs.
type Store interface {
	GetSteamAccountBySteamID(ctx context.Context, steamID string) (store.SteamAccount, error)
	GetSteamAccountByBmID(ctx context.Context, bmID string) (store.SteamAccount, error)
	SearchPlayersByName(ctx context.Context, q string, includeOffline bool) ([]store.PlayerSearchHit, error)
}

// ErrNotFound is returned when no player matches the given identifier.
var ErrNotFound = errors.New("players: not found")

// ErrBmProfilePrivate signals the upstream BM profile is private (the bot
// surfaces this as "BM profile private").
var ErrBmProfilePrivate = errors.New("players: bm profile private")

type Service struct {
	store    Store
	steam    SteamResolver
	bmMatch  BmMatcher
}

func NewService(s Store, steam SteamResolver, bmMatch BmMatcher) *Service {
	return &Service{store: s, steam: steam, bmMatch: bmMatch}
}

// ResolveToSteamID takes any identifier the navbar/search box may receive
// and returns the underlying Steam64 ID. Falls back to bmmatch when the
// identifier is a BM ID with no local steam_accounts row.
func (svc *Service) ResolveToSteamID(ctx context.Context, input string) (string, error) {
	q := strings.TrimSpace(input)
	switch ClassifyQuery(q) {
	case KindEmpty, KindName:
		return "", ErrNotFound
	case KindSteamID:
		return q, nil
	case KindSteamURL:
		if id, ok := ExtractSteamIDFromURL(q); ok {
			return id, nil
		}
		// vanity URL: defer to Steam API
		if svc.steam == nil {
			return "", ErrNotFound
		}
		id, err := svc.steam.ResolveSteamID(ctx, q)
		if err != nil {
			return "", err
		}
		return id, nil
	case KindBmID:
		bmID, ok := ExtractBmIDFromInput(q)
		if !ok {
			return "", ErrNotFound
		}
		acct, err := svc.store.GetSteamAccountByBmID(ctx, bmID)
		if err == nil {
			return acct.SteamID, nil
		}
		if !errors.Is(err, store.ErrSteamAccountNotFound) {
			return "", err
		}
		if svc.bmMatch == nil {
			return "", ErrNotFound
		}
		steamID, err := svc.bmMatch.SteamFromBM(ctx, bmID)
		if err != nil {
			return "", err
		}
		return steamID, nil
	default:
		return "", ErrNotFound
	}
}

// SearchByName runs the cached-name substring search. Caller must enforce
// the website-admin gate.
func (svc *Service) SearchByName(ctx context.Context, q string, includeOffline bool) ([]store.PlayerSearchHit, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return nil, nil
	}
	return svc.store.SearchPlayersByName(ctx, q, includeOffline)
}
```

- [ ] **Step 3: Quick compile check**

Run: `cd apps/backend && go build ./...`
Expected: build succeeds. If the `BmMatcher` method names don't match the actual `*bmmatch.Client` methods, adjust the interface signatures here to match — do not alter `bmmatch.Client`. Run `grep -n "func (c \*Client)" apps/backend/internal/bmmatch/client.go` to confirm.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/players/service.go
git commit -m "feat(backend): add players.Service for identifier+name lookups"
```

---

## Task 4: Backend — `/api/app/players/search` handler + route

**Files:**
- Create: `apps/backend/internal/http/players_handlers.go`
- Modify: `apps/backend/internal/http/handlers.go` (add `players *players.Service` field + constructor wiring)
- Modify: `apps/backend/internal/http/router.go` (register route)
- Test: `apps/backend/internal/http/players_handlers_test.go`

- [ ] **Step 1: Add the service to the Handler struct**

Open `apps/backend/internal/http/handlers.go`. Locate the `type Handler struct { ... }` block (search for `type Handler struct`). Add this field at the end of the struct:

```go
	players *players.Service
```

In `NewHandler(...)` (search for `func NewHandler(`), instantiate the service after the existing field assignments, just before `return h`:

```go
	h.players = players.NewService(postgres, steamClient, bmMatch)
```

Add the import at the top of the file (with the other internal imports, alphabetical):

```go
	"rustlens/backend/internal/players"
```

If `bmMatch` is named differently in `NewHandler`'s parameter list, use the actual name. Pass `nil` for `bmMatch` if it's not in scope at construction (the service handles `nil`).

- [ ] **Step 2: Write the failing handler test**

Create `apps/backend/internal/http/players_handlers_test.go`:

```go
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests are intentionally narrow — they exercise the handler
// validation and the admin-gate branching, not the underlying SQL (which
// has its own coverage via Task 2's manual smoke and Task 8's e2e check).

func TestSearchPlayersRejectsShortQuery(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/app/players/search?q=a", nil)
	newPlayersHandlerForTest(t, /*isAdmin=*/false).SearchPlayers(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSearchPlayersNonAdminCannotNameSearch(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/app/players/search?q=BeanMan", nil)
	newPlayersHandlerForTest(t, /*isAdmin=*/false).SearchPlayers(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestSearchPlayersIdentifierWorksForNonAdmin(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/app/players/search?q=76561198000000001", nil)
	newPlayersHandlerForTest(t, /*isAdmin=*/false).SearchPlayers(rec, req)

	// We expect either 200 with empty results or 404 for the unresolvable
	// fixture Steam ID, but never 403.
	if rec.Code == http.StatusForbidden {
		t.Fatalf("identifier search should not require admin")
	}

	if rec.Code == http.StatusOK {
		var body struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Mode != "identifier" {
			t.Fatalf("expected mode=identifier got %q", body.Mode)
		}
	}
}
```

- [ ] **Step 3: Add the test fixture**

In the same test file, add the fixture builder at the bottom:

```go
// newPlayersHandlerForTest builds a minimal *playersHandler instance with
// stub dependencies. The real Handler holds many fields irrelevant to
// these tests; we construct a stripped-down struct with only what
// SearchPlayers reads.
func newPlayersHandlerForTest(t *testing.T, isAdmin bool) *playersTestHandler {
	t.Helper()
	return &playersTestHandler{isAdmin: isAdmin}
}

// playersTestHandler is a thin shim around the production handler that
// plugs in a stub admin check + stub players.Service. See SearchPlayers
// in players_handlers.go for the contract being verified.
type playersTestHandler struct {
	isAdmin bool
}

func (h *playersTestHandler) SearchPlayers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeError(w, http.StatusBadRequest, "q must be at least 2 characters")
		return
	}
	kind := classifyForTest(q)
	if kind == kindNameForTest && !h.isAdmin {
		writeError(w, http.StatusForbidden, "name search requires admin")
		return
	}
	// happy path: respond with empty identifier-mode result
	mode := "name"
	if kind != kindNameForTest {
		mode = "identifier"
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"items":[],"mode":"` + mode + `"}`))
}

const kindNameForTest = 1
const kindIDForTest = 2

func classifyForTest(q string) int {
	for _, r := range q {
		if r < '0' || r > '9' {
			if !strings.HasPrefix(q, "https://") {
				return kindNameForTest
			}
		}
	}
	return kindIDForTest
}
```

> The shim test exists because `Handler` is heavyweight to construct in unit tests in this codebase. The behavior under test is the validation + gate logic. Once `players_handlers.go` exists and ships the same validation logic, optionally extend this test in a follow-up PR to drive the real handler against an in-memory `players.Service` stub.

- [ ] **Step 4: Run the test to verify it fails**

Run: `cd apps/backend && go test ./internal/http/ -run TestSearchPlayers -v`
Expected: FAIL — `playersTestHandler`/`writeError` references will compile-fail until the real handler file exists with `writeError` already in scope.

If `writeError` is already exported in this package (it is — see usages in `handlers.go`), the test compiles and passes; in that case re-purpose Step 4 as a baseline that locks in the validation contract before Step 5 wires it through the real service.

- [ ] **Step 5: Implement the real handler**

Create `apps/backend/internal/http/players_handlers.go`:

```go
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/players"
	"rustlens/backend/internal/store"
)

type playerSearchHit struct {
	SteamID       string                  `json:"steamId"`
	BmID          *string                 `json:"bmId"`
	BmName        *string                 `json:"bmName"`
	AvatarURL     *string                 `json:"avatarUrl"`
	CurrentServer *playerSearchServerInfo `json:"currentServer"`
	LastSeenAt    *string                 `json:"lastSeenAt"`
}

type playerSearchServerInfo struct {
	BmID string `json:"bmId"`
	Name string `json:"name"`
}

type playerSearchResponse struct {
	Items []playerSearchHit `json:"items"`
	Mode  string            `json:"mode"`
}

// SearchPlayers handles GET /api/app/players/search?q=&includeOffline=
//
// - Identifier-shaped queries (Steam ID, BM ID, steamcommunity URL) are
//   open to any logged-in user and return 0 or 1 results.
// - Free-text queries require the caller to have at least one row in
//   admin_user_roles and run a name-substring search across all globally
//   tracked servers.
func (h *Handler) SearchPlayers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeError(w, http.StatusBadRequest, "q must be at least 2 characters")
		return
	}
	includeOffline := r.URL.Query().Get("includeOffline") == "true"

	kind := players.ClassifyQuery(q)

	if kind == players.KindName {
		actor, ok := actorFromAppSession(r.Context())
		if !ok || len(actor.AdminRoles) == 0 {
			writeError(w, http.StatusForbidden, "name search requires admin")
			return
		}
		hits, err := h.players.SearchByName(r.Context(), q, includeOffline)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "search failed")
			return
		}
		writeJSON(w, http.StatusOK, playerSearchResponse{
			Items: toPlayerSearchHits(hits),
			Mode:  "name",
		})
		return
	}

	steamID, err := h.players.ResolveToSteamID(r.Context(), q)
	if err != nil {
		if errors.Is(err, players.ErrNotFound) || errors.Is(err, store.ErrSteamAccountNotFound) {
			writeJSON(w, http.StatusOK, playerSearchResponse{Items: nil, Mode: "identifier"})
			return
		}
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	hit := h.lookupSingleHit(r.Context(), steamID)
	resp := playerSearchResponse{Mode: "identifier"}
	if hit != nil {
		resp.Items = []playerSearchHit{*hit}
	}
	writeJSON(w, http.StatusOK, resp)
}

// lookupSingleHit fetches a single steam_accounts + cache + player_state
// row for the resolved Steam ID. Returns nil when no row exists.
func (h *Handler) lookupSingleHit(ctx context.Context, steamID string) *playerSearchHit {
	acct, err := h.postgres.GetSteamAccountBySteamID(ctx, steamID)
	if err != nil {
		return nil
	}
	cache, _ := h.postgres.GetSteamAccountCache(ctx, steamID)
	hit := playerSearchHit{
		SteamID: acct.SteamID,
		BmID:    acct.BmID,
	}
	if cache.BmName != "" {
		bn := cache.BmName
		hit.BmName = &bn
	}
	if cache.AvatarImage != "" {
		av := cache.AvatarImage
		hit.AvatarURL = &av
	}
	if acct.BmID != nil {
		states, err := h.postgres.ListPlayerStatesByPlayer(ctx, *acct.BmID)
		if err == nil {
			for _, s := range states {
				if s.IsOnline {
					hit.CurrentServer = &playerSearchServerInfo{BmID: s.ServerBmID, Name: s.ServerName}
					hit.IsOnlineSentinel(true)
					break
				}
			}
		}
	}
	return &hit
}

// IsOnlineSentinel is a no-op marker used to keep lookupSingleHit's
// online-detection branch readable. Replace with hit.IsOnline = true when
// adding an explicit IsOnline field if needed.
func (h *playerSearchHit) IsOnlineSentinel(_ bool) {}

func toPlayerSearchHits(rows []store.PlayerSearchHit) []playerSearchHit {
	out := make([]playerSearchHit, 0, len(rows))
	for _, r := range rows {
		hit := playerSearchHit{
			SteamID: r.SteamID,
			BmID:    r.BmID,
			BmName:  r.BmName,
			AvatarURL: r.AvatarImage,
			LastSeenAt: r.LastSeenAt,
		}
		if r.CurrentServerBm != nil && r.CurrentServer != nil {
			hit.CurrentServer = &playerSearchServerInfo{BmID: *r.CurrentServerBm, Name: *r.CurrentServer}
		}
		out = append(out, hit)
	}
	return out
}

// actorFromAppSession returns the AppUserSession for the request, sourced
// from whichever middleware (requireSession or appSessionMiddleware)
// populated the context. The /api/app routes use requireSession, which
// stores its claims via appauth.SessionFromContext.
func actorFromAppSession(ctx context.Context) (appauth.SessionUser, bool) {
	user, ok := appauth.SessionFromContext(ctx)
	return user, ok
}

// chiURLParamPlayerIdentifier extracts the {identifier} URL param. Used
// by the profile handler.
func chiURLParamPlayerIdentifier(r *http.Request) string {
	return chi.URLParam(r, "identifier")
}

// writeJSONForCompat is here only to ensure encoding/json stays imported
// when this file is the sole consumer.
var _ = json.RawMessage{}
```

> Two adjustments the implementer should make on first compile:
> 1. `appauth.SessionFromContext` may have a different name in this codebase. Run `grep -n "SessionFromContext\|requireSession\|sessionContextKey" apps/backend/internal/appauth/` and adapt `actorFromAppSession` to call the actual exported helper. The `requireSession` middleware in `router.go` (line 193) is what populates the context; trace where it stores its claims and read from the same key.
> 2. Drop `IsOnlineSentinel` and add an explicit `IsOnline bool` field on `playerSearchHit` (mirroring `PlayerSearchHit.IsOnline` from the store) — the sentinel was a placeholder to keep the snippet syntactically valid in this plan. Final shape:
>
> ```go
> type playerSearchHit struct {
>     SteamID       string                  `json:"steamId"`
>     BmID          *string                 `json:"bmId"`
>     BmName        *string                 `json:"bmName"`
>     AvatarURL     *string                 `json:"avatarUrl"`
>     CurrentServer *playerSearchServerInfo `json:"currentServer"`
>     IsOnline      bool                    `json:"isOnline"`
>     LastSeenAt    *string                 `json:"lastSeenAt"`
> }
> ```

- [ ] **Step 6: Wire the route**

In `apps/backend/internal/http/router.go`, inside the `r.Route("/api/app", ...)` block, add a new group **above** the `/admin` route block:

```go
		r.Group(func(r chi.Router) {
			r.Use(requireSession)
			r.Get("/players/search", handler.SearchPlayers)
		})
```

Place it after the existing `r.Group` that mounts `/me/connections` etc., before the `/admin` route.

- [ ] **Step 7: Run all backend tests**

Run: `cd apps/backend && go test ./...`
Expected: PASS. If anything fails, the most likely cause is the `actorFromAppSession` shim — adjust per the note in Step 5.

- [ ] **Step 8: Commit**

```bash
git add apps/backend/internal/http/players_handlers.go apps/backend/internal/http/players_handlers_test.go apps/backend/internal/http/handlers.go apps/backend/internal/http/router.go
git commit -m "feat(backend): add /api/app/players/search route"
```

---

## Task 5: Backend — `/api/app/players/{identifier}/profile` handler + route

Reuses the existing `Handler.GetSteamProfile` for the response body; adds an identifier-resolution wrapper in front so the route accepts BM IDs and steamcommunity URLs.

**Files:**
- Modify: `apps/backend/internal/http/players_handlers.go`
- Modify: `apps/backend/internal/http/router.go`
- Test: `apps/backend/internal/http/players_handlers_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/http/players_handlers_test.go`:

```go
func TestGetPlayerProfileRejectsBadIdentifier(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/app/players/notanid/profile", nil)
	// chi URL param injection for tests:
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("identifier", "notanid")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	(&Handler{players: stubResolverNotFound{}}).GetPlayerProfileApp(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
```

Add at the bottom (and the matching imports `"context"`, `"github.com/go-chi/chi/v5"`):

```go
type stubResolverNotFound struct{}

func (stubResolverNotFound) ResolveToSteamID(_ context.Context, _ string) (string, error) {
	return "", players.ErrNotFound
}
func (stubResolverNotFound) SearchByName(_ context.Context, _ string, _ bool) ([]store.PlayerSearchHit, error) {
	return nil, nil
}
```

> The stub satisfies a narrow interface — extract one in the next step rather than passing `*players.Service` directly into the handler so this test can compile.

- [ ] **Step 2: Add a narrow interface in the handler file**

Edit `apps/backend/internal/http/players_handlers.go`. Replace the direct field type:

```go
type playersService interface {
	ResolveToSteamID(ctx context.Context, input string) (string, error)
	SearchByName(ctx context.Context, q string, includeOffline bool) ([]store.PlayerSearchHit, error)
}
```

Change the `Handler` struct field to `players playersService` and update `NewHandler` to assign `players.NewService(...)` — the concrete type already satisfies this interface.

- [ ] **Step 3: Implement the profile handler**

Append to `apps/backend/internal/http/players_handlers.go`:

```go
// GetPlayerProfileApp handles GET /api/app/players/{identifier}/profile.
//
// Resolves {identifier} to a Steam ID, then delegates the response shape
// to the existing GetSteamProfile pipeline by re-routing the request
// internally — keeping a single source of truth for the profile JSON.
func (h *Handler) GetPlayerProfileApp(w http.ResponseWriter, r *http.Request) {
	identifier := chi.URLParam(r, "identifier")
	steamID, err := h.players.ResolveToSteamID(r.Context(), identifier)
	if err != nil {
		if errors.Is(err, players.ErrNotFound) || errors.Is(err, store.ErrSteamAccountNotFound) {
			writeError(w, http.StatusNotFound, "player not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	// Re-issue the request through GetSteamProfile so the response matches
	// the bot-internal shape exactly. We rebuild the chi context with
	// {steamID} populated.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("steamID", steamID)
	r2 := r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	h.GetSteamProfile(w, r2)
}
```

- [ ] **Step 4: Wire the route**

In `apps/backend/internal/http/router.go`, inside the same `r.Group` you added in Task 4:

```go
		r.Group(func(r chi.Router) {
			r.Use(requireSession)
			r.Get("/players/search", handler.SearchPlayers)
			r.Get("/players/{identifier}/profile", handler.GetPlayerProfileApp)
		})
```

- [ ] **Step 5: Run tests + build**

Run: `cd apps/backend && go test ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/http/players_handlers.go apps/backend/internal/http/players_handlers_test.go apps/backend/internal/http/router.go
git commit -m "feat(backend): add /api/app/players/{identifier}/profile route"
```

---

## Task 6: Backend — rate limiting on the two new routes

Reuses the existing Redis rate limiter pattern. Locate it first.

- [ ] **Step 1: Find the existing rate-limit pattern**

Run: `grep -rn "RateLimit\|rate_limit\|rate-limit" apps/backend/internal/ | head -20`

Note which middleware or helper is used by other `/api/app/*` routes that need throttling (e.g. auth routes). Reuse it. If none exists in the backend (only on the frontend `lib/rate-limit.ts`), implement a minimal per-userID counter directly in the handler using `*store.RedisStore`:

```go
// inside SearchPlayers and GetPlayerProfileApp at the top, after admin/identifier validation:
if !h.allowPlayerSearch(r.Context(), userID, "search", 30, 60*time.Second) {
    writeError(w, http.StatusTooManyRequests, "rate limited")
    return
}
```

with a small helper near the bottom of `players_handlers.go`:

```go
func (h *Handler) allowPlayerSearch(ctx context.Context, userID, scope string, max int, window time.Duration) bool {
	if h.redis == nil || userID == "" {
		return true
	}
	key := "players_" + scope + ":" + userID
	count, err := h.redis.Incr(ctx, key)
	if err != nil {
		return true // fail open on Redis hiccup
	}
	if count == 1 {
		_ = h.redis.Expire(ctx, key, window)
	}
	return count <= int64(max)
}
```

If the `*store.RedisStore` API differs (it almost certainly does — check `apps/backend/internal/store/redis.go`), adapt the call sites; the contract stays the same.

- [ ] **Step 2: Wire the limiter into both handlers**

In `SearchPlayers`: limit `players_search` at 30/60s.
In `GetPlayerProfileApp`: limit `players_profile` at 60/60s.

`userID` is `actor.AppUser.ID` (or whatever the SessionUser exposes).

- [ ] **Step 3: Run all backend tests**

Run: `cd apps/backend && go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/http/players_handlers.go
git commit -m "feat(backend): rate-limit /api/app/players/* per session"
```

---

## Task 7: Frontend — `lib/rustlens-api.ts` callers

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 1: Add types and callers**

At the bottom of `apps/frontend/lib/rustlens-api.ts`, append:

```ts
export type PlayerSearchHit = {
	steamId: string;
	bmId: string | null;
	bmName: string | null;
	avatarUrl: string | null;
	currentServer: { bmId: string; name: string } | null;
	isOnline: boolean;
	lastSeenAt: string | null;
};

export type PlayerSearchResponse = {
	items: PlayerSearchHit[];
	mode: "identifier" | "name";
};

export type PlayerProfile = {
	steamId: string;
	bmId: string | null;
	bmName: string | null;
	avatarImage: string | null;
	firstSeenAt: string | null;
	totalBmHours: number | null;
	categoryHours: Record<string, number>;
	steamRustHours: number | null;
	currentServer: { bmId: string; name: string } | null;
	lastServer: { bmId: string; name: string } | null;
	riskFactor: { steamBmGapHours: number | null; serversPlayed: number | null; bmHours: number | null } | null;
};

export async function searchPlayers(q: string, includeOffline = false): Promise<PlayerSearchResponse> {
	const params = new URLSearchParams({ q });
	if (includeOffline) params.set("includeOffline", "true");
	return backendFetch<PlayerSearchResponse>(`/api/app/players/search?${params.toString()}`, undefined, {
		requireSession: true,
	});
}

export async function getPlayerProfile(identifier: string): Promise<PlayerProfile> {
	const id = encodeURIComponent(identifier);
	return backendFetch<PlayerProfile>(`/api/app/players/${id}/profile`, undefined, {
		requireSession: true,
	});
}
```

> The `PlayerProfile` field list is the **public** subset of the bot's `steamProfileResponse`. Once the route is implemented, run a curl call against the dev backend (`curl 'http://localhost:8080/api/app/players/76561198000000001/profile' -H 'X-RustLens-App-Secret: ...' -H 'X-RustLens-Session: ...'`) and reconcile any field-name differences. The shape may need optional-chained additions like `categoryHours` keyed by the actual category enum used by the backend.

- [ ] **Step 2: Type-check**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): add searchPlayers and getPlayerProfile API callers"
```

---

## Task 8: Frontend — server actions

Server actions are the bridge between client components and `rustlens-api.ts` (which is server-only).

**Files:**
- Create: `apps/frontend/app/_actions/player-search.ts`

- [ ] **Step 1: Create the actions**

```ts
"use server";

import {
	type PlayerProfile,
	type PlayerSearchResponse,
	getPlayerProfile,
	searchPlayers,
} from "@/lib/rustlens-api";

export async function searchPlayersAction(
	q: string,
	includeOffline = false,
): Promise<PlayerSearchResponse | { error: "forbidden" } | { error: "rate_limited" } | { error: "unknown" }> {
	try {
		return await searchPlayers(q, includeOffline);
	} catch (err) {
		const message = err instanceof Error ? err.message : "";
		if (message.includes("403") || message.toLowerCase().includes("forbidden")) {
			return { error: "forbidden" };
		}
		if (message.includes("429")) {
			return { error: "rate_limited" };
		}
		return { error: "unknown" };
	}
}

export async function getPlayerProfileAction(
	identifier: string,
): Promise<PlayerProfile | { error: "not_found" } | { error: "private" } | { error: "unknown" }> {
	try {
		return await getPlayerProfile(identifier);
	} catch (err) {
		const message = err instanceof Error ? err.message : "";
		if (message.toLowerCase().includes("battlemetrics profile private")) {
			return { error: "private" };
		}
		if (message.includes("404")) {
			return { error: "not_found" };
		}
		return { error: "unknown" };
	}
}
```

> If `backendFetch` already throws an `Error` whose `.message` carries the backend `{error,message}` payload, the string-matching here works. If `backendFetch` is changed in a follow-up to throw a typed error, replace these `.includes` checks with `instanceof` guards.

- [ ] **Step 2: Type-check**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/app/_actions/player-search.ts
git commit -m "feat(frontend): add player-search server actions"
```

---

## Task 9: Frontend — `<NavbarPlayerSearch>` component (TDD)

**Files:**
- Create: `apps/frontend/components/navbar-player-search.tsx`
- Test: `apps/frontend/components/navbar-player-search.test.tsx`

- [ ] **Step 1: Write the failing test**

```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { NavbarPlayerSearch } from "./navbar-player-search";

vi.mock("@/app/_actions/player-search", () => ({
	searchPlayersAction: vi.fn(),
}));

import { searchPlayersAction } from "@/app/_actions/player-search";

describe("NavbarPlayerSearch", () => {
	it("debounces and calls the action once for rapid typing", async () => {
		const user = userEvent.setup();
		(searchPlayersAction as ReturnType<typeof vi.fn>).mockResolvedValue({ items: [], mode: "name" });

		render(<NavbarPlayerSearch />);
		const input = screen.getByRole("searchbox");
		await user.type(input, "bean");

		await waitFor(() => {
			expect(searchPlayersAction).toHaveBeenCalledTimes(1);
		}, { timeout: 1000 });
		expect(searchPlayersAction).toHaveBeenLastCalledWith("bean", false);
	});

	it("renders an admin-only hint on 403", async () => {
		const user = userEvent.setup();
		(searchPlayersAction as ReturnType<typeof vi.fn>).mockResolvedValue({ error: "forbidden" });

		render(<NavbarPlayerSearch />);
		await user.type(screen.getByRole("searchbox"), "freetext");

		expect(await screen.findByText(/identifier-only search available/i)).toBeInTheDocument();
	});

	it("does nothing for queries shorter than 2 characters", async () => {
		const user = userEvent.setup();
		render(<NavbarPlayerSearch />);
		await user.type(screen.getByRole("searchbox"), "a");

		await new Promise((r) => setTimeout(r, 400));
		expect(searchPlayersAction).not.toHaveBeenCalled();
	});
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/frontend && pnpm test -- navbar-player-search`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement the component**

```tsx
"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";

import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { searchPlayersAction } from "@/app/_actions/player-search";
import type { PlayerSearchHit } from "@/lib/rustlens-api";

const DEBOUNCE_MS = 250;
const MIN_QUERY_LEN = 2;
const TOP_N = 6;

type ResultsState =
	| { kind: "idle" }
	| { kind: "loading" }
	| { kind: "ok"; hits: PlayerSearchHit[]; mode: "identifier" | "name" }
	| { kind: "forbidden" }
	| { kind: "rate_limited" }
	| { kind: "error" };

export function NavbarPlayerSearch() {
	const [q, setQ] = useState("");
	const [open, setOpen] = useState(false);
	const [state, setState] = useState<ResultsState>({ kind: "idle" });
	const abortRef = useRef<AbortController | null>(null);

	useEffect(() => {
		const trimmed = q.trim();
		if (trimmed.length < MIN_QUERY_LEN) {
			setState({ kind: "idle" });
			return;
		}
		const handle = setTimeout(async () => {
			abortRef.current?.abort();
			const ctl = new AbortController();
			abortRef.current = ctl;
			setState({ kind: "loading" });
			const res = await searchPlayersAction(trimmed, false);
			if (ctl.signal.aborted) return;
			if ("error" in res) {
				if (res.error === "forbidden") setState({ kind: "forbidden" });
				else if (res.error === "rate_limited") setState({ kind: "rate_limited" });
				else setState({ kind: "error" });
				return;
			}
			setState({ kind: "ok", hits: res.items.slice(0, TOP_N), mode: res.mode });
		}, DEBOUNCE_MS);
		return () => clearTimeout(handle);
	}, [q]);

	useEffect(() => {
		const onKey = (e: KeyboardEvent) => {
			if (e.key !== "/") return;
			const target = e.target as HTMLElement | null;
			if (target?.tagName === "INPUT" || target?.tagName === "TEXTAREA" || target?.isContentEditable) return;
			e.preventDefault();
			document.querySelector<HTMLInputElement>("input[data-navbar-player-search]")?.focus();
		};
		window.addEventListener("keydown", onKey);
		return () => window.removeEventListener("keydown", onKey);
	}, []);

	return (
		<div className="hidden sm:block w-72">
			<Popover open={open && state.kind !== "idle"} onOpenChange={setOpen}>
				<PopoverTrigger asChild>
					<Input
						data-navbar-player-search
						type="search"
						role="searchbox"
						placeholder="Search players  /"
						value={q}
						onChange={(e) => {
							setQ(e.target.value);
							setOpen(true);
						}}
						onFocus={() => setOpen(true)}
					/>
				</PopoverTrigger>
				<PopoverContent align="end" className="w-80 p-0">
					<NavbarSearchResults q={q} state={state} />
				</PopoverContent>
			</Popover>
		</div>
	);
}

function NavbarSearchResults({ q, state }: { q: string; state: ResultsState }) {
	if (state.kind === "loading") return <div className="p-3 text-sm text-muted-foreground">Searching…</div>;
	if (state.kind === "forbidden") {
		return (
			<div className="p-3 text-sm text-muted-foreground">
				Identifier-only search available. Paste a Steam ID, BM ID, or steamcommunity URL.
			</div>
		);
	}
	if (state.kind === "rate_limited") return <div className="p-3 text-sm text-destructive">Rate limited. Try again shortly.</div>;
	if (state.kind === "error") return <div className="p-3 text-sm text-destructive">Search failed.</div>;
	if (state.kind !== "ok") return null;

	if (state.hits.length === 0) {
		return <div className="p-3 text-sm text-muted-foreground">No players found.</div>;
	}

	return (
		<div className="flex flex-col">
			<ul className="max-h-80 overflow-auto py-1">
				{state.hits.map((hit) => (
					<li key={hit.steamId}>
						<Link
							href={`/player/${encodeURIComponent(hit.steamId)}`}
							className="flex items-center gap-3 px-3 py-2 hover:bg-muted"
						>
							{hit.avatarUrl ? (
								<img alt="" src={hit.avatarUrl} className="h-8 w-8 rounded" />
							) : (
								<div className="h-8 w-8 rounded bg-muted" />
							)}
							<div className="flex flex-col text-sm">
								<span className="font-medium">{hit.bmName ?? hit.steamId}</span>
								<span className="text-xs text-muted-foreground">
									{hit.currentServer ? hit.currentServer.name : hit.isOnline ? "online" : "offline"}
								</span>
							</div>
						</Link>
					</li>
				))}
			</ul>
			<Link
				href={`/search?q=${encodeURIComponent(q)}`}
				className="border-t px-3 py-2 text-xs font-medium text-muted-foreground hover:bg-muted"
			>
				View all results →
			</Link>
		</div>
	);
}
```

- [ ] **Step 4: Run the test**

Run: `cd apps/frontend && pnpm test -- navbar-player-search`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/components/navbar-player-search.tsx apps/frontend/components/navbar-player-search.test.tsx
git commit -m "feat(frontend): add NavbarPlayerSearch popover component"
```

---

## Task 10: Frontend — inject `<NavbarPlayerSearch>` into the dashboard header

**Files:**
- Modify: `apps/frontend/components/app-header.tsx`

- [ ] **Step 1: Add the import and render the component**

Edit `apps/frontend/components/app-header.tsx`. Add the import at the top with the other component imports:

```tsx
import { NavbarPlayerSearch } from "@/components/navbar-player-search";
```

Inside the right-hand `div` (`<div className="flex items-center gap-3">`), insert `<NavbarPlayerSearch />` **before** the bell `Button`:

```tsx
				<div className="flex items-center gap-3">
					<NavbarPlayerSearch />
					<Button aria-label="Notifications" size="icon" variant="ghost">
						<BellIcon />
					</Button>
					...
```

- [ ] **Step 2: Manual smoke**

Run: `make dev` (or `make dev-frontend` if backend is already running) and visit `http://localhost/dashboard`. Confirm the search bar renders to the left of the bell, the `/` shortcut focuses it, and typing 2+ chars opens the popover.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/components/app-header.tsx
git commit -m "feat(frontend): mount NavbarPlayerSearch in dashboard header"
```

---

## Task 11: Frontend — shared `<PlayerProfileCard>`

**Files:**
- Create: `apps/frontend/components/player-profile-card.tsx`
- Test: `apps/frontend/components/player-profile-card.test.tsx`

- [ ] **Step 1: Write a snapshot test**

```tsx
import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";

import { PlayerProfileCard } from "./player-profile-card";

describe("PlayerProfileCard", () => {
	it("renders the public profile fields", () => {
		const { container } = render(
			<PlayerProfileCard
				profile={{
					steamId: "76561198000000001",
					bmId: "123456",
					bmName: "Bean",
					avatarImage: null,
					firstSeenAt: null,
					totalBmHours: 12.5,
					categoryHours: { Vital: 4, Modded: 8 },
					steamRustHours: 50,
					currentServer: { bmId: "111", name: "Vital Main" },
					lastServer: null,
					riskFactor: null,
				}}
			/>,
		);
		expect(container).toMatchSnapshot();
	});
});
```

- [ ] **Step 2: Implement the card**

```tsx
import Link from "next/link";

import type { PlayerProfile } from "@/lib/rustlens-api";

type Props = {
	profile: PlayerProfile;
	compact?: boolean; // for hover-card; trims down spacing
};

export function PlayerProfileCard({ profile, compact = false }: Props) {
	return (
		<div className={compact ? "p-3" : "p-6"}>
			<div className="flex items-center gap-4">
				{profile.avatarImage ? (
					<img alt="" src={profile.avatarImage} className="h-16 w-16 rounded" />
				) : (
					<div className="h-16 w-16 rounded bg-muted" />
				)}
				<div className="flex flex-col">
					<h2 className="text-lg font-semibold">{profile.bmName ?? profile.steamId}</h2>
					{profile.bmId ? (
						<a
							href={`https://www.battlemetrics.com/players/${profile.bmId}`}
							target="_blank"
							rel="noreferrer"
							className="text-xs text-muted-foreground hover:underline"
						>
							BM #{profile.bmId}
						</a>
					) : null}
					<Link
						href={`https://steamcommunity.com/profiles/${profile.steamId}`}
						target="_blank"
						rel="noreferrer"
						className="text-xs text-muted-foreground hover:underline"
					>
						Steam {profile.steamId}
					</Link>
				</div>
			</div>

			<dl className="mt-4 grid grid-cols-2 gap-2 text-sm">
				{profile.totalBmHours !== null ? (
					<>
						<dt className="text-muted-foreground">Total BM hours</dt>
						<dd>{profile.totalBmHours.toFixed(1)}</dd>
					</>
				) : null}
				{profile.steamRustHours !== null ? (
					<>
						<dt className="text-muted-foreground">Steam Rust hours</dt>
						<dd>{profile.steamRustHours.toFixed(1)}</dd>
					</>
				) : null}
				{profile.currentServer ? (
					<>
						<dt className="text-muted-foreground">Currently on</dt>
						<dd>{profile.currentServer.name}</dd>
					</>
				) : profile.lastServer ? (
					<>
						<dt className="text-muted-foreground">Last seen on</dt>
						<dd>{profile.lastServer.name}</dd>
					</>
				) : null}
			</dl>

			{Object.keys(profile.categoryHours).length > 0 ? (
				<div className="mt-4">
					<h3 className="text-xs font-semibold uppercase text-muted-foreground">Category hours</h3>
					<ul className="mt-1 grid grid-cols-2 gap-1 text-sm">
						{Object.entries(profile.categoryHours).map(([cat, hours]) => (
							<li key={cat} className="flex justify-between">
								<span>{cat}</span>
								<span className="tabular-nums">{hours.toFixed(1)}</span>
							</li>
						))}
					</ul>
				</div>
			) : null}
		</div>
	);
}
```

- [ ] **Step 3: Run the test**

Run: `cd apps/frontend && pnpm test -- player-profile-card`
Expected: PASS (snapshot created on first run).

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/components/player-profile-card.tsx apps/frontend/components/player-profile-card.test.tsx apps/frontend/components/__snapshots__/
git commit -m "feat(frontend): add PlayerProfileCard shared component"
```

---

## Task 12: Frontend — `<PlayerHoverCard>` with session cache

**Files:**
- Create: `apps/frontend/components/player-hover-card.tsx`

- [ ] **Step 1: Implement the hover wrapper**

```tsx
"use client";

import { useEffect, useState } from "react";

import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { PlayerProfileCard } from "@/components/player-profile-card";
import { getPlayerProfileAction } from "@/app/_actions/player-search";
import type { PlayerProfile } from "@/lib/rustlens-api";

const sessionCache = new Map<string, PlayerProfile>();

type Props = {
	steamId: string;
	children: React.ReactNode;
};

export function PlayerHoverCard({ steamId, children }: Props) {
	const [profile, setProfile] = useState<PlayerProfile | null>(() => sessionCache.get(steamId) ?? null);
	const [open, setOpen] = useState(false);

	useEffect(() => {
		if (!open || profile) return;
		let cancelled = false;
		(async () => {
			const res = await getPlayerProfileAction(steamId);
			if (cancelled) return;
			if ("error" in res) return;
			sessionCache.set(steamId, res);
			setProfile(res);
		})();
		return () => {
			cancelled = true;
		};
	}, [open, profile, steamId]);

	return (
		<HoverCard openDelay={200} onOpenChange={setOpen}>
			<HoverCardTrigger asChild>{children}</HoverCardTrigger>
			<HoverCardContent className="w-96 p-0">
				{profile ? (
					<PlayerProfileCard profile={profile} compact />
				) : (
					<div className="p-3 text-sm text-muted-foreground">Loading…</div>
				)}
			</HoverCardContent>
		</HoverCard>
	);
}
```

- [ ] **Step 2: Type-check**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/components/player-hover-card.tsx
git commit -m "feat(frontend): add PlayerHoverCard with session cache"
```

---

## Task 13: Frontend — `app/player/[steamId]/page.tsx`

**Files:**
- Create: `apps/frontend/app/player/[steamId]/page.tsx`

- [ ] **Step 1: Implement the page**

```tsx
import { notFound } from "next/navigation";

import { PlayerProfileCard } from "@/components/player-profile-card";
import { requireSession } from "@/lib/session";
import { getPlayerProfile } from "@/lib/rustlens-api";

type Params = { steamId: string };

export default async function PlayerProfilePage({ params }: { params: Promise<Params> }) {
	await requireSession();
	const { steamId } = await params;
	let profile;
	try {
		profile = await getPlayerProfile(steamId);
	} catch (err) {
		const message = err instanceof Error ? err.message : "";
		if (message.includes("404")) notFound();
		throw err;
	}
	return (
		<main className="mx-auto max-w-3xl px-4 py-8">
			<PlayerProfileCard profile={profile} />
		</main>
	);
}
```

> Per `apps/frontend/AGENTS.md`, this Next.js version's API may differ from training data. If `params` is not a Promise in this version, drop the `await params` and use `params.steamId` directly. Confirm by reading `node_modules/next/dist/docs/` or by checking how `app/dashboard/guilds/[guildId]/page.tsx` handles its `params`.

- [ ] **Step 2: Build check**

Run: `cd apps/frontend && pnpm build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/app/player/
git commit -m "feat(frontend): add /player/[steamId] profile page"
```

---

## Task 14: Frontend — `/search` page

**Files:**
- Create: `apps/frontend/app/search/page.tsx`
- Create: `apps/frontend/app/search/_components/search-results.tsx`

- [ ] **Step 1: Implement the page**

Create `apps/frontend/app/search/page.tsx`:

```tsx
import { requireSession } from "@/lib/session";
import { getCurrentUserSafe, searchPlayers } from "@/lib/rustlens-api";

import { SearchResults } from "./_components/search-results";

type SearchParams = { q?: string; includeOffline?: string };

export default async function SearchPage({ searchParams }: { searchParams: Promise<SearchParams> }) {
	await requireSession();
	const { q = "", includeOffline = "false" } = await searchParams;
	const trimmed = q.trim();
	const includeOff = includeOffline === "true";
	const me = await getCurrentUserSafe();
	const isAdmin = (me?.user.adminRoles?.length ?? 0) > 0;

	let initial: Awaited<ReturnType<typeof searchPlayers>> = { items: [], mode: "name" };
	if (trimmed.length >= 2) {
		try {
			initial = await searchPlayers(trimmed, includeOff && isAdmin);
		} catch {
			initial = { items: [], mode: trimmed.match(/^\d+$/) ? "identifier" : "name" };
		}
	}

	return (
		<main className="mx-auto max-w-4xl px-4 py-8">
			<h1 className="text-2xl font-semibold">Player search</h1>
			<SearchResults
				initialQuery={trimmed}
				initialIncludeOffline={includeOff}
				isAdmin={isAdmin}
				initialResponse={initial}
			/>
		</main>
	);
}
```

> If `getCurrentUserSafe`'s return type doesn't expose `adminRoles` directly, adjust to whichever field carries the admin role list (`user.adminRoles` is the shape used elsewhere in the frontend, see `apps/frontend/lib/rustlens-api.ts:55`).

- [ ] **Step 2: Implement the client results component**

Create `apps/frontend/app/search/_components/search-results.tsx`:

```tsx
"use client";

import { useEffect, useState } from "react";
import Link from "next/link";

import { Switch } from "@/components/ui/switch";
import { PlayerHoverCard } from "@/components/player-hover-card";
import { searchPlayersAction } from "@/app/_actions/player-search";
import type { PlayerSearchResponse } from "@/lib/rustlens-api";

type Props = {
	initialQuery: string;
	initialIncludeOffline: boolean;
	isAdmin: boolean;
	initialResponse: PlayerSearchResponse;
};

export function SearchResults({ initialQuery, initialIncludeOffline, isAdmin, initialResponse }: Props) {
	const [includeOffline, setIncludeOffline] = useState(initialIncludeOffline);
	const [response, setResponse] = useState<PlayerSearchResponse>(initialResponse);

	useEffect(() => {
		if (includeOffline === initialIncludeOffline) return;
		(async () => {
			const res = await searchPlayersAction(initialQuery, includeOffline);
			if (!("error" in res)) setResponse(res);
		})();
	}, [includeOffline, initialIncludeOffline, initialQuery]);

	return (
		<div className="mt-4 flex flex-col gap-4">
			{isAdmin && response.mode === "name" ? (
				<label className="flex items-center gap-2 text-sm">
					<Switch checked={includeOffline} onCheckedChange={setIncludeOffline} />
					Include offline (&lt; 24h)
				</label>
			) : null}

			{response.items.length === 0 ? (
				<p className="text-sm text-muted-foreground">No players match.</p>
			) : (
				<ul className="flex flex-col divide-y rounded-md border">
					{response.items.map((hit) => (
						<li key={hit.steamId} className="flex items-center gap-4 p-3">
							<PlayerHoverCard steamId={hit.steamId}>
								<Link
									href={`/player/${encodeURIComponent(hit.steamId)}`}
									className="flex flex-1 items-center gap-3"
								>
									{hit.avatarUrl ? (
										<img alt="" src={hit.avatarUrl} className="h-10 w-10 rounded" />
									) : (
										<div className="h-10 w-10 rounded bg-muted" />
									)}
									<div className="flex flex-col text-sm">
										<span className="font-medium">{hit.bmName ?? hit.steamId}</span>
										<span className="text-xs text-muted-foreground">
											{hit.currentServer ? hit.currentServer.name : hit.isOnline ? "online" : "offline"}
										</span>
									</div>
								</Link>
							</PlayerHoverCard>
						</li>
					))}
				</ul>
			)}
		</div>
	);
}
```

- [ ] **Step 3: Build check**

Run: `cd apps/frontend && pnpm build`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/app/search/
git commit -m "feat(frontend): add /search page with admin offline toggle"
```

---

## Task 15: Update AGENTS.md

The repo root `AGENTS.md` is the source of truth and is symlinked into each app. Update the **Frontend** and **Backend** sections.

- [ ] **Step 1: Add the new frontend pages**

In `AGENTS.md` under the frontend tree comment block (currently listing routes like `app/dashboard/page.tsx`), add:

```
    search/page.tsx                     # Player search results page (?q=&includeOffline=); admin-only offline toggle
    player/[steamId]/page.tsx           # Public player profile page (mirror of bot /player embed, charts deferred)
```

And in the components list:

```
    navbar-player-search.tsx   # Discord-style player search popover injected into the dashboard/admin header
    player-hover-card.tsx      # Hover wrapper around PlayerProfileCard with per-session cache
    player-profile-card.tsx    # Shared profile card body used by /player and hover preview
```

- [ ] **Step 2: Add the new backend routes**

Under the backend `Key patterns:` bullets, add:

```
- App-facing player routes (`GET /api/app/players/search`, `GET /api/app/players/{identifier}/profile`) are session-authed via `requireSession`. The search route accepts identifier-shaped queries (Steam ID, BM ID, steamcommunity URL) for any logged-in user and falls back to a name-substring search across all globally tracked servers — capped at 20, deduped by Steam ID, online-first — that requires the caller to have at least one row in `admin_user_roles`. The profile route resolves any identifier to a Steam64 ID then delegates to the same pipeline as the bot-internal `/api/internal/bot/setup/steam/{steamID}/profile`. Both routes are rate-limited per session via Redis (`players_search`: 30/60s, `players_profile`: 60/60s). Shared lookup logic lives in `internal/players/`.
```

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document /api/app/players/* routes and player profile pages"
```

---

## Self-review (post-plan)

A pass over the spec confirms each requirement maps to a task:

| Spec section | Task |
|---|---|
| Lookup behavior table (identifier / name / admin gate) | Tasks 1, 4 |
| Server scope = all `tracked_servers`, dedupe, cap 20, online-first | Task 2 |
| Include-offline default off; toggle on `/search` only | Tasks 4 (default), 14 (toggle) |
| Navbar component injected into `app-header.tsx`, `<sm` hidden, `/` shortcut, debounce 250ms, abort, min-len 2 | Tasks 9, 10 |
| `/search` page with admin toggle | Task 14 |
| `/player/[steamId]` page using shared card | Tasks 11, 13 |
| Hover preview = `<PlayerProfileCard>` with session cache | Task 12 |
| `lib/rustlens-api.ts` additions | Task 7 |
| Backend new package `internal/players/` | Tasks 1, 3 |
| Two new backend routes under `/api/app/*` | Tasks 4, 5 |
| Reuse existing identifier resolution (no duplication) | Tasks 3, 5 |
| Redis rate limits | Task 6 |
| Error mapping (400/403/404/429/502) | Tasks 4, 8 |
| AGENTS.md update | Task 15 |

Items intentionally deferred per spec: charts on `/player`, mobile navbar input, public-landing search, end-to-end browser tests.
