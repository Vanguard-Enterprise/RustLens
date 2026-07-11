//! Steam↔Discord link CRUD (global + guild-scoped) + link-visibility, owned by
//! the `players` service. Mirrors the Go `internal/store/steam.go` link queries.
//! Schema is owned by Go's `EnsureSchema` — read/write only.
//!
//! The hidden-identity suppression clause is keyed on `discord_id` and appears
//! identically in every list query, so a hidden Discord identity vanishes from
//! both directions (Steam→Discord and Discord→Steam) without deleting rows.

use chrono::{DateTime, Utc};
use serde::Serialize;
use sqlx::{PgPool, Row};

/// The Steam-account upsert used by every hard-link/bulk path. Coerces a `bm_id`
/// already owned by a *different* `steam_id` to NULL (avoids a `23505` aborting a
/// whole batch), and never nulls an existing link (`COALESCE`). Mirrors Go
/// `upsertSteamAccountSQL` byte-for-byte.
pub const UPSERT_STEAM_ACCOUNT_SQL: &str = r#"
INSERT INTO steam_accounts (steam_id, bm_id)
VALUES ($1, (
    SELECT CASE
        WHEN $2::text IS NULL THEN NULL
        WHEN EXISTS (
            SELECT 1 FROM steam_accounts other
            WHERE other.bm_id = $2 AND other.steam_id <> $1
        ) THEN NULL
        ELSE $2
    END
))
ON CONFLICT (steam_id) DO UPDATE
SET bm_id = COALESCE(EXCLUDED.bm_id, steam_accounts.bm_id)
"#;

#[derive(Debug, thiserror::Error)]
pub enum LinkError {
    #[error("link already exists")]
    AlreadyExists,
    #[error("link not found")]
    NotFound,
    #[error("guild link not found")]
    GuildNotFound,
    #[error(transparent)]
    Db(#[from] sqlx::Error),
}

/// A global `steam_discord_links` row (`{steamId,discordId,type,source,createdAt}`).
#[derive(Debug, Clone, Serialize)]
pub struct LinkRow {
    #[serde(rename = "steamId")]
    pub steam_id: String,
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "type")]
    pub link_type: String,
    pub source: String,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
}

/// A `guild_steam_discord_links` row (`{guildId,discordId,steamId,createdAt}`).
#[derive(Debug, Clone, Serialize)]
pub struct GuildLinkRow {
    #[serde(rename = "guildId")]
    pub guild_id: String,
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "steamId")]
    pub steam_id: String,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
}

/// A `hidden_discord_link_identities` row.
#[derive(Debug, Clone, Serialize)]
pub struct HiddenIdentityRow {
    #[serde(rename = "discordId")]
    pub discord_id: String,
    pub reason: String,
    #[serde(rename = "createdBy")]
    pub created_by: String,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
    #[serde(rename = "updatedAt")]
    pub updated_at: DateTime<Utc>,
}

/// A `/setsteam` picker candidate (`{steamId,type,displayName,createdAt}`).
#[derive(Debug, Clone, Serialize)]
pub struct SetSteamCandidate {
    #[serde(rename = "steamId")]
    pub steam_id: String,
    #[serde(rename = "type")]
    pub link_type: String,
    #[serde(rename = "displayName")]
    pub display_name: String,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
}

/// A nickname-sync target: one guild-linked Steam mapping per Discord user
/// (`{guildId,discordId,steamId,bmId|null,bmName|null}`).
#[derive(Debug, Clone, Serialize)]
pub struct NicknameTarget {
    #[serde(rename = "guildId")]
    pub guild_id: String,
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "steamId")]
    pub steam_id: String,
    #[serde(rename = "bmId")]
    pub bm_id: Option<String>,
    #[serde(rename = "bmName")]
    pub bm_name: Option<String>,
}

/// One entry for `bulk_upsert_hard_links`.
#[derive(Debug, Clone)]
pub struct BulkHardLink {
    pub steam_id: String,
    pub bm_id: Option<String>,
    pub discord_id: String,
    pub source: String,
}

fn link_row(row: &sqlx::postgres::PgRow) -> Result<LinkRow, sqlx::Error> {
    Ok(LinkRow {
        steam_id: row.try_get("steam_id")?,
        discord_id: row.try_get("discord_id")?,
        link_type: row.try_get("type")?,
        source: row.try_get("source")?,
        created_at: row.try_get("created_at")?,
    })
}

fn guild_link_row(row: &sqlx::postgres::PgRow) -> Result<GuildLinkRow, sqlx::Error> {
    Ok(GuildLinkRow {
        guild_id: row.try_get("guild_id")?,
        discord_id: row.try_get("discord_id")?,
        steam_id: row.try_get("steam_id")?,
        created_at: row.try_get("created_at")?,
    })
}

// ---- global links ----

/// Create a **soft** link, ensuring the `steam_accounts` parent first. Returns
/// `AlreadyExists` when the `(steam_id, discord_id, soft)` row is already present.
pub async fn create_soft_link(
    pool: &PgPool,
    steam_id: &str,
    discord_id: &str,
    source: &str,
) -> Result<LinkRow, LinkError> {
    let mut tx = pool.begin().await?;
    sqlx::query("INSERT INTO steam_accounts (steam_id) VALUES ($1) ON CONFLICT (steam_id) DO NOTHING")
        .bind(steam_id)
        .execute(&mut *tx)
        .await?;
    let row = sqlx::query(
        r#"INSERT INTO steam_discord_links (steam_id, discord_id, type, source)
           VALUES ($1, $2, 'soft', $3)
           ON CONFLICT (steam_id, discord_id, type) DO NOTHING
           RETURNING steam_id, discord_id, type, source, created_at"#,
    )
    .bind(steam_id)
    .bind(discord_id)
    .bind(source)
    .fetch_optional(&mut *tx)
    .await?;
    let Some(row) = row else {
        return Err(LinkError::AlreadyExists);
    };
    let out = link_row(&row)?;
    tx.commit().await?;
    Ok(out)
}

/// Bulk-upsert **hard** links, one transaction. Each link upserts the Steam
/// account (bm_id-conflict-tolerant) then inserts the hard link
/// (`ON CONFLICT DO NOTHING`). Returns the number of input links.
pub async fn bulk_upsert_hard_links(
    pool: &PgPool,
    links: &[BulkHardLink],
) -> Result<usize, sqlx::Error> {
    let mut tx = pool.begin().await?;
    for link in links {
        sqlx::query(UPSERT_STEAM_ACCOUNT_SQL)
            .bind(&link.steam_id)
            .bind(&link.bm_id)
            .execute(&mut *tx)
            .await?;
        sqlx::query(
            r#"INSERT INTO steam_discord_links (steam_id, discord_id, type, source)
               VALUES ($1, $2, 'hard', $3)
               ON CONFLICT (steam_id, discord_id, type) DO NOTHING"#,
        )
        .bind(&link.steam_id)
        .bind(&link.discord_id)
        .bind(&link.source)
        .execute(&mut *tx)
        .await?;
    }
    tx.commit().await?;
    Ok(links.len())
}

/// List Discord links for a Steam ID (Steam→Discord), hidden-suppressed.
pub async fn list_links_by_steam(pool: &PgPool, steam_id: &str) -> Result<Vec<LinkRow>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT l.steam_id, l.discord_id, l.type, l.source, l.created_at
           FROM steam_discord_links l
           WHERE l.steam_id = $1
             AND NOT EXISTS (SELECT 1 FROM hidden_discord_link_identities hidden
                             WHERE hidden.discord_id = l.discord_id)
           ORDER BY created_at ASC"#,
    )
    .bind(steam_id)
    .fetch_all(pool)
    .await?;
    rows.iter().map(link_row).collect()
}

/// List Steam links for a Discord ID (Discord→Steam), hidden-suppressed.
pub async fn list_links_by_discord(
    pool: &PgPool,
    discord_id: &str,
) -> Result<Vec<LinkRow>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT l.steam_id, l.discord_id, l.type, l.source, l.created_at
           FROM steam_discord_links l
           WHERE l.discord_id = $1
             AND NOT EXISTS (SELECT 1 FROM hidden_discord_link_identities hidden
                             WHERE hidden.discord_id = l.discord_id)
           ORDER BY created_at ASC"#,
    )
    .bind(discord_id)
    .fetch_all(pool)
    .await?;
    rows.iter().map(link_row).collect()
}

/// List visible **hard** links for a set of Discord IDs (deep game-ban
/// expansion). Hidden-suppressed, `type='hard'`.
pub async fn list_visible_hard_links_by_discord_ids(
    pool: &PgPool,
    discord_ids: &[String],
) -> Result<Vec<LinkRow>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT l.steam_id, l.discord_id, l.type, l.source, l.created_at
           FROM steam_discord_links l
           WHERE l.discord_id = ANY($1)
             AND l.type = 'hard'
             AND NOT EXISTS (SELECT 1 FROM hidden_discord_link_identities hidden
                             WHERE hidden.discord_id = l.discord_id)
           ORDER BY l.discord_id ASC, l.created_at ASC"#,
    )
    .bind(discord_ids)
    .fetch_all(pool)
    .await?;
    rows.iter().map(link_row).collect()
}

/// Delete a **soft** link (hard links are never deletable). `NotFound` when no
/// soft row matched.
pub async fn delete_soft_link(
    pool: &PgPool,
    steam_id: &str,
    discord_id: &str,
) -> Result<(), LinkError> {
    let res = sqlx::query(
        "DELETE FROM steam_discord_links WHERE steam_id = $1 AND discord_id = $2 AND type = 'soft'",
    )
    .bind(steam_id)
    .bind(discord_id)
    .execute(pool)
    .await?;
    if res.rows_affected() == 0 {
        return Err(LinkError::NotFound);
    }
    Ok(())
}

/// List `/setsteam` candidates for a Discord ID (hidden-suppressed, hard>soft,
/// recent-5-soft, cap 25). DisplayName falls back bm_name→persona_name→steam_id.
pub async fn list_setsteam_candidates(
    pool: &PgPool,
    discord_id: &str,
) -> Result<Vec<SetSteamCandidate>, sqlx::Error> {
    let rows = sqlx::query(
        r#"
        WITH visible AS (
            SELECT sdl.steam_id, sdl.type, sdl.created_at
            FROM steam_discord_links sdl
            WHERE sdl.discord_id = $1
              AND NOT EXISTS (SELECT 1 FROM hidden_discord_link_identities hidden
                              WHERE hidden.discord_id = sdl.discord_id)
              AND (sdl.type = 'hard' OR sdl.created_at >= NOW() - INTERVAL '90 days')
        ),
        deduped AS (
            SELECT DISTINCT ON (steam_id) steam_id, type, created_at
            FROM visible
            ORDER BY steam_id, (type = 'hard') DESC, created_at DESC
        ),
        ranked AS (
            SELECT steam_id, type, created_at,
                ROW_NUMBER() OVER (PARTITION BY type ORDER BY created_at DESC) AS type_rn
            FROM deduped
        ),
        filtered AS (
            SELECT steam_id, type, created_at FROM ranked
            WHERE type = 'hard' OR type_rn <= 5
        ),
        capped AS (
            SELECT steam_id, type, created_at,
                ROW_NUMBER() OVER (ORDER BY (type = 'hard') DESC, created_at DESC) AS overall_rn
            FROM filtered
        )
        SELECT c.steam_id, c.type, c.created_at,
            COALESCE(NULLIF(bpc.bm_name, ''), NULLIF(sac.persona_name, ''), c.steam_id) AS display_name
        FROM capped c
        LEFT JOIN steam_accounts sa ON sa.steam_id = c.steam_id
        LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
        LEFT JOIN steam_account_cache sac ON sac.steam_id = c.steam_id
        WHERE c.overall_rn <= 25
        ORDER BY c.overall_rn
        "#,
    )
    .bind(discord_id)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|row| {
            Ok(SetSteamCandidate {
                steam_id: row.try_get("steam_id")?,
                link_type: row.try_get("type")?,
                display_name: row.try_get("display_name")?,
                created_at: row.try_get("created_at")?,
            })
        })
        .collect()
}

// ---- link visibility ----

/// Upsert a hidden-identity row for a Discord ID.
pub async fn hide_discord_link_identity(
    pool: &PgPool,
    discord_id: &str,
    reason: &str,
    created_by: &str,
) -> Result<HiddenIdentityRow, sqlx::Error> {
    let row = sqlx::query(
        r#"INSERT INTO hidden_discord_link_identities (discord_id, reason, created_by)
           VALUES ($1, $2, $3)
           ON CONFLICT (discord_id) DO UPDATE
           SET reason = EXCLUDED.reason, created_by = EXCLUDED.created_by, updated_at = NOW()
           RETURNING discord_id, reason, created_by, created_at, updated_at"#,
    )
    .bind(discord_id)
    .bind(reason)
    .bind(created_by)
    .fetch_one(pool)
    .await?;
    Ok(HiddenIdentityRow {
        discord_id: row.try_get("discord_id")?,
        reason: row.try_get("reason")?,
        created_by: row.try_get("created_by")?,
        created_at: row.try_get("created_at")?,
        updated_at: row.try_get("updated_at")?,
    })
}

/// Delete a hidden-identity row (idempotent; no error when absent).
pub async fn unhide_discord_link_identity(pool: &PgPool, discord_id: &str) -> Result<(), sqlx::Error> {
    sqlx::query("DELETE FROM hidden_discord_link_identities WHERE discord_id = $1")
        .bind(discord_id)
        .execute(pool)
        .await?;
    Ok(())
}

// ---- guild-scoped links ----

/// Create a guild-scoped link. `GuildNotFound` is never returned here;
/// `AlreadyExists` when the `(guild,discord,steam)` row exists.
pub async fn create_guild_link(
    pool: &PgPool,
    guild_id: &str,
    discord_id: &str,
    steam_id: &str,
) -> Result<GuildLinkRow, LinkError> {
    let row = sqlx::query(
        r#"INSERT INTO guild_steam_discord_links (guild_id, discord_id, steam_id)
           VALUES ($1, $2, $3)
           ON CONFLICT (guild_id, discord_id, steam_id) DO NOTHING
           RETURNING guild_id, discord_id, steam_id, created_at"#,
    )
    .bind(guild_id)
    .bind(discord_id)
    .bind(steam_id)
    .fetch_optional(pool)
    .await?;
    match row {
        Some(row) => Ok(guild_link_row(&row)?),
        None => Err(LinkError::AlreadyExists),
    }
}

async fn guild_links_query(pool: &PgPool, sql: &str, binds: &[&str]) -> Result<Vec<GuildLinkRow>, sqlx::Error> {
    let mut q = sqlx::query(sql);
    for b in binds {
        q = q.bind(*b);
    }
    let rows = q.fetch_all(pool).await?;
    rows.iter().map(guild_link_row).collect()
}

const GUILD_LINK_SUPPRESS: &str = "AND NOT EXISTS (SELECT 1 FROM hidden_discord_link_identities hidden WHERE hidden.discord_id = gsl.discord_id) ORDER BY created_at ASC";

/// List all guild links for a guild (hidden-suppressed).
pub async fn list_guild_links(pool: &PgPool, guild_id: &str) -> Result<Vec<GuildLinkRow>, sqlx::Error> {
    let sql = format!(
        "SELECT gsl.guild_id, gsl.discord_id, gsl.steam_id, gsl.created_at FROM guild_steam_discord_links gsl WHERE gsl.guild_id = $1 {GUILD_LINK_SUPPRESS}"
    );
    guild_links_query(pool, &sql, &[guild_id]).await
}

/// List a guild's links for one Discord ID (hidden-suppressed).
pub async fn list_guild_links_by_discord(
    pool: &PgPool,
    guild_id: &str,
    discord_id: &str,
) -> Result<Vec<GuildLinkRow>, sqlx::Error> {
    let sql = format!(
        "SELECT gsl.guild_id, gsl.discord_id, gsl.steam_id, gsl.created_at FROM guild_steam_discord_links gsl WHERE gsl.guild_id = $1 AND gsl.discord_id = $2 {GUILD_LINK_SUPPRESS}"
    );
    guild_links_query(pool, &sql, &[guild_id, discord_id]).await
}

/// List a guild's links for one Steam ID (hidden-suppressed).
pub async fn list_guild_links_by_steam(
    pool: &PgPool,
    guild_id: &str,
    steam_id: &str,
) -> Result<Vec<GuildLinkRow>, sqlx::Error> {
    let sql = format!(
        "SELECT gsl.guild_id, gsl.discord_id, gsl.steam_id, gsl.created_at FROM guild_steam_discord_links gsl WHERE gsl.guild_id = $1 AND gsl.steam_id = $2 {GUILD_LINK_SUPPRESS}"
    );
    guild_links_query(pool, &sql, &[guild_id, steam_id]).await
}

/// Delete a guild link. `GuildNotFound` when no row matched.
pub async fn delete_guild_link(
    pool: &PgPool,
    guild_id: &str,
    discord_id: &str,
    steam_id: &str,
) -> Result<(), LinkError> {
    let res = sqlx::query(
        "DELETE FROM guild_steam_discord_links WHERE guild_id = $1 AND discord_id = $2 AND steam_id = $3",
    )
    .bind(guild_id)
    .bind(discord_id)
    .bind(steam_id)
    .execute(pool)
    .await?;
    if res.rows_affected() == 0 {
        return Err(LinkError::GuildNotFound);
    }
    Ok(())
}

/// One nickname-sync target per Discord user (NOT hidden-suppressed), preferring
/// a row with a resolved bm_id+bm_name.
pub async fn list_guild_nickname_targets(
    pool: &PgPool,
    guild_id: &str,
) -> Result<Vec<NicknameTarget>, sqlx::Error> {
    let rows = sqlx::query(
        r#"
        SELECT guild_id, discord_id, steam_id, bm_id, bm_name
        FROM (
            SELECT gsl.guild_id, gsl.discord_id, gsl.steam_id, sa.bm_id,
                NULLIF(bpc.bm_name, '') AS bm_name,
                ROW_NUMBER() OVER (
                    PARTITION BY gsl.discord_id
                    ORDER BY
                        CASE WHEN sa.bm_id IS NOT NULL AND sa.bm_id <> ''
                                  AND bpc.bm_name IS NOT NULL AND bpc.bm_name <> ''
                             THEN 0 ELSE 1 END ASC,
                        gsl.created_at ASC, gsl.steam_id ASC
                ) AS nickname_rank
            FROM guild_steam_discord_links gsl
            LEFT JOIN steam_accounts sa ON sa.steam_id = gsl.steam_id
            LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
            WHERE gsl.guild_id = $1
        ) nickname_targets
        WHERE nickname_rank = 1
        ORDER BY discord_id ASC
        "#,
    )
    .bind(guild_id)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|row| {
            Ok(NicknameTarget {
                guild_id: row.try_get("guild_id")?,
                discord_id: row.try_get("discord_id")?,
                steam_id: row.try_get("steam_id")?,
                bm_id: row.try_get("bm_id")?,
                bm_name: row.try_get("bm_name")?,
            })
        })
        .collect()
}
