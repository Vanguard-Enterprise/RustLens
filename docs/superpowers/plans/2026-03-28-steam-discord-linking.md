# Steam & Discord Linking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add two PostgreSQL tables (`steam_accounts`, `steam_discord_links`) with Go store methods, HTTP handlers, chi routes, and TypeScript bot client methods to support Steam-to-Discord account linking.

**Architecture:** New store file `steam.go` with structs and CRUD methods on `*PostgresStore`, matching existing patterns (sentinel errors, `scan` helpers, parameterized queries). New handler methods in `handlers.go` and routes in `router.go` under the existing JWT-protected `/api/internal/bot/setup` prefix. New TypeScript types and methods on `BackendClient`.

**Tech Stack:** Go 1.24 (chi, pgx v5), TypeScript (discord.js bot client)

**Spec:** `docs/superpowers/specs/2026-03-28-steam-discord-linking-design.md`

---

## File Map

| Action | File | Responsibility |
|---|---|---|
| Modify | `apps/backend/internal/store/postgres.go` | Add `steam_accounts` and `steam_discord_links` table DDL to `EnsureSchema()` |
| Create | `apps/backend/internal/store/steam.go` | `SteamAccount` and `SteamDiscordLink` structs, sentinel errors, scan helpers, all CRUD methods |
| Create | `apps/backend/internal/store/steam_test.go` | Unit tests for validation helpers (scan helpers are tested implicitly via integration) |
| Modify | `apps/backend/internal/http/handlers.go` | Handler methods for all 7 steam/discord endpoints |
| Modify | `apps/backend/internal/http/router.go` | Register steam/discord routes under `/api/internal/bot/setup` |
| Modify | `apps/bot/src/backend-client.ts` | TypeScript types and 7 new methods on `BackendClient` |
| Modify | `AGENTS.md` | Document steam/discord linking in Backend section |
| Modify | `apps/backend/AGENTS.md` | Add `steam_accounts` and `steam_discord_links` table schemas |

---

### Task 1: Schema — Add table DDL to EnsureSchema

**Files:**
- Modify: `apps/backend/internal/store/postgres.go:51-131`

- [ ] **Step 1: Add steam_accounts and steam_discord_links DDL**

Add the following SQL to the end of the `EnsureSchema` exec string, just before the closing backtick+`)` on line 129:

```go
		CREATE TABLE IF NOT EXISTS steam_accounts (
			steam_id TEXT PRIMARY KEY,
			bm_id TEXT UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS steam_discord_links (
			steam_id TEXT NOT NULL REFERENCES steam_accounts(steam_id),
			discord_id TEXT NOT NULL,
			type TEXT NOT NULL CHECK (type IN ('soft', 'hard')),
			source TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (steam_id, discord_id, type)
		);

		CREATE INDEX IF NOT EXISTS steam_discord_links_discord_idx
		ON steam_discord_links (discord_id);
```

- [ ] **Step 2: Verify it compiles**

Run: `cd apps/backend && go build ./cmd/server`
Expected: Build succeeds with no errors.

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/store/postgres.go
git commit -m "feat(backend): add steam_accounts and steam_discord_links schema"
```

---

### Task 2: Store — Create steam.go with structs, errors, and scan helpers

**Files:**
- Create: `apps/backend/internal/store/steam.go`

- [ ] **Step 1: Create steam.go with types and helpers**

Create `apps/backend/internal/store/steam.go`:

```go
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrSteamAccountNotFound = errors.New("steam account not found")
var ErrLinkAlreadyExists = errors.New("steam discord link already exists")

type SteamAccount struct {
	SteamID   string
	BmID      *string
	CreatedAt time.Time
}

type SteamDiscordLink struct {
	SteamID   string
	DiscordID string
	Type      string
	Source    string
	CreatedAt time.Time
}

func scanSteamAccount(scanner rowScanner) (SteamAccount, error) {
	var record SteamAccount
	err := scanner.Scan(&record.SteamID, &record.BmID, &record.CreatedAt)
	if err != nil {
		return SteamAccount{}, err
	}
	return record, nil
}

func scanSteamDiscordLink(scanner rowScanner) (SteamDiscordLink, error) {
	var record SteamDiscordLink
	err := scanner.Scan(&record.SteamID, &record.DiscordID, &record.Type, &record.Source, &record.CreatedAt)
	if err != nil {
		return SteamDiscordLink{}, err
	}
	return record, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd apps/backend && go build ./cmd/server`
Expected: Build succeeds (unused imports will be flagged only if the context/time imports are truly unused — they won't be since `SteamAccount` uses `time.Time` and `context` is used by methods added in later steps, but Go will flag `context` as unused now). Remove the `context` import for now — it will be added back in step 3.

Actually, remove the `"context"` import line since no methods use it yet. The file should import only:

```go
import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)
```

Wait — `pgx` is also not used yet (only in scan helpers via `rowScanner` which is defined in `wipe_reacts.go`). Remove the `pgx` import too. The file should import only:

```go
import (
	"errors"
	"time"
)
```

Run: `cd apps/backend && go build ./cmd/server`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/store/steam.go
git commit -m "feat(backend): add steam account and link types with scan helpers"
```

---

### Task 3: Store — CRUD methods for SteamAccount

**Files:**
- Modify: `apps/backend/internal/store/steam.go`

- [ ] **Step 1: Add UpsertSteamAccount**

Add to `steam.go`, and add `"context"` and `"github.com/jackc/pgx/v5"` back to imports:

```go
func (p *PostgresStore) UpsertSteamAccount(ctx context.Context, steamID string, bmID *string) (SteamAccount, error) {
	var record SteamAccount
	err := p.pool.QueryRow(ctx, `
		INSERT INTO steam_accounts (steam_id, bm_id)
		VALUES ($1, $2)
		ON CONFLICT (steam_id) DO UPDATE
		SET bm_id = COALESCE(EXCLUDED.bm_id, steam_accounts.bm_id)
		RETURNING steam_id, bm_id, created_at
	`, steamID, bmID).Scan(&record.SteamID, &record.BmID, &record.CreatedAt)
	if err != nil {
		return SteamAccount{}, err
	}
	return record, nil
}
```

Note: `COALESCE(EXCLUDED.bm_id, steam_accounts.bm_id)` means: if the new bm_id is non-null, use it; otherwise keep the existing one. This prevents a soft-link upsert (which passes nil for bm_id) from wiping an existing bm_id.

- [ ] **Step 2: Add GetSteamAccountBySteamID**

```go
func (p *PostgresStore) GetSteamAccountBySteamID(ctx context.Context, steamID string) (SteamAccount, error) {
	record, err := scanSteamAccount(p.pool.QueryRow(ctx, `
		SELECT steam_id, bm_id, created_at
		FROM steam_accounts
		WHERE steam_id = $1
	`, steamID))
	if errors.Is(err, pgx.ErrNoRows) {
		return SteamAccount{}, ErrSteamAccountNotFound
	}
	return record, err
}
```

- [ ] **Step 3: Add GetSteamAccountByBmID**

```go
func (p *PostgresStore) GetSteamAccountByBmID(ctx context.Context, bmID string) (SteamAccount, error) {
	record, err := scanSteamAccount(p.pool.QueryRow(ctx, `
		SELECT steam_id, bm_id, created_at
		FROM steam_accounts
		WHERE bm_id = $1
	`, bmID))
	if errors.Is(err, pgx.ErrNoRows) {
		return SteamAccount{}, ErrSteamAccountNotFound
	}
	return record, err
}
```

- [ ] **Step 4: Verify it compiles**

Run: `cd apps/backend && go build ./cmd/server`
Expected: Build succeeds.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/store/steam.go
git commit -m "feat(backend): add steam account upsert and lookup methods"
```

---

### Task 4: Store — CRUD methods for SteamDiscordLink

**Files:**
- Modify: `apps/backend/internal/store/steam.go`

- [ ] **Step 1: Add CreateSteamDiscordLink**

This method upserts the steam_account row first (so FKs are satisfied), then inserts the link:

```go
func (p *PostgresStore) CreateSteamDiscordLink(ctx context.Context, link SteamDiscordLink) (SteamDiscordLink, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return SteamDiscordLink{}, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO steam_accounts (steam_id)
		VALUES ($1)
		ON CONFLICT (steam_id) DO NOTHING
	`, link.SteamID)
	if err != nil {
		return SteamDiscordLink{}, err
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO steam_discord_links (steam_id, discord_id, type, source)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (steam_id, discord_id, type) DO NOTHING
	`, link.SteamID, link.DiscordID, link.Type, link.Source)
	if err != nil {
		return SteamDiscordLink{}, err
	}

	if tag.RowsAffected() == 0 {
		return SteamDiscordLink{}, ErrLinkAlreadyExists
	}

	if err := tx.Commit(ctx); err != nil {
		return SteamDiscordLink{}, err
	}

	return link, nil
}
```

- [ ] **Step 2: Add ListLinksBySteamID**

```go
func (p *PostgresStore) ListLinksBySteamID(ctx context.Context, steamID string) ([]SteamDiscordLink, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT steam_id, discord_id, type, source, created_at
		FROM steam_discord_links
		WHERE steam_id = $1
		ORDER BY created_at ASC
	`, steamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]SteamDiscordLink, 0)
	for rows.Next() {
		record, err := scanSteamDiscordLink(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
```

- [ ] **Step 3: Add ListLinksByDiscordID**

```go
func (p *PostgresStore) ListLinksByDiscordID(ctx context.Context, discordID string) ([]SteamDiscordLink, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT steam_id, discord_id, type, source, created_at
		FROM steam_discord_links
		WHERE discord_id = $1
		ORDER BY created_at ASC
	`, discordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]SteamDiscordLink, 0)
	for rows.Next() {
		record, err := scanSteamDiscordLink(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
```

- [ ] **Step 4: Add DeleteSoftLink**

```go
func (p *PostgresStore) DeleteSoftLink(ctx context.Context, steamID string, discordID string) error {
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM steam_discord_links
		WHERE steam_id = $1 AND discord_id = $2 AND type = 'soft'
	`, steamID, discordID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSteamAccountNotFound
	}
	return nil
}
```

- [ ] **Step 5: Verify it compiles**

Run: `cd apps/backend && go build ./cmd/server`
Expected: Build succeeds.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/store/steam.go
git commit -m "feat(backend): add steam discord link CRUD methods"
```

---

### Task 5: Store — BulkUpsertHardLinks

**Files:**
- Modify: `apps/backend/internal/store/steam.go`

- [ ] **Step 1: Add BulkUpsertHardLinks**

This is a single transaction that upserts steam accounts (with bm_id) and inserts hard links idempotently:

```go
type BulkHardLink struct {
	SteamID   string
	BmID      *string
	DiscordID string
	Source    string
}

func (p *PostgresStore) BulkUpsertHardLinks(ctx context.Context, links []BulkHardLink) error {
	if len(links) == 0 {
		return nil
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, link := range links {
		_, err := tx.Exec(ctx, `
			INSERT INTO steam_accounts (steam_id, bm_id)
			VALUES ($1, $2)
			ON CONFLICT (steam_id) DO UPDATE
			SET bm_id = COALESCE(EXCLUDED.bm_id, steam_accounts.bm_id)
		`, link.SteamID, link.BmID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO steam_discord_links (steam_id, discord_id, type, source)
			VALUES ($1, $2, 'hard', $3)
			ON CONFLICT (steam_id, discord_id, type) DO NOTHING
		`, link.SteamID, link.DiscordID, link.Source)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd apps/backend && go build ./cmd/server`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/store/steam.go
git commit -m "feat(backend): add bulk upsert for hard links"
```

---

### Task 6: Handlers — Steam account and link endpoints

**Files:**
- Modify: `apps/backend/internal/http/handlers.go`

- [ ] **Step 1: Add steam ID validation regex**

Add after the existing `snowflakePattern` on line 21:

```go
var steamIDPattern = regexp.MustCompile(`^\d{17}$`)
```

- [ ] **Step 2: Add response serialization helpers**

Add after the existing `writeWipeReactScheduleList` function (search for it to find the right location):

```go
func steamAccountToMap(record store.SteamAccount) map[string]any {
	result := map[string]any{
		"steamId":   record.SteamID,
		"createdAt": record.CreatedAt,
	}
	if record.BmID != nil {
		result["bmId"] = *record.BmID
	} else {
		result["bmId"] = nil
	}
	return result
}

func steamDiscordLinkToMap(link store.SteamDiscordLink) map[string]any {
	return map[string]any{
		"steamId":   link.SteamID,
		"discordId": link.DiscordID,
		"type":      link.Type,
		"source":    link.Source,
		"createdAt": link.CreatedAt,
	}
}

func writeSteamAccount(w http.ResponseWriter, record store.SteamAccount) {
	writeJSON(w, http.StatusOK, steamAccountToMap(record))
}

func writeSteamDiscordLinkList(w http.ResponseWriter, links []store.SteamDiscordLink) {
	items := make([]map[string]any, 0, len(links))
	for _, link := range links {
		items = append(items, steamDiscordLinkToMap(link))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
```

- [ ] **Step 3: Add GetSteamAccount handler**

```go
func (h *Handler) GetSteamAccount(w http.ResponseWriter, r *http.Request) {
	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	account, err := h.postgres.GetSteamAccountBySteamID(r.Context(), steamID)
	if errors.Is(err, store.ErrSteamAccountNotFound) {
		writeError(w, http.StatusNotFound, "steam account not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetSteamAccount error for %s: %v", steamID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch steam account")
		return
	}

	writeSteamAccount(w, account)
}
```

- [ ] **Step 4: Add GetSteamAccountByBmID handler**

```go
func (h *Handler) GetSteamAccountByBmID(w http.ResponseWriter, r *http.Request) {
	bmID := chi.URLParam(r, "bmID")
	if bmID == "" {
		writeError(w, http.StatusBadRequest, "bmID is required")
		return
	}

	account, err := h.postgres.GetSteamAccountByBmID(r.Context(), bmID)
	if errors.Is(err, store.ErrSteamAccountNotFound) {
		writeError(w, http.StatusNotFound, "steam account not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetSteamAccountByBmID error for %s: %v", bmID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch steam account")
		return
	}

	writeSteamAccount(w, account)
}
```

- [ ] **Step 5: Add ListLinksBySteamID handler**

```go
func (h *Handler) ListLinksBySteamID(w http.ResponseWriter, r *http.Request) {
	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	links, err := h.postgres.ListLinksBySteamID(r.Context(), steamID)
	if err != nil {
		log.Printf("[backend] ListLinksBySteamID error for %s: %v", steamID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch links")
		return
	}

	writeSteamDiscordLinkList(w, links)
}
```

- [ ] **Step 6: Add ListLinksByDiscordID handler**

```go
func (h *Handler) ListLinksByDiscordID(w http.ResponseWriter, r *http.Request) {
	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	links, err := h.postgres.ListLinksByDiscordID(r.Context(), discordID)
	if err != nil {
		log.Printf("[backend] ListLinksByDiscordID error for %s: %v", discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch links")
		return
	}

	writeSteamDiscordLinkList(w, links)
}
```

- [ ] **Step 7: Add CreateSoftLink handler**

```go
type createSoftLinkRequest struct {
	SteamID   string `json:"steamId"`
	DiscordID string `json:"discordId"`
	Source    string `json:"source"`
}

func (h *Handler) CreateSoftLink(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request createSoftLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.SteamID == "" || !steamIDPattern.MatchString(request.SteamID) {
		writeError(w, http.StatusBadRequest, "steamId must be a 17-digit number")
		return
	}
	if !validateSnowflakeValue(request.DiscordID) {
		writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
		return
	}
	if request.Source == "" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
	}

	link, err := h.postgres.CreateSteamDiscordLink(r.Context(), store.SteamDiscordLink{
		SteamID:   request.SteamID,
		DiscordID: request.DiscordID,
		Type:      "soft",
		Source:    request.Source,
	})
	if errors.Is(err, store.ErrLinkAlreadyExists) {
		writeError(w, http.StatusConflict, "soft link already exists for this steam and discord pair")
		return
	}
	if err != nil {
		log.Printf("[backend] CreateSoftLink error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create soft link")
		return
	}

	writeJSON(w, http.StatusCreated, steamDiscordLinkToMap(link))
}
```

- [ ] **Step 8: Add BulkUpsertHardLinks handler**

```go
type bulkHardLinkEntry struct {
	SteamID   string  `json:"steamId"`
	BmID      *string `json:"bmId"`
	DiscordID string  `json:"discordId"`
	Source    string  `json:"source"`
}

type bulkHardLinksRequest struct {
	Links []bulkHardLinkEntry `json:"links"`
}

func (h *Handler) BulkUpsertHardLinks(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request bulkHardLinksRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(request.Links) == 0 {
		writeError(w, http.StatusBadRequest, "links array must not be empty")
		return
	}

	storeLinks := make([]store.BulkHardLink, 0, len(request.Links))
	for i, entry := range request.Links {
		if entry.SteamID == "" || !steamIDPattern.MatchString(entry.SteamID) {
			writeError(w, http.StatusBadRequest, "links["+strconv.Itoa(i)+"].steamId must be a 17-digit number")
			return
		}
		if !validateSnowflakeValue(entry.DiscordID) {
			writeError(w, http.StatusBadRequest, "links["+strconv.Itoa(i)+"].discordId must be a valid Discord snowflake")
			return
		}
		if entry.Source == "" {
			writeError(w, http.StatusBadRequest, "links["+strconv.Itoa(i)+"].source is required")
			return
		}
		storeLinks = append(storeLinks, store.BulkHardLink{
			SteamID:   entry.SteamID,
			BmID:      entry.BmID,
			DiscordID: entry.DiscordID,
			Source:    entry.Source,
		})
	}

	if err := h.postgres.BulkUpsertHardLinks(r.Context(), storeLinks); err != nil {
		log.Printf("[backend] BulkUpsertHardLinks error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to upsert hard links")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "count": len(storeLinks)})
}
```

- [ ] **Step 9: Add DeleteSoftLink handler**

```go
func (h *Handler) DeleteSoftLink(w http.ResponseWriter, r *http.Request) {
	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	err := h.postgres.DeleteSoftLink(r.Context(), steamID, discordID)
	if errors.Is(err, store.ErrSteamAccountNotFound) {
		writeError(w, http.StatusNotFound, "soft link not found")
		return
	}
	if err != nil {
		log.Printf("[backend] DeleteSoftLink error for %s/%s: %v", steamID, discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete soft link")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
```

- [ ] **Step 10: Verify it compiles**

Run: `cd apps/backend && go build ./cmd/server`
Expected: Build succeeds (handlers reference store methods and chi router params — all exist now).

- [ ] **Step 11: Commit**

```bash
git add apps/backend/internal/http/handlers.go
git commit -m "feat(backend): add steam and discord link HTTP handlers"
```

---

### Task 7: Router — Register steam/discord routes

**Files:**
- Modify: `apps/backend/internal/http/router.go:50-71`

- [ ] **Step 1: Add steam/discord routes**

Inside the existing `r.Route("/api/internal/bot/setup", ...)` block, after the last wipe-react-schedule route (line 70: `r.Post("/.../mark-posted", ...)`), add:

```go
		// Steam & Discord linking endpoints
		r.Get("/steam/{steamID}", handler.GetSteamAccount)
		r.Get("/steam/by-bm/{bmID}", handler.GetSteamAccountByBmID)
		r.Get("/steam/{steamID}/links", handler.ListLinksBySteamID)
		r.Post("/steam/links", handler.CreateSoftLink)
		r.Post("/steam/bulk-hard-links", handler.BulkUpsertHardLinks)
		r.Delete("/steam/{steamID}/links/{discordID}/soft", handler.DeleteSoftLink)
		r.Get("/discord/{discordID}/links", handler.ListLinksByDiscordID)
```

Note on route ordering: `/steam/by-bm/{bmID}` must be registered before `/steam/{steamID}` in chi, because chi matches literal segments before wildcards. Actually, chi v5 uses a trie-based router that handles this correctly — `/steam/by-bm/` is a more specific path than `/steam/{steamID}` so it takes priority regardless of registration order. But placing it adjacent makes the intent clear.

Wait — correction: chi matches routes in registration order for conflicting patterns. `/steam/by-bm/{bmID}` and `/steam/{steamID}` would conflict because `by-bm` could match `{steamID}`. To avoid ambiguity, register the literal path first:

```go
		// Steam & Discord linking endpoints
		r.Get("/steam/by-bm/{bmID}", handler.GetSteamAccountByBmID)
		r.Get("/steam/links", handler.CreateSoftLink) // wait, this is POST
		r.Post("/steam/links", handler.CreateSoftLink)
		r.Post("/steam/bulk-hard-links", handler.BulkUpsertHardLinks)
		r.Get("/steam/{steamID}", handler.GetSteamAccount)
		r.Get("/steam/{steamID}/links", handler.ListLinksBySteamID)
		r.Delete("/steam/{steamID}/links/{discordID}/soft", handler.DeleteSoftLink)
		r.Get("/discord/{discordID}/links", handler.ListLinksByDiscordID)
```

Corrected — the final route block to add:

```go
		// Steam & Discord linking endpoints
		r.Get("/steam/by-bm/{bmID}", handler.GetSteamAccountByBmID)
		r.Post("/steam/links", handler.CreateSoftLink)
		r.Post("/steam/bulk-hard-links", handler.BulkUpsertHardLinks)
		r.Get("/steam/{steamID}", handler.GetSteamAccount)
		r.Get("/steam/{steamID}/links", handler.ListLinksBySteamID)
		r.Delete("/steam/{steamID}/links/{discordID}/soft", handler.DeleteSoftLink)
		r.Get("/discord/{discordID}/links", handler.ListLinksByDiscordID)
```

- [ ] **Step 2: Verify it compiles**

Run: `cd apps/backend && go build ./cmd/server`
Expected: Build succeeds.

- [ ] **Step 3: Run all backend tests**

Run: `cd apps/backend && go test ./...`
Expected: All existing tests pass.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/http/router.go
git commit -m "feat(backend): register steam/discord linking routes"
```

---

### Task 8: Bot client — Add TypeScript types and methods

**Files:**
- Modify: `apps/bot/src/backend-client.ts`

- [ ] **Step 1: Add TypeScript types**

Add after the existing `WipeReactScheduleListResponse` type (around line 83):

```typescript
export type SteamAccount = {
  steamId: string;
  bmId: string | null;
  createdAt: string;
};

export type SteamDiscordLink = {
  steamId: string;
  discordId: string;
  type: "soft" | "hard";
  source: string;
  createdAt: string;
};

export type SteamDiscordLinkListResponse = {
  items: SteamDiscordLink[];
};

export type BulkHardLinkEntry = {
  steamId: string;
  bmId?: string | null;
  discordId: string;
  source: string;
};
```

- [ ] **Step 2: Add BackendClient methods**

Add after the existing `markWipeReactSchedulePosted` method (end of the class, before the closing `}`):

```typescript
  getSteamAccount(workerId: string, steamId: string) {
    return this.call<SteamAccount>(
      workerId,
      `/api/internal/bot/setup/steam/${steamId}`,
      "GET",
    );
  }

  getSteamAccountByBmID(workerId: string, bmId: string) {
    return this.call<SteamAccount>(
      workerId,
      `/api/internal/bot/setup/steam/by-bm/${bmId}`,
      "GET",
    );
  }

  getLinksBySteamID(workerId: string, steamId: string) {
    return this.call<SteamDiscordLinkListResponse>(
      workerId,
      `/api/internal/bot/setup/steam/${steamId}/links`,
      "GET",
    );
  }

  getLinksByDiscordID(workerId: string, discordId: string) {
    return this.call<SteamDiscordLinkListResponse>(
      workerId,
      `/api/internal/bot/setup/discord/${discordId}/links`,
      "GET",
    );
  }

  createSoftLink(workerId: string, steamId: string, discordId: string, source: string) {
    return this.call<SteamDiscordLink>(
      workerId,
      `/api/internal/bot/setup/steam/links`,
      "POST",
      { steamId, discordId, source },
    );
  }

  bulkUpsertHardLinks(workerId: string, links: BulkHardLinkEntry[]) {
    return this.call<{ status: string; count: number }>(
      workerId,
      `/api/internal/bot/setup/steam/bulk-hard-links`,
      "POST",
      { links },
    );
  }

  deleteSoftLink(workerId: string, steamId: string, discordId: string) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/setup/steam/${steamId}/links/${discordId}/soft`,
      "DELETE",
    );
  }
```

- [ ] **Step 3: Verify it type-checks**

Run: `pnpm --filter @rustlens/bot lint`
Expected: No type errors.

- [ ] **Step 4: Commit**

```bash
git add apps/bot/src/backend-client.ts
git commit -m "feat(bot): add steam/discord linking client methods"
```

---

### Task 9: Documentation — Update AGENTS.md files

**Files:**
- Modify: `AGENTS.md`
- Modify: `apps/backend/AGENTS.md`

- [ ] **Step 1: Update root AGENTS.md**

In the **Backend (Go)** section, after the `store/` file tree block (which ends with `redis.go`), add:

```
      steam.go             # Steam account & Discord link CRUD (upsert, lookup, bulk hard links, soft link delete)
```

Then, after the "Key patterns" bullet list, add a new paragraph:

```
**Steam & Discord linking**

`steam_accounts` stores known Steam players, optionally with their BattleMetrics player ID (`bm_id`). The `bm_id` is populated when a hard link is created via BattleMetrics scrape.

`steam_discord_links` maps Steam accounts to Discord users with a `type` and `source`:
- **soft** — user self-reported via bot command or frontend. No verification that the Steam account actually belongs to them. Source examples: `bot_command`, `frontend`.
- **hard** — discovered from a trusted source (e.g., BattleMetrics API) where the mapping is authoritatively verified. Most likely valid. Source examples: `battlemetrics`.

A single (steam_id, discord_id) pair can have both a soft and hard link simultaneously. The relationship is many-to-many: one Steam account can link to multiple Discord users and vice versa.

Bot-internal endpoints under `/api/internal/bot/setup/steam/` and `/api/internal/bot/setup/discord/` provide CRUD for both tables. Admins can only delete soft links.
```

Also update the router.go description line to mention the new routes:

```
      router.go              # chi routes: /api/docs, /api/healthz, /api/internal/bot/shards/*, /api/internal/bot/setup/*, incl. steam/discord linking
```

- [ ] **Step 2: Update backend AGENTS.md**

Add two new table schema sections after the existing `wipe_react_schedules` section:

```markdown
### `steam_accounts`

Known Steam player accounts with optional BattleMetrics ID.

| Column | Type | Notes |
|---|---|---|
| `steam_id` | `TEXT` | PK (17-digit Steam ID) |
| `bm_id` | `TEXT UNIQUE` | Nullable, BattleMetrics player ID |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

### `steam_discord_links`

Maps Steam accounts to Discord users. A pair can have both soft and hard links.

| Column | Type | Notes |
|---|---|---|
| `steam_id` | `TEXT NOT NULL` | FK to `steam_accounts.steam_id` |
| `discord_id` | `TEXT NOT NULL` | Discord snowflake |
| `type` | `TEXT NOT NULL` | `soft` or `hard` (CHECK constraint) |
| `source` | `TEXT NOT NULL DEFAULT ''` | Origin: `bot_command`, `frontend`, `battlemetrics` |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

**PK**: `(steam_id, discord_id, type)` — allows both link types per pair.

**Index**: `steam_discord_links_discord_idx ON (discord_id)` — reverse lookups by Discord user.

**Link types:**
- **soft** — user self-reported, unverified. Admins can delete.
- **hard** — from trusted source (BattleMetrics), authoritatively verified. Cannot be deleted via API.
```

- [ ] **Step 3: Verify no syntax issues**

Skim both files for markdown formatting. No build step needed.

- [ ] **Step 4: Commit**

```bash
git add AGENTS.md apps/backend/AGENTS.md
git commit -m "docs: document steam/discord linking tables and endpoints"
```
