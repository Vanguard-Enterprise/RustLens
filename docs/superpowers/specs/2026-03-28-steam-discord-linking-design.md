# Steam & Discord Linking — Design Spec

## Overview

Two new PostgreSQL tables to store Steam player accounts and their links to Discord users. Links have a type (soft or hard) indicating trust level, and a source indicating origin.

## Definitions

- **Soft link** — User self-reported via bot command or frontend. No verification that the Steam account belongs to them. Untrustworthy but useful as a signal.
- **Hard link** — Discovered from a trusted source (e.g., BattleMetrics API) where the mapping is authoritatively verified. Most likely valid.
- **bm_id** — BattleMetrics player ID. Populated when a hard link is created via BattleMetrics scrape, which provides both the Steam-to-Discord mapping and the BM player ID.

A single (steam_id, discord_id) pair can have both a soft and hard link simultaneously. The relationship is many-to-many: one Steam account can link to multiple Discord users, and one Discord user can link to multiple Steam accounts.

## Schema

```sql
CREATE TABLE IF NOT EXISTS steam_accounts (
    steam_id   TEXT PRIMARY KEY,
    bm_id      TEXT UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS steam_discord_links (
    steam_id   TEXT NOT NULL REFERENCES steam_accounts(steam_id),
    discord_id TEXT NOT NULL,
    type       TEXT NOT NULL CHECK (type IN ('soft', 'hard')),
    source     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (steam_id, discord_id, type)
);

CREATE INDEX IF NOT EXISTS steam_discord_links_discord_idx
    ON steam_discord_links (discord_id);
```

Key constraints:
- FK from `steam_discord_links.steam_id` to `steam_accounts.steam_id` — a link cannot exist without the Steam account row.
- `bm_id` is UNIQUE but nullable — not all Steam accounts have a BM ID.
- Composite PK `(steam_id, discord_id, type)` allows the same pair to have both soft and hard links but prevents duplicate entries of the same type.
- Index on `discord_id` for reverse lookups ("all Steam accounts for this Discord user").
- `source` is free-text (e.g., `"battlemetrics"`, `"bot_command"`, `"frontend"`).

## Store Layer

New file: `apps/backend/internal/store/steam.go`

### Structs

```go
type SteamAccount struct {
    SteamID   string
    BmID      *string  // nil when not yet known
    CreatedAt time.Time
}

type SteamDiscordLink struct {
    SteamID   string
    DiscordID string
    Type      string   // "soft" or "hard"
    Source    string
    CreatedAt time.Time
}
```

### Methods on `*PostgresStore`

| Method | Description |
|---|---|
| `UpsertSteamAccount(steamID, bmID)` | Insert or update bm_id on conflict |
| `GetSteamAccountBySteamID(steamID)` | Lookup by steam_id |
| `GetSteamAccountByBmID(bmID)` | Lookup by bm_id |
| `CreateSteamDiscordLink(link)` | Upserts the steam_account row (without bm_id if not provided) then inserts the link |
| `BulkUpsertHardLinks(links []SteamDiscordLink)` | BattleMetrics batch — upserts steam_accounts with bm_id and inserts hard links in one transaction |
| `ListLinksBySteamID(steamID)` | All links for a Steam account |
| `ListLinksByDiscordID(discordID)` | All links for a Discord user |
| `DeleteSoftLink(steamID, discordID)` | Admin delete — only removes the soft link type |

### Sentinel Errors

- `ErrSteamAccountNotFound`
- `ErrLinkAlreadyExists`

### BulkUpsertHardLinks Transaction

1. For each entry: `INSERT INTO steam_accounts (steam_id, bm_id) ... ON CONFLICT (steam_id) DO UPDATE SET bm_id = EXCLUDED.bm_id` (upserts bm_id)
2. For each entry: `INSERT INTO steam_discord_links ... ON CONFLICT DO NOTHING` (idempotent re-scrapes)

## API Routes

All bot-internal, JWT-protected, under `/api/internal/bot/setup`:

| Method | Path | Description |
|---|---|---|
| POST | `/steam/bulk-hard-links` | BattleMetrics scrape batch upsert |
| POST | `/steam/links` | Create a single soft link (from bot command) |
| GET | `/steam/{steamID}` | Get steam account by steam_id |
| GET | `/steam/by-bm/{bmID}` | Get steam account by bm_id |
| GET | `/steam/{steamID}/links` | All links for a steam account |
| GET | `/discord/{discordID}/links` | All links for a discord user |
| DELETE | `/steam/{steamID}/links/{discordID}/soft` | Admin delete a soft link |

These reuse the existing `botJWTMiddleware` and handler patterns. Input validation uses the existing snowflake regex for discord IDs. Steam IDs are validated as 17-digit numeric strings.

## Bot Client

New methods on the existing HTTP client in `apps/bot/src/backend-client.ts`:

| Method | Description |
|---|---|
| `bulkUpsertHardLinks(links)` | POST to bulk-hard-links endpoint |
| `createSoftLink(steamID, discordID, source)` | POST to create a single soft link |
| `getSteamAccount(steamID)` | GET by steam_id |
| `getSteamAccountByBmID(bmID)` | GET by bm_id |
| `getLinksBySteamID(steamID)` | GET all links for a steam account |
| `getLinksByDiscordID(discordID)` | GET all links for a discord user |
| `deleteSoftLink(steamID, discordID)` | DELETE a soft link (admin) |

## Bot Integration Flows

**Soft link flow:** User runs a bot command (e.g., `/link steam <steam_id>`) -> bot calls `createSoftLink(steamID, interaction.user.id, "bot_command")` -> backend upserts the steam_account row (without bm_id) and inserts the soft link.

**Hard link flow:** A scheduled or triggered BattleMetrics scrape collects verified Steam-to-Discord mappings -> bot calls `bulkUpsertHardLinks(links)` with source `"battlemetrics"`.

The actual bot commands and BattleMetrics scrape logic are out of scope for this spec.

## AGENTS.md Update

Add to the Backend (Go) section, after the existing store description:

> **Steam & Discord linking**
>
> `steam_accounts` stores known Steam players, optionally with their BattleMetrics player ID (`bm_id`). The `bm_id` is populated when a hard link is created via BattleMetrics scrape.
>
> `steam_discord_links` maps Steam accounts to Discord users with a `type` and `source`:
> - **soft** — user self-reported via bot command or frontend. No verification that the Steam account actually belongs to them. Source examples: `bot_command`, `frontend`.
> - **hard** — discovered from a trusted source (e.g., BattleMetrics API) where the mapping is authoritatively verified. Most likely valid. Source examples: `battlemetrics`.
>
> A single (steam_id, discord_id) pair can have both a soft and hard link simultaneously. The relationship is many-to-many: one Steam account can link to multiple Discord users and vice versa.
>
> Bot-internal endpoints under `/api/internal/bot/setup/steam/` and `/api/internal/bot/setup/discord/` provide CRUD for both tables. Admins can only delete soft links.
