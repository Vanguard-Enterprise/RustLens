//! Player name-search across all globally tracked servers, for the app
//! `/api/app/players/search` name-mode. Mirrors `internal/store/players_search.go`.

use chrono::{DateTime, SecondsFormat, Utc};
use sqlx::{PgPool, Row};

/// One name-search hit row.
#[derive(Debug, Clone)]
pub struct PlayerSearchHit {
    pub steam_id: String,
    pub bm_id: Option<String>,
    pub bm_name: Option<String>,
    pub avatar_image: Option<String>,
    pub current_server_bm: Option<String>,
    pub current_server: Option<String>,
    pub is_online: bool,
    /// RFC3339 UTC.
    pub last_seen_at: Option<String>,
}

/// ILIKE search across all globally tracked servers (`bm_name` preferred, else
/// `persona_name`), deduped by Steam ID, online-first, capped at 20.
/// `include_offline` also returns rows offline within the last 24h.
pub async fn search_players_by_name(
    pool: &PgPool,
    q: &str,
    include_offline: bool,
) -> Result<Vec<PlayerSearchHit>, sqlx::Error> {
    let rows = sqlx::query(
        r#"
        SELECT * FROM (
            SELECT DISTINCT ON (sa.steam_id)
                sa.steam_id,
                sa.bm_id,
                NULLIF(bpc.bm_name, '') AS bm_name,
                NULLIF(sac.avatar_image, '') AS avatar_image,
                ts.bm_id AS server_bm_id,
                ts.name AS server_name,
                ps.is_online,
                COALESCE(ps.online_since, ps.offline_since, ps.updated_at) AS last_seen
            FROM steam_accounts sa
            JOIN player_states ps ON ps.player_bm_id = sa.bm_id
            JOIN tracked_servers ts ON ts.bm_id = ps.server_bm_id
            LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
            LEFT JOIN steam_account_cache sac ON sac.steam_id = sa.steam_id
            WHERE (bpc.bm_name ILIKE '%' || $1 || '%' OR sac.persona_name ILIKE '%' || $1 || '%')
              AND (ps.is_online OR ($2 AND ps.offline_since IS NOT NULL AND ps.offline_since > now() - interval '24 hours'))
            ORDER BY sa.steam_id, ps.is_online DESC, COALESCE(ps.online_since, ps.offline_since, ps.updated_at) DESC
        ) t
        ORDER BY t.is_online DESC, t.last_seen DESC
        LIMIT 20
        "#,
    )
    .bind(q)
    .bind(include_offline)
    .fetch_all(pool)
    .await?;

    rows.iter()
        .map(|row| {
            let last_seen: Option<DateTime<Utc>> = row.try_get("last_seen")?;
            Ok(PlayerSearchHit {
                steam_id: row.try_get("steam_id")?,
                bm_id: row.try_get("bm_id")?,
                bm_name: row.try_get("bm_name")?,
                avatar_image: row.try_get("avatar_image")?,
                current_server_bm: row.try_get("server_bm_id")?,
                current_server: row.try_get("server_name")?,
                is_online: row.try_get("is_online")?,
                last_seen_at: last_seen.map(|t| t.to_rfc3339_opts(SecondsFormat::Secs, true)),
            })
        })
        .collect()
}
