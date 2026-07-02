//! Unauthenticated BattleMetrics JSON:API client, mirroring the Go
//! `internal/battlemetrics` client (`client.go`, `sessions.go`,
//! `server_history.go`). Every request is sent through a [`rustlens_outbound`]
//! provider (BattleMetrics is the only proxied upstream); no auth header is ever
//! sent. Response shapes match the Go structs byte-for-byte so downstream
//! consumers (the future `players`/`servers` services) see identical data.

use std::sync::Arc;

use chrono::{DateTime, Utc};
use reqwest::header::ACCEPT;
use reqwest::{Method, Request, Response};
use serde::Deserialize;
use url::Url;

use rustlens_outbound::OutboundSender;

/// BattleMetrics public API base (Go `defaultBaseURL`).
pub const DEFAULT_BASE_URL: &str = "https://api.battlemetrics.com";

/// Errors from the client. `PlayerNotFound` is the sentinel Go returns for a 404
/// on the profile / history endpoints (sessions 404 yields an empty list, not an
/// error).
#[derive(Debug, thiserror::Error)]
pub enum BmError {
    #[error("battlemetrics player not found")]
    PlayerNotFound,
    #[error("invalid battlemetrics id: {0:?}")]
    InvalidId(String),
    #[error("invalid history range")]
    InvalidRange,
    #[error("battlemetrics returned status {0}")]
    Status(u16),
    #[error("battlemetrics server history returned status {0}: {1}")]
    HistoryStatus(u16, String),
    #[error(transparent)]
    Http(#[from] reqwest::Error),
    #[error("decode battlemetrics response: {0}")]
    Decode(#[from] serde_json::Error),
    #[error("build url: {0}")]
    Url(#[from] url::ParseError),
}

/// A player surfaced by a server's `include=player` list.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Player {
    pub bm_id: String,
    pub name: String,
}

/// A player's per-server playtime block (from profile `included` servers).
#[derive(Debug, Clone)]
pub struct PlayerServer {
    pub server_id: String,
    pub server_name: String,
    /// Seconds played on this server (`meta.timePlayed`).
    pub time_played: i64,
    pub first_seen_at: Option<DateTime<Utc>>,
    pub last_seen_at: Option<DateTime<Utc>>,
    pub game: String,
    pub rust_type: String,
}

/// A `name`-type identifier period (for name-history / gap analytics).
#[derive(Debug, Clone)]
pub struct PlayerNameIdentifier {
    pub name: String,
    pub last_seen: Option<DateTime<Utc>>,
}

/// The parsed player profile (Go `PlayerProfile`).
#[derive(Debug, Clone)]
pub struct PlayerProfile {
    pub bm_id: String,
    pub steam_id: String,
    pub bm_name: String,
    pub created_at: Option<DateTime<Utc>>,
    pub servers: Vec<PlayerServer>,
    pub name_identifiers: Vec<PlayerNameIdentifier>,
}

/// One play session (Go `PlayerSession`).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PlayerSession {
    pub start: Option<DateTime<Utc>>,
    pub stop: Option<DateTime<Utc>>,
}

/// One player-count-history data point (Go `ServerPlayerCountPoint`).
/// `value` is a float, matching the BattleMetrics wire type.
#[derive(Debug, Clone, PartialEq)]
pub struct ServerPlayerCountPoint {
    pub timestamp: DateTime<Utc>,
    pub min: i32,
    pub value: f64,
    pub max: i32,
}

/// The BattleMetrics client. Cheap to clone (holds an `Arc` sender).
#[derive(Clone)]
pub struct Client {
    sender: Arc<dyn OutboundSender>,
    base_url: String,
}

impl Client {
    /// Build a client sending through `sender` (the proxied outbound provider).
    pub fn new(sender: Arc<dyn OutboundSender>) -> Self {
        Self {
            sender,
            base_url: DEFAULT_BASE_URL.to_string(),
        }
    }

    /// Override the base URL (tests point this at a mock).
    pub fn with_base_url(mut self, base_url: impl Into<String>) -> Self {
        self.base_url = base_url.into();
        self
    }

    /// `GET /servers/{id}?include=player` → the players currently listed on the
    /// server. Non-2xx → `Status`.
    pub async fn fetch_server_players(&self, server_id: &str) -> Result<Vec<Player>, BmError> {
        require_numeric(server_id)?;
        let mut url = self.url(&format!("/servers/{server_id}"))?;
        url.query_pairs_mut().append_pair("include", "player");

        let resp = self.send(url).await?;
        let status = resp.status().as_u16();
        if !(200..300).contains(&status) {
            return Err(BmError::Status(status));
        }
        let payload: ServerPlayersPayload = decode(resp).await?;
        Ok(payload
            .included
            .into_iter()
            .filter(|i| i.type_ == "player")
            .filter_map(|i| {
                let bm_id = if i.id.is_empty() { i.attributes.id } else { i.id };
                if bm_id.is_empty() {
                    None
                } else {
                    Some(Player {
                        bm_id,
                        name: i.attributes.name,
                    })
                }
            })
            .collect())
    }

    /// `GET /players/{id}?include=server,identifier` → the player profile.
    /// 404 → `PlayerNotFound`; other non-2xx → `Status`.
    pub async fn fetch_player_profile(&self, player_id: &str) -> Result<PlayerProfile, BmError> {
        require_numeric(player_id)?;
        let mut url = self.url(&format!("/players/{player_id}"))?;
        url.query_pairs_mut().append_pair("include", "server,identifier");

        let resp = self.send(url).await?;
        let status = resp.status().as_u16();
        if status == 404 {
            return Err(BmError::PlayerNotFound);
        }
        if !(200..300).contains(&status) {
            return Err(BmError::Status(status));
        }
        let payload: ProfilePayload = decode(resp).await?;
        Ok(parse_player_profile(payload))
    }

    /// `GET /players/{id}/relationships/sessions?filter[servers]={srv}&page[size]={n}`,
    /// cursor-paginated up to `max_pages`. 404 → empty list (not an error).
    /// `page_size <= 0` → 100; `max_pages <= 0` → 2.
    pub async fn fetch_player_sessions(
        &self,
        player_bm_id: &str,
        server_bm_id: &str,
        page_size: i32,
        max_pages: i32,
    ) -> Result<Vec<PlayerSession>, BmError> {
        require_numeric(player_bm_id)?;
        if !server_bm_id.is_empty() {
            require_numeric(server_bm_id)?;
        }
        let (page_size, max_pages) = session_defaults(page_size, max_pages);

        let mut url = self.url(&format!("/players/{player_bm_id}/relationships/sessions"))?;
        {
            let mut q = url.query_pairs_mut();
            if !server_bm_id.is_empty() {
                q.append_pair("filter[servers]", server_bm_id);
            }
            q.append_pair("page[size]", &page_size.to_string());
        }
        self.fetch_sessions_paged(url, max_pages).await
    }

    /// Like [`fetch_player_sessions`] but across all servers (no `filter[servers]`).
    pub async fn fetch_player_sessions_all_servers(
        &self,
        player_bm_id: &str,
        page_size: i32,
        max_pages: i32,
    ) -> Result<Vec<PlayerSession>, BmError> {
        self.fetch_player_sessions(player_bm_id, "", page_size, max_pages)
            .await
    }

    /// `GET /servers/{id}/player-count-history?start={rfc3339}&stop={rfc3339}`.
    /// Requires `stop > start`. 404 → `PlayerNotFound`; other non-2xx →
    /// `HistoryStatus` with up to 2048 bytes of the body.
    pub async fn fetch_server_player_count_history(
        &self,
        server_id: &str,
        start: DateTime<Utc>,
        stop: DateTime<Utc>,
    ) -> Result<Vec<ServerPlayerCountPoint>, BmError> {
        require_numeric(server_id)?;
        if stop <= start {
            return Err(BmError::InvalidRange);
        }
        let mut url = self.url(&format!("/servers/{server_id}/player-count-history"))?;
        url.query_pairs_mut()
            .append_pair("start", &start.to_rfc3339_opts(chrono::SecondsFormat::Secs, true))
            .append_pair("stop", &stop.to_rfc3339_opts(chrono::SecondsFormat::Secs, true));

        let resp = self.send(url).await?;
        let status = resp.status().as_u16();
        if status == 404 {
            return Err(BmError::PlayerNotFound);
        }
        if !(200..300).contains(&status) {
            let body = resp.text().await.unwrap_or_default();
            let body: String = body.chars().take(2048).collect();
            return Err(BmError::HistoryStatus(status, body));
        }
        let payload: HistoryPayload = decode(resp).await?;
        Ok(payload
            .data
            .into_iter()
            .filter(|p| p.type_ == "dataPoint")
            .filter_map(|p| {
                p.attributes.timestamp.map(|ts| ServerPlayerCountPoint {
                    timestamp: ts.with_timezone(&Utc),
                    min: p.attributes.min,
                    value: p.attributes.value,
                    max: p.attributes.max,
                })
            })
            .collect())
    }

    /// Fetch sessions following JSON:API `links.next` up to `max_pages`. A 404 on
    /// any page yields the sessions gathered so far (Go returns empty on a 404
    /// first page). Other non-2xx → `Status`.
    async fn fetch_sessions_paged(
        &self,
        first: Url,
        max_pages: i32,
    ) -> Result<Vec<PlayerSession>, BmError> {
        let mut out = Vec::new();
        let mut next = Some(first);
        let mut page = 0;
        while let Some(url) = next.take() {
            if page >= max_pages {
                break;
            }
            page += 1;

            let resp = self.send(url).await?;
            let status = resp.status().as_u16();
            if status == 404 {
                return Ok(out);
            }
            if !(200..300).contains(&status) {
                return Err(BmError::Status(status));
            }
            let payload: SessionsPayload = decode(resp).await?;
            for s in payload.data.into_iter().filter(|s| s.type_ == "session") {
                out.push(PlayerSession {
                    start: s.attributes.start,
                    stop: s.attributes.stop,
                });
            }
            next = match payload.links.next.trim() {
                "" => None,
                n => Some(Url::parse(n)?),
            };
        }
        Ok(out)
    }

    fn url(&self, path: &str) -> Result<Url, BmError> {
        Ok(Url::parse(&format!("{}{}", self.base_url, path))?)
    }

    /// Build a `GET` request with the `Accept: application/json` header (no auth)
    /// and send it through the outbound provider.
    async fn send(&self, url: Url) -> Result<Response, BmError> {
        let mut req = Request::new(Method::GET, url);
        req.headers_mut()
            .insert(ACCEPT, reqwest::header::HeaderValue::from_static("application/json"));
        Ok(self.sender.send(req).await?)
    }
}

async fn decode<T: for<'de> Deserialize<'de>>(resp: Response) -> Result<T, BmError> {
    let bytes = resp.bytes().await?;
    Ok(serde_json::from_slice(&bytes)?)
}

fn require_numeric(id: &str) -> Result<(), BmError> {
    if !id.is_empty() && id.bytes().all(|b| b.is_ascii_digit()) {
        Ok(())
    } else {
        Err(BmError::InvalidId(id.to_string()))
    }
}

fn session_defaults(page_size: i32, max_pages: i32) -> (i32, i32) {
    let ps = if page_size <= 0 { 100 } else { page_size };
    let mp = if max_pages <= 0 { 2 } else { max_pages };
    (ps, mp)
}

fn parse_player_profile(payload: ProfilePayload) -> PlayerProfile {
    let mut profile = PlayerProfile {
        bm_id: payload.data.id,
        steam_id: String::new(),
        bm_name: payload.data.attributes.name,
        created_at: payload.data.attributes.created_at,
        servers: Vec::new(),
        name_identifiers: Vec::new(),
    };
    for item in payload.included {
        match item.type_.as_str() {
            "server" => profile.servers.push(PlayerServer {
                server_id: item.id,
                server_name: item.attributes.name,
                time_played: item.meta.time_played,
                first_seen_at: item.meta.first_seen,
                last_seen_at: item.meta.last_seen,
                game: item.relationships.game.data.id,
                rust_type: item.attributes.details.rust_type,
            }),
            "identifier" => match item.attributes.type_.as_str() {
                "steamID" => {
                    if profile.steam_id.is_empty() {
                        profile.steam_id = item.attributes.identifier;
                    }
                }
                "name" => profile.name_identifiers.push(PlayerNameIdentifier {
                    name: item.attributes.identifier,
                    last_seen: item.attributes.last_seen.or(item.meta.last_seen),
                }),
                _ => {}
            },
            _ => {}
        }
    }
    profile
}

// ---- JSON:API deserialization shapes (mirror the Go anonymous structs) ----

#[derive(Deserialize)]
struct ServerPlayersPayload {
    #[serde(default)]
    included: Vec<IncludedPlayer>,
}

#[derive(Deserialize)]
struct IncludedPlayer {
    #[serde(rename = "type", default)]
    type_: String,
    #[serde(default)]
    id: String,
    #[serde(default)]
    attributes: PlayerAttrs,
}

#[derive(Deserialize, Default)]
struct PlayerAttrs {
    #[serde(default)]
    id: String,
    #[serde(default)]
    name: String,
}

#[derive(Deserialize)]
struct ProfilePayload {
    data: PlayerResource,
    #[serde(default)]
    included: Vec<IncludedResource>,
}

#[derive(Deserialize)]
struct PlayerResource {
    #[serde(default)]
    id: String,
    #[serde(default)]
    attributes: PlayerResourceAttrs,
}

#[derive(Deserialize, Default)]
struct PlayerResourceAttrs {
    #[serde(default)]
    name: String,
    #[serde(rename = "createdAt", default)]
    created_at: Option<DateTime<Utc>>,
}

#[derive(Deserialize, Default)]
struct IncludedResource {
    #[serde(rename = "type", default)]
    type_: String,
    #[serde(default)]
    id: String,
    #[serde(default)]
    attributes: IncludedAttrs,
    #[serde(default)]
    relationships: IncludedRelationships,
    #[serde(default)]
    meta: IncludedMeta,
}

#[derive(Deserialize, Default)]
struct IncludedAttrs {
    #[serde(default)]
    name: String,
    #[serde(rename = "type", default)]
    type_: String,
    #[serde(default)]
    identifier: String,
    #[serde(rename = "lastSeen", default)]
    last_seen: Option<DateTime<Utc>>,
    #[serde(default)]
    details: IncludedDetails,
}

#[derive(Deserialize, Default)]
struct IncludedDetails {
    #[serde(rename = "rust_type", default)]
    rust_type: String,
}

#[derive(Deserialize, Default)]
struct IncludedRelationships {
    #[serde(default)]
    game: GameRel,
}

#[derive(Deserialize, Default)]
struct GameRel {
    #[serde(default)]
    data: GameRelData,
}

#[derive(Deserialize, Default)]
struct GameRelData {
    #[serde(default)]
    id: String,
}

#[derive(Deserialize, Default)]
struct IncludedMeta {
    #[serde(rename = "timePlayed", default)]
    time_played: i64,
    #[serde(rename = "firstSeen", default)]
    first_seen: Option<DateTime<Utc>>,
    #[serde(rename = "lastSeen", default)]
    last_seen: Option<DateTime<Utc>>,
}

#[derive(Deserialize)]
struct SessionsPayload {
    #[serde(default)]
    data: Vec<SessionData>,
    #[serde(default)]
    links: SessionLinks,
}

#[derive(Deserialize)]
struct SessionData {
    #[serde(rename = "type", default)]
    type_: String,
    #[serde(default)]
    attributes: SessionAttrs,
}

#[derive(Deserialize, Default)]
struct SessionAttrs {
    #[serde(default)]
    start: Option<DateTime<Utc>>,
    #[serde(default)]
    stop: Option<DateTime<Utc>>,
}

#[derive(Deserialize, Default)]
struct SessionLinks {
    #[serde(default)]
    next: String,
}

#[derive(Deserialize)]
struct HistoryPayload {
    #[serde(default)]
    data: Vec<HistoryPoint>,
}

#[derive(Deserialize)]
struct HistoryPoint {
    #[serde(rename = "type", default)]
    type_: String,
    #[serde(default)]
    attributes: HistoryAttrs,
}

#[derive(Deserialize, Default)]
struct HistoryAttrs {
    #[serde(default)]
    timestamp: Option<DateTime<Utc>>,
    #[serde(default)]
    min: i32,
    #[serde(default)]
    value: f64,
    #[serde(default)]
    max: i32,
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::VecDeque;
    use std::sync::Mutex;

    struct Captured {
        url: String,
        accept: Option<String>,
        has_auth: bool,
    }

    struct MockSender {
        calls: Mutex<Vec<Captured>>,
        responses: Mutex<VecDeque<(u16, String)>>,
    }

    impl MockSender {
        fn new(responses: Vec<(u16, &str)>) -> Arc<Self> {
            Arc::new(Self {
                calls: Mutex::new(Vec::new()),
                responses: Mutex::new(
                    responses.into_iter().map(|(c, b)| (c, b.to_string())).collect(),
                ),
            })
        }
    }

    #[async_trait::async_trait]
    impl OutboundSender for MockSender {
        async fn send(&self, req: Request) -> reqwest::Result<Response> {
            self.calls.lock().unwrap().push(Captured {
                url: req.url().to_string(),
                accept: req
                    .headers()
                    .get(ACCEPT)
                    .and_then(|v| v.to_str().ok())
                    .map(String::from),
                has_auth: req.headers().contains_key(reqwest::header::AUTHORIZATION),
            });
            let (code, body) = self
                .responses
                .lock()
                .unwrap()
                .pop_front()
                .expect("no canned response");
            Ok(http::Response::builder().status(code).body(body).unwrap().into())
        }
        fn proxy_count(&self) -> usize {
            0
        }
    }

    fn client(mock: Arc<MockSender>) -> Client {
        Client::new(mock).with_base_url("https://bm.test")
    }

    fn query_pairs(url: &str) -> Vec<(String, String)> {
        Url::parse(url)
            .unwrap()
            .query_pairs()
            .map(|(k, v)| (k.into_owned(), v.into_owned()))
            .collect()
    }

    #[test]
    fn require_numeric_matches_go() {
        assert!(require_numeric("123").is_ok());
        assert!(require_numeric("").is_err());
        assert!(require_numeric("12a").is_err());
        assert!(require_numeric("-1").is_err());
    }

    #[test]
    fn session_defaults_fill_zeros() {
        assert_eq!(session_defaults(0, 0), (100, 2));
        assert_eq!(session_defaults(-5, -1), (100, 2));
        assert_eq!(session_defaults(50, 5), (50, 5));
    }

    #[tokio::test]
    async fn server_players_parses_and_builds_request() {
        let body = r#"{"included":[
            {"type":"player","id":"111","attributes":{"name":"Alice"}},
            {"type":"server","id":"999","attributes":{"name":"srv"}},
            {"type":"player","id":"","attributes":{"id":"222","name":"Bob"}},
            {"type":"player","id":"","attributes":{"id":"","name":"ghost"}}
        ]}"#;
        let mock = MockSender::new(vec![(200, body)]);
        let players = client(mock.clone())
            .fetch_server_players("123")
            .await
            .unwrap();
        assert_eq!(
            players,
            vec![
                Player { bm_id: "111".into(), name: "Alice".into() },
                Player { bm_id: "222".into(), name: "Bob".into() },
            ]
        );
        let call = &mock.calls.lock().unwrap()[0];
        assert!(call.url.starts_with("https://bm.test/servers/123?"));
        assert_eq!(query_pairs(&call.url), vec![("include".into(), "player".into())]);
        assert_eq!(call.accept.as_deref(), Some("application/json"));
        assert!(!call.has_auth, "battlemetrics requests must be unauthenticated");
    }

    #[tokio::test]
    async fn server_players_non_2xx_is_status_error() {
        let mock = MockSender::new(vec![(503, "")]);
        let err = client(mock).fetch_server_players("123").await.unwrap_err();
        assert!(matches!(err, BmError::Status(503)));
    }

    #[tokio::test]
    async fn player_profile_parses_all_shapes() {
        let body = r#"{
          "data":{"id":"555","attributes":{"name":"MainName","createdAt":"2020-01-02T03:04:05Z"}},
          "included":[
            {"type":"server","id":"900","attributes":{"name":"Server A","details":{"rust_type":"modded"}},
             "relationships":{"game":{"data":{"id":"rust"}}},
             "meta":{"timePlayed":7200,"firstSeen":"2021-01-01T00:00:00Z","lastSeen":"2021-02-01T00:00:00Z"}},
            {"type":"identifier","id":"i1","attributes":{"type":"steamID","identifier":"76561198000000000"}},
            {"type":"identifier","id":"i2","attributes":{"type":"steamID","identifier":"IGNORED_SECOND"}},
            {"type":"identifier","id":"i3","attributes":{"type":"name","identifier":"OldNick","lastSeen":"2022-03-03T00:00:00Z"}}
          ]
        }"#;
        let mock = MockSender::new(vec![(200, body)]);
        let p = client(mock.clone())
            .fetch_player_profile("555")
            .await
            .unwrap();
        assert_eq!(p.bm_id, "555");
        assert_eq!(p.bm_name, "MainName");
        assert_eq!(p.steam_id, "76561198000000000", "first steamID wins");
        assert!(p.created_at.is_some());
        assert_eq!(p.servers.len(), 1);
        let s = &p.servers[0];
        assert_eq!(s.server_id, "900");
        assert_eq!(s.server_name, "Server A");
        assert_eq!(s.time_played, 7200);
        assert_eq!(s.game, "rust");
        assert_eq!(s.rust_type, "modded");
        assert!(s.first_seen_at.is_some() && s.last_seen_at.is_some());
        assert_eq!(p.name_identifiers.len(), 1);
        assert_eq!(p.name_identifiers[0].name, "OldNick");
        assert!(p.name_identifiers[0].last_seen.is_some());

        let call = &mock.calls.lock().unwrap()[0];
        assert_eq!(
            query_pairs(&call.url),
            vec![("include".into(), "server,identifier".into())]
        );
    }

    #[tokio::test]
    async fn player_profile_404_is_not_found() {
        let mock = MockSender::new(vec![(404, r#"{"errors":[{"detail":"Unknown player"}]}"#)]);
        let err = client(mock).fetch_player_profile("555").await.unwrap_err();
        assert!(matches!(err, BmError::PlayerNotFound));
    }

    #[tokio::test]
    async fn sessions_paginate_via_links_next() {
        let page1 = r#"{"data":[{"type":"session","attributes":{"start":"2023-01-01T00:00:00Z","stop":"2023-01-01T01:00:00Z"}}],"links":{"next":"https://bm.test/players/1/relationships/sessions?page[after]=x"}}"#;
        let page2 = r#"{"data":[{"type":"session","attributes":{"start":"2023-01-02T00:00:00Z","stop":null}},{"type":"other","attributes":{}}],"links":{"next":""}}"#;
        let mock = MockSender::new(vec![(200, page1), (200, page2)]);
        let sessions = client(mock.clone())
            .fetch_player_sessions("1", "42", 0, 0)
            .await
            .unwrap();
        assert_eq!(sessions.len(), 2, "one session per page, non-session filtered");
        assert!(sessions[1].stop.is_none());
        let calls = mock.calls.lock().unwrap();
        assert_eq!(calls.len(), 2, "followed links.next once");
        let q = query_pairs(&calls[0].url);
        assert!(q.contains(&("filter[servers]".into(), "42".into())));
        assert!(q.contains(&("page[size]".into(), "100".into())));
    }

    #[tokio::test]
    async fn sessions_all_servers_omits_filter() {
        let mock = MockSender::new(vec![(200, r#"{"data":[],"links":{"next":""}}"#)]);
        client(mock.clone())
            .fetch_player_sessions_all_servers("1", 0, 0)
            .await
            .unwrap();
        let q = query_pairs(&mock.calls.lock().unwrap()[0].url);
        assert!(!q.iter().any(|(k, _)| k == "filter[servers]"));
        assert!(q.contains(&("page[size]".into(), "100".into())));
    }

    #[tokio::test]
    async fn sessions_404_is_empty() {
        let mock = MockSender::new(vec![(404, "")]);
        let sessions = client(mock)
            .fetch_player_sessions("1", "42", 0, 0)
            .await
            .unwrap();
        assert!(sessions.is_empty());
    }

    #[tokio::test]
    async fn history_parses_float_value_and_filters() {
        let body = r#"{"data":[
            {"type":"dataPoint","attributes":{"timestamp":"2024-01-01T00:00:00Z","min":3,"value":12.5,"max":40}},
            {"type":"notADataPoint","attributes":{"timestamp":"2024-01-02T00:00:00Z","min":1,"value":2,"max":3}}
        ]}"#;
        let mock = MockSender::new(vec![(200, body)]);
        let start = "2024-01-01T00:00:00Z".parse::<DateTime<Utc>>().unwrap();
        let stop = "2024-01-10T00:00:00Z".parse::<DateTime<Utc>>().unwrap();
        let points = client(mock.clone())
            .fetch_server_player_count_history("77", start, stop)
            .await
            .unwrap();
        assert_eq!(points.len(), 1, "non-dataPoint filtered");
        assert_eq!(points[0].value, 12.5);
        assert_eq!(points[0].min, 3);
        assert_eq!(points[0].max, 40);
        let q = query_pairs(&mock.calls.lock().unwrap()[0].url);
        assert!(q.contains(&("start".into(), "2024-01-01T00:00:00Z".into())));
        assert!(q.contains(&("stop".into(), "2024-01-10T00:00:00Z".into())));
    }

    #[tokio::test]
    async fn history_invalid_range_rejected() {
        let mock = MockSender::new(vec![]);
        let t = "2024-01-01T00:00:00Z".parse::<DateTime<Utc>>().unwrap();
        let err = client(mock)
            .fetch_server_player_count_history("77", t, t)
            .await
            .unwrap_err();
        assert!(matches!(err, BmError::InvalidRange));
    }

    #[tokio::test]
    async fn history_404_is_not_found() {
        let mock = MockSender::new(vec![(404, "")]);
        let start = "2024-01-01T00:00:00Z".parse::<DateTime<Utc>>().unwrap();
        let stop = "2024-01-10T00:00:00Z".parse::<DateTime<Utc>>().unwrap();
        let err = client(mock)
            .fetch_server_player_count_history("77", start, stop)
            .await
            .unwrap_err();
        assert!(matches!(err, BmError::PlayerNotFound));
    }
}
