---
name: battlemetrics-api
description: Use when interacting with BattleMetrics API - fetching servers, players, bans, sessions, RCON commands, ban lists, game features, activity logs, or player identifiers. Also use when building HTTP clients or integrations for game server management.
---

# BattleMetrics API

## Overview

REST API for game server data, player tracking, ban management, and RCON. Uses JSON:API format with OAuth 2.0 Bearer tokens.

## Quick Reference

| | |
|---|---|
| **Base URL** | `https://api.battlemetrics.com` |
| **Auth** | `Authorization: Bearer <token>` |
| **Format** | JSON:API (`application/json`) |
| **Tokens** | Generate at battlemetrics.com developers area |
| **Pagination** | Cursor-based: `page[size]`, `page[key]`, `page[rel]=next\|prev` |
| **Sorting** | `sort=field` (asc), `sort=-field` (desc), comma-separate multiple |
| **Sparse fields** | `fields[resource]=attr1,attr2` |
| **Includes** | `include=relation1,relation2` |

## Endpoints

### Servers

| Method | Endpoint | Scopes | Description |
|--------|----------|--------|-------------|
| `GET` | `/servers` | — | List/search servers |
| `GET` | `/servers/{id}` | — | Get server details (include: player, identifier, session, uptime:7/30/90, serverEvent, serverGroup, serverDescription, organization) |
| `POST` | `/servers/{id}/rcon` | rcon | Connect RCON (portRCON, rconPassword in body) |
| `DELETE` | `/servers/{id}/rcon` | rcon | Disconnect RCON |
| `POST` | `/servers/{id}/command` | rcon | Send RCON command |

**Server filters:** `filter[search]`, `filter[game]`, `filter[status]` (online), `filter[countries][]`, `filter[players][min]`, `filter[players][max]`, `filter[features][{id}]`

### Players

| Method | Endpoint | Scopes | Description |
|--------|----------|--------|-------------|
| `GET` | `/players` | — | List/search players |
| `GET` | `/players/{id}` | — | Get player (include: server, identifier, playerFlag, flagPlayer) |
| `GET` | `/players/{id}/servers/{server_id}` | — | Player info for specific server |
| `POST` | `/players/match` | — | Match player by identifier |

**Player filters:** `filter[search]`, `filter[servers]`, `filter[organizations]`, `filter[online]`, `filter[public]`, `filter[before]`, `filter[after]`, `filter[firstSeen]` (range `start:end`), `filter[lastSeen]` (range), `filter[identifiers]`, `filter[playerFlags]`, `filter[sessions][at]`

### Bans

| Method | Endpoint | Scopes | Description |
|--------|----------|--------|-------------|
| `GET` | `/bans` | ban:read | List bans (meta: active/expired/total counts) |
| `POST` | `/bans` | ban:create | Create ban |
| `GET` | `/bans/{id}` | ban:read | Get ban |
| `PATCH` | `/bans/{id}` | ban:update | Update ban |
| `DELETE` | `/bans/{id}` | ban:delete | Delete ban |
| `POST` | `/bans/import` | ban:import | Bulk import (won't kick online, no immediate search index) |

**Ban filters:** `filter[expired]`, `filter[exempt]`, `filter[server]`, `filter[organization]`, `filter[player]`, `filter[banList]`, `filter[users]`, `filter[search]`

**Ban attributes:** uid, timestamp, reason, note, expires (null=permanent), identifiers, orgWide, autoAddEnabled, nativeEnabled

**Ban relationships:** player, server, organization, user, banList, trigger

### Ban Lists

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/ban-lists` | List ban lists |
| `POST` | `/ban-lists` | Create ban list (action: none/log/kick) |
| `GET` | `/ban-lists/{id}` | Get ban list |
| `PATCH` | `/ban-lists/{id}` | Update ban list |
| `GET` | `/ban-lists/{id}/organizations/{org_id}` | Read org subscription |
| `DELETE` | `/ban-lists/{id}/organizations/{org_id}` | Unsubscribe org |

### Sessions

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/sessions` | List sessions (include: identifier, server, player) |

**Session filters:** `filter[servers]`, `filter[games]`, `filter[organizations]`, `filter[players]`, `filter[identifiers]`, `filter[range]` (ISO range `start:end`), `filter[at]` (point in time)

### Game Features

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/game-features` | List features (featureType: select/boolean/range/dateRange) |

**Filters:** `filter[game]`

### Activity Logs

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/activity` | Organization activity logs |

### Player Notes, Flags, Counters

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET/POST` | `/players/{id}/relationships/notes` | Player notes |
| `GET/POST` | `/players/{id}/relationships/flags` | Player flags |
| `GET/POST` | `/players/{id}/relationships/counters` | Player counters |

## JSON:API Response Shape

```json
{
  "data": {
    "type": "player",
    "id": "42",
    "attributes": { "name": "Quark", "createdAt": "..." },
    "relationships": {
      "server": { "data": { "type": "server", "id": "42" } }
    }
  },
  "included": [],
  "links": {
    "next": "https://api.battlemetrics.com/players?page[size]=10&page[key]=...",
    "prev": "..."
  }
}
```

List responses return `data` as an array. Some include `meta` (e.g. bans: `{ active, expired, total }`).

## Pagination

Cursor-based. Follow `links.next`/`links.prev` from responses.

```
?page[size]=25&page[key]=<cursor>&page[rel]=next
```

`page[offset]` also supported for some endpoints.

## Common Patterns

**Filter by time range** (colon-separated ISO timestamps):
```
filter[firstSeen]=2024-01-01T00:00:00Z:2024-12-31T23:59:59Z
```

**Include related data:**
```
GET /players?include=server,identifier&fields[player]=name&fields[server]=name,ip
```

**Create a ban:**
```json
{
  "data": {
    "type": "ban",
    "attributes": {
      "reason": "Cheating",
      "note": "Private admin note",
      "expires": "2024-06-01T00:00:00Z",
      "orgWide": true,
      "autoAddEnabled": true,
      "identifiers": [
        1000,
        { "type": "steamID", "identifier": "7656119...", "manual": true }
      ]
    },
    "relationships": {
      "player": { "data": { "type": "player", "id": "42" } },
      "organization": { "data": { "type": "organization", "id": "1" } },
      "banList": { "data": { "type": "banList", "id": "uuid-here" } }
    }
  }
}
```

## Scopes

Tokens are scoped. Key scopes: `ban:read`, `ban:create`, `ban:update`, `ban:delete`, `ban:import`, `rcon:read`, `rcon`.

## Full docs

For complete endpoint details, use context7 with library ID `/websites/battlemetrics_developers`.
